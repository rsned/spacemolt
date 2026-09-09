package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/overmind/supervisor"
)

// parseAgentList splits a comma-separated --nominate value, trimming blanks so a
// trailing comma cannot enqueue an empty agent id.
func parseAgentList(s string) []string {
	var out []string
	for part := range strings.SplitSeq(s, ",") {
		if id := strings.TrimSpace(part); id != "" {
			out = append(out, id)
		}
	}

	return out
}

// nominateAgents appends a home->away loan request for each agent and returns
// which were added and which already had a trip open.
//
// This exists because self-nomination is a HAULER-ONLY path: nominateUnlockFn is
// wired from haul.go and nothing else calls it, which is why haul reached 100%
// on the pirate unlock while every other fleet stayed where it started. The
// reconciler is already fleet-agnostic (--home/--away), so an operator-side way
// to enqueue is the only missing piece.
//
// Like the worker path this only APPENDS. It never edits fleet membership and
// never moves anyone: moving is the reconciler's job, in a strict
// stop-then-start order, because an agent started in two fleets loses its game
// session to session_replaced.
//
// A batch reports per-agent outcomes instead of failing wholesale, so one
// already-nominated agent does not block the rest of a rotation.
func nominateAgents(ledgerPath string, agents []string, home, away, reason string) (added, skipped []string, err error) {
	led, err := supervisor.LoadSecondments(ledgerPath)
	if err != nil {
		// A corrupt ledger must not be overwritten by a nomination: that would
		// erase trips already in flight and could re-nominate an agent mid-move.
		return nil, nil, fmt.Errorf("secondment ledger unreadable: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, id := range agents {
		if led.Nominate(supervisor.Secondment{
			AgentID:     id,
			HomeFleet:   home,
			AwayFleet:   away,
			Reason:      reason,
			NominatedAt: now,
		}) {
			added = append(added, id)
			continue
		}
		skipped = append(skipped, id)
	}
	if len(added) == 0 {
		// Nothing changed; do not rewrite the file.
		return added, skipped, nil
	}
	if err := supervisor.SaveSecondments(ledgerPath, led); err != nil {
		return nil, nil, fmt.Errorf("save secondment ledger: %w", err)
	}

	return added, skipped, nil
}
