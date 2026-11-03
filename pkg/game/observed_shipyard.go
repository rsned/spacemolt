package game

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// ObservedShipyard is one shipyard reply (commission quote, commission, or
// license purchase) extracted from a server payload. It feeds the hull
// production-cost knowledge base.
type ObservedShipyard struct {
	Kind                  string // "quote" | "commission" | "license"
	ShipClass, Empire     string
	Labor                 int
	MaterialCost          int
	ProvideMaterialsTotal int
	YardMargin            int
	ShipyardTierHere      int
	CostPaid              int
	Royalty               *int
	BaseID, FactionID     string // from state: Player.DockedAtBase, Player.FactionID
	Tick                  int64
	SeenAt                time.Time
	Raw                   []byte // JSON of the matched object
}

// ShipyardObserver receives shipyard observations. Implementations must not
// block; it is invoked from the response-handling goroutine.
type ShipyardObserver func(ObservedShipyard)

// SetShipyardObserver registers a callback fired for shipyard replies.
func (c *Client) SetShipyardObserver(fn ShipyardObserver) {
	c.shipyardObserverMu.Lock()
	defer c.shipyardObserverMu.Unlock()
	c.shipyardObserver = fn
}

// ShipyardObserver returns the registered observer (nil if none).
func (c *Client) ShipyardObserver() ShipyardObserver {
	c.shipyardObserverMu.RLock()
	defer c.shipyardObserverMu.RUnlock()
	return c.shipyardObserver
}

var royaltyRE = regexp.MustCompile(`([\d,]+) royalty`)

// parseRoyalty returns the number from the first "<n> royalty" match across
// texts, or nil when none match.
func parseRoyalty(texts ...string) *int {
	for _, t := range texts {
		if m := royaltyRE.FindStringSubmatch(t); m != nil {
			if n, err := strconv.Atoi(strings.ReplaceAll(m[1], ",", "")); err == nil {
				return &n
			}
		}
	}
	return nil
}

// notifyShipyardFromPayload detects commission_quote, commission_ship and
// buy_ship_license replies (flat, or wrapped in payload["result"]) and
// dispatches them to the registered observer. No-op when none is registered.
func (c *Client) notifyShipyardFromPayload(payload map[string]any) {
	cb := c.ShipyardObserver()
	if cb == nil {
		return
	}
	candidates := []map[string]any{payload}
	if inner, ok := payload["result"].(map[string]any); ok {
		candidates = append(candidates, inner)
	}
	for _, m := range candidates {
		if obs, ok := c.buildShipyardObservation(m); ok {
			cb(obs)
			return
		}
	}
}

func (c *Client) buildShipyardObservation(m map[string]any) (ObservedShipyard, bool) {
	_, hasClass := m["ship_class"]
	if !hasClass {
		return ObservedShipyard{}, false
	}
	var obs ObservedShipyard
	switch {
	case hasKey(m, "can_commission"):
		var q serverapi.CommissionQuoteResponse
		if !remarshal(m, &q) {
			return ObservedShipyard{}, false
		}
		obs = ObservedShipyard{
			Kind: "quote", ShipClass: q.ShipClass, Labor: q.LaborCost, MaterialCost: q.MaterialCost,
			ProvideMaterialsTotal: q.ProvideMaterialsTotal, ShipyardTierHere: q.ShipyardTierHere,
			Royalty: parseRoyalty(append(append([]string{}, q.Blockers...), q.Message)...),
		}
		if f, ok := m["yard_margin"].(float64); ok {
			obs.YardMargin = int(f)
		}
	case hasKey(m, "commission_id"):
		var s serverapi.CommissionShipResponse
		if !remarshal(m, &s) {
			return ObservedShipyard{}, false
		}
		obs = ObservedShipyard{
			Kind: "commission", ShipClass: s.ShipClass, Labor: s.LaborCost, MaterialCost: s.MaterialCost,
			Royalty: parseRoyalty(s.Message),
		}
	case hasKey(m, "cost_paid") && hasKey(m, "royalty_percent"):
		var l serverapi.ShipLicenseResponse
		if !remarshal(m, &l) {
			return ObservedShipyard{}, false
		}
		obs = ObservedShipyard{Kind: "license", ShipClass: l.ShipClass, Empire: l.Empire, CostPaid: l.CostPaid}
	default:
		return ObservedShipyard{}, false
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return ObservedShipyard{}, false
	}
	obs.Raw = raw
	st := c.GetState()
	obs.BaseID = st.Player.DockedAtBase
	obs.FactionID = st.Player.FactionID
	obs.Tick = c.currentTick()
	obs.SeenAt = time.Now().UTC()
	return obs, true
}

func hasKey(m map[string]any, k string) bool {
	_, ok := m[k]
	return ok
}

func remarshal(m map[string]any, dest any) bool {
	b, err := json.Marshal(m)
	return err == nil && json.Unmarshal(b, dest) == nil
}
