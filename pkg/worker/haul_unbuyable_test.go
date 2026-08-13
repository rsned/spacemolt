package worker

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/market"
)

// serverInvalidItem is the shape the game server actually returns when the market
// cannot resolve an item id — copied from a live hauler log rather than invented,
// because the classifier keys off this text.
// The trailing period is the server's, not ours: this string is quoted exactly as it
// arrives so the classifier is tested against reality (hence the ST1005 waiver).
var serverInvalidItem = errors.New("invalid_item: Unknown item 'reactive_armor_hardener'. Use exact item ID (e.g. 'iron_ore') or full name (e.g. 'Iron Ore').") //nolint:staticcheck

func TestIsUnbuyableItemErrRecognisesTheServerRejection(t *testing.T) {
	if !isUnbuyableItemErr(serverInvalidItem) {
		t.Fatal("the live invalid_item rejection must classify as unbuyable")
	}
}

// TestIsUnbuyableItemErrIgnoresTransientFailures guards the expensive mistake in the
// other direction: blocking an item fleet-wide for a week because one hauler was
// broke or full would silently delete real revenue.
func TestIsUnbuyableItemErrIgnoresTransientFailures(t *testing.T) {
	for _, err := range []error{
		nil,
		errors.New("insufficient credits"),
		errors.New("cargo hold full"),
		errors.New("Another action is already pending (dock)"),
		errors.New("not connected"),
	} {
		if isUnbuyableItemErr(err) {
			t.Fatalf("%v must NOT be treated as an unbuyable item", err)
		}
	}
}

// buyLegDeps builds a hauler standing at the buy station with a spread wide enough to
// clear the gate, so the run reaches the Buy call and nothing else can abort it first.
func buyLegDeps(buyErr error) (*fakeClient, *fakeStore) {
	fc := &fakeClient{
		state: &game.State{
			System:  game.SystemData{ID: "a", Name: "A"},
			Fuel:    100,
			MaxFuel: 100,
			Credits: 1_000_000,
			Ship:    game.Ship{CargoCapacity: 500},
		},
		route:  []game.RouteStep{{SystemID: "a", Name: "A"}},
		buyErr: buyErr,
	}
	f := &fakeStore{
		admitOK: true,
		prices: []market.ItemStationPrice{
			{StationID: "buy-stn", HasSell: true, BestAsk: 10, AskQty: 400},
			{StationID: "sell-stn", HasBuy: true, BestBid: 200, BidQty: 400},
		},
	}
	return fc, f
}

// TestUnbuyableBuyBlocksTheItemInsteadOfReleasingIt is the regression that matters:
// releasing the row returns it to the pool where it re-ranks at the TOP (it prices as
// a fat opportunity precisely because nobody can fill it), so the fleet re-claims and
// re-fails forever. The item must be blocked at the scanner instead.
func TestUnbuyableBuyBlocksTheItemInsteadOfReleasingIt(t *testing.T) {
	o := buyLegOpp()
	fc, f := buyLegDeps(serverInvalidItem)
	_, n2id := graphFor([]string{"a"}, [2]string{"a", "a"})
	var out strings.Builder
	if err := runClaimedHaul(context.Background(), HaulDeps{Client: fc, Market: f, AgentID: "t"}, &out, o, n2id, nil, haulFuel{}, 7); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(f.unbuyable) != 1 || f.unbuyable[0] != o.ItemID {
		t.Fatalf("expected the item blocked once, got %v", f.unbuyable)
	}
	if len(f.released) != 0 {
		t.Fatalf("an unbuyable row must NOT go back in the pool, got releases %v", f.released)
	}
	if len(f.bookClaimsReleased) == 0 {
		t.Fatal("the book-claim cap slot must still be freed")
	}
	if len(f.settled) != 0 {
		t.Fatalf("nothing was bought, so nothing may settle, got %v", f.settled)
	}
}

// TestTransientBuyFailureStillReleasesTheRow proves the new branch did not swallow the
// old one: an ordinary failure is still somebody else's chance.
func TestTransientBuyFailureStillReleasesTheRow(t *testing.T) {
	o := buyLegOpp()
	fc, f := buyLegDeps(errors.New("insufficient credits"))
	_, n2id := graphFor([]string{"a"}, [2]string{"a", "a"})
	if err := runClaimedHaul(context.Background(), HaulDeps{Client: fc, Market: f, AgentID: "t"}, io.Discard, o, n2id, nil, haulFuel{}, 7); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(f.unbuyable) != 0 {
		t.Fatalf("a transient failure must not block the item, got %v", f.unbuyable)
	}
	if len(f.released) != 1 {
		t.Fatalf("a transient failure must return the row to the pool, got %v", f.released)
	}
}
