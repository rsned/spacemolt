package creditreq

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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
	// EventReleased: a "paid … already" hold dropped once its 24 h window
	// passed, so a still-broke agent can file a fresh request.
	EventReleased = "released"
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
	r := bufio.NewReaderSize(f, 64*1024)
	for {
		line, err := readLine(r)
		if len(line) > 0 {
			var e Entry
			if json.Unmarshal(line, &e) == nil && !e.At.Before(t) {
				out = append(out, e)
			}
		}
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return out, err
		}
	}
}

// maxHistoryLine bounds one history line; longer lines are skipped whole
// (returned empty) so one corrupt line cannot block every later read.
const maxHistoryLine = 1 << 20

// readLine returns the next line without its newline, or nil for a line over
// maxHistoryLine. io.EOF is returned with any final unterminated line.
func readLine(r *bufio.Reader) ([]byte, error) {
	var line []byte
	tooLong := false
	for {
		chunk, err := r.ReadSlice('\n')
		if !tooLong {
			line = append(line, chunk...)
			if len(line) > maxHistoryLine {
				tooLong = true
				line = nil
			}
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if tooLong {
			return nil, err
		}
		return bytes.TrimRight(line, "\r\n"), err
	}
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
