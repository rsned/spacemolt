package game

import (
	"log"
	"sync"
	"sync/atomic"
	"time"
)

// orphanStats tracks responses that arrived tagged with a request_id
// the router did not recognize. See spec: "Orphan handling".
type orphanStats struct {
	count      atomic.Int64
	mu         sync.Mutex
	lastID     string
	lastType   string
	lastSeen   time.Time
	lastLogged time.Time

	// logInterval is the minimum gap between WARN log lines. Defaults
	// to 1s; tests override.
	logInterval time.Duration
	// logFunc is overridable for tests. Defaults to log.Printf.
	logFunc func(string, ...any)
}

func newOrphanStats() *orphanStats {
	return &orphanStats{
		logInterval: time.Second,
		logFunc:     log.Printf,
	}
}

// record bumps the counter, stores the most-recent ID/type, and emits
// a rate-limited WARN line.
func (o *orphanStats) record(requestID, respType string) {
	o.count.Add(1)
	o.mu.Lock()
	o.lastID = requestID
	o.lastType = respType
	o.lastSeen = time.Now()
	shouldLog := o.lastLogged.IsZero() || time.Since(o.lastLogged) >= o.logInterval
	if shouldLog {
		o.lastLogged = time.Now()
	}
	o.mu.Unlock()

	if shouldLog {
		o.logFunc("WARN: orphan response request_id=%s type=%s", requestID, respType)
	}
}

// Count returns the cumulative orphan count.
func (o *orphanStats) Count() int64 { return o.count.Load() }

// Last returns the most-recent orphan's ID, type, and timestamp. Used by
// Client.DiagnosticStats.
func (o *orphanStats) Last() (id, respType string, at time.Time) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.lastID, o.lastType, o.lastSeen
}
