package worker

import (
	"context"
	"io"
	"slices"
	"testing"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// Every engagement, however it ends, files its battle.
func TestHuntCapturesAttacksAfterEveryFight(t *testing.T) {
	cases := []struct {
		name string
		opts huntFakeOpts
	}{
		{"resolved", huntFakeOpts{
			board:     []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 1}},
			creatures: huntGrazers("c1"),
		}},
		{"fled", huntFakeOpts{
			board:         []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 3}},
			creatures:     huntGrazers("c1", "c2", "c3"),
			battleHullPct: 10,
		}},
		{"gave up", huntFakeOpts{
			board:       []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 1}},
			creatures:   huntGrazers("c1"),
			neverCloses: true,
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newHuntFake(t, tc.opts)
			var got []string
			deps := huntDeps(c, io.Discard)
			deps.FleeAtHull = 0.3
			deps.CaptureAttacks = func(_ context.Context, id string) error {
				got = append(got, id)
				return nil
			}
			if err := Hunt(context.Background(), deps); err != nil {
				t.Fatalf("Hunt: %v", err)
			}
			if c.huntCalls == 0 {
				t.Fatal("no engagement happened")
			}
			if len(got) != c.huntCalls {
				t.Fatalf("CaptureAttacks calls = %v, want one per engagement (%d)", got, c.huntCalls)
			}
			if got[0] != "battle-c1" {
				t.Errorf("first captured battle = %q, want battle-c1", got[0])
			}
		})
	}
}

// With no battle id known there is nothing to fetch: no call, not an empty id.
func TestHuntSkipsAttackCaptureWithoutABattleID(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{
		board:     []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 1}},
		creatures: huntGrazers("c1"),
	})
	called := false
	deps := huntDeps(c, io.Discard)
	deps.CaptureAttacks = func(context.Context, string) error { called = true; return nil }
	c.state.LastBattleID = ""
	huntCaptureAttacks(context.Background(), deps, io.Discard, "")
	if called {
		t.Error("CaptureAttacks called with no battle id")
	}
}

type huntScanFake struct {
	*huntFake
	scanned []string
}

func (f *huntScanFake) ScanTarget(_ context.Context, id string) error {
	f.scanned = append(f.scanned, id)
	return nil
}

type huntScanKB struct {
	knowledge.Base
	unscanned []string
}

func (k *huntScanKB) UnscannedWildlifeSpecies(_ context.Context, species []string) ([]string, error) {
	var out []string
	for _, s := range species {
		if slices.Contains(k.unscanned, s) {
			out = append(out, s)
		}
	}
	return out, nil
}

// An unscanned species at the ground is scanned once on arrival, not once per
// fight.
func TestHuntScansUnknownOnArrival(t *testing.T) {
	f := newHuntFake(t, huntFakeOpts{
		board:     []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 2}},
		creatures: huntGrazers("c1", "c2"),
	})
	c := &huntScanFake{huntFake: f}
	deps := huntDeps(f, io.Discard)
	deps.Client = c
	deps.KB = &huntScanKB{Base: deps.KB, unscanned: []string{"belt_grazer"}}

	if err := Hunt(context.Background(), deps); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if len(c.scanned) != 1 {
		t.Errorf("scans = %v, want exactly one for the arrival (not one per fight)", c.scanned)
	}
}

// A second engagement that learns no battle id must not re-capture the first.
func TestHuntDoesNotRecaptureAStaleBattleID(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{
		board:         []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 2}},
		creatures:     huntGrazers("c1", "c2"),
		noNewBattleID: true,
	})
	var got []string
	deps := huntDeps(c, io.Discard)
	deps.CaptureAttacks = func(_ context.Context, id string) error {
		got = append(got, id)
		return nil
	}
	if err := Hunt(context.Background(), deps); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if c.huntCalls != 2 {
		t.Fatalf("hunt calls = %d, want 2", c.huntCalls)
	}
	if !slices.Equal(got, []string{"battle-c1"}) {
		t.Errorf("captures = %v, want only [battle-c1]", got)
	}
}
