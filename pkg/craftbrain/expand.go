package craftbrain

import (
	"context"
	"fmt"
	"slices"
	"sort"

	"github.com/rsned/spacemolt/pkg/navigation"
)

// DefaultCraftBase is where hand-crafts are sited. Any docked station will do,
// so the plan names the target's own hub rather than inventing a location.
// It is not a real base id: a Source that can pick a concrete stand-in (e.g.
// the operator's current system) should resolve it in SystemOf, otherwise
// every haul into a hand-craft site degrades to StatusUnknownRoute.
const DefaultCraftBase = "any_docked_station"

// Plan computes the full recursive work to build qty of target.
//
// Items are visited in topological order (consumers before producers), so an
// item's demand is final before its runs are rounded. This is why the expander
// is not a tree walk: a shared intermediate needed 4+4+4 must round once at 12,
// not three times at 4.
func (e *Engine) Plan(ctx context.Context, target string, qty int, opts Options) (*Plan, error) {
	if qty < 1 {
		return nil, fmt.Errorf("quantity must be >= 1, got %d", qty)
	}
	recipes, err := e.src.Recipes(ctx)
	if err != nil {
		return nil, fmt.Errorf("load recipes: %w", err)
	}
	prod := producerIndex(recipes)
	if len(prod[target]) == 0 {
		return nil, fmt.Errorf("no recipe produces %q", target)
	}

	order, dropped := topoOrder(target, prod)

	p := &Plan{
		Target:   target,
		Quantity: qty,
		Leaves:   map[string]MineOrBuy{},
		Surplus:  map[string]int{},
	}
	for _, d := range dropped {
		p.Diagnostics = append(p.Diagnostics, fmt.Sprintf("cycle broken: dropped recipe edge %s", d))
	}
	// A coverage failure degrades the footer but must not kill an otherwise
	// good plan — and it says so out loud rather than implying an empty catalog.
	if cov, err := e.src.Coverage(ctx); err != nil {
		p.Diagnostics = append(p.Diagnostics, fmt.Sprintf("catalog coverage unavailable: %v", err))
	} else {
		p.Coverage = cov
	}

	demand := map[string]int{target: qty}
	// producedBy maps item -> every node id that makes it available. An item
	// can be BOTH hauled in and crafted, so a consumer must depend on all of
	// them; a single id would silently drop the haul edge.
	producedBy := map[string][]string{}
	ids := &idGen{}

	for _, item := range order {
		need := demand[item]
		if need <= 0 {
			continue
		}

		// Subtract stock we already hold, hauling it in if it sits elsewhere.
		rem, hauls, err := e.consumeOnHand(ctx, item, need, DefaultCraftBase, opts, ids)
		if err != nil {
			return nil, err
		}
		p.Nodes = append(p.Nodes, hauls...)
		for _, h := range hauls {
			// inventory.go already zeroes Jumps (and sets StatusUnknownRoute)
			// for a leg whose distance it could not resolve, so this sum is
			// safe as written; the RouteInf guard below is a second line of
			// defense so a future regression there can't silently inflate
			// this footer to a billion jumps again.
			if h.Jumps < navigation.RouteInf {
				p.TotalHaulJumps += h.Jumps
			}
			producedBy[item] = append(producedBy[item], h.ID)
		}
		if rem == 0 {
			continue
		}

		// Raw leaf: report mine-and-buy options; the operator decides.
		if len(prod[item]) == 0 {
			node := Node{
				ID: ids.next("mine"), Kind: KindMine, ItemID: item, Qty: rem, Status: StatusOK,
			}
			p.Nodes = append(p.Nodes, node)
			producedBy[item] = append(producedBy[item], node.ID)
			mb := MineOrBuy{Mineable: true}
			// finditem.Result embeds market.ItemSeller: BestPrice / TotalQty,
			// NOT Price / Quantity.
			if sellers, err := e.src.Buyable(ctx, item, rem); err == nil && len(sellers) > 0 {
				mb.MarketPrice = sellers[0].BestPrice
				mb.MarketQty = sellers[0].TotalQty
				mb.BestStation = sellers[0].StationID
			}
			p.Leaves[item] = mb
			continue
		}

		s, sited, err := e.chooseSiting(ctx, item, rem, prod[item], p.TotalTicks, opts)
		if err != nil {
			return nil, err
		}
		if !sited {
			// facility_only with no known facility: try the market.
			sellers, err := e.src.Buyable(ctx, item, rem)
			if err == nil && len(sellers) > 0 {
				node := Node{ID: ids.next("buy"), Kind: KindBuy, ItemID: item, Qty: rem, Status: StatusOK}
				if sellers[0].StationID != "" {
					node.StationID = sellers[0].StationID
				}
				p.Nodes = append(p.Nodes, node)
				producedBy[item] = append(producedBy[item], node.ID)
				continue
			}
			node := Node{
				ID: ids.next("blocked"), Kind: KindBlocked, ItemID: item, Qty: rem,
				Status: StatusBlocked,
				Reason: "facility_only, no public facility known and no market seller",
			}
			p.Nodes = append(p.Nodes, node)
			producedBy[item] = append(producedBy[item], node.ID)
			continue
		}

		node := Node{
			ID: ids.next("craft"), Kind: KindCraft, ItemID: item, Qty: rem,
			Runs: s.runs, RecipeID: s.recipe.ID, FeeTotal: s.feeTotal,
			TicksEst: s.ticks, Status: s.status, Reason: s.reason,
			StationID: DefaultCraftBase,
		}
		if s.facility != nil {
			node.StationID = s.facility.StationID
			node.FacilityID = s.facility.FacilityID
		}
		p.Nodes = append(p.Nodes, node)
		producedBy[item] = append(producedBy[item], node.ID)
		p.TotalFee += s.feeTotal
		p.TotalTicks += s.ticks

		// Bank rounding surplus and any secondary outputs; later items in the
		// topological order draw on them before crafting more. A facility-sited
		// craft's runs were sized off the facility's own output_per_run
		// (site.go), so surplus must be computed the same way — using the
		// recipe's catalog output here would disagree with runs whenever the
		// two differ.
		perRun := outputPerRun(s.recipe, item)
		if s.facility != nil {
			perRun = s.facility.OutputPerRun
		}
		if made := s.runs * perRun; made > rem {
			p.Surplus[item] += made - rem
		}
		for _, out := range s.recipe.Outputs {
			if out.ItemID == item {
				continue
			}
			p.Surplus[out.ItemID] += s.runs * out.Quantity
		}

		// Push demand to this recipe's inputs, net of banked surplus.
		for _, in := range s.recipe.Inputs {
			want := in.Quantity * s.runs
			if bank := p.Surplus[in.ItemID]; bank > 0 {
				used := min(bank, want)
				p.Surplus[in.ItemID] -= used
				want -= used
			}
			if want > 0 {
				demand[in.ItemID] += want
			}
		}
	}

	// Wire dependencies: a craft depends on every node that makes its inputs
	// available — hauls and crafts and buys alike.
	for i := range p.Nodes {
		n := &p.Nodes[i]
		if n.Kind != KindCraft || n.RecipeID == "" {
			continue
		}
		r := recipes[n.RecipeID]
		var deps []string
		for _, in := range r.Inputs {
			for _, id := range producedBy[in.ItemID] {
				if id != n.ID {
					deps = append(deps, id)
				}
			}
		}
		sort.Strings(deps)
		deps = slices.Compact(deps)
		n.DependsOn = deps
	}

	for item, q := range p.Surplus {
		if q == 0 {
			delete(p.Surplus, item)
		}
	}
	return p, nil
}
