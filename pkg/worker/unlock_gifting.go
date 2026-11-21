package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// giftUnlockMission is the one-time station mission whose 1,000-credit reward
// counts as EARNED credits — the threshold send_gift checks
// (gift_not_unlocked: "You must earn at least 1000 credits before sending
// gifts"). Gifted credits do not count, so a marketbot funded only by gifts
// can never consign until it runs this.
const giftUnlockMission = "market_participation_buying"

// giftUnlockQty is the mission's objective: buy 10 units of anything.
const giftUnlockQty = 10

// giftUnlockReward is the mission's credit reward. A purchase that costs as
// much buys nothing, so the verb refuses it.
const giftUnlockReward = 1000

// giftUnlockClient is the slice of game.GameClient unlock_gifting needs.
type giftUnlockClient interface {
	GetState() *game.State
	GetMissions(ctx context.Context) error
	GetActiveMissions(ctx context.Context) error
	AcceptMission(ctx context.Context, missionID string) error
	CompleteMission(ctx context.Context, missionID string) error
	ViewMarket(ctx context.Context, payload map[string]any) error
	GetMarketListings() []game.MarketListing
	Buy(ctx context.Context, itemID string, quantity float64) error
	GetRawJSON(key string) []byte
}

// unlockGifting runs the operator recipe at the docked station: accept
// market_participation_buying, buy 10 of the cheapest local item, complete it.
//
// It is idempotent. The mission is one-time, so once done it is neither on the
// board nor active and the call is a no-op; a run interrupted after accept
// finds the active instance and finishes it without re-accepting.
func unlockGifting(ctx context.Context, c giftUnlockClient, out io.Writer, sleep func(context.Context, time.Duration)) error {
	if st := c.GetState(); st == nil || !st.IsDocked() {
		return fmt.Errorf("unlock_gifting: not docked")
	}

	instance, err := giftUnlockActiveInstance(ctx, c, sleep)
	if err != nil {
		return err
	}
	if instance == "" {
		offered, berr := giftUnlockOffered(ctx, c, sleep)
		if isNoMissionServicesErr(berr) {
			fmt.Fprintf(out, "unlock_gifting: no mission board at this station — skipped\n") //nolint:errcheck
			return nil
		}
		if berr != nil {
			return berr
		}
		if !offered {
			fmt.Fprintf(out, "unlock_gifting: %s not offered here and not active — already completed\n", giftUnlockMission) //nolint:errcheck
			return nil
		}
	}

	// Choose (and afford) the items before accepting, so a station with
	// nothing buyable leaves no half-done mission behind.
	if err = c.ViewMarket(ctx, map[string]any{}); err != nil {
		return fmt.Errorf("unlock_gifting: view_market: %w", err)
	}
	ranked := rankTen(c.GetMarketListings())
	if len(ranked) == 0 {
		return fmt.Errorf("unlock_gifting: no item with %d units on offer here", giftUnlockQty)
	}
	if ranked[0].cost >= giftUnlockReward {
		return fmt.Errorf("unlock_gifting: cheapest %d here (%s) costs %.0f, not less than the %d reward",
			giftUnlockQty, ranked[0].item, ranked[0].cost, giftUnlockReward)
	}
	if credits := c.GetState().GetCredits(); ranked[0].cost > credits {
		return fmt.Errorf("unlock_gifting: %d %s costs %.0f, have %.0f", giftUnlockQty, ranked[0].item, ranked[0].cost, credits)
	}

	if instance == "" {
		if err = c.AcceptMission(ctx, giftUnlockMission); err != nil {
			return fmt.Errorf("unlock_gifting: accept: %w", err)
		}
		sleep(ctx, game.SleepQuick)
		if instance, err = giftUnlockActiveInstance(ctx, c, sleep); err != nil {
			return err
		}
		if instance == "" {
			instance = giftUnlockMission // board id doubles as the instance id
		}
	}

	bought, err := giftUnlockBuy(ctx, c, ranked, sleep)
	if err != nil {
		return err
	}

	if err = c.CompleteMission(ctx, instance); err != nil && isMissionIncompleteErr(err) {
		// A buy queued for the next tick may not have landed yet.
		sleep(ctx, game.SleepTick)
		err = c.CompleteMission(ctx, instance)
	}
	if err != nil {
		return fmt.Errorf("unlock_gifting: complete %s: %w", instance, err)
	}
	fmt.Fprintf(out, "unlock_gifting: bought %d %s for ~%.0f; completed %s — gifting unlocked\n", //nolint:errcheck
		giftUnlockQty, bought.item, bought.cost, giftUnlockMission)
	return nil
}

