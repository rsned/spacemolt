package sweep

import (
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// Two ships hold two clusters; one finds a lane into the other's. Each keeps
// its own: the other ship's systems are a wall, not a refusal.
func TestClaimClusterWallsOffAnotherShipsClaim(t *testing.T) {
	path := frontierFixture(t)
	for agent, sys := range map[string]string{"explorer-1": "a", "explorer-2": "d"} {
		if _, ok, err := ClaimCluster(path, agent, sys, frontierNow); err != nil || !ok {
			t.Fatalf("%s claim %s ok=%v err=%v", agent, sys, ok, err)
		}
	}
	// explorer-2, in d, finds lanes to a new system q and to c (explorer-1's).
	if err := AddSystem(path, FrontierInput{ID: "q", X: 310, Y: 0}, "d", "explorer-2", frontierNow); err != nil {
		t.Fatal(err)
	}
	if err := AddSystem(path, FrontierInput{ID: "c"}, "d", "explorer-2", frontierNow); err != nil {
		t.Fatal(err)
	}
	ids, ok, err := ClaimCluster(path, "explorer-2", "d", frontierNow)
	if err != nil || !ok || !slices.Equal(ids, []string{"d", "q"}) {
		t.Fatalf("explorer-2 reclaim: %v ok=%v err=%v, want [d q]", ids, ok, err)
	}
	ids, ok, err = ClaimCluster(path, "explorer-1", "a", frontierNow)
	if err != nil || !ok || !slices.Equal(ids, []string{"a", "b", "c"}) {
		t.Errorf("explorer-1 reclaim: %v ok=%v err=%v, want [a b c]", ids, ok, err)
	}
	// Arriving in a system another ship holds is still refused.
	if _, ok, err = ClaimCluster(path, "explorer-2", "c", frontierNow); err != nil || ok {
		t.Errorf("explorer-2 claimed from explorer-1's system: ok=%v err=%v", ok, err)
	}
}

// A todo system between two ships' clusters can still be claimed by a third.
func TestClaimClusterTodoBetweenTwoClaims(t *testing.T) {
	path := frontierFixture(t)
	for agent, sys := range map[string]string{"explorer-1": "a", "explorer-2": "d"} {
		if _, ok, err := ClaimCluster(path, agent, sys, frontierNow); err != nil || !ok {
			t.Fatalf("%s claim %s ok=%v err=%v", agent, sys, ok, err)
		}
	}
	// explorer-2 finds t by a lane from c, which is explorer-1's: t stays todo.
	if err := AddSystem(path, FrontierInput{ID: "t", X: 200, Y: 0}, "c", "explorer-2", frontierNow); err != nil {
		t.Fatal(err)
	}
	if err := AddSystem(path, FrontierInput{ID: "t"}, "d", "explorer-2", frontierNow); err != nil {
		t.Fatal(err)
	}
	ids, ok, err := ClaimCluster(path, "explorer-3", "t", frontierNow)
	if err != nil || !ok || !slices.Equal(ids, []string{"t"}) {
		t.Errorf("explorer-3 claim t: %v ok=%v err=%v, want [t]", ids, ok, err)
	}
}

// A ship giving up on a target must not rewrite a result another ship filed.
func TestFinishSystemKeepsAnotherShipsResult(t *testing.T) {
	path := frontierFixture(t)
	if _, ok, err := ClaimCluster(path, "explorer-1", "d", frontierNow); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if err := FinishSystem(path, "explorer-1", FrontierSystem{ID: "d", Status: FrontierDone, SurveyPower: 90, POIsVisited: 4}, frontierNow); err != nil {
		t.Fatal(err)
	}
	err := FinishSystem(path, "explorer-2", FrontierSystem{ID: "d", Status: FrontierIncomplete, Reason: "unreachable"}, frontierNow)
	if err == nil {
		t.Error("explorer-2 overwrote explorer-1's finished system")
	}
	if d := loadFrontier(t, path).Systems["d"]; d.Status != FrontierDone || d.Claimant != "explorer-1" || d.POIsVisited != 4 {
		t.Errorf("d = %+v, want explorer-1's result kept", d)
	}
	// The same ship may still revise its own result.
	if err := FinishSystem(path, "explorer-1", FrontierSystem{ID: "d", Status: FrontierIncomplete, Reason: "x"}, frontierNow); err != nil {
		t.Errorf("explorer-1 revising its own result: %v", err)
	}
}

// A ship's own unfinished claims come back to it, nearest first.
func TestOwnClaim(t *testing.T) {
	path := frontierFixture(t)
	if got, err := OwnClaim(path, "explorer-1", 0, 0); err != nil || got != nil {
		t.Fatalf("own claim with none held: %+v err=%v", got, err)
	}
	if _, ok, err := ClaimCluster(path, "explorer-1", "a", frontierNow); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if err := FinishSystem(path, "explorer-1", FrontierSystem{ID: "c", Status: FrontierDone}, frontierNow); err != nil {
		t.Fatal(err)
	}
	got, err := OwnClaim(path, "explorer-1", 125, 0)
	if err != nil || got == nil || got.ID != "b" {
		t.Errorf("own claim near c = %+v err=%v, want b (c is done)", got, err)
	}
	if got, _ := OwnClaim(path, "explorer-2", 125, 0); got != nil {
		t.Errorf("explorer-2 got explorer-1's claim %s", got.ID)
	}
}

// Systems written off as unreachable go back to todo; other incompletes stay.
func TestReopenUnreachable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.json")
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	in := []FrontierInput{{ID: "a", X: 400}, {ID: "b", X: 500}, {ID: "c", X: 600}, {ID: "d", X: 700}}
	if err := SaveFrontierRun(path, NewFrontierRun(in, nil, now)); err != nil {
		t.Fatal(err)
	}
	for id, reason := range map[string]string{"a": "unreachable", "b": "unreachable by lane", "c": "hostile: leviathan"} {
		if err := FinishSystem(path, "explorer-1", FrontierSystem{ID: id, Status: FrontierIncomplete, Reason: reason}, now); err != nil {
			t.Fatal(err)
		}
	}
	got, err := ReopenUnreachable(path, now, "a")
	if err != nil || !slices.Equal(got, []string{"a"}) {
		t.Fatalf("reopen a = %v, %v", got, err)
	}
	got, err = ReopenUnreachable(path, now)
	if err != nil || !slices.Equal(got, []string{"b"}) {
		t.Fatalf("reopen all = %v, %v, want only b left to reopen", got, err)
	}
	run, err := LoadFrontierRun(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b", "d"} {
		if s := run.Systems[id]; s.Status != FrontierTodo || s.Claimant != "" || s.Reason != "" {
			t.Errorf("%s = %+v, want a clean todo", id, s)
		}
	}
	if s := run.Systems["c"]; s.Status != FrontierIncomplete {
		t.Errorf("c = %q, want the hostile system left incomplete", s.Status)
	}
}

