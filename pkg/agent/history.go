package agent

import (
	"sync"
	"time"
)

// HistoryEntry represents a single action in the agent's history
type HistoryEntry struct {
	Tick       int64     `json:"tick"`
	Timestamp  time.Time `json:"timestamp"`
	Action     string    `json:"action"`
	Target     string    `json:"target,omitempty"`
	Confidence float64   `json:"confidence"`
	Result     string    `json:"result"`
	Error      string    `json:"error,omitempty"`
	Reasoning  string    `json:"reasoning,omitempty"`
}

// History maintains a ring buffer of action history entries
type History struct {
	mu      sync.RWMutex
	entries []HistoryEntry
	maxSize int
	index   int // Current write position in ring buffer
	full    bool // Whether buffer has wrapped around
}

// NewHistory creates a new history tracker with specified max size
func NewHistory(maxSize int) *History {
	if maxSize <= 0 {
		maxSize = 100
	}
	return &History{
		entries: make([]HistoryEntry, maxSize),
		maxSize: maxSize,
		index:   0,
		full:    false,
	}
}

// Add appends a new entry to the history (ring buffer)
func (h *History) Add(entry HistoryEntry) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.entries[h.index] = entry
	h.index++

	if h.index >= h.maxSize {
		h.index = 0
		h.full = true
	}
}

// GetRecent returns the most recent N entries (up to limit)
func (h *History) GetRecent(limit int) []HistoryEntry {
	h.mu.RLock()
	defer h.mu.RUnlock()

	// Calculate actual size
	size := h.index
	if h.full {
		size = h.maxSize
	}

	if size == 0 {
		return []HistoryEntry{}
	}

	// Cap limit to actual size
	if limit > size {
		limit = size
	}

	// Create result slice
	result := make([]HistoryEntry, 0, limit)

	if h.full {
		// Buffer has wrapped - read backwards from current position
		pos := h.index - 1
		if pos < 0 {
			pos = h.maxSize - 1
		}

		for i := 0; i < limit; i++ {
			result = append(result, h.entries[pos])
			pos--
			if pos < 0 {
				pos = h.maxSize - 1
			}
		}
	} else {
		// Buffer hasn't wrapped - read backwards from current position
		pos := h.index - 1
		for i := 0; i < limit && pos >= 0; i++ {
			result = append(result, h.entries[pos])
			pos--
		}
	}

	// Reverse to get chronological order (oldest first)
	for i := 0; i < len(result)/2; i++ {
		j := len(result) - 1 - i
		result[i], result[j] = result[j], result[i]
	}

	return result
}

// GetSince returns all entries since a specific tick
func (h *History) GetSince(tick int64) []HistoryEntry {
	h.mu.RLock()
	defer h.mu.RUnlock()

	// Calculate actual size
	size := h.index
	if h.full {
		size = h.maxSize
	}

	if size == 0 {
		return []HistoryEntry{}
	}

	result := make([]HistoryEntry, 0)

	// Iterate through all entries
	if h.full {
		// Start from oldest entry (current write position)
		for i := 0; i < h.maxSize; i++ {
			pos := (h.index + i) % h.maxSize
			if h.entries[pos].Tick >= tick {
				result = append(result, h.entries[pos])
			}
		}
	} else {
		// Just iterate from start to current position
		for i := 0; i < h.index; i++ {
			if h.entries[i].Tick >= tick {
				result = append(result, h.entries[i])
			}
		}
	}

	return result
}

// Size returns the current number of entries in history
func (h *History) Size() int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.full {
		return h.maxSize
	}
	return h.index
}

// Clear removes all entries from history
func (h *History) Clear() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.index = 0
	h.full = false
	h.entries = make([]HistoryEntry, h.maxSize)
}
