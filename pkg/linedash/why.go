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
			if j.Recipe == n.Recipe || (n.RecipeName != "" && j.Recipe == n.RecipeName) { // live listings carry the display name
				runs += j.RunsRemaining
				eta = max(eta, j.ETATicks)
			}
		}
	}
	if !seen {
		return
	}
	nr.Detail = append(nr.Detail, fmt.Sprintf("queue: %d runs, ETA %d ticks", runs, eta))
	// A job logs crafting.completed only when the whole job ends, so a busy
	// facility can go a CraftWindow without one: runs still queued mean the
	// line is working. Only the no-craft verdict is lifted; a choke with an
	// alert (output not drawn) stands.
	if runs > 0 && nr.State == StateChoked && len(nr.Alerts) == 0 {
		nr.State = StateOptimal
		nr.Detail = slices.DeleteFunc(nr.Detail, func(d string) bool { return strings.HasPrefix(d, noCraftDetail) })
	}
}

// lineKeys are the outcome keys of agent's work on this line.
func (ev *evaluator) lineKeys(agent string) []string { return ev.line.TaskKeys[agent] }

// latestFailure is the newest failed outcome among the agents' line keys.
// Each key holds only its latest outcome, so a success after a failure has
// already replaced it; keys no longer on the line are never read.
// When the node names its own keys (Derive sets them), only those are read,
// so a crafter's delivery refusal never explains its craft node.
func (ev *evaluator) latestFailure(agents, nodeKeys []string) (string, outcomes.Outcome, bool) {
	var who string
	var best outcomes.Outcome
	found := false
	for _, a := range agents {
		keys := ev.lineKeys(a)
		if len(nodeKeys) > 0 {
			keys = nodeKeys
		}
		for _, k := range keys {
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
		if who, o, ok := ev.latestFailure(n.Agents, n.Keys); ok {
			nr.Why = who + ": " + o.Reason
			continue
		}
		symptom := string(nr.State)
		switch {
		case n.Type == NodeStore && nr.State == StateStarved && ev.feedingLeg(n.ID) != "":
			symptom = ev.feedingLeg(n.ID)
		case len(nr.Alerts) > 0:
			symptom = string(nr.Alerts[0])
		case len(nr.Detail) > 0:
			symptom = nr.Detail[0]
		}
		nr.Why = causeUnknown + symptom
	}
	// Prefer a node that knows its cause; "cause unknown" only when none does.
	fallback := ""
	for _, nr := range nodes {
		if nr.State != lineState || nr.Why == "" {
			continue
		}
		if !strings.HasPrefix(nr.Why, causeUnknown) {
			return nr.Why
		}
		if fallback == "" {
			fallback = nr.Why
		}
	}
	return fallback
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

// causeUnknown opens a why that has no recorded refusal behind it.
const causeUnknown = "cause unknown: "

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
	// Only agents whose line work happens at their own station -- crafters and
	// sellers -- are expected to sit at their pin; a hauler's job is to be away.
	atStation := map[string]bool{}
	for _, n := range ev.line.Nodes {
		if n.Type == NodeProduce || n.Type == NodeSell {
			for _, a := range n.Agents {
				atStation[a] = true
			}
		}
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
		if l, ok := ev.in.Live[a]; ok && atStation[a] && home != "" && l.Docked && ev.within(l.LastSeen, ev.in.Cfg.StaleData) && !same(l.POI, home) {
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

// feedingLeg describes the first haul or consign edge into an empty tank, for
// a why that has no refusal of its own: "water_ice 0 — no haul from trader-9
// in 4h".
func (ev *evaluator) feedingLeg(nodeID string) string {
	for _, e := range ev.line.Edges {
		if e.To != nodeID || e.Kind == EdgeCraft || len(e.Agents) == 0 {
			continue
		}
		kind := "haul"
		if e.Kind == EdgeConsign {
			kind = "consign"
		}
		return fmt.Sprintf("%s 0 — no %s from %s in %s", e.Item, kind, strings.Join(e.Agents, ", "), fmtDur(ev.in.Cfg.HaulWindow))
	}
	return ""
}
