// Command line-dashboard serves a read-only SCADA-style dashboard of the
// contraband production lines declared in data/overmind/lines.yaml: a top
// board with one row per line, a per-line node/edge diagram, and a JSON API.
//
// It opens assets.db and market.db read-only, reads the fleet yamls and
// status files, and never writes anything. Each request reuses a cached
// evaluation no older than half the refresh interval.
//
//	line-dashboard --addr :8093 --refresh 60
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/rsned/spacemolt/pkg/linedash"
)

type options struct {
	lines, overmindDir, agentsDir, assetsDB, marketDB string
	refresh                                           int
}

// server holds the long-lived DB handles and the cached evaluation.
type server struct {
	opt    options
	logger *log.Logger

	buildMu sync.Mutex // serialises builds and guards the handles below
	tanks   *linedash.Tanks
	events  *linedash.Events
	market  *linedash.Market
	lastCfg *linedash.Config // last lines.yaml that loaded

	mu      sync.RWMutex
	reports []linedash.LineReport
	builtAt time.Time
}

func main() {
	var opt options
	addr := flag.String("addr", ":8093", "listen address for the dashboard")
	flag.StringVar(&opt.lines, "lines", "data/overmind/lines.yaml", "production line definitions")
	flag.StringVar(&opt.overmindDir, "overmind-dir", "data/overmind", "directory of fleet yamls and *-status.json files")
	flag.StringVar(&opt.agentsDir, "agents-dir", "data/agents", "per-agent data directory (shopkeep.json)")
	flag.StringVar(&opt.assetsDB, "assets-db", "data/assets.db", "assets database (storage snapshots, action log), opened read-only")
	flag.StringVar(&opt.marketDB, "market-db", "data/market.db", "market database, opened read-only")
	flag.IntVar(&opt.refresh, "refresh", 60, "auto-refresh interval in seconds (0 disables)")
	flag.Parse()

	s := &server{opt: opt, logger: log.New(log.Writer(), "[line-dashboard] ", log.LstdFlags)}
	defer s.closeHandles()

	reports := s.current(context.Background())

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleBoard)
	mux.HandleFunc("GET /line/{id}", s.handleLine)
	mux.HandleFunc("GET /api/lines.json", s.handleLinesJSON)
	mux.HandleFunc("GET /api/line/{file}", s.handleLineJSON)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok"))
	})

	s.logger.Printf("serving %d lines on %s", len(reports), *addr)

	srv := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil {
		s.logger.Fatalf("server: %v", err)
	}
}

// current returns the cached reports, rebuilding them when they are older
// than refresh/2 seconds.
func (s *server) current(ctx context.Context) []linedash.LineReport {
	maxAge := time.Duration(s.opt.refresh) * time.Second / 2
	s.mu.RLock()
	reports, builtAt := s.reports, s.builtAt
	s.mu.RUnlock()
	if !builtAt.IsZero() && time.Since(builtAt) < maxAge {
		return reports
	}

	s.buildMu.Lock()
	defer s.buildMu.Unlock()
	s.mu.RLock()
	reports, builtAt = s.reports, s.builtAt
	s.mu.RUnlock()
	if !builtAt.IsZero() && time.Since(builtAt) < maxAge {
		return reports // another request rebuilt while we waited
	}
	reports = s.build(ctx)
	s.mu.Lock()
	s.reports, s.builtAt = reports, time.Now()
	s.mu.Unlock()

	return reports
}

// build reloads lines.yaml and every source and evaluates and lays out every
// line. Callers hold buildMu. Every failure is logged and surfaced as a
// "<source>: <err>" StaleSources entry; build always returns something to
// render.
func (s *server) build(ctx context.Context) []linedash.LineReport {
	now := time.Now().UTC()
	var global []string
	warn := func(source string, err error) {
		msg := source + ": " + oneLine(err)
		s.logger.Printf("%s", msg)
		if !slices.Contains(global, msg) {
			global = append(global, msg)
		}
	}

	cfg, err := linedash.LoadLines(s.opt.lines)
	switch {
	case err == nil:
		s.lastCfg = &cfg
	case s.lastCfg != nil:
		warn("lines.yaml (using last good copy)", err)
		cfg = *s.lastCfg
	default:
		warn("lines.yaml", err)

		return []linedash.LineReport{{
			ID: "lines", Name: "lines.yaml unreadable", State: linedash.StateUnknown,
			StaleSources: global, GeneratedAt: now,
		}}
	}

	s.openHandles(ctx, warn)
	roster, err := linedash.LoadRoster(s.opt.overmindDir)
	if err != nil {
		warn("fleet yamls", err)
	}
	live, err := linedash.LoadLive(s.opt.overmindDir)
	if err != nil {
		warn("status files", err)
	}

	reports := make([]linedash.LineReport, 0, len(cfg.Lines))
	for _, line := range cfg.Lines {
		in := linedash.Inputs{
			Now: now, Cfg: cfg.Thresholds, Roster: roster, Live: live,
			Tanks:  map[string][]linedash.TankLevel{},
			Demand: map[string]linedash.Demand{},
			Asks:   map[string]*linedash.OurAsk{},
		}
		var local []string
		lwarn := func(source string, err error) {
			msg := source + ": " + oneLine(err)
			s.logger.Printf("line %s: %s", line.ID, msg)
			if !slices.Contains(local, msg) {
				local = append(local, msg)
			}
		}
		s.loadLineInputs(ctx, line, &in, lwarn)
		r := linedash.Evaluate(line, in)
		r.StaleSources = append(r.StaleSources, global...)
		r.StaleSources = append(r.StaleSources, local...)
		linedash.Layout(&r)
		reports = append(reports, r)
	}

	return reports
}

