// Command line-dashboard serves a read-only SCADA-style dashboard of every
// CRFT production, upkeep and sales line, derived on each rebuild from the
// agents' schedules, the fleet yamls, the knowledge DB and our asks: a board
// grouped by station and category, a per-line node/edge diagram, and a JSON
// API. data/overmind/line-overrides.yaml holds thresholds and per-line
// renames; it never adds structure.
//
// It opens the knowledge DB, assets.db and market.db read-only, reads the
// fleet yamls, status files and per-agent outcome/queue files, and never
// writes anything. Each request reuses a cached
// evaluation no older than half the refresh interval.
//
//	line-dashboard --addr :8093 --refresh 60
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"log"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/linedash"
	"github.com/rsned/spacemolt/pkg/outcomes"
)

type options struct {
	overrides, overmindDir, agentsDir, assetsDB, marketDB string
	kbDB, facilityCatalog, faction, deferFleets           string
	refresh                                               int
}

// server holds the long-lived DB handles and the cached evaluation.
type server struct {
	opt    options
	logger *log.Logger

	buildMu   sync.Mutex // serialises builds and guards the handles below
	tanks     *linedash.Tanks
	events    *linedash.Events
	market    *linedash.Market
	lastLines []linedash.Line // last good derivation, kept when reference data fails to load

	mu       sync.RWMutex
	reports  []linedash.LineReport
	warnings []string // board-level: overrides, capture cadence, derivation problems
	builtAt  time.Time
}

func main() {
	var opt options
	addr := flag.String("addr", ":8093", "listen address for the dashboard")
	flag.StringVar(&opt.overrides, "overrides", "data/overmind/line-overrides.yaml", "thresholds and per-line name/category/hidden overrides")
	flag.StringVar(&opt.kbDB, "kb-db", "data/spacemolt-knowledge.db", "knowledge DB (stations, recipes, items, our facilities), opened read-only")
	flag.StringVar(&opt.facilityCatalog, "facility-catalog", "data/game-api/latest/catalog_facilities.json", "facility catalog (upkeep inputs)")
	flag.StringVar(&opt.faction, "faction", "CRFT", "our faction tag")
	flag.StringVar(&opt.deferFleets, "defer-fleets", "mb", "comma-separated fleets whose lines are deferred (drone marketbots)")
	flag.StringVar(&opt.overmindDir, "overmind-dir", "data/overmind", "directory of fleet yamls and *-status.json files")
	flag.StringVar(&opt.agentsDir, "agents-dir", "data/agents", "per-agent data directory (shopkeep.json)")
	flag.StringVar(&opt.assetsDB, "assets-db", "data/assets.db", "assets database (storage snapshots, action log), opened read-only")
	flag.StringVar(&opt.marketDB, "market-db", "data/market.db", "market database, opened read-only")
	flag.IntVar(&opt.refresh, "refresh", 60, "auto-refresh interval in seconds (0 disables)")
	flag.Parse()

	s := &server{opt: opt, logger: log.New(log.Writer(), "[line-dashboard] ", log.LstdFlags)}
	defer s.closeHandles()

	reports := s.current(context.Background(), false)

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
// than refresh/2 seconds. force rebuilds regardless of age (the page's
// Refresh link and a ?refresh=1 query); a forced rebuild still serialises
// on buildMu, so two operators clicking at once build once.
func (s *server) current(ctx context.Context, force bool) []linedash.LineReport {
	maxAge := time.Duration(s.opt.refresh) * time.Second / 2
	s.mu.RLock()
	reports, builtAt := s.reports, s.builtAt
	s.mu.RUnlock()
	if !force && !builtAt.IsZero() && time.Since(builtAt) < maxAge {
		return reports
	}

	s.buildMu.Lock()
	defer s.buildMu.Unlock()
	s.mu.RLock()
	reports, builtAt = s.reports, s.builtAt
	s.mu.RUnlock()
	if !force && !builtAt.IsZero() && time.Since(builtAt) < maxAge {
		return reports // another request rebuilt while we waited
	}
	// The build outlives the request that triggered it: its result is cached
	// for every viewer, so a client disconnect must not fail its queries.
	bctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), game.SleepLong)
	defer cancel()
	reports, warnings := s.build(bctx)
	s.mu.Lock()
	s.reports, s.warnings, s.builtAt = reports, warnings, time.Now()
	s.mu.Unlock()

	return reports
}

