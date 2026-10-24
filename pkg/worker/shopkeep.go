package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// ShopRule lists one item from a resident's personal storage on the station
// exchange.
//
// It is an explicit ALLOW-LIST entry, never a pattern: a sale is irreversible,
// and reserved goods (energy crystals, fuel cells) live in the same storage.
type ShopRule struct {
	// Item is the exact item id to sell.
	Item string
	// Start is the first ask's price per unit.
	Start int
	// Floor is the lowest price a stale ask is stepped down to.
	Floor int
}

// ParseShopRules parses a comma-separated list of item:start[:floor] rules.
// Start must be at least 1; floor defaults to start/2 and may not exceed
// start. An empty spec yields no rules. A malformed rule is an error, never
// skipped: a typo that parsed to fewer rules would leave goods unlisted, and
// one that parsed to a wrong price would sell them cheap.
func ParseShopRules(s string) ([]ShopRule, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	var rules []ShopRule
	seen := map[string]bool{}
	for raw := range strings.SplitSeq(s, ",") {
		parts := strings.Split(strings.TrimSpace(raw), ":")
		if len(parts) < 2 || len(parts) > 3 {
			return nil, fmt.Errorf("shop rule %q: want item:start[:floor]", raw)
		}
		item := strings.TrimSpace(parts[0])
		if item == "" {
			return nil, fmt.Errorf("shop rule %q: empty item", raw)
		}
		if seen[item] {
			return nil, fmt.Errorf("shop rule %q: item %s listed twice", raw, item)
		}
		start, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil || start < 1 {
			return nil, fmt.Errorf("shop rule %q: start must be an integer >= 1", raw)
		}
		floor := start / 2
		if len(parts) == 3 {
			floor, err = strconv.Atoi(strings.TrimSpace(parts[2]))
			if err != nil || floor < 0 {
				return nil, fmt.Errorf("shop rule %q: floor must be an integer >= 0", raw)
			}
			if floor > start {
				return nil, fmt.Errorf("shop rule %q: floor %d above start %d", raw, floor, start)
			}
		}
		seen[item] = true
		rules = append(rules, ShopRule{Item: item, Start: start, Floor: floor})
	}

	return rules, nil
}

// ShopState is the shopkeeper's memory between passes, persisted per agent.
type ShopState struct {
	Items map[string]ShopItemState
}

// ShopItemState is what the shopkeeper last did for one item.
type ShopItemState struct {
	// Price is the current ask price; it carries across fills.
	Price int
	// OrderID is the open ask's order id: "" when none is outstanding,
	// "pending" when a post's reply carried no id.
	OrderID string
	// PostedAt is when the ask was posted or last repriced.
	PostedAt time.Time
	// LastFillAt is when an ask was last seen to have filled.
	LastFillAt time.Time
}

// shopPending marks a posted ask whose reply named no order id.
const shopPending = "pending"

// LoadShopState reads the state file at path. A missing file is an empty
// state, not an error.
func LoadShopState(path string) (*ShopState, error) {
	st := &ShopState{Items: map[string]ShopItemState{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read shop state: %w", err)
	}
	if err := json.Unmarshal(b, st); err != nil {
		return nil, fmt.Errorf("decode shop state %s: %w", path, err)
	}
	if st.Items == nil {
		st.Items = map[string]ShopItemState{}
	}

	return st, nil
}

// Save writes the state to path as JSON, via a temp file and rename so a
// crash mid-write never leaves a truncated file behind.
func (s *ShopState) Save(path string) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encode shop state: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("shop state dir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return fmt.Errorf("write shop state: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace shop state: %w", err)
	}

	return nil
}

// ShopOptions tunes the shopkeeper. Zero fields take the defaults.
type ShopOptions struct {
	// Lot is the most units one ask lists. Default 50.
	Lot int
	// StepPct is the percent a price moves up after a fill or down when
	// stale. Default 5.
	StepPct int
	// StaleAfter is how long an unfilled ask stands before it is stepped
	// down. Default 6h.
	StaleAfter time.Duration
	// Now is the clock. Default time.Now.
	Now func() time.Time
}

