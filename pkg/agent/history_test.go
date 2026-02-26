package agent

import (
	"sync"
	"testing"
	"time"
)

func TestNewHistory_DefaultSize(t *testing.T) {
	h := NewHistory(0)
	if h.maxSize != 100 {
		t.Errorf("NewHistory(0) maxSize = %d, want 100", h.maxSize)
	}
}

func TestNewHistory_NegativeSize(t *testing.T) {
	h := NewHistory(-5)
	if h.maxSize != 100 {
		t.Errorf("NewHistory(-5) maxSize = %d, want 100", h.maxSize)
	}
}

func TestNewHistory_CustomSize(t *testing.T) {
	h := NewHistory(50)
	if h.maxSize != 50 {
		t.Errorf("NewHistory(50) maxSize = %d, want 50", h.maxSize)
	}
}

func TestHistory_AddAndSize(t *testing.T) {
	h := NewHistory(10)

	if h.Size() != 0 {
		t.Errorf("initial size = %d, want 0", h.Size())
	}

	for i := range 5 {
		h.Add(HistoryEntry{Tick: int64(i), Action: "mine"})
	}

	if h.Size() != 5 {
		t.Errorf("after 5 adds, size = %d, want 5", h.Size())
	}
}

func TestHistory_RingBufferWrap(t *testing.T) {
	h := NewHistory(3)

	// Add 5 entries to a buffer of size 3 (should wrap)
	for i := range 5 {
		h.Add(HistoryEntry{Tick: int64(i), Action: "act"})
	}

	if h.Size() != 3 {
		t.Errorf("after wrapping, size = %d, want 3", h.Size())
	}
}

func TestHistory_GetRecent_Empty(t *testing.T) {
	h := NewHistory(10)

	result := h.GetRecent(5)
	if len(result) != 0 {
		t.Errorf("GetRecent on empty = %d entries, want 0", len(result))
	}
}

func TestHistory_GetRecent_FewerThanLimit(t *testing.T) {
	h := NewHistory(10)

	h.Add(HistoryEntry{Tick: 1, Action: "mine"})
	h.Add(HistoryEntry{Tick: 2, Action: "sell"})

	result := h.GetRecent(5)
	if len(result) != 2 {
		t.Fatalf("GetRecent(5) with 2 entries = %d, want 2", len(result))
	}
	// Should be in chronological order
	if result[0].Tick != 1 {
		t.Errorf("result[0].Tick = %d, want 1", result[0].Tick)
	}
	if result[1].Tick != 2 {
		t.Errorf("result[1].Tick = %d, want 2", result[1].Tick)
	}
}

func TestHistory_GetRecent_ExactLimit(t *testing.T) {
	h := NewHistory(10)

	for i := range 10 {
		h.Add(HistoryEntry{Tick: int64(i), Action: "act"})
	}

	result := h.GetRecent(10)
	if len(result) != 10 {
		t.Fatalf("GetRecent(10) = %d, want 10", len(result))
	}
	// First should be tick 0, last should be tick 9
	if result[0].Tick != 0 {
		t.Errorf("result[0].Tick = %d, want 0", result[0].Tick)
	}
	if result[9].Tick != 9 {
		t.Errorf("result[9].Tick = %d, want 9", result[9].Tick)
	}
}

func TestHistory_GetRecent_AfterWrap(t *testing.T) {
	h := NewHistory(3)

	// Add ticks 0,1,2,3,4 (buffer wraps, should contain 2,3,4)
	for i := range 5 {
		h.Add(HistoryEntry{Tick: int64(i), Action: "act"})
	}

	result := h.GetRecent(3)
	if len(result) != 3 {
		t.Fatalf("GetRecent(3) after wrap = %d, want 3", len(result))
	}

	// Should return ticks 2, 3, 4 in chronological order
	if result[0].Tick != 2 {
		t.Errorf("result[0].Tick = %d, want 2", result[0].Tick)
	}
	if result[1].Tick != 3 {
		t.Errorf("result[1].Tick = %d, want 3", result[1].Tick)
	}
	if result[2].Tick != 4 {
		t.Errorf("result[2].Tick = %d, want 4", result[2].Tick)
	}
}

func TestHistory_GetRecent_LimitedAfterWrap(t *testing.T) {
	h := NewHistory(5)

	for i := range 8 {
		h.Add(HistoryEntry{Tick: int64(i), Action: "act"})
	}

	// Request only 2 most recent
	result := h.GetRecent(2)
	if len(result) != 2 {
		t.Fatalf("GetRecent(2) = %d, want 2", len(result))
	}
	if result[0].Tick != 6 {
		t.Errorf("result[0].Tick = %d, want 6", result[0].Tick)
	}
	if result[1].Tick != 7 {
		t.Errorf("result[1].Tick = %d, want 7", result[1].Tick)
	}
}

