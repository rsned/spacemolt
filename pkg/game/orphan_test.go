package game

import (
	"sync"
	"testing"
	"time"
)

func TestOrphanStats_Record(t *testing.T) {
	o := newOrphanStats()
	o.record("req-1", "action_result")
	o.record("req-2", "error")

	if got := o.Count(); got != 2 {
		t.Errorf("Count = %d, want 2", got)
	}
	if id, _, _ := o.Last(); id != "req-2" {
		t.Errorf("LastID = %q, want req-2", id)
	}
}

func TestOrphanStats_RecordConcurrent(t *testing.T) {
	o := newOrphanStats()
	var wg sync.WaitGroup
	for range 1000 {
		wg.Go(func() {
			o.record("id", "type")
		})
	}
	wg.Wait()
	if got := o.Count(); got != 1000 {
		t.Errorf("Count = %d, want 1000", got)
	}
}

func TestOrphanStats_LogRateLimit(t *testing.T) {
	o := newOrphanStats()
	o.logInterval = 100 * time.Millisecond
	var mu sync.Mutex
	logged := 0
	o.logFunc = func(format string, args ...any) {
		mu.Lock()
		logged++
		mu.Unlock()
	}
	// 100 events in ~10ms must produce at most 1 log line.
	for range 100 {
		o.record("id", "type")
	}
	mu.Lock()
	got := logged
	mu.Unlock()
	if got > 1 {
		t.Errorf("logged = %d, want <= 1 in burst", got)
	}
}
