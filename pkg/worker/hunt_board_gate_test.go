package worker

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// The 2026-09-11 measurement: pirate-6 through pirate-10 each emitted
//
//	send_tally total=116 completed_missions=29 get_active_missions=29 get_missions=29 get_status=29
//
// byte-identical across five agents. Four queries at the SAME count means one
// pass issues exactly one of each and accomplishes nothing -- 29 passes per 5
// minutes, accept_mission=0. Per the server team, board contents are computed
// from our own state and postings turn over on a server-owned timer, so asking
// again immediately returns the board we already have. The three mission
// queries were 43.9% of ALL fleet traffic, and the IP-block counter aggregates
// every request from the address, so this is budget, not just noise.
func TestHuntBoardGate_FirstPassAlwaysPolls(t *testing.T) {
	var g huntBoardGate
	if !g.shouldPoll("station-a", time.Unix(0, 0)) {
		t.Fatal("a gate that has never polled must poll")
	}
}

// The core of the fix: a dry pass at an unchanged station must not re-ask.
func TestHuntBoardGate_SkipsWhileDryAtSameStation(t *testing.T) {
	var g huntBoardGate
	t0 := time.Unix(1000, 0)
	g.shouldPoll("station-a", t0)
	g.record("station-a", t0, true /*dry*/)

	if g.shouldPoll("station-a", t0.Add(game.SleepTick)) {
		t.Fatal("polled again one tick after a dry pass at the same station")
	}
	if g.shouldPoll("station-a", t0.Add(game.SleepMissionBoardPoll-time.Second)) {
		t.Fatal("polled again just inside the backoff window")
	}
}

// Board turnover is server-owned, so the timer is the backstop that eventually
// notices a new posting even when nothing on our side moved.
func TestHuntBoardGate_PollsAgainAfterTheInterval(t *testing.T) {
	var g huntBoardGate
	t0 := time.Unix(1000, 0)
	g.record("station-a", t0, true)

	if !g.shouldPoll("station-a", t0.Add(game.SleepMissionBoardPoll)) {
		t.Fatalf("did not poll after %v elapsed", game.SleepMissionBoardPoll)
	}
}

// "Check after something changes on your side" -- docking somewhere new is a
// different board entirely, so the backoff must not survive the move.
func TestHuntBoardGate_DockingElsewherePollsImmediately(t *testing.T) {
	var g huntBoardGate
	t0 := time.Unix(1000, 0)
	g.record("station-a", t0, true)

	if !g.shouldPoll("station-b", t0.Add(game.SleepTick)) {
		t.Fatal("a new station must be read immediately, not after the backoff")
	}
}

// ⭐ The regression that would matter most. An agent holding an active mission
// is NOT dry: skipping its queries would stop huntResumeJob from finding the
// mission and strand the job forever. Backoff applies only to a pass that
// found nothing to resume AND nothing admissible to accept.
func TestHuntBoardGate_NeverSkipsWhenTheLastPassFoundWork(t *testing.T) {
	var g huntBoardGate
	t0 := time.Unix(1000, 0)
	g.record("station-a", t0, false /*not dry: found a job*/)

	if !g.shouldPoll("station-a", t0.Add(time.Second)) {
		t.Fatal("a working agent must keep reading its active missions every pass")
	}
}

// Going dry and then finding work again must clear the backoff, or an agent
// that picks up a mission right after a dry spell inherits the skip.
func TestHuntBoardGate_WorkClearsAnEarlierDrySpell(t *testing.T) {
	var g huntBoardGate
	t0 := time.Unix(1000, 0)
	g.record("station-a", t0, true)
	g.record("station-a", t0.Add(game.SleepMissionBoardPoll), false)

	if !g.shouldPoll("station-a", t0.Add(game.SleepMissionBoardPoll+time.Second)) {
		t.Fatal("backoff survived a productive pass")
	}
}

// A nil gate is the zero-config path (tests, play_as, any caller that does not
// thread one through). It must behave exactly as today: always poll.
func TestHuntBoardGate_NilGateAlwaysPolls(t *testing.T) {
	var g *huntBoardGate
	if !g.shouldPoll("station-a", time.Unix(0, 0)) {
		t.Fatal("a nil gate must not suppress polling")
	}
	g.record("station-a", time.Unix(0, 0), true) // must not panic
}

// countCalls reports how many times cmd appears in the fake's call log.
func countCalls(calls []string, cmd string) int {
	n := 0
	for _, c := range calls {
		if c == cmd {
			n++
		}
	}
	return n
}

// The end-to-end behaviour the fleet needs: a pass that finds nothing must not
// make the NEXT pass re-ask. Without the gate these two passes issue two
// identical sets of mission queries ~10s apart, which is precisely the
// pirate-6..10 signature (29 of each per 5 minutes, accept_mission=0).
func TestHunt_DryPassDoesNotReReadTheBoardNextPass(t *testing.T) {
	f := newHuntFake(t, huntFakeOpts{noActive: true}) // empty board, nothing active
	var out strings.Builder
	deps := huntDeps(f, &out)
	deps.boardGate = &huntBoardGate{}
	now := time.Unix(1000, 0)
	deps.NowFn = func() time.Time { return now }

	if err := Hunt(context.Background(), deps); err != nil {
		t.Fatalf("pass 1: %v", err)
	}
	board1 := countCalls(f.calls, "get_missions")
	done1 := countCalls(f.calls, "completed_missions")
	if board1 == 0 {
		t.Fatal("pass 1 must read the board at least once")
	}

	now = now.Add(game.SleepTick) // one tick later, still parked at the same station
	if err := Hunt(context.Background(), deps); err != nil {
		t.Fatalf("pass 2: %v", err)
	}

	if got := countCalls(f.calls, "get_missions"); got != board1 {
		t.Errorf("get_missions %d -> %d: re-read the board one tick after a dry pass", board1, got)
	}
	if got := countCalls(f.calls, "completed_missions"); got != done1 {
		t.Errorf("completed_missions %d -> %d: re-read the chain evidence while dry", done1, got)
	}
}

// The backstop: once the interval elapses the board is read again, so a
// posting that turned over server-side is still picked up.
func TestHunt_ReReadsTheBoardAfterTheInterval(t *testing.T) {
	f := newHuntFake(t, huntFakeOpts{noActive: true})
	var out strings.Builder
	deps := huntDeps(f, &out)
	deps.boardGate = &huntBoardGate{}
	now := time.Unix(1000, 0)
	deps.NowFn = func() time.Time { return now }

	if err := Hunt(context.Background(), deps); err != nil {
		t.Fatalf("pass 1: %v", err)
	}
	board1 := countCalls(f.calls, "get_missions")

	now = now.Add(game.SleepMissionBoardPoll)
	if err := Hunt(context.Background(), deps); err != nil {
		t.Fatalf("pass 2: %v", err)
	}

	if got := countCalls(f.calls, "get_missions"); got <= board1 {
		t.Errorf("get_missions stayed at %d after %v; the backstop never fired", got, game.SleepMissionBoardPoll)
	}
}