func TestHistory_GetSince_Empty(t *testing.T) {
	h := NewHistory(10)

	result := h.GetSince(0)
	if len(result) != 0 {
		t.Errorf("GetSince on empty = %d, want 0", len(result))
	}
}

func TestHistory_GetSince_NoWrap(t *testing.T) {
	h := NewHistory(10)

	for i := range 5 {
		h.Add(HistoryEntry{Tick: int64(i * 10)})
	}

	// Get entries since tick 20 (should get ticks 20, 30, 40)
	result := h.GetSince(20)
	if len(result) != 3 {
		t.Fatalf("GetSince(20) = %d entries, want 3", len(result))
	}
	if result[0].Tick != 20 {
		t.Errorf("result[0].Tick = %d, want 20", result[0].Tick)
	}
}

func TestHistory_GetSince_AfterWrap(t *testing.T) {
	h := NewHistory(3)

	// Add ticks 10,20,30,40,50 (wraps, buffer has 30,40,50)
	for i := 1; i <= 5; i++ {
		h.Add(HistoryEntry{Tick: int64(i * 10)})
	}

	result := h.GetSince(40)
	if len(result) != 2 {
		t.Fatalf("GetSince(40) after wrap = %d, want 2", len(result))
	}
	if result[0].Tick != 40 {
		t.Errorf("result[0].Tick = %d, want 40", result[0].Tick)
	}
	if result[1].Tick != 50 {
		t.Errorf("result[1].Tick = %d, want 50", result[1].Tick)
	}
}

func TestHistory_GetSince_NoMatches(t *testing.T) {
	h := NewHistory(10)

	for i := range 5 {
		h.Add(HistoryEntry{Tick: int64(i)})
	}

	result := h.GetSince(100)
	if len(result) != 0 {
		t.Errorf("GetSince(100) = %d, want 0", len(result))
	}
}

func TestHistory_Clear(t *testing.T) {
	h := NewHistory(10)

	for i := range 5 {
		h.Add(HistoryEntry{Tick: int64(i)})
	}

	h.Clear()

	if h.Size() != 0 {
		t.Errorf("after Clear(), size = %d, want 0", h.Size())
	}

	result := h.GetRecent(10)
	if len(result) != 0 {
		t.Errorf("after Clear(), GetRecent = %d entries, want 0", len(result))
	}
}

func TestHistory_ClearThenAdd(t *testing.T) {
	h := NewHistory(5)

	for i := range 5 {
		h.Add(HistoryEntry{Tick: int64(i)})
	}

	h.Clear()

	h.Add(HistoryEntry{Tick: 100, Action: "after_clear"})

	if h.Size() != 1 {
		t.Errorf("after clear+add, size = %d, want 1", h.Size())
	}

	result := h.GetRecent(5)
	if len(result) != 1 {
		t.Fatalf("after clear+add, GetRecent = %d, want 1", len(result))
	}
	if result[0].Tick != 100 {
		t.Errorf("result[0].Tick = %d, want 100", result[0].Tick)
	}
}

func TestHistory_GetRecent_ZeroLimit(t *testing.T) {
	h := NewHistory(10)

	h.Add(HistoryEntry{Tick: 1})

	result := h.GetRecent(0)
	if len(result) != 0 {
		t.Errorf("GetRecent(0) = %d, want 0", len(result))
	}
}

func TestHistory_ConcurrentAddAndRead(t *testing.T) {
	h := NewHistory(100)

	var wg sync.WaitGroup

	// Writers
	for i := range 10 {
		wg.Add(1)
		go func(start int) {
			defer wg.Done()
			for j := range 10 {
				h.Add(HistoryEntry{
					Tick:      int64(start*10 + j),
					Timestamp: time.Now(),
					Action:    "test",
				})
			}
		}(i)
	}

	// Readers
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 10 {
				_ = h.GetRecent(5)
				_ = h.GetSince(0)
				_ = h.Size()
			}
		}()
	}

	wg.Wait()

	if h.Size() != 100 {
		t.Errorf("after concurrent writes, size = %d, want 100", h.Size())
	}
}

func TestHistory_SingleEntry(t *testing.T) {
	h := NewHistory(1)

	h.Add(HistoryEntry{Tick: 1, Action: "first"})
	if h.Size() != 1 {
		t.Errorf("size = %d, want 1", h.Size())
	}

	// Add another - should overwrite
	h.Add(HistoryEntry{Tick: 2, Action: "second"})
	if h.Size() != 1 {
		t.Errorf("size = %d, want 1 (ring buffer of 1)", h.Size())
	}

	result := h.GetRecent(1)
	if len(result) != 1 {
		t.Fatalf("GetRecent(1) = %d, want 1", len(result))
	}
	if result[0].Tick != 2 {
		t.Errorf("result[0].Tick = %d, want 2", result[0].Tick)
	}
}
