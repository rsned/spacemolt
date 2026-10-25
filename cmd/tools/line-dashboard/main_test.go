package main

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/assets"
	"github.com/rsned/spacemolt/pkg/linedash"
	"github.com/rsned/spacemolt/pkg/market"
)

var fixtureLines = filepath.Join("..", "..", "..", "pkg", "linedash", "testdata", "lines_moonshine.yaml")

func testServer(dir string) *server {
	return &server{
		opt: options{
			lines:       fixtureLines,
			overmindDir: filepath.Join(dir, "overmind"),
			agentsDir:   filepath.Join(dir, "agents"),
			assetsDB:    filepath.Join(dir, "assets.db"),
			marketDB:    filepath.Join(dir, "market.db"),
			refresh:     60,
		},
		logger: log.New(io.Discard, "", 0),
	}
}

// A wrong --overmind-dir must say so on every line and must not turn into a
// confident "no bots on node".
func TestBuildWarnsOnEmptyOvermindDir(t *testing.T) {
	s := testServer(t.TempDir())
	defer s.closeHandles()

	reports := s.build(context.Background())
	if len(reports) != 1 {
		t.Fatalf("got %d reports, want 1", len(reports))
	}
	r := reports[0]
	for _, want := range []string{"overmind-dir: no fleet yamls found", "overmind-dir: no status files found"} {
		if !slices.Contains(r.StaleSources, want) {
			t.Errorf("StaleSources %v lack %q", r.StaleSources, want)
		}
	}
	for _, n := range r.Nodes {
		if n.ID == "ice_mine" && (n.State != linedash.StateUnknown || slices.Contains(n.Alerts, linedash.AlertNoBotsLive)) {
			t.Errorf("ice_mine state=%q alerts=%v, want unknown without no-bots", n.State, n.Alerts)
		}
	}
}

// A client that goes away mid-build must not fail the build's queries and
// poison the cached report for every viewer.
func TestCurrentIgnoresCancelledRequest(t *testing.T) {
	dir := t.TempDir()
	s := testServer(dir)
	defer s.closeHandles()
	a, err := assets.Open(assets.Config{DBPath: s.opt.assetsDB})
	if err != nil {
		t.Fatal(err)
	}
	if err = a.Close(); err != nil {
		t.Fatal(err)
	}
	m, err := market.Open(market.Config{DBPath: s.opt.marketDB, WAL: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, r := range s.current(ctx, false) {
		for _, src := range r.StaleSources {
			if strings.Contains(src, "canceled") {
				t.Errorf("StaleSources entry %q from a cancelled request context", src)
			}
		}
	}
}

// TestCurrentForceRebuilds pins the manual refresh: within the cache window a
// plain call returns the cached build, a forced call rebuilds.
func TestCurrentForceRebuilds(t *testing.T) {
	dir := t.TempDir()
	s := testServer(dir)
	defer s.closeHandles()
	ctx := context.Background()

	s.current(ctx, false)
	s.mu.RLock()
	first := s.builtAt
	s.mu.RUnlock()
	if first.IsZero() {
		t.Fatal("first call did not build")
	}

	s.current(ctx, false)
	s.mu.RLock()
	cached := s.builtAt
	s.mu.RUnlock()
	if !cached.Equal(first) {
		t.Fatalf("unforced call rebuilt inside the cache window: %v -> %v", first, cached)
	}

	s.current(ctx, true)
	s.mu.RLock()
	forcedAt := s.builtAt
	s.mu.RUnlock()
	if !forcedAt.After(first) {
		t.Fatalf("forced call did not rebuild: builtAt still %v", forcedAt)
	}
}

func TestForcedQuery(t *testing.T) {
	for _, tc := range []struct {
		url  string
		want bool
	}{
		{"/line/moonshine", false},
		{"/line/moonshine?refresh=1", true},
		{"/api/lines.json?refresh", true},
	} {
		r := httptest.NewRequest(http.MethodGet, tc.url, nil)
		if got := forced(r); got != tc.want {
			t.Errorf("forced(%q) = %v, want %v", tc.url, got, tc.want)
		}
	}
}
