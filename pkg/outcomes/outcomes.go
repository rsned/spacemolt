// Package outcomes records the latest outcome of each piece of work a worker
// does -- a scheduled task, an idle-script line, a fleet behaviour -- in the
// agent's own directory, so the line dashboard can name the rule or condition
// that broke a line instead of only its symptom.
//
// It is a leaf package on purpose: the worker writes the file and the
// dashboard reads it, and neither should import the other.
package outcomes

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// FileName is the outcome file in data/agents/<agent-id>/.
const FileName = "task-outcomes.json"

// Outcome statuses.
const (
	StatusOK     = "ok"
	StatusFailed = "failed"
)

// reasonMax caps a stored reason. Server messages run long; the dashboard
// shows one line.
const reasonMax = 300

// Outcome is the latest result of one keyed piece of work.
//
// Keys: "task:<schedule id>", "idle:<command line>", "behaviour:<name>"
// (consign, consign_offer, faction_pull, shopkeep).
type Outcome struct {
	Key     string    `json:"key"`
	Command string    `json:"command"`
	Status  string    `json:"status"`
	Reason  string    `json:"reason,omitempty"`
	At      time.Time `json:"at"`
}

// Log writes one agent's outcome file.
type Log struct {
	dir string
	mu  sync.Mutex
}

// New returns a Log writing to agentDir/FileName.
func New(agentDir string) *Log {
	return &Log{dir: agentDir}
}

// Record stores the outcome of key, replacing any earlier one. A nil err is
// an ok outcome. A failed write is dropped: a lost record costs one stale why
// on the dashboard, and must never stop the worker that did the work.
func (l *Log) Record(key, command string, err error, at time.Time) {
	if l == nil {
		return
	}
	o := Outcome{Key: key, Command: command, Status: StatusOK, At: at.UTC()}
	if err != nil {
		o.Status, o.Reason = StatusFailed, Reason(err)
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	m, lerr := Load(l.dir)
	if lerr != nil {
		m = map[string]Outcome{} // a corrupt file is replaced, not appended to
	}
	m[key] = o
	raw, merr := json.MarshalIndent(m, "", "  ")
	if merr != nil {
		return
	}
	path := filepath.Join(l.dir, FileName)
	tmp := path + ".tmp"
	if os.WriteFile(tmp, raw, 0o644) != nil { //nolint:gosec // operator-readable by design
		return
	}
	if os.Rename(tmp, path) != nil {
		_ = os.Remove(tmp)
	}
}

// Load reads agentDir's outcome file. A missing file is an empty map.
func Load(agentDir string) (map[string]Outcome, error) {
	raw, err := os.ReadFile(filepath.Join(agentDir, FileName)) // #nosec G304 -- the agent's own directory
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]Outcome{}, nil
	}
	if err != nil {
		return nil, err
	}
	m := map[string]Outcome{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}

	return m, nil
}

// Reason renders err on one line, capped at reasonMax runes.
func Reason(err error) string {
	if err == nil {
		return ""
	}
	s := strings.ReplaceAll(err.Error(), "\n", "; ")
	if r := []rune(s); len(r) > reasonMax {
		s = string(r[:reasonMax])
	}

	return s
}
