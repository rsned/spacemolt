// Command overmind-dashboard serves the unified fleet ops dashboard: live
// galaxy map, per-agent cards, and fleet accounting, backed by the overmind
// status files, the knowledge DB, and market.db — strictly read-only.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/rsned/spacemolt/pkg/ovdash"
)

const (
	snapshotEvery  = 2 * time.Second
	accountEvery   = 30 * time.Second
	keyframeEvery  = 60 * time.Second
	staleAfter     = 60 * time.Second
	earningsWindow = 24 * time.Hour
)

type serverConfig struct {
	KBPath, MarketPath, StatusDir, DistDir string
}

type server struct {
	cfg    serverConfig
	galaxy *ovdash.Galaxy
	hub    *ovdash.Hub

	mu   sync.RWMutex
	snap *ovdash.Snapshot
	acct ovdash.Accounting
	// lastAcct is only ever written by refresh(), which is called
	// exclusively from the single run() goroutine (plus once synchronously
	// at startup before that goroutine is launched) — so it needs no lock.
	lastAcct time.Time
}

func newServer(ctx context.Context, cfg serverConfig) (*server, error) {
	g, err := ovdash.LoadGalaxy(ctx, cfg.KBPath)
	if err != nil {
		return nil, err
	}
	return &server{cfg: cfg, galaxy: g, hub: ovdash.NewHub()}, nil
}

// refresh performs one loop turn: read snapshot, diff, broadcast, and (when
// due) refresh accounting. Split out from run() so tests drive it directly.
func (s *server) refresh(ctx context.Context, now time.Time) {
	snap, err := ovdash.ReadSnapshot(s.cfg.StatusDir, s.galaxy, now, staleAfter)
	if err != nil {
		log.Printf("snapshot: %v", err)
	}
	if snap == nil {
		return
	}
	s.mu.Lock()
	prev := s.snap
	s.snap = snap
	s.mu.Unlock()

	d := ovdash.Diff(prev, snap)
	// Keyframe must be updated before delta broadcast: a client connecting
	// between the two calls is never left with neither (it either gets the
	// fresh keyframe, or was registered for the delta).
	s.hub.SetKeyframe("snapshot", snap)
	if !d.Empty() || prev == nil {
		s.hub.Broadcast("delta", d)
	}

	if now.Sub(s.lastAcct) >= accountEvery {
		haul, freight, missions, err := ovdash.LoadEarnings(ctx, s.cfg.MarketPath, now, earningsWindow)
		if err != nil {
			log.Printf("earnings: %v", err) // keep last-good acct
		} else {
			acct := ovdash.BuildAccounting(snap, haul, freight, missions, earningsWindow)
			s.mu.Lock()
			s.acct = acct
			s.mu.Unlock()
			s.hub.Broadcast("accounting", acct)
		}
		s.lastAcct = now
	}
}

func (s *server) mux() *http.ServeMux {
	m := http.NewServeMux()
	m.HandleFunc("GET /api/overmind/systems", func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResp(w, s.galaxy.Systems)
	})
	m.HandleFunc("GET /api/overmind/agents", func(w http.ResponseWriter, _ *http.Request) {
		s.mu.RLock()
		snap := s.snap
		s.mu.RUnlock()
		writeJSONResp(w, snap)
	})
	m.HandleFunc("GET /api/overmind/accounting", func(w http.ResponseWriter, _ *http.Request) {
		s.mu.RLock()
		acct := s.acct
		s.mu.RUnlock()
		writeJSONResp(w, acct)
	})
	m.Handle("GET /api/overmind/stream", s.hub)
	m.Handle("/", http.FileServer(http.Dir(s.cfg.DistDir)))
	return m
}

func writeJSONResp(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("encode response: %v", err)
	}
}

func (s *server) run(ctx context.Context) {
	snapTick := time.NewTicker(snapshotEvery)
	kfTick := time.NewTicker(keyframeEvery)
	defer snapTick.Stop()
	defer kfTick.Stop()
	for {
		select {
		case <-ctx.Done():
			s.hub.CloseAll()
			return
		case now := <-snapTick.C:
			s.refresh(ctx, now)
		case <-kfTick.C:
			s.mu.RLock()
			snap := s.snap
			s.mu.RUnlock()
			if snap != nil {
				s.hub.Broadcast("snapshot", snap)
			}
		}
	}
}

func main() {
	addr := flag.String("addr", ":8091", "HTTP listen address")
	kb := flag.String("kb", "data/spacemolt-knowledge.db", "knowledge DB path (read-only)")
	marketDB := flag.String("market-db", "data/market.db", "market DB path (read-only)")
	statusDir := flag.String("status-dir", "data/overmind", "overmind status-file directory")
	dist := flag.String("dist", "frontend/dist", "built frontend directory")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv, err := newServer(ctx, serverConfig{KBPath: *kb, MarketPath: *marketDB, StatusDir: *statusDir, DistDir: *dist})
	if err != nil {
		log.Fatalf("overmind-dashboard: %v", err)
	}
	srv.refresh(ctx, time.Now())
	go srv.run(ctx)

	log.Printf("overmind-dashboard: %d systems loaded, serving on %s", len(srv.galaxy.Systems), *addr)
	httpSrv := &http.Server{Addr: *addr, Handler: srv.mux(), ReadHeaderTimeout: 10 * time.Second}
	go func() { <-ctx.Done(); _ = httpSrv.Close() }()
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