// build derives every line and evaluates it. Callers hold buildMu; build
// always returns something to render. Source problems that affect
// every line are appended to each report's StaleSources (so a line never looks
// healthy on a missing source); overrides, cadence and derivation problems are
// returned as board warnings.
func (s *server) build(ctx context.Context) ([]linedash.LineReport, []string) {
	now := time.Now().UTC()
	var global, warnings []string
	warn := func(source string, err error) {
		msg := source + ": " + oneLine(err)
		s.logger.Printf("%s", msg)
		if !slices.Contains(global, msg) {
			global = append(global, msg)
		}
	}
	placeholder := func(name string) []linedash.LineReport {
		return []linedash.LineReport{{
			ID: "lines", Name: name, State: linedash.StateUnknown,
			StaleSources: global, GeneratedAt: now,
		}}
	}

	ov, err := linedash.LoadOverrides(s.opt.overrides)
	if err != nil {
		warn("line overrides (using defaults)", err)
	}
	s.openHandles(ctx, warn)
	roster, err := linedash.LoadRoster(s.opt.overmindDir)
	if err != nil {
		warn("fleet yamls", err)
	}
	if len(roster) == 0 {
		warn("overmind-dir", errors.New("no fleet yamls found"))
	}
	live, err := linedash.LoadLive(s.opt.overmindDir)
	if err != nil {
		warn("status files", err)
	}
	if len(live) == 0 {
		warn("overmind-dir", errors.New("no status files found"))
	}
	sched, err := linedash.LoadSchedules(s.opt.agentsDir)
	if err != nil {
		warnings = append(warnings, "schedules: "+oneLine(err))
	}

	var ref linedash.RefData
	lines := s.lastLines
	factionID, ferr := linedash.LookupFactionID(ctx, s.opt.assetsDB, s.opt.faction)
	if ferr != nil {
		warn("faction id", ferr)
	} else if ref, err = linedash.LoadRefData(ctx, s.opt.kbDB, s.opt.facilityCatalog, factionID); err != nil {
		warn("reference data (showing last good lines)", err)
	} else {
		var listings []linedash.Listing
		if s.market != nil {
			if listings, err = s.market.OurListings(ctx, now.Add(-time.Hour)); err != nil {
				warn("market.db listings", err)
			}
		}
		deferred := strings.Split(s.opt.deferFleets, ",")
		lines = linedash.Derive(linedash.DeriveInputs{
			Roster: roster, Schedules: sched, Ref: ref, Listings: attributeListings(listings, roster, ref, deferred),
			FactionTag: s.opt.faction, DeferFleets: deferred,
		})
		s.lastLines = lines
	}
	if lines == nil {
		return placeholder("no lines derived"), warnings
	}
	lines, ow := linedash.ApplyOverrides(lines, ov)
	warnings = append(warnings, ow...)
	warnings = append(warnings, linedash.CadenceWarnings(lines, sched)...)

	same := func(a, b string) bool {
		sa, okA := ref.Station(a)
		sb, okB := ref.Station(b)
		if okA && okB {
			return sa.BaseID == sb.BaseID
		}
		return a == b
	}
	reports := make([]linedash.LineReport, 0, len(lines))
	for _, line := range lines {
		in := linedash.Inputs{
			Now: now, Cfg: ov.Thresholds, Roster: roster, Live: live,
			Tanks:       map[string][]linedash.TankLevel{},
			Demand:      map[string]linedash.Demand{},
			Asks:        map[string]*linedash.OurAsk{},
			Outcomes:    map[string]map[string]outcomes.Outcome{},
			Queues:      map[string]linedash.CraftQueue{},
			SameStation: same,
		}
		var local []string
		lwarn := func(source string, err error) {
			msg := source + ": " + oneLine(err)
			s.logger.Printf("line %s: %s", line.ID, msg)
			if !slices.Contains(local, msg) {
				local = append(local, msg)
			}
		}
		s.loadLineInputs(ctx, line, &in, sched, lwarn)
		r := linedash.Evaluate(line, in)
		r.StaleSources = append(r.StaleSources, global...)
		r.StaleSources = append(r.StaleSources, local...)
		linedash.Layout(&r)
		reports = append(reports, r)
	}
	if len(reports) == 0 {
		return placeholder("no lines derived"), warnings
	}

	return reports, warnings
}