func (o ShopOptions) withDefaults() ShopOptions {
	if o.Lot <= 0 {
		o.Lot = 50
	}
	if o.StepPct <= 0 {
		o.StepPct = 5
	}
	if o.StaleAfter <= 0 {
		o.StaleAfter = 6 * time.Hour
	}
	if o.Now == nil {
		o.Now = time.Now
	}

	return o
}

// Shopkeep keeps one ask open per allow-listed item in this station's
// personal storage, run once per idle pass by a docked resident.
//
// Per rule: an open ask of ours that has stood longer than StaleAfter is
// stepped down by StepPct, never below Floor. With no open ask, a previously
// posted order that has gone is taken as filled and the price steps up by
// StepPct; then, if any units are held, a new ask of up to Lot units is
// posted. The first ever ask is at Start.
//
// It never posts blind. Without a storage reading or a full reading of our
// own orders it does nothing: an unread order book could hide an open ask and
// cause a double listing. Order-command failures are logged and skipped, so a
// pass never fails on a sale; the only error returned is a cancelled context.
func Shopkeep(ctx context.Context, client game.GameClient, rules []ShopRule, st *ShopState,
	opts ShopOptions, out io.Writer,
) (posted, repriced int, err error) {
	if client == nil || st == nil || len(rules) == 0 {
		return 0, 0, nil
	}
	if out == nil {
		out = io.Discard
	}
	opts = opts.withDefaults()
	if st.Items == nil {
		st.Items = map[string]ShopItemState{}
	}
	state := client.GetState()
	if state == nil || !state.Doc || state.Player.Username == "" {
		return 0, 0, nil
	}

	var held map[string]int
	ok := client.ViewStorage(ctx) == nil
	if ok {
		held, ok = consignHeld(client.GetRawJSON("storage"))
	}
	if !ok {
		_, _ = fmt.Fprintln(out, "shopkeep: no storage reading; skipping")
		return 0, 0, ctx.Err()
	}
	asks, ok := shopOwnAsks(ctx, client, state.Player.Username)
	if !ok {
		_, _ = fmt.Fprintln(out, "shopkeep: no order book reading; skipping")
		return 0, 0, ctx.Err()
	}

	now := opts.Now()
	for _, r := range rules {
		if err := ctx.Err(); err != nil {
			return posted, repriced, err
		}
		cur, known := st.Items[r.Item]
		units := held[r.Item]
		ask, open := pickShopAsk(asks[r.Item], cur.OrderID)
		if !known && units < 1 && !open {
			continue
		}

		if open {
			// Adopt what the book says: it is the truth about a pending or
			// hand-posted ask.
			cur.OrderID = ask.OrderID
			if cur.Price <= 0 {
				cur.Price = ask.PriceEach
			}
			if cur.PostedAt.IsZero() {
				cur.PostedAt = now
			}
			if now.Sub(cur.PostedAt) > opts.StaleAfter {
				next := max(r.Floor, cur.Price*(100-opts.StepPct)/100)
				if next < cur.Price {
					if merr := client.ModifyOrder(ctx, map[string]any{
						"order_id": ask.OrderID, "price_each": next,
					}); merr != nil {
						_, _ = fmt.Fprintf(out, "shopkeep: reprice %s order %s failed: %v\n", r.Item, ask.OrderID, merr)
					} else {
						cur.Price = next
						cur.PostedAt = now
						repriced++
						_, _ = fmt.Fprintf(out, "shopkeep: repriced %s @%d order %s\n", r.Item, next, ask.OrderID)
					}
				}
			}
			st.Items[r.Item] = cur
			continue
		}

		if cur.OrderID != "" {
			// A pending post that never showed up may have failed rather
			// than filled, so only a known order's absence steps the price.
			if cur.OrderID != shopPending {
				cur.Price = cur.Price * (100 + opts.StepPct) / 100
				cur.LastFillAt = now
			}
			cur.OrderID = ""
		}
		if cur.Price <= 0 {
			cur.Price = r.Start
		}
		cur.Price = max(cur.Price, r.Floor)

		if units >= 1 {
			qty := min(units, opts.Lot)
			prev := bytes.Clone(client.GetRawJSON("create_sell_order"))
			if cerr := client.CreateSellOrder(ctx, map[string]any{
				"item_id": r.Item, "quantity": qty, "price_each": cur.Price,
			}); cerr != nil {
				_, _ = fmt.Fprintf(out, "shopkeep: post %d %s @%d failed: %v\n", qty, r.Item, cur.Price, cerr)
			} else {
				id := ""
				if reply := client.GetRawJSON("create_sell_order"); !bytes.Equal(reply, prev) {
					id = shopReplyOrderID(reply, r.Item)
				}
				if id == "" {
					id = shopPending
				}
				cur.OrderID = id
				cur.PostedAt = now
				posted++
				_, _ = fmt.Fprintf(out, "shopkeep: posted %d %s @%d order %s\n", qty, r.Item, cur.Price, id)
			}
		}
		st.Items[r.Item] = cur
	}

	return posted, repriced, nil
}

