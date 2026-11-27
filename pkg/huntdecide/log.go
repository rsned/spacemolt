package huntdecide

import (
	"context"
	_ "embed" // for the record schema
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// DecisionsDir is the per-agent subdirectory holding the JSON-lines logs.
const DecisionsDir = "hunt-decisions"

//go:embed record.schema.json
var recordSchema []byte

// DecisionRecord is the log line for one decision and the choice made.
type DecisionRecord struct {
	Type string    `json:"type"` // "decision"
	At   time.Time `json:"at"`
	Decision
	Choice
}

// OutcomeRecord is the log line for an observed result.
type OutcomeRecord struct {
	Type     string         `json:"type"` // "outcome"
	At       time.Time      `json:"at"`
	SortieID string         `json:"sortie_id"`
	Seq      int            `json:"seq"`   // decision it answers; 0 = none
	Phase    string         `json:"phase"` // "provision" or "target"
	Data     map[string]any `json:"data"`
}

// SortieRecord is the log line for the start or end of a sortie.
type SortieRecord struct {
	Type     string    `json:"type"` // "sortie_start" or "sortie_end"
	At       time.Time `json:"at"`
	SortieID string    `json:"sortie_id"`
	State    State     `json:"state"`
	Result   string    `json:"result,omitempty"` // sortie_end only
}

// Log appends JSON-lines records to dated files under an agent directory.
type Log struct {
	agentDir string
	mu       sync.Mutex
}

// NewLog returns a Log writing under agentDir/hunt-decisions.
func NewLog(agentDir string) *Log {
	return &Log{agentDir: agentDir}
}

// Append writes rec as one JSON line to the file named for the UTC date of
// now. It is safe for concurrent use and a no-op on a nil Log.
func (l *Log) Append(now time.Time, rec any) error {
	if l == nil {
		return nil
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	b = append(b, '\n')

	l.mu.Lock()
	defer l.mu.Unlock()
	dir := filepath.Join(l.agentDir, DecisionsDir)
	if err = os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	name := filepath.Join(dir, now.UTC().Format("2006-01-02")+".jsonl")
	f, err := os.OpenFile(name, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err = f.Write(b); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// Recorder wraps a Decider and logs every decision with the choice made.
type Recorder struct {
	Inner Decider
	Log   *Log
	Now   func() time.Time // nil -> time.Now
}

// Decide asks the inner decider, logs the decision and returns the inner
// choice unchanged.
func (r Recorder) Decide(ctx context.Context, d Decision) Choice {
	c := r.Inner.Decide(ctx, d)
	now := time.Now
	if r.Now != nil {
		now = r.Now
	}
	at := now()
	// A failed log write is dropped on purpose: losing a record must never
	// change the choice or stop the hunt.
	_ = r.Log.Append(at, DecisionRecord{Type: "decision", At: at, Decision: d, Choice: c})
	return c
}
