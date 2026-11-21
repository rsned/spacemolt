package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// giftUnlockFake is the narrow surface unlock_gifting uses: the mission board,
// active missions, the local market and the wallet.
type giftUnlockFake struct {
	docked   bool
	credits  float64
	board    []serverapi.MissionBoardEntry
	active   []serverapi.ActiveMission
	listings []game.MarketListing
	calls    []string
	buyErr   error
	// unbuyable items fail buy with the server's invalid_item rejection.
	unbuyable map[string]bool
	// completeErrs are returned, in order, by successive CompleteMission calls.
	completeErrs []error
	missionsErr  error
}

func (f *giftUnlockFake) GetState() *game.State {
	return &game.State{Credits: f.credits, Doc: f.docked}
}

func (f *giftUnlockFake) GetMissions(context.Context) error {
	f.calls = append(f.calls, "get_missions")
	return f.missionsErr
}

func (f *giftUnlockFake) GetActiveMissions(context.Context) error {
	f.calls = append(f.calls, "get_active_missions")
	return nil
}

func (f *giftUnlockFake) AcceptMission(_ context.Context, id string) error {
	f.calls = append(f.calls, "accept:"+id)
	f.active = append(f.active, serverapi.ActiveMission{MissionID: "inst-1", TemplateID: id})
	return nil
}

func (f *giftUnlockFake) CompleteMission(_ context.Context, id string) error {
	f.calls = append(f.calls, "complete:"+id)
	if len(f.completeErrs) > 0 {
		err := f.completeErrs[0]
		f.completeErrs = f.completeErrs[1:]
		return err
	}
	return nil
}

func (f *giftUnlockFake) ViewMarket(context.Context, map[string]any) error {
	f.calls = append(f.calls, "view_market")
	return nil
}

func (f *giftUnlockFake) GetMarketListings() []game.MarketListing { return f.listings }

func (f *giftUnlockFake) Buy(_ context.Context, item string, qty float64) error {
	f.calls = append(f.calls, fmt.Sprintf("buy:%s:%.0f", item, qty))
	if f.unbuyable[item] {
		return fmt.Errorf("invalid_item: Unknown item '%s'. Use exact item ID", item)
	}
	return f.buyErr
}

func (f *giftUnlockFake) GetRawJSON(key string) []byte {
	var v any
	switch key {
	case "missions":
		v = serverapi.GetMissionsResponse{Missions: f.board}
	case "active_missions":
		v = serverapi.GetActiveMissionsResponse{Missions: f.active}
	default:
		return nil
	}
	b, _ := json.Marshal(v)
	return b
}

func sell(item string, qty, price float64) game.MarketListing {
	return game.MarketListing{ItemID: item, Type: "sell", Quantity: qty, PricePerUnit: price}
}

func boardWithBuying() []serverapi.MissionBoardEntry {
	return []serverapi.MissionBoardEntry{
		{MissionID: "market_participation_selling", TemplateID: "market_participation_selling"},
		{MissionID: giftUnlockMission, TemplateID: giftUnlockMission},
	}
}

