package main

import (
	"sort"
	"time"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// demandClass labels who is creating the demand for a row.
type demandClass string

const (
	classStation demandClass = "STN"    // Source == "station" (Station Manager)
	classAboveSM demandClass = "PLR>SM" // player order priced above the best station order
	classPlayer  demandClass = "PLR"    // player order, no higher station competitor
)

type demandSort int

const (
	sortByProceeds demandSort = iota // default: FulfillValue desc
	sortByPrice                      // Price desc
	sortByAge                        // most-recently captured first
)

type onlyFilter int

const (
	onlyAll onlyFilter = iota
	onlyFulfillable
	onlyCraftable
)

// demandOptions are the report filters/sort parsed from `demand` flags.
type demandOptions struct {
	item        string
	station     string
	minPrice    float64
	maxAge      time.Duration // 0 = no max-age filter
	stationOnly bool
	only        onlyFilter
	sort        demandSort
	limit       int // 0 = no limit
}

// demandReportRow is one (station, item) demand line in the report.
type demandReportRow struct {
	StationID    string      `json:"station_id"`
	SystemID     string      `json:"system_id"`
	ItemID       string      `json:"item_id"`
	ItemName     string      `json:"item_name"`
	Price        float64     `json:"price"`
	Quantity     float64     `json:"quantity"`
	Class        demandClass `json:"class"`
	OnHand       float64     `json:"on_hand"`
	FulfillQty   float64     `json:"fulfill_qty"`
	FulfillValue float64     `json:"fulfill_value"`
	CanCraft     int         `json:"can_craft"`
	CapturedAt   time.Time   `json:"captured_at"`
	AgeStale     bool        `json:"age_stale"`
}

type demandReport struct {
	Rows         []demandReportRow `json:"rows"`
	TotalFulfill float64           `json:"total_fulfill"`
	Generated    time.Time         `json:"generated"`
}

const demandStaleAfter = 24 * time.Hour // station freshness threshold

// classifyDemand inspects the deep buy orders for one (station, item) and
// returns the headline class, best price, and total demand quantity.
func classifyDemand(orders []knowledge.MarketBuyOrderRow) (demandClass, float64, float64) {
	var bestStation, topPrice, totalQty float64
	var topSource string
	for _, o := range orders {
		if o.PriceEach <= 0 || o.Quantity <= 0 {
			continue
		}
		totalQty += o.Quantity
		if o.Source == "station" && o.PriceEach > bestStation {
			bestStation = o.PriceEach
		}
		if o.PriceEach > topPrice {
			topPrice = o.PriceEach
			topSource = o.Source
		}
	}
	switch {
	case topSource == "station":
		return classStation, topPrice, totalQty
	case bestStation > 0 && topPrice > bestStation:
		return classAboveSM, topPrice, totalQty
	default:
		return classPlayer, topPrice, totalQty
	}
}

type demandAgg struct {
	stationID, systemID, itemID, itemName string
	price, qty                            float64
	class                                 demandClass
	captured                              time.Time
}

// buildDemandReport scores each (station, item) of captured buy orders against
// on-hand inventory and craftability, applies filters, and sorts. Pure:
// callers pass `now` explicitly for testability.
func buildDemandReport(
	deep []knowledge.MarketBuyOrderRow,
	onHand map[string]float64,
	canCraft map[string]int,
	now time.Time,
	opts demandOptions,
) demandReport {
	key := func(s, i string) string { return s + "\x00" + i }
	agg := map[string]*demandAgg{}

	deepByKey := map[string][]knowledge.MarketBuyOrderRow{}
	for _, o := range deep {
		k := key(o.StationID, o.ItemID)
		deepByKey[k] = append(deepByKey[k], o)
	}
	for k, ords := range deepByKey {
		cls, price, qty := classifyDemand(ords)
		a := &demandAgg{stationID: ords[0].StationID, systemID: ords[0].SystemID, itemID: ords[0].ItemID}
		a.class, a.price, a.qty = cls, price, qty
		for _, o := range ords {
			if o.CapturedAt.After(a.captured) {
				a.captured = o.CapturedAt
			}
			if a.itemName == "" {
				a.itemName = o.ItemName
			}
		}
		agg[k] = a
	}

	var rows []demandReportRow
	for _, a := range agg {
		if opts.item != "" && a.itemID != opts.item {
			continue
		}
		if opts.station != "" && a.stationID != opts.station {
			continue
		}
		if a.price < opts.minPrice {
			continue
		}
		if opts.stationOnly && a.class != classStation {
			continue
		}
		if opts.maxAge > 0 && now.Sub(a.captured) > opts.maxAge {
			continue
		}

		onhand := onHand[a.itemID]
		fulfill := onhand
		if fulfill > a.qty {
			fulfill = a.qty
		}
		craft := canCraft[a.itemID]

		switch opts.only {
		case onlyFulfillable:
			if fulfill <= 0 {
				continue
			}
		case onlyCraftable:
			if craft <= 0 {
				continue
			}
		}

		row := demandReportRow{
			StationID: a.stationID, SystemID: a.systemID, ItemID: a.itemID, ItemName: a.itemName,
			Price: a.price, Quantity: a.qty, Class: a.class,
			OnHand: onhand, FulfillQty: fulfill, FulfillValue: fulfill * a.price,
			CanCraft: craft, CapturedAt: a.captured,
			AgeStale: now.Sub(a.captured) > demandStaleAfter,
		}
		rows = append(rows, row)
	}

	sortDemandRows(rows, opts.sort)
	if opts.limit > 0 && len(rows) > opts.limit {
		rows = rows[:opts.limit]
	}
	// TotalFulfill is recomputed from the final (post-limit) rows so the header
	// total matches the visible rows, mirroring sellable's TotalProceeds.
	var total float64
	for _, r := range rows {
		total += r.FulfillValue
	}
	return demandReport{Rows: rows, TotalFulfill: total, Generated: now}
}

// sortDemandRows orders rows by the chosen key with deterministic tie-breakers
// (item_id, station_id) so output and tests are stable despite map iteration.
func sortDemandRows(rows []demandReportRow, mode demandSort) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		switch mode {
		case sortByPrice:
			if a.Price != b.Price {
				return a.Price > b.Price
			}
		case sortByAge:
			if !a.CapturedAt.Equal(b.CapturedAt) {
				return a.CapturedAt.After(b.CapturedAt)
			}
		default: // sortByProceeds
			if a.FulfillValue != b.FulfillValue {
				return a.FulfillValue > b.FulfillValue
			}
		}
		if a.ItemID != b.ItemID {
			return a.ItemID < b.ItemID
		}
		return a.StationID < b.StationID
	})
}
