package worker

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

const releaseStorage = `{"items":[{"item_id":"carbon_ore","quantity":99985},{"item_id":"iron_ore","quantity":5}]}`

func newReleaseDispatch(t *testing.T, docked bool, base, poi string) (*fakeClient, *WorkerDispatch, *bytes.Buffer) {
	t.Helper()
	st := &game.State{Doc: docked, CurrentPOI: poi}
	st.Player.DockedAtBase = base
	fc := &fakeClient{state: st, raw: map[string][]byte{"storage": []byte(releaseStorage)}}
	var out bytes.Buffer
	d := NewWorkerDispatch(fc, nil, nil, &out)
	d.AgentID = "prophet-1"
	d.AgentsDir = t.TempDir()
	dir := filepath.Join(d.AgentsDir, "trucker-101")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "credentials.json"), []byte(`{"username":"u_mother_trucker_101"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return fc, d, &out
}

// An owner docked at the route's station gifts its stock straight from
// storage to the route's trucker (2026-11-20 route fleet).
func TestReleaseGiftsStorageAboveKeepToTrucker(t *testing.T) {
	fc, d, out := newReleaseDispatch(t, true, "treasure_cache_trading_post", "treasure_cache_poi")
	if err := d.Run(context.Background(), []string{"release", "carbon_ore", "treasure_cache_trading_post", "trucker-101", "50000"}); err != nil {
		t.Fatal(err)
	}
	want := "gift:u_mother_trucker_101:carbon_ore:49985:storage"
	if !slices.Contains(fc.calls, want) {
		t.Fatalf("calls %v, want %s", fc.calls, want)
	}
	if !strings.Contains(out.String(), "release: gifted 49985 carbon_ore to trucker-101") {
		t.Errorf("log %q", out.String())
	}
}

// Stations are dual-named: the POI id must match as well as the base id.
func TestReleaseMatchesStationByPOI(t *testing.T) {
	fc, d, _ := newReleaseDispatch(t, true, "treasure_cache_trading_post", "treasure_cache_poi")
	if err := d.Run(context.Background(), []string{"release", "carbon_ore", "treasure_cache_poi", "trucker-101"}); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(fc.calls, "gift:u_mother_trucker_101:carbon_ore:99985:storage") {
		t.Fatalf("calls %v", fc.calls)
	}
}

// Elsewhere, or undocked, it costs nothing: owners run it ten-minutely.
func TestReleaseElsewhereIsANoCallNoop(t *testing.T) {
	for _, tc := range []struct {
		docked bool
		base   string
	}{{true, "ironhearth_station"}, {false, ""}} {
		fc, d, _ := newReleaseDispatch(t, tc.docked, tc.base, "x")
		if err := d.Run(context.Background(), []string{"release", "carbon_ore", "treasure_cache_trading_post", "trucker-101"}); err != nil {
			t.Fatal(err)
		}
		if len(fc.calls) != 0 {
			t.Fatalf("docked=%v base=%q: calls %v, want none", tc.docked, tc.base, fc.calls)
		}
	}
}

func TestReleaseAtOrBelowKeepGiftsNothing(t *testing.T) {
	fc, d, _ := newReleaseDispatch(t, true, "treasure_cache_trading_post", "p")
	if err := d.Run(context.Background(), []string{"release", "iron_ore", "treasure_cache_trading_post", "trucker-101", "5"}); err != nil {
		t.Fatal(err)
	}
	for _, c := range fc.calls {
		if strings.HasPrefix(c, "gift:") {
			t.Fatalf("gifted at the keep floor: %v", fc.calls)
		}
	}
}

func TestReleaseBadArgs(t *testing.T) {
	_, d, _ := newReleaseDispatch(t, true, "s", "p")
	for _, args := range [][]string{{"release", "carbon_ore", "s"}, {"release", "carbon_ore", "s", "trucker-101", "-1"}} {
		if err := d.Run(context.Background(), args); err == nil {
			t.Errorf("%v accepted", args)
		}
	}
}
