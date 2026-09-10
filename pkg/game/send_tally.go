package game

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// sendTally counts outbound commands by type over a window.
//
// It exists because the 2026-09-09 IP block could not be diagnosed: the server
// keeps no record of what tripped a block, and our own logs recorded only that
// one happened. The total in trackMessageSent was no help either — it says how
// much we sent, never WHAT, so a single wasteful command repeated by every
// worker looks exactly like healthy fleet-wide traffic.
//
// The reference point that makes this worth instrumenting: other operators
// reportedly run ~1000 agents on a single IP without blocks. Our 170 are
// therefore not inherently too many, and "shrink the fleet" is treating a
// symptom. Something specific is burning requests, and naming the command is
// the only way to find it.
type sendTally struct {
	mu     sync.Mutex
	counts map[string]int64
	total  int64
}

// sendSnapshot is one window's worth of counts, already detached from the tally.
type sendSnapshot struct {
	Total  int64
	Counts map[string]int64
}

// record counts one outbound command.
func (t *sendTally) record(cmd string) {
	if cmd == "" {
		cmd = "(empty)"
	}
	t.mu.Lock()
	if t.counts == nil {
		t.counts = make(map[string]int64, 16)
	}
	t.counts[cmd]++
	t.total++
	t.mu.Unlock()
}

// snapshot returns the window's counts and RESETS, so each logged line is a
// rate over its own window. A cumulative counter would flatten as uptime grew
// and hide a spike that started late.
func (t *sendTally) snapshot() sendSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()

	snap := sendSnapshot{Total: t.total, Counts: t.counts}
	if snap.Counts == nil {
		snap.Counts = map[string]int64{}
	}
	t.counts = nil
	t.total = 0

	return snap
}

// String renders the window for the worker log, ordered by descending volume so
// the worst offender is first and a fleet-wide tally can be summed with grep.
// Ties break by name to keep the output stable between windows.
func (s sendSnapshot) String() string {
	type kv struct {
		cmd string
		n   int64
	}
	pairs := make([]kv, 0, len(s.Counts))
	for cmd, n := range s.Counts {
		pairs = append(pairs, kv{cmd, n})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].n != pairs[j].n {
			return pairs[i].n > pairs[j].n
		}

		return pairs[i].cmd < pairs[j].cmd
	})

	var b strings.Builder
	fmt.Fprintf(&b, "send_tally total=%d", s.Total)
	for _, p := range pairs {
		fmt.Fprintf(&b, " %s=%d", p.cmd, p.n)
	}

	return b.String()
}

// SendTallySnapshot returns and clears this client's per-command send counts.
// Exported so the worker can log a window periodically; returns an empty
// snapshot when nothing was sent, which the caller should not log.
func (c *Client) SendTallySnapshot() sendSnapshot {
	return c.sendTally.snapshot()
}
