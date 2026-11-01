package linedash

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/outcomes"
)

// CraftQueue is one agent's captured craft queue (craft-queue.json, written by
// the worker's capture_craft_queue).
type CraftQueue struct {
	CapturedAt time.Time   `json:"captured_at"`
	Jobs       []QueuedJob `json:"jobs"`
}

// QueuedJob is one queued craft job.
type QueuedJob struct {
	Recipe        string `json:"recipe"`
	RunsRemaining int    `json:"runs_remaining"`
	ETATicks      int    `json:"eta_ticks"`
	Position      int    `json:"position"`
	BaseID        string `json:"base_id"`
}

// LoadCraftQueue reads agentDir/craft-queue.json. A missing file is a zero
// CraftQueue (never captured) and a nil error.
func LoadCraftQueue(agentDir string) (CraftQueue, error) {
	raw, err := os.ReadFile(filepath.Join(agentDir, "craft-queue.json")) // #nosec G304 -- the agent's own directory
	if errors.Is(err, fs.ErrNotExist) {
		return CraftQueue{}, nil
	}
	if err != nil {
		return CraftQueue{}, err
	}
	var q CraftQueue
	if err := json.Unmarshal(raw, &q); err != nil {
		return CraftQueue{}, fmt.Errorf("craft queue %s: %w", agentDir, err)
	}
	return q, nil
}

// addQueue shows a produce node's backlog from its agents' captured queues.
// A capture is a freshness source, so an old one greys the node; an agent
// with no capture at all adds nothing (not every crafter captures yet).
func (ev *evaluator) addQueue(n Node, nr *NodeReport) {
	runs, eta, seen := 0, 0, false
	for _, a := range n.Agents {
		q, ok := ev.in.Queues[a]
		if !ok || q.CapturedAt.IsZero() {
			continue
		}
		seen = true
		nr.srcs = append(nr.srcs, source{name: a + " craft queue", at: q.CapturedAt})
		for _, j := range q.Jobs {
			if j.Recipe == n.Recipe {
				runs += j.RunsRemaining
				eta = max(eta, j.ETATicks)
			}
		}
	}
	if seen {
		nr.Detail = append(nr.Detail, fmt.Sprintf("queue: %d runs, ETA %d ticks", runs, eta))
	}
}

// lineKeys are the outcome keys of agent's work on this line.
func (ev *evaluator) lineKeys(agent string) []string { return ev.line.TaskKeys[agent] }

// latestFailure is the newest failed outcome among the agents' line keys.
// Each key holds only its latest outcome, so a success after a failure has
// already replaced it; keys no longer on the line are never read.
func (ev *evaluator) latestFailure(agents []string) (string, outcomes.Outcome, bool) {
	var who string
	var best outcomes.Outcome
	found := false
	for _, a := range agents {
		for _, k := range ev.lineKeys(a) {
			o, ok := ev.in.Outcomes[a][k]
			if !ok || o.Status != outcomes.StatusFailed {
				continue
			}
			if !found || o.At.After(best.At) {
				who, best, found = a, o, true
			}
		}
	}
	return who, best, found
}

// applyWhy fills each starved or choked node's Why and returns the line's:
// the Why of the first node, in node order, at the line's worst state.
//
// Order of preference: an empty shared input tank (the cause is upstream and
// other lines compete for it), then the newest recorded refusal of the node's
// agents, then "cause unknown" plus the symptom -- never an inferred cause.
func (ev *evaluator) applyWhy(nodes []NodeReport, index map[string]int, lineState State) string {
	for i := range nodes {
		nr := &nodes[i]
		if nr.State != StateStarved && nr.State != StateChoked {
			continue
		}
		n := ev.nodes[nr.ID]
		if n.Type == NodeProduce {
			if up, ok := index[ev.from(n.ID)]; ok {
				if why := sharedEmpty(nodes[up], n); why != "" {
					nr.Why = why
					continue
				}
			}
		}
		if who, o, ok := ev.latestFailure(n.Agents); ok {
			nr.Why = who + ": " + o.Reason
			continue
		}
		symptom := string(nr.State)
		switch {
		case len(nr.Alerts) > 0:
			symptom = string(nr.Alerts[0])
		case len(nr.Detail) > 0:
			symptom = nr.Detail[0]
		}
		nr.Why = "cause unknown: " + symptom
	}
	for _, nr := range nodes {
		if nr.State == lineState && nr.Why != "" {
			return nr.Why
		}
	}
	return ""
}

