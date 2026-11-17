package worker

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// default_stance only covers fights the ship did NOT start, so the hunt role
// gets it too: a pirate or a predator that jumps a hunter mid-transit must
// meet a ship already trying to leave.
func TestDefaultStanceClientArmsHunt(t *testing.T) {
	f := &fleeFake{}
	bc := DefaultStanceClient("hunt", f)
	if bc == nil {
		t.Fatal("hunt must get a default-stance client")
	}
	if err := ensureDefaultStanceFlee(context.Background(), io.Discard, &game.Ship{}, bc); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 1 || f.calls[0] != "default_stance flee" {
		t.Fatalf("calls = %v, want one default_stance flee", f.calls)
	}
}

// Each pass re-asserts default_stance flee (it resets when the ship changes
// owner), and a fight the hunter starts on purpose still opens and fires:
// the server opens a fight you start at fire, and huntPolicy sets the stance
// every tick.
func TestHuntSetsDefaultStanceFleeAndStillFires(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{
		board:     []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 1}},
		creatures: huntGrazers("c1"),
	})
	var log strings.Builder
	if err := Hunt(context.Background(), huntDeps(c, &log)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	ds := firstCall(c, "battle:default_stance:flee")
	hunt := firstCall(c, "hunt:c1")
	if ds < 0 || hunt < 0 || ds > hunt {
		t.Fatalf("want default_stance flee before the engagement, calls %v\n%s", c.calls, log.String())
	}
	if countAction(c, "stance:fire") == 0 {
		t.Errorf("a hunter-started fight must still fire, actions %v", c.battleActions)
	}
	if len(c.wrecks) != 1 || !called(c, "complete:hex-first_hunt_belt_grazers") {
		t.Errorf("want the kill landed and the mission completed, calls %v\n%s", c.calls, log.String())
	}
	if c.fled {
		t.Errorf("a winning fight must not flee, actions %v", c.battleActions)
	}
}

// A ship already flagged default flee is not re-sent the command.
func TestHuntSkipsDefaultStanceWhenAlreadyFlee(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{})
	c.state.Ship.DefaultStance = "flee"
	if err := Hunt(context.Background(), huntDeps(c, io.Discard)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if called(c, "default_stance") {
		t.Errorf("default stance already flee; calls %v", c.calls)
	}
}

// Caught in a fight at the top of a pass: flee, and do nothing else this pass
// (every non-battle command is refused in_battle anyway).
func TestHuntFleesABattleAtPassStart(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{
		board:     []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 1}},
		creatures: huntGrazers("c1"),
	})
	c.state.InBattle = true
	var log strings.Builder
	if err := Hunt(context.Background(), huntDeps(c, &log)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if !called(c, "battle:stance:flee") {
		t.Fatalf("an unstarted battle at pass start must be fled, calls %v\n%s", c.calls, log.String())
	}
	if called(c, "accept:") || c.huntCalls != 0 || called(c, "travel:") {
		t.Errorf("nothing else may run while caught in a battle, calls %v", c.calls)
	}
}

// A dock refused in_battle (the battle_started push not yet seen) is the same
// signal: flee rather than try to fly to a station.
func TestHuntFleesWhenTheDockIsRefusedInBattle(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{startPOI: "pollux_outer_belt", unsetDocked: true})
	c.dockErr = errors.New("in_battle: you cannot do that while in a battle")
	var log strings.Builder
	if err := Hunt(context.Background(), huntDeps(c, &log)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if !called(c, "battle:stance:flee") {
		t.Fatalf("dock refused in_battle must flee, calls %v\n%s", c.calls, log.String())
	}
	if called(c, "find_route") || called(c, "travel:") {
		t.Errorf("must not try to relocate while in a battle, calls %v", c.calls)
	}
}

// Still a participant between engagements: flee, not just end the pass.
func TestHuntFleesWhenStillInBattleBetweenEngagements(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{
		board:          []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 2}},
		creatures:      huntGrazers("c1", "c2"),
		lingerInBattle: true,
	})
	var log strings.Builder
	if err := Hunt(context.Background(), huntDeps(c, &log)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	hunt := lastCall(c, "hunt:")
	flee := lastCall(c, "battle:stance:flee")
	if flee < 0 || flee < hunt {
		t.Fatalf("a lingering battle between engagements must be fled, calls %v\n%s", c.calls, log.String())
	}
}

// huntBattleTravelFake wraps any hunt fake so its travel is cut short by a
// fight the hunter did not start.
type huntBattleTravelFake struct {
	game.GameClient
	f *huntFake
}

func (w *huntBattleTravelFake) Travel(_ context.Context, poi string) (*game.TravelResult, error) {
	w.f.calls = append(w.f.calls, "travel:"+poi)
	w.f.state.InBattle = true
	return &game.TravelResult{Canceled: true}, nil
}

// A transit that ends in a fight (pirate or creature jumps the hunter on the
// way to the ground) must flee, not leave the ship sitting in the battle
// until the next pass fails every command.
func TestHuntFleesWhenTransitEndsInABattle(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{
		board:     []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 1}},
		creatures: huntGrazers("c1"),
	})
	w := &huntBattleTravelFake{GameClient: c, f: c}
	var log strings.Builder
	deps := huntDeps(c, &log)
	deps.Client = w
	if err := Hunt(context.Background(), deps); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	travel := firstCall(c, "travel:pollux_outer_belt")
	flee := lastCall(c, "battle:stance:flee")
	if travel < 0 || flee < travel {
		t.Fatalf("an interrupted transit into a battle must flee, calls %v\n%s", c.calls, log.String())
	}
	if c.huntCalls != 0 {
		t.Errorf("must not engage from inside an unstarted battle, calls %v", c.calls)
	}
}

// The delivery leg is a transit like any other.
func TestHuntFleesWhenTheDeliveryLegEndsInABattle(t *testing.T) {
	kb := huntNeedKBFixture(t)
	f := newHuntNeedFake(t, "gauntlet", huntFakeOpts{
		heldAtStart:    true,
		activeMissions: []serverapi.ActiveMission{huntCausticActive(2)},
	})
	f.state.Ship.Cargo = []game.CargoItem{{ItemID: "caustic_sac", Quantity: 10}}
	var log strings.Builder
	deps := huntNeedDeps(f, kb, &log)
	deps.Client = &huntBattleTravelFake{GameClient: f, f: f.huntFake}
	if err := Hunt(context.Background(), deps); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	travel := firstCall(f.huntFake, "travel:levy_base")
	flee := lastCall(f.huntFake, "battle:stance:flee")
	if travel < 0 || flee < travel {
		t.Fatalf("a delivery leg cut short by a battle must flee, calls %v\n%s", f.calls, log.String())
	}
	if called(f.huntFake, "complete:") {
		t.Errorf("completed from inside a battle, calls %v", f.calls)
	}
}

// So is a dry-board roaming move.
func TestHuntFleesWhenARoamEndsInABattle(t *testing.T) {
	t.Parallel()
	f := huntRoamFake(t, huntFakeOpts{creatures: huntGrazers("c1")})
	var log strings.Builder
	deps := huntDeps(f, &log)
	deps.KB = huntRoamKB(t, true)
	deps.Client = &huntBattleTravelFake{GameClient: f, f: f}
	if err := Hunt(context.Background(), deps); err != nil {
		t.Fatal(err)
	}
	travel := firstCall(f, "travel:east_station")
	flee := lastCall(f, "battle:stance:flee")
	if travel < 0 || flee < travel {
		t.Fatalf("a roam cut short by a battle must flee, calls %v\n%s", f.calls, log.String())
	}
}
