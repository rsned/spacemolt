// Command unlock-progress reports how far each agent in the pirate-unlock pool
// has walked the smuggling chain.
//
// It exists because the obvious signal is too slow to steer by: the pirate
// baseline in data/assets.db comes from the daily capture_faction schedule, so
// an agent that banks the unlock at 14:05 still reads as hostile until the next
// midnight. mission_results in market.db is written as each mission finishes, so
// progress shows up within minutes and partway agents are visible at all —
// "cleared two of three rungs" has no representation in a baseline number.
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	_ "modernc.org/sqlite"
	"gopkg.in/yaml.v3"
)

func main() {
	marketDB := flag.String("market-db", "data/market.db", "path to market.db (mission_results)")
	assetsDB := flag.String("assets-db", "data/assets.db", "path to assets.db (pirate baseline, daily)")
	fleet := flag.String("fleet", "data/overmind/unlock-fleet.yaml", "fleet file naming the pool")
	all := flag.Bool("all", false, "include agents that have not started the chain")
	flag.Parse()

	pool, err := readPool(*fleet)
	if err != nil {
		fmt.Fprintln(os.Stderr, "unlock-progress:", err)
		os.Exit(1)
	}
	comps, err := readCompletions(*marketDB)
	if err != nil {
		fmt.Fprintln(os.Stderr, "unlock-progress:", err)
		os.Exit(1)
	}
	// Baseline is advisory: it confirms an unlock the chain already implies, and
	// its staleness is the reason this tool exists, so a missing or unreadable
	// assets.db must not stop the report.
	baseline, baseAt := readBaselines(*assetsDB)

	rows := Compute(pool, comps)

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "AGENT\tPROGRESS\tCLEARED\tWHEN\tNEXT\tBASELINE") //nolint:errcheck
	var unlocked, started, idle int
	for _, p := range rows {
		if p.Unlocked {
			unlocked++
		} else if p.Step > 0 {
			started++
		} else {
			idle++
			if !*all {
				continue
			}
		}
		bar := strings.Repeat("#", p.Step) + strings.Repeat(".", len(Chain)-p.Step)
		when, last, next := "-", "-", p.Next
		if p.Step > 0 {
			when = p.LastAt.UTC().Format("01-02 15:04")
			last = p.Last
		}
		if p.Unlocked {
			next = "UNLOCKED"
		}
		b := "-"
		if v, ok := baseline[p.AgentID]; ok {
			b = fmt.Sprintf("%d", v)
			if v < 10 && p.Unlocked {
				b += " (stale)"
			}
		}
		fmt.Fprintf(w, "%s\t%s %d/%d\t%s\t%s\t%s\t%s\n", //nolint:errcheck
			p.AgentID, bar, p.Step, len(Chain), last, when, next, b)
	}
	_ = w.Flush()

	fmt.Printf("\n%d unlocked, %d part-way, %d not started (%d in pool)\n",
		unlocked, started, idle, len(rows))
	if !baseAt.IsZero() {
		fmt.Printf("baseline column captured %s (daily) — the chain columns are live\n",
			baseAt.UTC().Format("2006-01-02 15:04Z"))
	}
	if !*all && idle > 0 {
		fmt.Printf("%d not-started agents hidden; pass -all to list them\n", idle)
	}
}

// readPool returns the agent ids named in a fleet file, minus any the overrides
// sidecar has removed — a seconded agent is running someone else's job and is
// not making chain progress, so listing it as a stalled pool member is a lie.
func readPool(path string) ([]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read fleet: %w", err)
	}
	var f struct {
		Workers []struct {
			AgentID string `yaml:"agent_id"`
		} `yaml:"workers"`
	}
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("parse fleet: %w", err)
	}
	removed := map[string]bool{}
	ovPath := strings.TrimSuffix(path, "-fleet.yaml") + "-overrides.json"
	if ov, err := os.ReadFile(ovPath); err == nil {
		var o struct {
			Removed []string `json:"removed"`
		}
		if yaml.Unmarshal(ov, &o) == nil { // YAML is a JSON superset
			for _, a := range o.Removed {
				removed[a] = true
			}
		}
	}
	var out []string
	for _, w := range f.Workers {
		if w.AgentID != "" && !removed[w.AgentID] {
			out = append(out, w.AgentID)
		}
	}
	sort.Strings(out)

	return out, nil
}

func readCompletions(path string) ([]Completion, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("open market db: %w", err)
	}
	defer db.Close() //nolint:errcheck

	q := `select agent_id, template_id, finished_at from mission_results
	      where outcome = 'completed' and template_id in (?, ?, ?)`
	rows, err := db.Query(q, Chain[0], Chain[1], Chain[2])
	if err != nil {
		return nil, fmt.Errorf("query mission_results: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var out []Completion
	for rows.Next() {
		var c Completion
		var fin string
		if err := rows.Scan(&c.AgentID, &c.TemplateID, &fin); err != nil {
			return nil, err
		}
		c.FinishedAt, _ = time.Parse(time.RFC3339, fin)
		out = append(out, c)
	}

	return out, rows.Err()
}

// readBaselines returns each agent's best pirate baseline and when it was
// captured. Errors are swallowed: this column is a cross-check, not the report.
func readBaselines(path string) (map[string]int, time.Time) {
	out := map[string]int{}
	var newest time.Time
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return out, newest
	}
	defer db.Close() //nolint:errcheck

	rows, err := db.Query(`select a.agent_id, max(s.baseline), max(s.captured_at)
	                       from agent_standings s join agents a on a.player_id = s.player_id
	                       where s.faction like 'pirate_%' group by a.agent_id`)
	if err != nil {
		return out, newest
	}
	defer rows.Close() //nolint:errcheck

	for rows.Next() {
		var agent, cap string
		var base sql.NullInt64
		if rows.Scan(&agent, &base, &cap) != nil {
			continue
		}
		if base.Valid {
			out[agent] = int(base.Int64)
		}
		if t, err := time.Parse(time.RFC3339, cap); err == nil && t.After(newest) {
			newest = t
		}
	}

	return out, newest
}
