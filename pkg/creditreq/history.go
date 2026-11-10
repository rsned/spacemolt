package creditreq

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Event names for Entry.Event.
const (
	EventFiled      = "filed"
	EventPaid       = "paid"
	EventHeld       = "held"
	EventSendFailed = "send_failed"
)

// Entry is one line of the append-only payment history.
type Entry struct {
	At       time.Time `json:"at"`
	AgentID  string    `json:"agent_id"`
	Username string    `json:"username"`
	Event    string    `json:"event"` // filed | paid | held | send_failed
	Amount   int64     `json:"amount,omitempty"`
	Detail   string    `json:"detail,omitempty"`
}

// History is a handle on the JSONL history file.
type History struct {
	path string
}

// NewHistory returns a history handle on path. The file need not exist yet.
func NewHistory(path string) *History { return &History{path: path} }

// Append writes e as one line. Each entry is a single O_APPEND write, which
// is atomic for lines this small, so no lock is needed.
func (h *History) Append(e Entry) error {
	if err := os.MkdirAll(filepath.Dir(h.path), 0o755); err != nil {
		return fmt.Errorf("creditreq history: mkdir: %w", err)
	}
	line, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("creditreq history: marshal: %w", err)
	}
	f, err := os.OpenFile(h.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("creditreq history: open: %w", err)
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		_ = f.Close()
		return fmt.Errorf("creditreq history: write: %w", err)
	}
	return f.Close()
}

// Since returns entries at or after t, skipping malformed lines. A missing
// file is empty history.
func (h *History) Since(t time.Time) ([]Entry, error) {
	f, err := os.Open(h.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("creditreq history: open: %w", err)
	}
	defer f.Close() //nolint:errcheck
	var out []Entry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var e Entry
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue
		}
		if !e.At.Before(t) {
			out = append(out, e)
		}
	}
	return out, sc.Err()
}

// PaidSince counts the agent's paid entries at or after t.
func PaidSince(entries []Entry, agentID string, t time.Time) int {
	n := 0
	for _, e := range entries {
		if e.Event == EventPaid && e.AgentID == agentID && !e.At.Before(t) {
			n++
		}
	}
	return n
}

// TotalPaidSince sums all paid amounts at or after t.
func TotalPaidSince(entries []Entry, t time.Time) int64 {
	var sum int64
	for _, e := range entries {
		if e.Event == EventPaid && !e.At.Before(t) {
			sum += e.Amount
		}
	}
	return sum
}
