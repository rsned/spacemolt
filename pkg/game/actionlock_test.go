package game

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestActionLock_DistinctActionsDoNotBlock(t *testing.T) {
	m := newActionLockMap()
	ctx := context.Background()

	rel1, err := m.lock(ctx, "mine")
	if err != nil {
		t.Fatalf("lock mine: %v", err)
	}
	rel2, err := m.lock(ctx, "travel")
	if err != nil {
		t.Fatalf("lock travel must not block: %v", err)
	}
	rel1()
	rel2()
}

func TestActionLock_SameActionSerializes(t *testing.T) {
	m := newActionLockMap()

	rel, err := m.lock(context.Background(), "mine")
	if err != nil {
		t.Fatalf("lock 1: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := m.lock(ctx, "mine"); err == nil {
		t.Fatal("expected second lock(mine) to block until ctx expires")
	}
	rel()
}

func TestActionLock_FIFOConcurrent(t *testing.T) {
	m := newActionLockMap()

	var counter atomic.Int32
	var wg sync.WaitGroup
	const N = 100
	for range N {
		wg.Go(func() {
			rel, err := m.lock(context.Background(), "mine")
			if err != nil {
				t.Errorf("lock: %v", err)
				return
			}
			defer rel()
			// Critical section: counter must be incremented atomically
			// with no parallel entrants.
			before := counter.Load()
			time.Sleep(time.Microsecond)
			after := counter.Load()
			if before != after {
				t.Errorf("interleaving detected: before=%d after=%d", before, after)
			}
			counter.Add(1)
		})
	}
	wg.Wait()
	if got := counter.Load(); got != N {
		t.Errorf("counter = %d, want %d", got, N)
	}
}

func TestActionLock_LazyCreationConcurrent(t *testing.T) {
	m := newActionLockMap()

	var wg sync.WaitGroup
	for i := range 50 {
		i := i
		wg.Go(func() {
			action := fmt.Sprintf("action-%d", i%26)
			rel, err := m.lock(context.Background(), action)
			if err != nil {
				t.Errorf("lock: %v", err)
				return
			}
			rel()
		})
	}
	wg.Wait()
}
