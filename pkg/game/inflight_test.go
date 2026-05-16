package game

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestInflight_AcquireRelease(t *testing.T) {
	s := newInflight(2)
	ctx := context.Background()
	if err := s.Acquire(ctx); err != nil {
		t.Fatalf("Acquire 1: %v", err)
	}
	if err := s.Acquire(ctx); err != nil {
		t.Fatalf("Acquire 2: %v", err)
	}
	s.Release()
	if err := s.Acquire(ctx); err != nil {
		t.Fatalf("Acquire after Release: %v", err)
	}
}

func TestInflight_BlocksAtCap(t *testing.T) {
	s := newInflight(1)
	ctx := context.Background()
	if err := s.Acquire(ctx); err != nil {
		t.Fatalf("Acquire 1: %v", err)
	}

	ctx2, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := s.Acquire(ctx2); err == nil {
		t.Fatal("expected Acquire to fail (ctx expired)")
	}
}

func TestInflight_CapInvariant(t *testing.T) {
	const capN = 16
	s := newInflight(capN)

	var current atomic.Int32
	var peak atomic.Int32
	var wg sync.WaitGroup
	for range 200 {
		wg.Go(func() {
			if err := s.Acquire(context.Background()); err != nil {
				t.Errorf("Acquire: %v", err)
				return
			}
			defer s.Release()
			n := current.Add(1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			time.Sleep(time.Millisecond)
			current.Add(-1)
		})
	}
	wg.Wait()
	if got := peak.Load(); got > capN {
		t.Errorf("peak concurrent = %d, want <= %d", got, capN)
	}
}

func TestInflight_NoLeakOnCancel(t *testing.T) {
	s := newInflight(2)
	for range 1000 {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // pre-cancel
		_ = s.Acquire(ctx)
	}
	// All cancelled acquires should have returned an error without
	// consuming a slot. Cap should still be 2 free.
	ctx := context.Background()
	if err := s.Acquire(ctx); err != nil {
		t.Errorf("Acquire after cancel storm: %v", err)
	}
	if err := s.Acquire(ctx); err != nil {
		t.Errorf("Acquire 2 after cancel storm: %v", err)
	}
}
