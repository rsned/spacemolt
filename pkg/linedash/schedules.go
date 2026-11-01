package linedash

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Task is one scheduled command from data/agents/<id>/schedule.json.
type Task struct {
	ID        int    `json:"id"`
	Frequency string `json:"frequency"`
	Command   string `json:"command"`
}

// LoadSchedules reads every agent's schedule.json under agentsDir. A missing
// dir is an empty map. A non-nil error with a non-nil map means "partial": the
// named files were skipped and every other agent is present.
func LoadSchedules(agentsDir string) (map[string][]Task, error) {
	out := map[string][]Task{}
	entries, err := os.ReadDir(agentsDir)
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return out, fmt.Errorf("read agents dir: %w", err)
	}
	var bad []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(agentsDir, e.Name(), "schedule.json")) // #nosec G304 -- agent dirs we enumerate
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			bad = append(bad, e.Name()+": "+err.Error())
			continue
		}
		tasks, err := decodeSchedule(raw)
		if err != nil {
			bad = append(bad, e.Name()+": "+err.Error())
			continue
		}
		out[e.Name()] = tasks
	}
	if len(bad) > 0 {
		sort.Strings(bad)
		return out, fmt.Errorf("schedules skipped: %s", strings.Join(bad, "; "))
	}

	return out, nil
}

// decodeSchedule accepts the current bare-array shape and the older
// {"tasks": [...]} wrapper.
func decodeSchedule(raw []byte) ([]Task, error) {
	var tasks []Task
	if err := json.Unmarshal(raw, &tasks); err == nil {
		return tasks, nil
	}
	var wrapped struct {
		Tasks []Task `json:"tasks"`
	}
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		return nil, err
	}

	return wrapped.Tasks, nil
}

// TaskSpec is a parsed line-relevant scheduled command.
type TaskSpec struct {
	Verb, Recipe, Item, From, To, Recipient string
	Qty                                     int
	MaxPrice                                float64
}

// ParseTask parses the four verbs that make line structure:
//
//	craft <recipe> <qty> [preset]
//	deliver <item> <qty> <from> <to> <recipient>
//	faction_deposit <item>
//	buy_directed <item> <qty> <station> <max_price> <recipient>
//
// Any other command, or one with missing or malformed arguments, is not ok.
func ParseTask(cmd string) (TaskSpec, bool) {
	f := strings.Fields(cmd)
	if len(f) == 0 {
		return TaskSpec{}, false
	}
	s := TaskSpec{Verb: f[0]}
	switch f[0] {
	case "craft":
		if len(f) < 3 {
			return TaskSpec{}, false
		}
		s.Recipe = f[1]
		return s, atoiTo(f[2], &s.Qty)
	case "deliver":
		if len(f) < 6 {
			return TaskSpec{}, false
		}
		s.Item, s.From, s.To, s.Recipient = f[1], f[3], f[4], f[5]
		return s, atoiTo(f[2], &s.Qty)
	case "faction_deposit":
		if len(f) < 2 {
			return TaskSpec{}, false
		}
		s.Item = f[1]
		return s, true
	case "buy_directed":
		if len(f) < 6 {
			return TaskSpec{}, false
		}
		s.Item, s.From, s.Recipient = f[1], f[3], f[5]
		p, err := strconv.ParseFloat(f[4], 64)
		if err != nil {
			return TaskSpec{}, false
		}
		s.MaxPrice = p
		return s, atoiTo(f[2], &s.Qty)
	}

	return TaskSpec{}, false
}

func atoiTo(s string, dst *int) bool {
	n, err := strconv.Atoi(s)
	if err != nil {
		return false
	}
	*dst = n

	return true
}