// giftUnlockBuy buys ten of the cheapest affordable item, moving past items
// the exchange lists but refuses to trade (invalid_item).
func giftUnlockBuy(ctx context.Context, c giftUnlockClient, ranked []tenCost, sleep func(context.Context, time.Duration)) (tenCost, error) {
	credits := c.GetState().GetCredits()
	var lastErr error
	for _, cand := range ranked {
		if cand.cost >= giftUnlockReward || cand.cost > credits {
			break // ranked by cost: nothing further is cheaper
		}
		err := c.Buy(ctx, cand.item, giftUnlockQty)
		if err == nil {
			sleep(ctx, game.SleepQuick)
			return cand, nil
		}
		if !isUnbuyableItemErr(err) {
			return tenCost{}, fmt.Errorf("unlock_gifting: buy %d %s: %w", giftUnlockQty, cand.item, err)
		}
		lastErr = err
	}
	return tenCost{}, fmt.Errorf("unlock_gifting: no buyable item under the reward: %w", lastErr)
}

// isMissionIncompleteErr reports the server's refusal to complete a mission
// whose objective has not registered yet.
func isMissionIncompleteErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "mission_incomplete")
}

// isNoMissionServicesErr reports a station too small to run a mission board.
func isNoMissionServicesErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "does not offer mission services")
}

// giftUnlockActiveInstance returns the active instance id of the mission, or
// "" when it is not active.
func giftUnlockActiveInstance(ctx context.Context, c giftUnlockClient, sleep func(context.Context, time.Duration)) (string, error) {
	if err := c.GetActiveMissions(ctx); err != nil {
		return "", fmt.Errorf("unlock_gifting: get_active_missions: %w", err)
	}
	sleep(ctx, game.SleepQuick)
	raw := c.GetRawJSON("active_missions")
	if len(raw) == 0 {
		return "", nil
	}
	var resp serverapi.GetActiveMissionsResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", fmt.Errorf("unlock_gifting: parse active missions: %w", err)
	}
	for _, m := range resp.Missions {
		if m.TemplateID == giftUnlockMission || m.MissionID == giftUnlockMission {
			return m.MissionID, nil
		}
	}
	return "", nil
}

// giftUnlockOffered reports whether the local board offers the mission.
func giftUnlockOffered(ctx context.Context, c giftUnlockClient, sleep func(context.Context, time.Duration)) (bool, error) {
	if err := c.GetMissions(ctx); err != nil {
		return false, fmt.Errorf("unlock_gifting: get_missions: %w", err)
	}
	sleep(ctx, game.SleepQuick)
	raw := c.GetRawJSON("missions")
	if len(raw) == 0 {
		return false, fmt.Errorf("unlock_gifting: get_missions returned no data")
	}
	var resp serverapi.GetMissionsResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return false, fmt.Errorf("unlock_gifting: parse board: %w", err)
	}
	for _, m := range resp.Missions {
		if m.TemplateID == giftUnlockMission || m.MissionID == giftUnlockMission {
			return true, nil
		}
	}
	return false, nil
}

// tenCost is what the first ten units of one item cost on the local book.
type tenCost struct {
	item string
	cost float64
}

// rankTen prices ten units of every item, cheapest first, walking each item's
// sell book from the lowest ask up (a 1-cr ask with 4 units does not make ten
// units cost 10). Items with fewer than ten on offer are left out.
func rankTen(listings []game.MarketListing) []tenCost {
	asks := map[string][]game.MarketListing{}
	for _, l := range listings {
		if l.Type == "sell" && l.Quantity > 0 && l.PricePerUnit > 0 {
			asks[l.ItemID] = append(asks[l.ItemID], l)
		}
	}
	var out []tenCost
	for id, book := range asks {
		sort.Slice(book, func(i, j int) bool { return book[i].PricePerUnit < book[j].PricePerUnit })
		need, c := float64(giftUnlockQty), 0.0
		for _, l := range book {
			take := min(need, l.Quantity)
			c += take * l.PricePerUnit
			if need -= take; need == 0 {
				break
			}
		}
		if need == 0 {
			out = append(out, tenCost{id, c})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].cost != out[j].cost {
			return out[i].cost < out[j].cost
		}
		return out[i].item < out[j].item
	})
	return out
}
