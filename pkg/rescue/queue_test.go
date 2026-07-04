package rescue

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
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