// loadLineInputs fills the DB-backed inputs of one line. A nil handle (open
// failed) leaves its inputs empty; the open error is already reported.
func (s *server) loadLineInputs(ctx context.Context, line linedash.Line, in *linedash.Inputs, warn func(string, error)) {
	agents := linedash.AgentsOf(line)
	if s.tanks != nil {
		for _, n := range line.Nodes {
			if len(n.Holders) == 0 {
				continue
			}
			levels, err := s.tanks.Levels(ctx, n)
			if err != nil {
				warn("assets.db", err)

				continue
			}
			in.Tanks[n.ID] = levels
		}
	}
	if s.events != nil {
		evs, err := s.events.Window(ctx, agents, in.Now.Add(-eventWindow(in.Cfg)))
		if err != nil {
			warn("assets.db action log", err)
		}
		in.Events = evs
		cursors, err := s.events.CursorAges(ctx, agents)
		if err != nil {
			warn("assets.db cursors", err)
		}
		in.Cursors = cursors
	}
	if s.market != nil {
		for _, n := range line.Nodes {
			if n.Type != linedash.NodeSell {
				continue
			}
			item := sellItem(line, n)
			d, err := s.market.Demand(ctx, n.Station, item)
			if err != nil {
				warn("market.db", err)
			} else {
				in.Demand[n.ID] = d
			}
			for _, a := range n.Agents {
				ask, ok, err := s.market.OurAsk(ctx, a, n.Station, item)
				if err != nil {
					warn("market.db", err)

					continue
				}
				if ok {
					in.Asks[n.ID] = &ask

					break
				}
			}
		}
	}
}

// sellItem mirrors the evaluator: the line's output item when the node
// carries it (or carries nothing), else the node's first item.
func sellItem(line linedash.Line, n linedash.Node) string {
	if len(n.Items) == 0 || slices.Contains(n.Items, line.OutputItem) {
		return line.OutputItem
	}

	return n.Items[0]
}

// eventWindow is the longest look-back any rule needs.
func eventWindow(t linedash.Thresholds) time.Duration {
	return max(t.RateWindow, t.HaulWindow, t.MineWindow, t.CraftWindow, t.SellWindow, t.SellStale)
}

// openHandles opens any DB handle not yet open; a failure is reported and
// retried on the next build.
func (s *server) openHandles(ctx context.Context, warn func(string, error)) {
	var err error
	if s.tanks == nil {
		if s.tanks, err = linedash.OpenTanks(ctx, s.opt.assetsDB); err != nil {
			s.tanks = nil
			warn("assets.db", err)
		}
	}
	if s.events == nil {
		if s.events, err = linedash.OpenEvents(ctx, s.opt.assetsDB); err != nil {
			s.events = nil
			warn("assets.db action log", err)
		}
	}
	if s.market == nil {
		if s.market, err = linedash.OpenMarket(ctx, s.opt.marketDB, s.opt.agentsDir); err != nil {
			s.market = nil
			warn("market.db", err)
		}
	}
}

func (s *server) closeHandles() {
	s.buildMu.Lock()
	defer s.buildMu.Unlock()
	closeOne := func(name string, closeFn func() error) {
		if err := closeFn(); err != nil {
			s.logger.Printf("close %s: %v", name, err)
		}
	}
	if s.tanks != nil {
		closeOne("assets.db", s.tanks.Close)
	}
	if s.events != nil {
		closeOne("assets.db action log", s.events.Close)
	}
	if s.market != nil {
		closeOne("market.db", s.market.Close)
	}
}

func oneLine(err error) string {
	return strings.ReplaceAll(err.Error(), "\n", "; ")
}

func (s *server) find(ctx context.Context, id string) (linedash.LineReport, bool) {
	for _, r := range s.current(ctx) {
		if r.ID == id {
			return r, true
		}
	}

	return linedash.LineReport{}, false
}

func (s *server) handleBoard(w http.ResponseWriter, r *http.Request) {
	page, err := linedash.RenderBoard(s.current(r.Context()), s.opt.refresh)
	s.writeHTML(w, page, err)
}

func (s *server) handleLine(w http.ResponseWriter, r *http.Request) {
	rep, ok := s.find(r.Context(), r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)

		return
	}
	page, err := linedash.RenderLine(rep, s.opt.refresh)
	s.writeHTML(w, page, err)
}

func (s *server) handleLinesJSON(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, s.current(r.Context()))
}

func (s *server) handleLineJSON(w http.ResponseWriter, r *http.Request) {
	id, ok := strings.CutSuffix(r.PathValue("file"), ".json")
	if !ok {
		http.NotFound(w, r)

		return
	}
	rep, found := s.find(r.Context(), id)
	if !found {
		http.NotFound(w, r)

		return
	}
	s.writeJSON(w, rep)
}

func (s *server) writeHTML(w http.ResponseWriter, page []byte, err error) {
	if err != nil {
		s.logger.Printf("render: %v", err)
		http.Error(w, "render failed", http.StatusInternalServerError)

		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if _, err := w.Write(page); err != nil {
		s.logger.Printf("write response: %v", err)
	}
}

func (s *server) writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		s.logger.Printf("write json: %v", err)
	}
}