// The operator recipe (2026-10-22, the_banker 100 -> 1,090): accept the buying
// mission, buy 10 of the cheapest local item, complete it for 1,000 earned
// credits — which is what lifts gift_not_unlocked.
func TestUnlockGiftingRunsTheRecipe(t *testing.T) {
	f := &giftUnlockFake{
		docked: true, credits: 5000, board: boardWithBuying(),
		listings: []game.MarketListing{
			sell("gold_ore", 500, 40),
			sell("iron_ore", 4, 1),    // cheapest unit, but only 4 on offer
			sell("iron_ore", 100, 9),  // ...so 10 of it costs 4*1 + 6*9 = 58
			sell("carbon_ore", 50, 3), // 10 for 30: the cheapest ten
			{ItemID: "lithium_ore", Type: "buy", Quantity: 900, PricePerUnit: 1},
		},
	}
	if err := unlockGifting(context.Background(), f, io.Discard, noSleep); err != nil {
		t.Fatalf("unlockGifting: %v", err)
	}
	want := []string{"accept:" + giftUnlockMission, "buy:carbon_ore:10", "complete:inst-1"}
	got := slices.DeleteFunc(slices.Clone(f.calls), func(c string) bool {
		return !strings.HasPrefix(c, "accept:") && !strings.HasPrefix(c, "buy:") && !strings.HasPrefix(c, "complete:")
	})
	if !slices.Equal(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
}

// Not on the board and not active means it was already completed (it is a
// one-time mission): nothing to do, and nothing is bought.
func TestUnlockGiftingIsANoOpOnceDone(t *testing.T) {
	f := &giftUnlockFake{docked: true, credits: 5000,
		board:    []serverapi.MissionBoardEntry{{MissionID: "market_participation_selling"}},
		listings: []game.MarketListing{sell("carbon_ore", 50, 3)}}
	if err := unlockGifting(context.Background(), f, io.Discard, noSleep); err != nil {
		t.Fatalf("unlockGifting: %v", err)
	}
	for _, c := range f.calls {
		if strings.HasPrefix(c, "buy:") || strings.HasPrefix(c, "accept:") {
			t.Fatalf("already-done agent still acted: %v", f.calls)
		}
	}
}

// A run interrupted after accept resumes: no second accept, buy, complete.
func TestUnlockGiftingResumesAnActiveMission(t *testing.T) {
	f := &giftUnlockFake{docked: true, credits: 5000,
		active:   []serverapi.ActiveMission{{MissionID: "inst-7", TemplateID: giftUnlockMission}},
		listings: []game.MarketListing{sell("carbon_ore", 50, 3)}}
	if err := unlockGifting(context.Background(), f, io.Discard, noSleep); err != nil {
		t.Fatalf("unlockGifting: %v", err)
	}
	if slices.ContainsFunc(f.calls, func(c string) bool { return strings.HasPrefix(c, "accept:") }) {
		t.Fatalf("re-accepted an active mission: %v", f.calls)
	}
	if !slices.Contains(f.calls, "complete:inst-7") {
		t.Fatalf("did not complete the active instance: %v", f.calls)
	}
}

func TestUnlockGiftingRefusals(t *testing.T) {
	cases := map[string]*giftUnlockFake{
		"undocked": {docked: false, credits: 5000, board: boardWithBuying(),
			listings: []game.MarketListing{sell("carbon_ore", 50, 3)}},
		"no item with ten on offer": {docked: true, credits: 5000, board: boardWithBuying(),
			listings: []game.MarketListing{sell("carbon_ore", 9, 3)}},
		"costs the reward": {docked: true, credits: 50000, board: boardWithBuying(),
			listings: []game.MarketListing{sell("gold_ore", 50, 100)}},
		"cannot afford ten": {docked: true, credits: 20, board: boardWithBuying(),
			listings: []game.MarketListing{sell("carbon_ore", 50, 3)}},
	}
	for name, f := range cases {
		t.Run(name, func(t *testing.T) {
			if err := unlockGifting(context.Background(), f, io.Discard, noSleep); err == nil {
				t.Fatal("want an error")
			}
			if slices.ContainsFunc(f.calls, func(c string) bool { return strings.HasPrefix(c, "buy:") }) {
				t.Fatalf("bought anyway: %v", f.calls)
			}
		})
	}
}

// A listed item the exchange refuses (chaff_bundle at Gold Run and Treasure
// Cache, 2026-11-20) must not block the unlock: buy the next-cheapest ten.
func TestUnlockGiftingSkipsAnUnbuyableItem(t *testing.T) {
	f := &giftUnlockFake{docked: true, credits: 5000, board: boardWithBuying(),
		unbuyable: map[string]bool{"chaff_bundle": true},
		listings:  []game.MarketListing{sell("chaff_bundle", 50, 1), sell("carbon_ore", 50, 3)}}
	if err := unlockGifting(context.Background(), f, io.Discard, noSleep); err != nil {
		t.Fatalf("unlockGifting: %v", err)
	}
	if !slices.Contains(f.calls, "buy:carbon_ore:10") || !slices.Contains(f.calls, "complete:inst-1") {
		t.Fatalf("did not fall through to carbon_ore: %v", f.calls)
	}
}

// A buy queued for the next tick may not have landed when complete runs
// (marketbot_the_anvil, 2026-11-20): wait a tick and complete once more.
func TestUnlockGiftingRetriesAnIncompleteObjective(t *testing.T) {
	f := &giftUnlockFake{docked: true, credits: 5000, board: boardWithBuying(),
		listings:     []game.MarketListing{sell("carbon_ore", 50, 3)},
		completeErrs: []error{fmt.Errorf("mission_incomplete: Objective incomplete: Buy 10 units of any item on the exchange")}}
	if err := unlockGifting(context.Background(), f, io.Discard, noSleep); err != nil {
		t.Fatalf("unlockGifting: %v", err)
	}
	n := 0
	for _, c := range f.calls {
		if c == "complete:inst-1" {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("complete calls = %d, want 2: %v", n, f.calls)
	}
}

// Small stations have no mission board at all; that is a skip, not an error
// to repeat every hour.
func TestUnlockGiftingSkipsAStationWithoutMissions(t *testing.T) {
	f := &giftUnlockFake{docked: true, credits: 5000,
		missionsErr: fmt.Errorf("get_missions: Conjunction Junction does not offer mission services. Try a larger base or station."),
		listings:    []game.MarketListing{sell("carbon_ore", 50, 3)}}
	if err := unlockGifting(context.Background(), f, io.Discard, noSleep); err != nil {
		t.Fatalf("unlockGifting: %v", err)
	}
	if slices.ContainsFunc(f.calls, func(c string) bool { return strings.HasPrefix(c, "buy:") }) {
		t.Fatalf("bought at a station with no missions: %v", f.calls)
	}
}

func noSleep(context.Context, time.Duration) {}
