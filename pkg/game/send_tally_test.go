package game

import (
	"strings"
	"testing"
)

// The diagnostic the 2026-09-09 block needed and did not have. Other operators
// reportedly run ~1000 agents on one IP without blocks, so our 170 are not
// inherently too many — something specific is burning requests, and naming the
// command is the only way to find it.
func TestSendTallyCountsPerCommand(t *testing.T) {
	var t1 sendTally
	t1.record("get_status")
	t1.record("get_status")
	t1.record("mine")

	snap := t1.snapshot()
	if snap.Counts["get_status"] != 2 {
		t.Fatalf("get_status = %d, want 2", snap.Counts["get_status"])
	}
	if snap.Counts["mine"] != 1 {
		t.Fatalf("mine = %d, want 1", snap.Counts["mine"])
	}
	if snap.Total != 3 {
		t.Fatalf("Total = %d, want 3", snap.Total)
	}
}

// snapshot must RESET, so each logged line is a rate over its own window rather
// than a cumulative total that flattens out and hides a late-starting spike.
func TestSendTallySnapshotResets(t *testing.T) {
	var t1 sendTally
	t1.record("dock")
	t1.snapshot()

	if got := t1.snapshot(); got.Total != 0 {
		t.Fatalf("Total = %d after a reset, want 0", got.Total)
	}
}

// An empty window must report nothing, so a quiet agent produces no log line at
// all rather than a row of zeroes across 170 agents.
func TestSendTallyEmptySnapshotIsEmpty(t *testing.T) {
	var t1 sendTally
	if got := t1.snapshot(); got.Total != 0 || len(got.Counts) != 0 {
		t.Fatalf("empty tally produced %+v", got)
	}
}

// The line is what an operator greps and sums across the fleet, so it must be
// ordered by volume — the worst offender first — and name the total.
func TestSendTallyStringIsOrderedByVolume(t *testing.T) {
	s := sendSnapshot{
		Total:  14,
		Counts: map[string]int64{"get_status": 2, "get_nearby": 9, "mine": 3},
	}.String()

	if !strings.HasPrefix(s, "send_tally total=14") {
		t.Fatalf("line %q does not lead with the total", s)
	}
	iNearby := strings.Index(s, "get_nearby=9")
	iMine := strings.Index(s, "mine=3")
	iStatus := strings.Index(s, "get_status=2")
	if iNearby < 0 || iMine < 0 || iStatus < 0 {
		t.Fatalf("line %q is missing a command", s)
	}
	if iNearby >= iMine || iMine >= iStatus {
		t.Fatalf("line %q is not ordered by descending volume", s)
	}
}

// Concurrent sends must not race the snapshot: the client sends from more than
// one goroutine, and a tally that corrupts under load is worse than none.
func TestSendTallyConcurrent(t *testing.T) {
	var t1 sendTally
	done := make(chan struct{})
	for range 4 {
		go func() {
			for range 250 {
				t1.record("get_status")
			}
			done <- struct{}{}
		}()
	}
	for range 4 {
		<-done
	}
	if got := t1.snapshot().Counts["get_status"]; got != 1000 {
		t.Fatalf("get_status = %d, want 1000", got)
	}
}
