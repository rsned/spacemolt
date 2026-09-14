package assets

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// TaxEstimate is the forward-looking tax position for one agent.
type TaxEstimate struct {
	AssessedPropertyValue  int64
	PropertyTaxTotal       int64
	IncomeTaxTotal         int64
	TaxableIncomeToDate    int64
	TaxableMarketIncome    int64
	MarketSalesToDate      int64
	MarketCOGSDeducted     int64
	MarketLossCarryforward int64
	TaxPrepaid             int64
	NextAssessmentSeconds  int64
	CollectionActive       bool
	Note                   string
	// SalesTaxRates is kept verbatim. The per-empire rate depends on whether
	// you are a citizen, a foreigner or stateless, and the shape is a map whose
	// keys are empire ids — typing it here would need a migration every time an
	// empire is added.
	SalesTaxRates string
	Ships         []TaxShipValue

	// OutstandingBountyTotal is debt ALREADY owed, summed over empires. It is
	// not the same thing as the *_tax_total fields above, which are the next
	// levy: an agent can afford next week's bill and still be detained today.
	// Paying clears that empire's whole criminal record, so this is not purely
	// a tax figure.
	OutstandingBountyTotal int64
	Bounties               []TaxBounty
	// InactivityExempt distinguishes "owes nothing" from "not being billed".
	InactivityExempt bool
}

// TaxBounty is one empire's outstanding debt against an agent. pay_bounty
// settles a single empire at a time, so this is the payable unit.
type TaxBounty struct {
	Empire string
	Bounty int64
}

// TaxShipValue is one hull's contribution to the assessed property value.
type TaxShipValue struct {
	ShipID string
	Value  int64
}

// TaxEstimateFrom decodes a get_tax_estimate reply.
//
// Returns ok=false for an empty body, which is what a failed or unanswered call
// leaves in the raw cache — capture must not record a zero assessment as though
// the agent genuinely owed nothing.
func TaxEstimateFrom(raw []byte) (TaxEstimate, bool, error) {
	if len(raw) == 0 {
		return TaxEstimate{}, false, nil
	}
	var resp serverapi.GetTaxEstimateResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return TaxEstimate{}, false, fmt.Errorf("assets: decode tax estimate: %w", err)
	}

	t := TaxEstimate{
		AssessedPropertyValue:  resp.AssessedPropertyValue,
		PropertyTaxTotal:       resp.PropertyTaxTotal,
		IncomeTaxTotal:         resp.IncomeTaxTotal,
		TaxableIncomeToDate:    resp.TaxableIncomeToDate,
		TaxableMarketIncome:    resp.TaxableMarketIncome,
		MarketSalesToDate:      resp.MarketSalesToDate,
		MarketCOGSDeducted:     resp.MarketCostOfGoodsDeducted,
		MarketLossCarryforward: resp.MarketLossCarryforward,
		TaxPrepaid:             resp.TaxPrepaid,
		NextAssessmentSeconds:  resp.NextAssessmentApproxSeconds,
		CollectionActive:       resp.TaxCollectionActive,
		Note:                   resp.Note,
	}
	if len(resp.SalesTaxRates) > 0 {
		t.SalesTaxRates = string(resp.SalesTaxRates)
	}
	t.Ships = taxShipsFrom(resp.AssessedPropertyByShip)

	t.InactivityExempt = resp.InactivityExempt
	for _, b := range resp.OutstandingBounties {
		if b.Empire == "" {
			continue
		}
		t.Bounties = append(t.Bounties, TaxBounty{Empire: b.Empire, Bounty: b.Bounty})
		t.OutstandingBountyTotal += b.Bounty
	}
	sortTaxBounties(t.Bounties)

	return t, true, nil
}

// taxShipsFrom unpacks assessed_property_by_ship, which has been seen both as a
// {ship_id: value} map and as a list of objects. Neither shape is documented, so
// both are accepted and anything else yields no ships rather than an error — the
// scalar totals are the load-bearing part and must not be lost to a shape change.
func taxShipsFrom(raw json.RawMessage) []TaxShipValue {
	if len(raw) == 0 {
		return nil
	}

	var asMap map[string]json.Number
	if err := json.Unmarshal(raw, &asMap); err == nil {
		out := make([]TaxShipValue, 0, len(asMap))
		for id, v := range asMap {
			n, _ := v.Int64()
			out = append(out, TaxShipValue{ShipID: id, Value: n})
		}
		sortTaxShips(out)

		return out
	}

	var asList []struct {
		ShipID string      `json:"ship_id"`
		ID     string      `json:"id"`
		Value  json.Number `json:"value"`
	}
	if err := json.Unmarshal(raw, &asList); err == nil {
		out := make([]TaxShipValue, 0, len(asList))
		for _, e := range asList {
			id := e.ShipID
			if id == "" {
				id = e.ID
			}
			if id == "" {
				continue
			}
			n, _ := e.Value.Int64()
			out = append(out, TaxShipValue{ShipID: id, Value: n})
		}
		sortTaxShips(out)

		return out
	}

	return nil
}

// sortTaxShips gives the rows a stable order: highest value first, since the
// question this table answers is which hull to move or sell.
func sortTaxShips(rows []TaxShipValue) {
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0; j-- {
			a, b := rows[j-1], rows[j]
			if a.Value > b.Value || (a.Value == b.Value && a.ShipID <= b.ShipID) {
				break
			}
			rows[j-1], rows[j] = b, a
		}
	}
}

// sortTaxBounties orders by descending debt, ties by empire, so the empire that
// most needs paying is first.
func sortTaxBounties(rows []TaxBounty) {
	slices.SortFunc(rows, func(a, b TaxBounty) int {
		if a.Bounty != b.Bounty {
			return cmp.Compare(b.Bounty, a.Bounty)
		}

		return cmp.Compare(a.Empire, b.Empire)
	})
}
