package worker

import (
	"context"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// The other 63% of the mission-query waste. `unlock` and `missionrunner` both
// declare `idle: missions`, so the unlock pool (28% of the queries) and
// mission-learn (35%) run this one function. Live signature 2026-09-11:
//
//	trader-6  send_tally total=36 find_route=12 get_active_missions=12 get_missions=12
//	trader-8  send_tally total=38 get_active_missions=13 get_missions=13 find_route=12
//
// Nothing but queries -- no accept, no travel, no mutation of any kind.
// Matching counts mean one pass issues one of each and changes nothing.
func TestMissions_DryPassDoesNotReReadTheBoardNextPass(t *testing.T) {
	fc := freightBoardClient(t, boardJSON(t), nil) // empty board
	deps := missionDeps(fc, &fakeMissionStore{}, missionKB())
	deps.State = &missionRunState{}
	now := time.Unix(1000, 0)
	deps.Now = func() time.Time { return now }

	if err := Missions(context.Background(), deps); err != nil {
		t.Fatalf("pass 1: %v", err)
	}
	board1 := countCalls(fc.calls, "get_missions")
	active1 := countCalls(fc.calls, "get_active_missions")
	if board1 == 0 {
		t.Fatal("pass 1 must read the board at least once")
	}
	if deps.State.dry != 1 {
		t.Fatalf("pass 1 must record one dry pass, got %d", deps.State.dry)
	}

	now = now.Add(game.SleepTick) // one tick later, same station
	if err := Missions(context.Background(), deps); err != nil {
		t.Fatalf("pass 2: %v", err)
	}

	if got := countCalls(fc.calls, "get_missions"); got != board1 {
		t.Errorf("get_missions %d -> %d: re-read the board one tick after a dry pass", board1, got)
	}
	if got := countCalls(fc.calls, "get_active_missions"); got != active1 {
		t.Errorf("get_active_missions %d -> %d: re-read actives while dry", active1, got)
	}
}

// The backstop still fires, so server-side board turnover is picked up.
func TestMissions_ReReadsTheBoardAfterTheInterval(t *testing.T) {
	fc := freightBoardClient(t, boardJSON(t), nil)
	deps := missionDeps(fc, &fakeMissionStore{}, missionKB())
	deps.State = &missionRunState{}
	now := time.Unix(1000, 0)
	deps.Now = func() time.Time { return now }

	if err := Missions(context.Background(), deps); err != nil {
		t.Fatalf("pass 1: %v", err)
	}
	board1 := countCalls(fc.calls, "get_missions")

	now = now.Add(game.SleepMissionBoardPoll)
	if err := Missions(context.Background(), deps); err != nil {
		t.Fatalf("pass 2: %v", err)
	}
	if got := countCalls(fc.calls, "get_missions"); got <= board1 {
		t.Errorf("get_missions stayed at %d after %v; backstop never fired", got, game.SleepMissionBoardPoll)
	}
}

// ⭐ The regression guard that matters: a worker that found WORK last pass is
// not dry, and must keep reading. deps.State.dry is reset to 0 on real work,
// so the gate keys off it directly rather than inventing a second notion of
// idleness that could drift from the first.
func TestMissions_WorkingWorkerKeepsReadingEveryPass(t *testing.T) {
	fc := freightBoardClient(t, boardJSON(t), nil)
	deps := missionDeps(fc, &fakeMissionStore{}, missionKB())
	deps.State = &missionRunState{} // dry == 0: last pass did real work
	now := time.Unix(1000, 0)
	deps.Now = func() time.Time { return now }

	if err := Missions(context.Background(), deps); err != nil {
		t.Fatalf("pass 1: %v", err)
	}
	board1 := countCalls(fc.calls, "get_missions")

	deps.State.dry = 0 // simulate the pass having executed work
	now = now.Add(time.Second)
	if err := Missions(context.Background(), deps); err != nil {
		t.Fatalf("pass 2: %v", err)
	}
	if got := countCalls(fc.calls, "get_missions"); got <= board1 {
		t.Errorf("a working worker stopped reading the board (%d -> %d)", board1, got)
	}
}

// A nil State disables the gate entirely, matching how every other piece of
// cross-pass mission memory degrades.
func TestMissions_NilStateAlwaysReadsTheBoard(t *testing.T) {
	fc := freightBoardClient(t, boardJSON(t), nil)
	deps := missionDeps(fc, &fakeMissionStore{}, missionKB())
	deps.State = nil
	now := time.Unix(1000, 0)
	deps.Now = func() time.Time { return now }

	for i := range 2 {
		if err := Missions(context.Background(), deps); err != nil {
			t.Fatalf("pass %d: %v", i+1, err)
		}
	}
	if got := countCalls(fc.calls, "get_missions"); got < 2 {
		t.Errorf("nil State must not suppress board reads, got %d reads in 2 passes", got)
	}
}

// ⭐ The case the first cut of this gate missed, found live on 2026-09-11.
//
// trader-2/6/8 are PINNED workers. A pinned worker that finds nothing does not
// reposition -- after missionDryPassLimit it parks for missionParkWindow and
// camps its local board. But missionDryPass returns BEFORE `dry++` while
// parked, so a parked worker sits at dry == 0 forever, and a gate keyed only
// on dry never fires for it. Live result: they kept the full
// `find_route=12 get_active_missions=12 get_missions=12` signature on the new
// binary while unpinned workers were being gated normally.
//
// Parked is the strongest possible statement that a worker is idle -- it has
// already decided to camp rather than burn fuel. It must poll slowest of all,
// not fastest.
func TestMissions_ParkedWorkerIsGatedEvenWithDryZero(t *testing.T) {
	fc := freightBoardClient(t, boardJSON(t), nil)
	deps := missionDeps(fc, &fakeMissionStore{}, missionKB())
	now := time.Unix(1000, 0)
	deps.Now = func() time.Time { return now }
	// Exactly the live shape: parked, and dry already reset to 0 by the park.
	deps.State = &missionRunState{
		dry:          0,
		parkedUntil:  now.Add(30 * time.Minute),
		boardPoll:    now.Add(-time.Second),
		boardStation: "haven",
	}

	if err := Missions(context.Background(), deps); err != nil {
		t.Fatalf("Missions: %v", err)
	}

	if got := countCalls(fc.calls, "get_missions"); got != 0 {
		t.Errorf("a parked worker re-read the board %d time(s); parking means camp, not poll", got)
	}
}

// A park that has EXPIRED must read again, or a worker would camp forever.
func TestMissions_ExpiredParkReadsTheBoardAgain(t *testing.T) {
	fc := freightBoardClient(t, boardJSON(t), nil)
	deps := missionDeps(fc, &fakeMissionStore{}, missionKB())
	now := time.Unix(1000, 0)
	deps.Now = func() time.Time { return now }
	deps.State = &missionRunState{
		dry:          0,
		parkedUntil:  now.Add(-time.Second), // park is over
		boardPoll:    now.Add(-time.Second),
		boardStation: "haven",
	}

	if err := Missions(context.Background(), deps); err != nil {
		t.Fatalf("Missions: %v", err)
	}
	if got := countCalls(fc.calls, "get_missions"); got == 0 {
		t.Error("an expired park must resume reading the board")
	}
}
