package game

import "strings"

// TradeRoute defines a one-way trade leg: buy ore at one empire, sell at another.
// Routes are grouped in round-trip pairs (outbound + return).
type TradeRoute struct {
	Name             string  `json:"name"`               // Human-readable name
	BuyEmpire        string  `json:"buy_empire"`          // Empire to buy from (lowercase)
	SellEmpire       string  `json:"sell_empire"`         // Empire to sell at (lowercase)
	OreID            string  `json:"ore_id"`              // Item to trade (e.g., "silicon_ore")
	OreName          string  `json:"ore_name"`            // Display name
	ExpectedBuy      float64 `json:"expected_buy"`        // Expected buy price per unit
	ExpectedSell     float64 `json:"expected_sell"`       // Expected sell price per unit
	Priority         int     `json:"priority"`            // Route priority (higher = better)
	ReturnRouteIndex int     `json:"return_route_index"`  // Index of the paired return leg (-1 if none)
}

// Margin returns the expected profit per unit.
func (r TradeRoute) Margin() float64 {
	return r.ExpectedSell - r.ExpectedBuy
}

// knownTradeRoutes is the static table of known profitable inter-empire routes.
// Each round-trip is stored as two entries (outbound + return) linked by ReturnRouteIndex.
var knownTradeRoutes = []TradeRoute{
	// Route 1: Nebula → Crimson silicon (outbound)
	{Name: "Nebula→Crimson Silicon", BuyEmpire: "nebula", SellEmpire: "crimson", OreID: "silicon_ore", OreName: "Silicon", ExpectedBuy: 2, ExpectedSell: 28, Priority: 10, ReturnRouteIndex: 1},
	// Route 1: Crimson → Nebula titanium (return)
	{Name: "Crimson→Nebula Titanium", BuyEmpire: "crimson", SellEmpire: "nebula", OreID: "titanium_ore", OreName: "Titanium", ExpectedBuy: 14, ExpectedSell: 50, Priority: 10, ReturnRouteIndex: 0},

	// Route 2: Nebula → Solarian water ice (outbound)
	{Name: "Nebula→Solarian Water Ice", BuyEmpire: "nebula", SellEmpire: "solarian", OreID: "water_ice", OreName: "Water Ice", ExpectedBuy: 7, ExpectedSell: 32, Priority: 8, ReturnRouteIndex: 3},
	// Route 2: Solarian → Nebula nickel (return)
	{Name: "Solarian→Nebula Nickel", BuyEmpire: "solarian", SellEmpire: "nebula", OreID: "nickel_ore", OreName: "Nickel", ExpectedBuy: 1, ExpectedSell: 5, Priority: 8, ReturnRouteIndex: 2},

	// Route 3: Solarian → Crimson nickel (outbound)
	{Name: "Solarian→Crimson Nickel", BuyEmpire: "solarian", SellEmpire: "crimson", OreID: "nickel_ore", OreName: "Nickel", ExpectedBuy: 1, ExpectedSell: 6, Priority: 9, ReturnRouteIndex: 5},
	// Route 3: Crimson → Solarian titanium (return)
	{Name: "Crimson→Solarian Titanium", BuyEmpire: "crimson", SellEmpire: "solarian", OreID: "titanium_ore", OreName: "Titanium", ExpectedBuy: 14, ExpectedSell: 50, Priority: 9, ReturnRouteIndex: 4},

	// Route 4: Nebula → Crimson iron (outbound)
	{Name: "Nebula→Crimson Iron", BuyEmpire: "nebula", SellEmpire: "crimson", OreID: "iron_ore", OreName: "Iron", ExpectedBuy: 1, ExpectedSell: 4, Priority: 6, ReturnRouteIndex: 7},
	// Route 4: Crimson → Nebula cobalt (return)
	{Name: "Crimson→Nebula Cobalt", BuyEmpire: "crimson", SellEmpire: "nebula", OreID: "cobalt_ore", OreName: "Cobalt", ExpectedBuy: 12, ExpectedSell: 50, Priority: 6, ReturnRouteIndex: 6},

	// Route 5: Voidborn → Solarian palladium (outbound)
	{Name: "Voidborn→Solarian Palladium", BuyEmpire: "voidborn", SellEmpire: "solarian", OreID: "palladium_ore", OreName: "Palladium", ExpectedBuy: 11, ExpectedSell: 100, Priority: 7, ReturnRouteIndex: 9},
	// Route 5: Solarian → Voidborn nickel (return)
	{Name: "Solarian→Voidborn Nickel", BuyEmpire: "solarian", SellEmpire: "voidborn", OreID: "nickel_ore", OreName: "Nickel", ExpectedBuy: 1, ExpectedSell: 4, Priority: 7, ReturnRouteIndex: 8},

	// Route 6: Voidborn → Crimson silicon (outbound)
	{Name: "Voidborn→Crimson Silicon", BuyEmpire: "voidborn", SellEmpire: "crimson", OreID: "silicon_ore", OreName: "Silicon", ExpectedBuy: 1, ExpectedSell: 28, Priority: 8, ReturnRouteIndex: 11},
	// Route 6: Crimson → Voidborn titanium (return)
	{Name: "Crimson→Voidborn Titanium", BuyEmpire: "crimson", SellEmpire: "voidborn", OreID: "titanium_ore", OreName: "Titanium", ExpectedBuy: 14, ExpectedSell: 25, Priority: 8, ReturnRouteIndex: 10},

	// Route 7: Solarian → Nebula nickel (outbound)
	{Name: "Solarian→Nebula Nickel", BuyEmpire: "solarian", SellEmpire: "nebula", OreID: "nickel_ore", OreName: "Nickel", ExpectedBuy: 1, ExpectedSell: 5, Priority: 5, ReturnRouteIndex: 13},
	// Route 7: Nebula → Solarian silicon (return)
	{Name: "Nebula→Solarian Silicon", BuyEmpire: "nebula", SellEmpire: "solarian", OreID: "silicon_ore", OreName: "Silicon", ExpectedBuy: 2, ExpectedSell: 28, Priority: 5, ReturnRouteIndex: 12},
}

