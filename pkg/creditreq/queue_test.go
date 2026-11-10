package creditreq

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

func newTestQueue(t *testing.T) (*Queue, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "credit_requests.json")
	return NewQueue(p), p
}

func TestFile_OneOpenRecordPerAgent(t *testing.T) {
	q, _ := newTestQueue(t)
	filed, err := q.File(Record{AgentID: "a1", Username: "u1"})
	if err != nil || !filed {
		t.Fatalf("first File = %v, %v", filed, err)
	}
	filed, err = q.File(Record{AgentID: "a1", Username: "u1"})
	if err != nil || filed {
		t.Fatalf("second File = %v, %v; want false", filed, err)
	}
	if err = q.Remove("a1"); err != nil {
		t.Fatal(err)
	}
	filed, err = q.File(Record{AgentID: "a1"})
	if err != nil || !filed {
		t.Fatalf("File after Remove = %v, %v", filed, err)
	}
	recs, _ := q.List()
	if len(recs) != 1 || recs[0].Status != StatusPending || recs[0].RequestedAt.IsZero() {
		t.Fatalf("unexpected records: %+v", recs)
	}
}

func TestFile_HeldBlocksFiling(t *testing.T) {
	q, _ := newTestQueue(t)
	if _, err := q.File(Record{AgentID: "a1"}); err != nil {
		t.Fatal(err)
	}
	ok, err := q.Update("a1", func(r *Record) { r.Status = StatusHeld; r.HeldReason = "cap" })
	if err != nil || !ok {
		t.Fatalf("Update = %v, %v", ok, err)
	}
	filed, err := q.File(Record{AgentID: "a1"})
	if err != nil || filed {
		t.Fatalf("File while held = %v, %v; want false", filed, err)
	}
}

func TestFile_ReplacesPaidRecord(t *testing.T) {
	q, _ := newTestQueue(t)
	_, _ = q.File(Record{AgentID: "a1"})
	_, _ = q.Update("a1", func(r *Record) { r.Status = StatusPaid })
	filed, err := q.File(Record{AgentID: "a1"})
	if err != nil || !filed {
		t.Fatalf("File over paid = %v, %v", filed, err)
	}
	recs, _ := q.List()
	if len(recs) != 1 || recs[0].Status != StatusPending {
		t.Fatalf("unexpected records: %+v", recs)
	}
}

func TestList_MissingFileIsEmpty(t *testing.T) {
	q, _ := newTestQueue(t)
	recs, err := q.List()
	if err != nil || len(recs) != 0 {
		t.Fatalf("List = %v, %v", recs, err)
	}
}

func TestConcurrentFile(t *testing.T) {
	_, p := newTestQueue(t)
	qs := []*Queue{NewQueue(p), NewQueue(p)}
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := qs[i%2].File(Record{AgentID: fmt.Sprintf("a%d", i)}); err != nil {
				t.Errorf("File: %v", err)
			}
		}()
	}
	wg.Wait()
	recs, err := qs[0].List()
	if err != nil || len(recs) != 20 {
		t.Fatalf("got %d records, err %v; want 20", len(recs), err)
	}
}

func TestUpdate_UnknownAgent(t *testing.T) {
	q, _ := newTestQueue(t)
	ok, err := q.Update("nobody", func(r *Record) { r.Attempts++ })
	if ok || err != nil {
		t.Fatalf("Update = %v, %v; want false, nil", ok, err)
	}
}
