package worker

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/market"
)

// An unreadable claim store must not disable the park FOREVER.
//
// The safe-point check answers "unsafe" when GetClaimedByAgent errors, which is the
// right call for one pass: guessing "safe" mid-claim strands paid-for cargo.
// But the answer was permanent and silent, and that combination defeated the
// whole stand-down mechanism on 2026-09-21: sixteen haulers were flagged to
// park, nine of them holding no claim at all, and not one parked. market.db
// was corrupt and heavily contended, so the 5-second safe-point read kept
// failing -- and nothing was logged, so the park simply appeared not to work.
//
// The escalation is the point: a worker held by drain or park is going to be
// stopped either way, and the supervisor force-stops it at
// DefaultRemoveDrainTimeout regardless of this answer. Refusing to park is
// therefore not the safe option after a sustained outage -- it just converts a
// deliberate stop into a forced one, with no record of why.
func TestAtSafePoint_ReportsUnsafeWhileTheStoreIsBrieflyUnreadable(t *testing.T) {
	var out bytes.Buffer
	var fails safePointFailures
	f := &fakeStore{claimedErr: errors.New("database disk image is malformed")}

	for i := range SafePointFailureGrace - 1 {
		if safePointWithGrace(f, "trader-1", &fails, &out) {
			t.Fatalf("failure %d: reported safe while the store is unreadable", i+1)
		}
	}
	if !strings.Contains(out.String(), "safe-point") {
		t.Errorf("an unreadable safe-point check must say so; log was %q", out.String())
	}
}

func TestAtSafePoint_StopsBlockingOnceTheOutageIsSustained(t *testing.T) {
	var out bytes.Buffer
	var fails safePointFailures
	f := &fakeStore{claimedErr: errors.New("database is locked")}

	for range SafePointFailureGrace - 1 {
		safePointWithGrace(f, "trader-1", &fails, &out)
	}
	if !safePointWithGrace(f, "trader-1", &fails, &out) {
		t.Fatalf("after %d consecutive failures the hold must be honoured rather than blocked forever", SafePointFailureGrace)
	}
	if !strings.Contains(out.String(), "assuming safe") {
		t.Errorf("the escalation must be loud; log was %q", out.String())
	}
}

// A single blip must not count toward the grace: only a SUSTAINED outage
// justifies giving up on the check.
func TestAtSafePoint_SuccessResetsTheFailureRun(t *testing.T) {
	var fails safePointFailures
	f := &fakeStore{claimedErr: errors.New("boom")}
	for range SafePointFailureGrace - 1 {
		safePointWithGrace(f, "trader-1", &fails, nil)
	}
	f.claimedErr = nil // store recovers, agent holds nothing
	if !safePointWithGrace(f, "trader-1", &fails, nil) {
		t.Fatal("a recovered store with no claim is genuinely safe")
	}
	f.claimedErr = errors.New("boom again")
	if safePointWithGrace(f, "trader-1", &fails, nil) {
		t.Error("the failure run must restart after a success, not resume where it left off")
	}
}

// The ordinary path is unchanged: holding a claim is unsafe, holding none is
// safe, and neither touches the grace counter.
func TestAtSafePoint_HoldingAClaimIsUnsafeIndefinitely(t *testing.T) {
	var fails safePointFailures
	f := &fakeStore{claimedByAgent: []market.ArbitrageOpportunity{{ID: 1}}}
	for i := range SafePointFailureGrace + 5 {
		if safePointWithGrace(f, "trader-1", &fails, nil) {
			t.Fatalf("pass %d: a held claim is never a safe point", i+1)
		}
	}
}