// sharedEmpty names an input item that is empty in a tank other lines also
// draw on.
func sharedEmpty(up NodeReport, n Node) string {
	for _, item := range sortedKeys(n.InputsPerRun) {
		if c := up.ItemShares[item]; c >= 2 && up.Units[item] == 0 {
			return fmt.Sprintf("%s 0 (shared with %d lines)", item, c)
		}
	}
	return ""
}

// keyEnsureHome is the worker's record of its trip to the pinned station.
const keyEnsureHome = "behaviour:ensure_home"

// applyConformance flags agents not doing their assignment: every outcome of
// their line work (task:* and behaviour:* keys) within CraftWindow failed,
// their latest trip home was refused, or they are docked away from their
// pinned station. Idle-script lines are housekeeping that succeed every pass
// whatever the line's state, so they never count; ensure_home is judged on
// its own, since a successful trip home says nothing about the crafts. Stale
// nodes keep only their staleness.
func (ev *evaluator) applyConformance(nodes []NodeReport) {
	same := ev.in.SameStation
	if same == nil {
		same = func(a, b string) bool { return a == b }
	}
	off := map[string]bool{}
	for _, a := range AgentsOf(ev.line) {
		recent, failed := 0, 0
		for _, k := range ev.lineKeys(a) {
			o, ok := ev.in.Outcomes[a][k]
			if !ok || !ev.within(o.At, ev.in.Cfg.CraftWindow) {
				continue
			}
			if k == keyEnsureHome {
				if o.Status == outcomes.StatusFailed {
					off[a] = true
				}
				continue
			}
			if !strings.HasPrefix(k, "task:") && !strings.HasPrefix(k, "behaviour:") {
				continue
			}
			recent++
			if o.Status == outcomes.StatusFailed {
				failed++
			}
		}
		if recent > 0 && failed == recent {
			off[a] = true
		}
		home := ev.in.Roster[a].Station
		if l, ok := ev.in.Live[a]; ok && home != "" && l.Docked && ev.within(l.LastSeen, ev.in.Cfg.StaleData) && !same(l.POI, home) {
			off[a] = true
		}
	}
	for i := range nodes {
		if nodes[i].Stale {
			continue
		}
		if slices.ContainsFunc(ev.nodes[nodes[i].ID].Agents, func(a string) bool { return off[a] }) {
			addAlert(&nodes[i], AlertNotConforming)
		}
	}
}

// CurrentOutcomes drops task outcomes that no longer describe the agent's
// schedule. Schedule ids are reused (max+1) once the top task is retired, so
// a "task:N" outcome counts only while task N still runs the command it was
// recorded for. Idle-line and behaviour keys pass through.
func CurrentOutcomes(o map[string]outcomes.Outcome, tasks []Task) map[string]outcomes.Outcome {
	current := make(map[string]string, len(tasks))
	for _, t := range tasks {
		current[fmt.Sprintf("task:%d", t.ID)] = t.Command
	}
	out := make(map[string]outcomes.Outcome, len(o))
	for k, v := range o {
		if cmd, isTask := current[k]; isTask {
			if cmd == v.Command {
				out[k] = v
			}
			continue
		}
		if strings.HasPrefix(k, "task:") {
			continue // a task id the schedule no longer has
		}
		out[k] = v
	}
	return out
}
