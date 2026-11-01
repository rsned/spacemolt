package linedash

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"
	"sort"

	"gopkg.in/yaml.v3"
)

// Overrides is data/overmind/line-overrides.yaml: the dashboard's thresholds
// and the only hand-kept line data. An override renames, recategorises or
// hides a derived line; it never adds or removes structure.
type Overrides struct {
	Thresholds Thresholds              `yaml:"thresholds"`
	Lines      map[string]LineOverride `yaml:"lines"`
}

// LineOverride adjusts one derived line, keyed by its id.
type LineOverride struct {
	Name     string `yaml:"name,omitempty"`
	Category string `yaml:"category,omitempty"`
	Hidden   bool   `yaml:"hidden,omitempty"`
}

// LoadOverrides reads the overrides file. A missing file is all defaults; a
// misspelt key is an error, so a typo cannot silently do nothing.
func LoadOverrides(path string) (Overrides, error) {
	o := Overrides{Thresholds: DefaultThresholds()}
	data, err := os.ReadFile(path) // #nosec G304 -- operator-supplied path
	if errors.Is(err, fs.ErrNotExist) {
		return o, nil
	}
	if err != nil {
		return o, fmt.Errorf("read line overrides: %w", err)
	}
	var raw Overrides
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&raw); err != nil && !errors.Is(err, io.EOF) {
		return o, fmt.Errorf("parse line overrides %s: %w", path, err)
	}
	raw.Thresholds = raw.Thresholds.withDefaults()

	return raw, nil
}

// ApplyOverrides renames, recategorises and hides derived lines. An override
// naming a line that no longer exists comes back as a warning for the board.
func ApplyOverrides(lines []Line, o Overrides) ([]Line, []string) {
	seen := map[string]bool{}
	out := make([]Line, 0, len(lines))
	for _, l := range lines {
		ov, ok := o.Lines[l.ID]
		seen[l.ID] = true
		if !ok {
			out = append(out, l)
			continue
		}
		if ov.Hidden {
			continue
		}
		if ov.Name != "" {
			l.Name = ov.Name
		}
		if ov.Category != "" {
			l.Category = ov.Category
		}
		out = append(out, l)
	}
	var warns []string
	for _, id := range sortedMapKeys(o.Lines) {
		if !seen[id] {
			warns = append(warns, "override names unknown line "+id)
		}
	}

	return out, warns
}

// hourlyOrFiner are the schedule frequencies that keep a line's sources inside
// the 2h staleness limit.
var hourlyOrFiner = []string{"ten_minutely", "quarter_hourly", "half_hourly", "hourly"}

// cadenceCaptures must run hourly or finer on every line agent, or its nodes
// grey out.
var cadenceCaptures = []string{"capture_action_log", "capture_storage"}

// CadenceWarnings names each line agent whose schedule lacks an hourly-or-
// finer capture the dashboard depends on. An agent with no schedule on record
// is not ours to judge and is skipped.
func CadenceWarnings(lines []Line, sched map[string][]Task) []string {
	var agents []string
	for _, l := range lines {
		agents = append(agents, AgentsOf(l)...)
	}
	slices.Sort(agents)
	agents = slices.Compact(agents)
	var out []string
	for _, a := range agents {
		tasks, ok := sched[a]
		if !ok {
			continue
		}
		for _, c := range cadenceCaptures {
			if !slices.ContainsFunc(tasks, func(t Task) bool {
				return t.Command == c && slices.Contains(hourlyOrFiner, t.Frequency)
			}) {
				out = append(out, fmt.Sprintf("%s: needs hourly %s", a, c))
			}
		}
	}
	sort.Strings(out)

	return out
}
