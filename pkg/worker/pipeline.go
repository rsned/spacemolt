package worker

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Pipeline pins a worker to one mine-and-hand-off route: mine Item at POI in
// SystemID, then hand the whole Item line plus any byproduct line the trip grew to Recipient
// at To.
type Pipeline struct {
	Item, SystemID, POI, To, Recipient string
}

// ParsePipeline parses the --pipeline flag, "item,system,poi,to,recipient".
// All five fields must be non-empty. The empty string means "no pipeline" and
// returns nil, nil.
func ParsePipeline(s string) (*Pipeline, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	if len(parts) != 5 {
		return nil, fmt.Errorf("pipeline %q: want item,system,poi,to,recipient (5 fields), got %d", s, len(parts))
	}
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
		if parts[i] == "" {
			return nil, fmt.Errorf("pipeline %q: field %d is empty", s, i+1)
		}
	}
	return &Pipeline{Item: parts[0], SystemID: parts[1], POI: parts[2], To: parts[3], Recipient: parts[4]}, nil
}

// PipelineMine runs one pinned mine-and-hand-off pass: it targets the ship's
// free hold (min 1) of the configured item and calls MineQtyAt.
//
// MineQtyAt returns nil even when only a byproduct was delivered, so a nil
// return here does not mean the target quantity landed.
func (d *WorkerDispatch) PipelineMine(ctx context.Context) error {
	if d.Pipeline == nil {
		return errors.New("pipeline_mine: no --pipeline configured")
	}
	qty := 1
	if st := d.Client.GetState(); st != nil {
		qty = max(int(st.Ship.CargoCapacity-st.Ship.CargoUsed), 1)
	}
	p := d.Pipeline
	return d.MineQtyAt(ctx, p.Item, qty, p.SystemID, p.POI, p.To, p.Recipient)
}