// Better positions replace the ledger's for systems still to be flown to;
// a finished system keeps the position it was explored at.
func TestSetPositions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.json")
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	in := []FrontierInput{{ID: "a", X: 400}, {ID: "b", X: 500}, {ID: "c", X: 600}}
	if err := SaveFrontierRun(path, NewFrontierRun(in, nil, now)); err != nil {
		t.Fatal(err)
	}
	if err := FinishSystem(path, "explorer-1", FrontierSystem{ID: "b", Status: FrontierDone}, now); err != nil {
		t.Fatal(err)
	}
	n, err := SetPositions(path, map[string]Point{"a": {X: 44, Y: 4806}, "b": {X: 1, Y: 1}, "c": {X: 600, Y: 0}, "zzz": {X: 9, Y: 9}}, now)
	if err != nil || n != 1 {
		t.Fatalf("SetPositions = %d, %v, want 1 moved", n, err)
	}
	run, err := LoadFrontierRun(path)
	if err != nil {
		t.Fatal(err)
	}
	if a := run.Systems["a"]; a.X != 44 || a.Y != 4806 {
		t.Errorf("a at (%v, %v), want (44, 4806)", a.X, a.Y)
	}
	if b := run.Systems["b"]; b.X != 500 {
		t.Errorf("b at %v, want the finished system left at 500", b.X)
	}
	if run.Systems["zzz"] != nil {
		t.Error("a system the ledger did not know was added")
	}
}