// attributeListings gives each of our asks the first non-deferred agent
// pinned to its station, so the sell node reads that agent's shop state. A
// station whose only pinned agents are in a deferred fleet (the marketbots'
// shops) is dropped with its asks; one with no pinned agent of ours is a
// faction sell order and keeps an empty agent.
func attributeListings(ls []linedash.Listing, roster map[string]linedash.RosterEntry, ref linedash.RefData, deferFleets []string) []linedash.Listing {
	agents := make([]string, 0, len(roster))
	for a := range roster {
		agents = append(agents, a)
	}
	slices.Sort(agents)
	out := make([]linedash.Listing, 0, len(ls))
	for _, l := range ls {
		st, ok := ref.Station(l.StationID)
		if !ok {
			continue
		}
		deferredOnly := false
		for _, a := range agents {
			e := roster[a]
			pin, pinned := ref.Station(e.Station)
			if e.Removed || e.Station == "" || !pinned || pin.BaseID != st.BaseID {
				continue
			}
			if slices.Contains(deferFleets, e.Fleet) {
				deferredOnly = l.Agent == ""
				continue
			}
			l.Agent, deferredOnly = a, false
			break
		}
		if !deferredOnly {
			out = append(out, l)
		}
	}
	return out
}

func (s *server) loadLineInputs(ctx context.Context, line linedash.Line, in *linedash.Inputs, sched map[string][]linedash.Task, warn func(string, error)) {
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
	for _, a := range agents {
		dir := filepath.Join(s.opt.agentsDir, a)
		if o, err := outcomes.Load(dir); err != nil {
			warn("task outcomes", err)
		} else if len(o) > 0 {
			in.Outcomes[a] = linedash.CurrentOutcomes(o, sched[a]) // a reused task id must not inherit the old task's failure
		}
		if q, err := linedash.LoadCraftQueue(dir); err != nil {
			warn("craft queue", err)
		} else if !q.CapturedAt.IsZero() {
			in.Queues[a] = q
		}
	}
	if s.market != nil {
		for _, n := range line.Nodes {
			if n.Type != linedash.NodeSell {
				continue
			}
			item := linedash.SellItem(n, line.OutputItem)
			d, err := s.market.Demand(ctx, n.Station, item)
			if err != nil {
				warn("market.db", err)
			} else {
				in.Demand[n.ID] = d
			}
			askers := n.Agents
			if len(askers) == 0 {
				askers = []string{""} // a faction sell order: the exchange row alone
			}
			for _, a := range askers {
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

// forced reports whether the request asks for a rebuild (?refresh=1).
func forced(r *http.Request) bool { return r.URL.Query().Has("refresh") }

func (s *server) find(ctx context.Context, id string, force bool) (linedash.LineReport, bool) {
	for _, r := range s.current(ctx, force) {
		if r.ID == id {
			return r, true
		}
	}

	return linedash.LineReport{}, false
}

func (s *server) handleBoard(w http.ResponseWriter, r *http.Request) {
	reports := s.current(r.Context(), forced(r))
	s.mu.RLock()
	warnings := s.warnings
	s.mu.RUnlock()
	page, err := linedash.RenderBoard(reports, warnings, s.opt.refresh)
	s.writeHTML(w, page, err)
}

func (s *server) handleLine(w http.ResponseWriter, r *http.Request) {
	rep, ok := s.find(r.Context(), r.PathValue("id"), forced(r))
	if !ok {
		http.NotFound(w, r)

		return
	}
	page, err := linedash.RenderLine(rep, s.opt.refresh)
	s.writeHTML(w, page, err)
}

func (s *server) handleLinesJSON(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, s.current(r.Context(), forced(r)))
}

func (s *server) handleLineJSON(w http.ResponseWriter, r *http.Request) {
	id, ok := strings.CutSuffix(r.PathValue("file"), ".json")
	if !ok {
		http.NotFound(w, r)

		return
	}
	rep, found := s.find(r.Context(), id, forced(r))
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
