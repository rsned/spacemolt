// Package calllog provides structured JSON logging for game API request/response pairs.
// Log files are automatically rotated at midnight in the PST8PDT timezone.
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
	Timestamp string          `json:"timestamp"`
	Request   json.RawMessage `json:"request"`
	Response  json.RawMessage `json:"response"`
}

// Logger writes JSON log entries for API request/response pairs.
// It automatically rotates log files at midnight in the PST8PDT timezone.
type Logger struct {
	dir     string
	prefix  string
	loc     *time.Location
	nowFunc func() time.Time

	mu         sync.Mutex
	file       *os.File
	currentDay string // YYYYMMDD in PST8PDT
}

// New creates a Logger that writes to dir with the given filename prefix.
// Files are named {prefix}.YYYYMMDD.log and rotated at midnight PST8PDT.
func New(dir, prefix string) (*Logger, error) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		return nil, fmt.Errorf("load PST8PDT timezone: %w", err)
	}
	return &Logger{
		dir:     dir,
		prefix:  prefix,
		loc:     loc,
		nowFunc: time.Now,
	}, nil
}

// Log writes a request/response pair as a single JSON line to the current log file.
// If the PST8PDT date has changed since the last write, the old file is flushed
// and closed, and a new file is created for the new date.
func (l *Logger) Log(request, response json.RawMessage) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.nowFunc()
	pstNow := now.In(l.loc)
	today := pstNow.Format("20060102")

	if l.currentDay != today {
		if err := l.rotateLocked(today); err != nil {
			return err
		}
	}

	entry := Entry{
		Timestamp: now.Format(time.RFC3339),
		Request:   request,
		Response:  response,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal log entry: %w", err)
	}

	data = append(data, '\n')
	_, err = l.file.Write(data)
	return err
}

// Close flushes and closes the current log file.
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.closeLocked()
}

// CurrentFile returns the path to the currently open log file, or empty string if none.
func (l *Logger) CurrentFile() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return ""
	}
	return l.file.Name()
}

func (l *Logger) closeLocked() error {
	if l.file != nil {
		if err := l.file.Sync(); err != nil {
			_ = l.file.Close()
			l.file = nil
			l.currentDay = ""
			return fmt.Errorf("sync log file: %w", err)
		}
		err := l.file.Close()
		l.file = nil
		l.currentDay = ""
		return err
	}
	return nil
}

func (l *Logger) rotateLocked(newDay string) error {
	if err := l.closeLocked(); err != nil {
		return fmt.Errorf("close previous log: %w", err)
	}

	if err := os.MkdirAll(l.dir, 0755); err != nil {
		return fmt.Errorf("create log directory: %w", err)
	}

	filename := fmt.Sprintf("%s.%s.log", l.prefix, newDay)
	path := filepath.Join(l.dir, filename)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}

	l.file = f
	l.currentDay = newDay
	return nil
}
