// Package calllog provides structured JSON logging for game API request/response pairs.
// Log files are automatically rotated at midnight in the PST8PDT timezone.
// Entries are split into two files: actions (mutations) and queries.
package calllog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Entry represents a single log record written as one line of JSON.
type Entry struct {
	State    StateSnapshot   `json:"state"`
	Request  json.RawMessage `json:"request"`
	Response json.RawMessage `json:"response"`
}

// StateSnapshot captures the agent's context at the time of a log entry.
type StateSnapshot struct {
	Timestamp string       `json:"timestamp"`
	Location  LocationInfo `json:"location"`
	Ship      ShipInfo     `json:"ship"`
}

// LocationInfo captures where the agent is.
type LocationInfo struct {
	System    string `json:"system"`
	POI       string `json:"poi"`
	Docked    bool   `json:"docked"`
	Traveling bool   `json:"traveling,omitempty"`
}

// ShipInfo captures the agent's ship state.
type ShipInfo struct {
	Name          string   `json:"name"`
	ClassID       string   `json:"class_id"`
	Hull          float64  `json:"hull"`
	MaxHull       float64  `json:"max_hull"`
	Shield        float64  `json:"shield"`
	MaxShield     float64  `json:"max_shield"`
	Fuel          float64  `json:"fuel"`
	MaxFuel       float64  `json:"max_fuel"`
	CargoUsed     float64  `json:"cargo_used"`
	CargoCapacity float64  `json:"cargo_capacity"`
	Modules       []string `json:"modules"`
}

// Logger writes JSON log entries for API request/response pairs, splitting
// mutations into an "actions" log and everything else into a "queries" log.
// Both files rotate at midnight in the PST8PDT timezone.
type Logger struct {
	actions  logFile
	queries  logFile
	loc      *time.Location
	nowFunc  func() time.Time
	mutations map[string]bool
}

// New creates a Logger that writes to dir with the given filename prefix.
// mutations is the set of message types that are actions/mutations (logged to the
// actions file); all other types go to the queries file.
// Files are named {prefix}.actions.YYYYMMDD.log and {prefix}.queries.YYYYMMDD.log.
func New(dir, prefix string, mutations map[string]bool) (*Logger, error) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		return nil, fmt.Errorf("load PST8PDT timezone: %w", err)
	}
	return &Logger{
		actions:   logFile{dir: dir, prefix: prefix + ".actions"},
		queries:   logFile{dir: dir, prefix: prefix + ".queries"},
		loc:       loc,
		nowFunc:   time.Now,
		mutations: mutations,
	}, nil
}

// Log writes a request/response pair to the appropriate log file (actions or queries)
// based on whether msgType is a mutation. If the PST8PDT date has changed since the
// last write to that file, the old file is flushed and closed, and a new one is created.
func (l *Logger) Log(msgType string, snap StateSnapshot, request, response json.RawMessage) error {
	now := l.nowFunc()
	pstNow := now.In(l.loc)
	today := pstNow.Format("20060102")

	snap.Timestamp = now.Format(time.RFC3339)
	entry := Entry{
		State:    snap,
		Request:  request,
		Response: response,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal log entry: %w", err)
	}
	data = append(data, '\n')

	if l.mutations[msgType] {
		return l.actions.write(data, today)
	}
	return l.queries.write(data, today)
}

// Close flushes and closes both log files.
func (l *Logger) Close() error {
	errA := l.actions.close()
	errQ := l.queries.close()
	if errA != nil {
		return errA
	}
	return errQ
}

// logFile manages a single rotating log file.
type logFile struct {
	dir        string
	prefix     string
	mu         sync.Mutex
	file       *os.File
	currentDay string
}

func (lf *logFile) write(data []byte, today string) error {
	lf.mu.Lock()
	defer lf.mu.Unlock()

	if lf.currentDay != today {
		if err := lf.rotateLocked(today); err != nil {
			return err
		}
	}
	_, err := lf.file.Write(data)
	return err
}

func (lf *logFile) close() error {
	lf.mu.Lock()
	defer lf.mu.Unlock()
	if lf.file != nil {
		if err := lf.file.Sync(); err != nil {
			_ = lf.file.Close()
			lf.file = nil
			lf.currentDay = ""
			return fmt.Errorf("sync log file: %w", err)
		}
		err := lf.file.Close()
		lf.file = nil
		lf.currentDay = ""
		return err
	}
	return nil
}

func (lf *logFile) rotateLocked(newDay string) error {
	if lf.file != nil {
		if err := lf.file.Sync(); err != nil {
			_ = lf.file.Close()
			lf.file = nil
			lf.currentDay = ""
			return fmt.Errorf("sync previous log: %w", err)
		}
		if err := lf.file.Close(); err != nil {
			lf.file = nil
			lf.currentDay = ""
			return fmt.Errorf("close previous log: %w", err)
		}
		lf.file = nil
	}

	if err := os.MkdirAll(lf.dir, 0755); err != nil {
		return fmt.Errorf("create log directory: %w", err)
	}

	filename := fmt.Sprintf("%s.%s.log", lf.prefix, newDay)
	path := filepath.Join(lf.dir, filename)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}

	lf.file = f
	lf.currentDay = newDay
	return nil
}
