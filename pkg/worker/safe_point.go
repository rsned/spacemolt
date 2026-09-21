package worker

import (
	"context"
	"fmt"
	"io"
	"sync/atomic"

	"github.com/rsned/spacemolt/pkg/game"
)

// SafePointFailureGrace is how many CONSECUTIVE unreadable safe-point checks a
// worker tolerates before it stops treating "cannot tell" as "not safe".
//
// A pass runs about once per tick (~11s), so this is roughly three and a half
// minutes -- deliberately just inside DefaultRemoveDrainTimeout (4m). Past that
// point the supervisor force-stops the worker anyway, so continuing to refuse
// the park buys nothing and loses the chance to stop deliberately.
const SafePointFailureGrace = 20

// safePointFailures counts consecutive unreadable safe-point checks for one
// worker. Zero value is ready to use.
type safePointFailures struct{ n atomic.Int64 }

// safePointWithGrace reports whether the role is between units of work.
//
// Holding a claim is unsafe for as long as it is held -- that never expires,
// because stopping mid-claim strands cargo the agent has already paid for.
// An unreadable store is different: it is unsafe for SafePointFailureGrace
// consecutive passes and then gives up and says safe, loudly.
//
// The permanent-and-silent version of this defeated the whole stand-down
// mechanism on 2026-09-21. Sixteen haulers were flagged to park, nine holding
// no claim at all, and none parked: market.db was corrupt and contended, every
// safe-point read failed, and because the failure logged nothing the park just
// looked broken. Refusing forever is not the conservative choice -- the
// supervisor force-stops at the drain timeout regardless, so the only thing
// the refusal changes is whether the stop is deliberate and recorded.
func safePointWithGrace(store OpportunityStore, agentID string, fails *safePointFailures, out io.Writer) bool {
	if store == nil {
		return true
	}
	if out == nil {
		out = io.Discard
	}
	ctx, cancel := context.WithTimeout(context.Background(), game.SleepShort)
	defer cancel()

	held, err := store.GetClaimedByAgent(ctx, agentID)
	if err == nil {
		fails.n.Store(0)
		return len(held) == 0
	}
	n := fails.n.Add(1)
	if n >= SafePointFailureGrace {
		fmt.Fprintf(out, "safe-point check unreadable for %d consecutive passes (%v); assuming safe so the hold can complete\n", n, err) //nolint:errcheck
		fails.n.Store(0)

		return true
	}
	fmt.Fprintf(out, "safe-point check failed (%d/%d): %v; treating as mid-work\n", n, SafePointFailureGrace, err) //nolint:errcheck

	return false
}
