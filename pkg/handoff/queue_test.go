package handoff

import (
	"path/filepath"
	"testing"
)

func TestEnqueueListRoundTrip(t *testing.T) {
	q := NewQueue(filepath.Join(t.TempDir(), "handoff-queue.json"))
	ok, err := q.Enqueue(Record{ID: "p1/haul-2", Holder: "marketbot_sol", Station: "sol_central",
		ItemID: "steel_plate", Qty: 40, Recipient: "craftsman-2", Status: StatusPending})
	if err != nil || !ok {
		t.Fatalf("enqueue = %v, %v", ok, err)
	}
	// Same ID again is a no-op, not a duplicate.
	ok, err = q.Enqueue(Record{ID: "p1/haul-2", Status: StatusPending})
	if err != nil || ok {
		t.Fatalf("re-enqueue = %v, %v; want false, nil", ok, err)
	}
	recs, err := q.List()
	if err != nil || len(recs) != 1 || recs[0].ItemID != "steel_plate" {
		t.Fatalf("list = %+v, %v", recs, err)
	}
}

func TestTransitionCompareAndSet(t *testing.T) {
	q := NewQueue(filepath.Join(t.TempDir(), "handoff-queue.json"))
	if _, err := q.Enqueue(Record{ID: "p1/n1", Status: StatusPending}); err != nil {
		t.Fatal(err)
	}
	ok, err := q.Transition("p1/n1", StatusPending, StatusDone, func(r *Record) { r.MovedQty = 40 })
	if err != nil || !ok {
		t.Fatalf("transition = %v, %v", ok, err)
	}
	// From-state no longer matches: CAS must refuse.
	ok, err = q.Transition("p1/n1", StatusPending, StatusFailed, nil)
	if err != nil || ok {
		t.Fatalf("stale transition = %v, %v; want false, nil", ok, err)
	}
	recs, _ := q.List()
	if recs[0].Status != StatusDone || recs[0].MovedQty != 40 {
		t.Fatalf("record = %+v", recs[0])
	}
}

// TestTransitionSameStatusUpdatesFields locks in a behavior pkg/worker's
// HandoffPass relies on for incremental progress persistence: Transition
// with from == to is not rejected as a no-op CAS — it still matches the
// current status, applies mutate, and bumps UpdatedAt, letting a caller use
// it as a "CAS-update fields on a still-pending record" primitive without a
// dedicated status transition.
func TestTransitionSameStatusUpdatesFields(t *testing.T) {
	q := NewQueue(filepath.Join(t.TempDir(), "handoff-queue.json"))
	if _, err := q.Enqueue(Record{ID: "p1/n1", Status: StatusPending}); err != nil {
		t.Fatal(err)
	}
	ok, err := q.Transition("p1/n1", StatusPending, StatusPending, func(r *Record) { r.MovedQty = 5 })
	if err != nil || !ok {
		t.Fatalf("same-status transition = %v, %v; want true, nil", ok, err)
	}
	recs, _ := q.List()
	if recs[0].Status != StatusPending || recs[0].MovedQty != 5 {
		t.Fatalf("record = %+v, want status pending, MovedQty 5", recs[0])
	}
	// The CAS guard still applies even with from == to: a record whose
	// actual status doesn't match from (still pending here, not done) must
	// refuse the update, not blindly overwrite fields.
	ok, err = q.Transition("p1/n1", StatusDone, StatusDone, func(r *Record) { r.MovedQty = 99 })
	if err != nil || ok {
		t.Fatalf("mismatched same-status transition = %v, %v; want false, nil", ok, err)
	}
	recs, _ = q.List()
	if recs[0].MovedQty != 5 {
		t.Fatalf("record MovedQty = %d after refused CAS, want unchanged 5", recs[0].MovedQty)
	}
}

func TestMissingFileIsEmptyQueue(t *testing.T) {
	q := NewQueue(filepath.Join(t.TempDir(), "nope.json"))
	recs, err := q.List()
	if err != nil || len(recs) != 0 {
		t.Fatalf("list = %v, %v; want empty, nil", recs, err)
	}
}