// empireToHomeSystem maps empire names to their home system IDs.
var empireToHomeSystem = map[string]string{
	"crimson":  "krynn",
	"nebula":   "haven",
	"solarian": "sol",
	"voidborn": "nexus_prime",
}

// EmpireHomeSystem returns the home system for a given empire name.
func EmpireHomeSystem(empire string) string {
	return empireToHomeSystem[strings.ToLower(empire)]
}

// FindTradeRoutes returns outbound trade routes where the agent's home empire
// is the buy endpoint (i.e., routes that start from the agent's empire).
// Routes are returned sorted by priority (highest first).
func FindTradeRoutes(homeEmpire string) []TradeRoute {
	empire := strings.ToLower(homeEmpire)
	var routes []TradeRoute
	for _, r := range knownTradeRoutes {
		if r.BuyEmpire == empire {
			routes = append(routes, r)
		}
	}

	// Sort by priority descending (simple insertion sort for small slice).
	for i := 1; i < len(routes); i++ {
		for j := i; j > 0 && routes[j].Priority > routes[j-1].Priority; j-- {
			routes[j], routes[j-1] = routes[j-1], routes[j]
		}
	}
	return routes
}

// FindRouteByOreAndTarget finds a specific outbound route by ore ID and target empire.
func FindRouteByOreAndTarget(homeEmpire, oreID, targetEmpire string) (outbound TradeRoute, returnLeg TradeRoute, found bool) {
	empire := strings.ToLower(homeEmpire)
	ore := strings.ToLower(oreID)
	target := strings.ToLower(targetEmpire)

	for _, r := range knownTradeRoutes {
		if r.BuyEmpire == empire && strings.ToLower(r.OreID) == ore && r.SellEmpire == target {
			outbound = r
			if r.ReturnRouteIndex >= 0 && r.ReturnRouteIndex < len(knownTradeRoutes) {
				returnLeg = knownTradeRoutes[r.ReturnRouteIndex]
			}
			return outbound, returnLeg, true
		}
	}
	return TradeRoute{}, TradeRoute{}, false
}

// GetReturnLeg returns the paired return route for a given trade route.
func GetReturnLeg(route TradeRoute) (TradeRoute, bool) {
	if route.ReturnRouteIndex >= 0 && route.ReturnRouteIndex < len(knownTradeRoutes) {
		return knownTradeRoutes[route.ReturnRouteIndex], true
	}
	return TradeRoute{}, false
}
