package rescue

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestQueueLifecycle(t *testing.T) {
	q := NewQueue(filepath.Join(t.TempDir(), "queue.json"))

	ok, err := q.Enqueue(Record{AgentID: "trader-8", Fleet: "haul", RescueFuel: 15})
	if err != nil || !ok {
		t.Fatalf("enqueue: ok=%v err=%v", ok, err)
	}
	if ok, _ := q.Enqueue(Record{AgentID: "trader-8"}); ok {
		t.Fatal("duplicate enqueue must be rejected")
	}

	recs, err := q.List()
	if err != nil || len(recs) != 1 {
		t.Fatalf("list: %v %v", recs, err)
	}
	if recs[0].Status != StatusPending || recs[0].RequestedAt == "" || recs[0].UpdatedAt == "" {
		t.Fatalf("enqueue must default status/timestamps: %+v", recs[0])
	}

	ok, err = q.Transition("trader-8", StatusPending, StatusClaimed, func(r *Record) { r.ClaimedBy = "assist-sol" })
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	if ok, _ := q.Transition("trader-8", StatusPending, StatusClaimed, nil); ok {
		t.Fatal("wrong-from transition must fail (CAS)")
	}
	if ok, _ := q.Transition("nobody", StatusPending, StatusClaimed, nil); ok {
		t.Fatal("unknown agent transition must fail")
	}
	if ok, _ := q.Transition("trader-8", StatusClaimed, StatusDone, nil); !ok {
		t.Fatal("done transition failed")
	}

	rec, err := q.Remove("trader-8")
	if err != nil || rec == nil || rec.ClaimedBy != "assist-sol" {
		t.Fatalf("remove: %+v %v", rec, err)
	}
	if rec, _ := q.Remove("trader-8"); rec != nil {
		t.Fatal("second remove must return nil")
	}
	if recs, _ := q.List(); len(recs) != 0 {
		t.Fatalf("queue should be empty, got %v", recs)
	}
}

// The takeover election widens on how long a rescue has gone UNCLAIMED, so
// every route back into pending must restart that clock. Stamping it inside
// Transition (rather than at each call site) is what makes that hold for
// routes added later.
func TestPendingSinceIsRestampedOnEveryEntryIntoPending(t *testing.T) {
	q := NewQueue(filepath.Join(t.TempDir(), "queue.json"))
	clock := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	q.now = func() time.Time { return clock }

	if _, err := q.Enqueue(Record{AgentID: "random-9"}); err != nil {
		t.Fatal(err)
	}
	recs, _ := q.List()
	filed := recs[0].PendingSince
	if filed == "" {
		t.Fatal("Enqueue must stamp PendingSince")
	}
	if recs[0].RequestedAt != filed {
		t.Fatalf("at enqueue the two stamps agree: %q vs %q", recs[0].RequestedAt, filed)
	}

	// Two days later the record is claimed, then released back to pending.
	clock = clock.Add(48 * time.Hour)
	if ok, _ := q.Transition("random-9", StatusPending, StatusClaimed, nil); !ok {
		t.Fatal("claim failed")
	}
	if ok, _ := q.Transition("random-9", StatusClaimed, StatusPending, nil); !ok {
		t.Fatal("release failed")
	}

	recs, _ = q.List()
	if recs[0].PendingSince == filed {
		t.Error("re-entering pending must restart the takeover clock, not inherit the filing time")
	}
	if recs[0].RequestedAt != filed {
		t.Errorf("RequestedAt is the filing time and must NOT move: %q, want %q", recs[0].RequestedAt, filed)
	}
	if want := clock.UTC().Format(time.RFC3339); recs[0].PendingSince != want {
		t.Errorf("PendingSince = %q, want %q", recs[0].PendingSince, want)
	}
}

// Leaving pending must not touch the stamp — otherwise a claim would reset the
// ladder for whoever the claim is later released to.
func TestPendingSinceUnchangedWhenLeavingPending(t *testing.T) {
	q := NewQueue(filepath.Join(t.TempDir(), "queue.json"))
	clock := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	q.now = func() time.Time { return clock }
	if _, err := q.Enqueue(Record{AgentID: "random-1"}); err != nil {
		t.Fatal(err)
	}
	recs, _ := q.List()
	before := recs[0].PendingSince

	clock = clock.Add(time.Hour)
	if ok, _ := q.Transition("random-1", StatusPending, StatusClaimed, nil); !ok {
		t.Fatal("claim failed")
	}
	recs, _ = q.List()
	if recs[0].PendingSince != before {
		t.Errorf("claiming must not restamp PendingSince: %q, want %q", recs[0].PendingSince, before)
	}
}

func TestHasFailedTracksPerAssister(t *testing.T) {
	rec := Record{FailedBy: []string{"assist-krynn", "assist-haven"}}
	for _, id := range []string{"assist-krynn", "assist-haven"} {
		if !rec.HasFailed(id) {
			t.Errorf("HasFailed(%q) = false, want true", id)
		}
	}
	if rec.HasFailed("assist-nexus") {
		t.Error("HasFailed must be false for an assister that has not tried")
	}
	if (Record{}).HasFailed("anyone") {
		t.Error("a fresh record has no failures")
	}
}

func TestQueueMissingFileIsEmpty(t *testing.T) {
	q := NewQueue(filepath.Join(t.TempDir(), "absent.json"))
	recs, err := q.List()
	if err != nil || len(recs) != 0 {
		t.Fatalf("missing file: %v %v", recs, err)
	}
}

func TestQueueCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewQueue(path).List(); err == nil {
		t.Fatal("corrupt file must return an error (caller logs and skips the tick)")
	}
}

func TestQueueConcurrentWriters(t *testing.T) {
	q := NewQueue(filepath.Join(t.TempDir(), "queue.json"))
	var wg sync.WaitGroup
	agents := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
	for _, id := range agents {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := q.Enqueue(Record{AgentID: id}); err != nil {
				t.Errorf("enqueue %s: %v", id, err)
			}
		}()
	}
	wg.Wait()
	recs, err := q.List()
	if err != nil || len(recs) != len(agents) {
		t.Fatalf("want %d records, got %d (%v)", len(agents), len(recs), err)
	}
}
