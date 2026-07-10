package craftbrain

import (
	"context"
	"fmt"
	"sort"

	"github.com/rsned/spacemolt/pkg/navigation"
)

// idGen hands out stable, readable node IDs: "haul-1", "craft-2", ...
type idGen struct{ n int }

func (g *idGen) next(prefix string) string {
	g.n++
	return fmt.Sprintf("%s-%d", prefix, g.n)
}

// consumeOnHand subtracts stock the fleet already holds from need, drawing
// nearest-first so the plan hauls the shortest distance. Stock already at
// destBase costs nothing and emits no node; remote stock emits one haul node
// per source base, attributed to its holder so Executor B knows whom to ask.
//
// Holdings older than opts.MaxStockAge are still consumed — refusing them
// would overstate the work — but their node is tagged StatusStale.
func (e *Engine) consumeOnHand(ctx context.Context, itemID string, need int, destBase string, opts Options, ids *idGen) (int, []Node, error) {
	if need <= 0 {
		return 0, nil, nil
	}
	holdings, err := e.src.OnHand(ctx, itemID)
	if err != nil {
		return need, nil, fmt.Errorf("on-hand %s: %w", itemID, err)
	}
	if len(holdings) == 0 {
		return need, nil, nil
	}

	destSys, err := e.src.SystemOf(ctx, destBase)
	if err != nil {
		return need, nil, fmt.Errorf("system of %s: %w", destBase, err)
	}

	// Resolve each holding's system so we can rank by jumps.
	type cand struct {
		h     Holding
		jumps int
	}
	systems := make([]string, 0, len(holdings))
	sysOf := make(map[string]string, len(holdings))
	for _, h := range holdings {
		s, err := e.src.SystemOf(ctx, h.BaseID)
		if err != nil {
			return need, nil, fmt.Errorf("system of %s: %w", h.BaseID, err)
		}
		sysOf[h.BaseID] = s
		if h.BaseID != destBase {
			systems = append(systems, s)
		}
	}
	dist := map[string]int{}
	if len(systems) > 0 {
		dist, err = e.src.Jumps(ctx, destSys, systems)
		if err != nil {
			return need, nil, fmt.Errorf("jumps from %s: %w", destSys, err)
		}
	}

	cands := make([]cand, 0, len(holdings))
	for _, h := range holdings {
		j := 0
		if h.BaseID != destBase {
			var ok bool
			if j, ok = dist[sysOf[h.BaseID]]; !ok {
				j = navigation.RouteInf
			}
		}
		cands = append(cands, cand{h: h, jumps: j})
	}
	// Nearest first; ties broken by base id for determinism.
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].jumps != cands[j].jumps {
			return cands[i].jumps < cands[j].jumps
		}
		return cands[i].h.BaseID < cands[j].h.BaseID
	})

	now := opts.now()
	var nodes []Node
	for _, c := range cands {
		if need == 0 {
			break
		}
		take := min(c.h.Qty, need)
		if take <= 0 {
			continue
		}
		need -= take
		if c.h.BaseID == destBase {
			continue // already where it is needed
		}
		status := StatusOK
		reason := ""
		if now.Sub(c.h.CapturedAt) > opts.MaxStockAge {
			status = StatusStale
			reason = fmt.Sprintf("stock last seen %s", c.h.CapturedAt.Format("2006-01-02T15:04Z"))
		}
		nodes = append(nodes, Node{
			ID:       ids.next("haul"),
			Kind:     KindHaul,
			ItemID:   itemID,
			Qty:      take,
			Holder:   c.h.Holder,
			FromBase: c.h.BaseID,
			ToBase:   destBase,
			Jumps:    c.jumps,
			Status:   status,
			Reason:   reason,
		})
	}
	return need, nodes, nil
}