// shopOwnAsks reads our open sell orders at this station, keyed by item. ok is
// false when there is no complete reading: a failed command, an undecodable
// reply, or a truncated page that may have left an ask of ours off it.
//
// view_orders, not view_market: the market book aggregates by price and
// carries no order ids or sellers, so it cannot identify our own ask.
func shopOwnAsks(ctx context.Context, client game.GameClient, username string) (map[string][]serverapi.ExchangeOrder, bool) {
	if err := client.ViewOrders(ctx); err != nil {
		return nil, false
	}
	raw := client.GetRawJSON("orders")
	if len(raw) == 0 {
		return nil, false
	}
	var resp serverapi.ViewOrdersResponse
	if err := json.Unmarshal(raw, &resp); err != nil || resp.HasMore || resp.OrdersTruncated {
		return nil, false
	}
	asks := map[string][]serverapi.ExchangeOrder{}
	for _, o := range resp.Orders {
		isSell := o.Side == "sell" || (o.Side == "" && o.OrderType == "sell")
		// Personal orders may omit created_by; an order naming someone else
		// is not ours.
		mine := o.CreatedBy == "" || o.CreatedBy == username
		// Remaining is omitempty: an untouched order may report 0 there.
		live := o.Remaining > 0 || (o.FilledQuantity == 0 && o.Quantity > 0)
		if isSell && mine && live && !o.FactionOrder && o.ItemID != "" && o.OrderID != "" {
			asks[o.ItemID] = append(asks[o.ItemID], o)
		}
	}

	return asks, true
}

// pickShopAsk chooses the ask to manage: the one the state remembers if it is
// still open, else the first.
func pickShopAsk(asks []serverapi.ExchangeOrder, want string) (serverapi.ExchangeOrder, bool) {
	if len(asks) == 0 {
		return serverapi.ExchangeOrder{}, false
	}
	for _, a := range asks {
		if a.OrderID == want {
			return a, true
		}
	}

	return asks[0], true
}

// shopReplyOrderID reads order_id out of a create_sell_order reply, either the
// action_result wrapper or a bare body. A reply naming a different item is
// not this post's.
func shopReplyOrderID(raw []byte, item string) string {
	var resp serverapi.CreateSellOrderResponse
	if err := json.Unmarshal(unwrapActionResult(raw), &resp); err != nil {
		return ""
	}
	if resp.ItemID != "" && resp.ItemID != item {
		return ""
	}

	return resp.OrderID
}
