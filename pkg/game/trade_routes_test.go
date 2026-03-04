package game

import (
	"testing"
)

func TestTradeRouteMargin(t *testing.T) {
	tests := []struct {
		name     string
		route    TradeRoute
		expected float64
	}{
		{
			name:     "positive margin",
			route:    TradeRoute{ExpectedBuy: 2, ExpectedSell: 28},
			expected: 26,
		},
		{
			name:     "zero margin",
			route:    TradeRoute{ExpectedBuy: 10, ExpectedSell: 10},
			expected: 0,
		},
		{
			name:     "negative margin",
			route:    TradeRoute{ExpectedBuy: 50, ExpectedSell: 10},
			expected: -40,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.route.Margin(); got != tt.expected {
				t.Errorf("Margin() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestEmpireHomeSystem(t *testing.T) {
	tests := []struct {
		empire string
		want   string
	}{
		{"crimson", "krynn"},
		{"nebula", "haven"},
		{"solarian", "sol"},
		{"voidborn", "nexus_prime"},
		{"CRIMSON", "krynn"},   // case-insensitive
		{"Nebula", "haven"},    // mixed case
		{"unknown", ""},        // unknown empire
		{"", ""},               // empty
	}

	for _, tt := range tests {
		t.Run(tt.empire, func(t *testing.T) {
			if got := EmpireHomeSystem(tt.empire); got != tt.want {
				t.Errorf("EmpireHomeSystem(%q) = %q, want %q", tt.empire, got, tt.want)
			}
		})
	}
}

func TestFindTradeRoutes(t *testing.T) {
	// Test with a known empire
	routes := FindTradeRoutes("nebula")
	if len(routes) == 0 {
		t.Fatal("expected routes for nebula empire")
	}

	// All routes should have nebula as buy empire
	for _, r := range routes {
		if r.BuyEmpire != "nebula" {
			t.Errorf("route %q has buy empire %q, want %q", r.Name, r.BuyEmpire, "nebula")
		}
	}

	// Routes should be sorted by priority descending
	for i := 1; i < len(routes); i++ {
		if routes[i].Priority > routes[i-1].Priority {
			t.Errorf("routes not sorted by priority: %d > %d at index %d",
				routes[i].Priority, routes[i-1].Priority, i)
		}
	}

	// Test case insensitivity
	routesUpper := FindTradeRoutes("NEBULA")
	if len(routesUpper) != len(routes) {
		t.Errorf("case insensitivity failed: got %d routes, want %d", len(routesUpper), len(routes))
	}

	// Test unknown empire
	unknown := FindTradeRoutes("unknown_empire")
	if len(unknown) != 0 {
		t.Errorf("expected 0 routes for unknown empire, got %d", len(unknown))
	}

	// Test empty empire
	empty := FindTradeRoutes("")
	if len(empty) != 0 {
		t.Errorf("expected 0 routes for empty empire, got %d", len(empty))
	}
}

func TestFindRouteByOreAndTarget(t *testing.T) {
	// Known route
	outbound, returnLeg, found := FindRouteByOreAndTarget("nebula", "silicon_ore", "crimson")
	if !found {
		t.Fatal("expected to find route")
	}
	if outbound.OreID != "silicon_ore" {
		t.Errorf("outbound ore = %q, want %q", outbound.OreID, "silicon_ore")
	}
	if outbound.BuyEmpire != "nebula" {
		t.Errorf("outbound buy empire = %q, want %q", outbound.BuyEmpire, "nebula")
	}
	if outbound.SellEmpire != "crimson" {
		t.Errorf("outbound sell empire = %q, want %q", outbound.SellEmpire, "crimson")
	}
	if returnLeg.BuyEmpire != "crimson" {
		t.Errorf("return leg buy empire = %q, want %q", returnLeg.BuyEmpire, "crimson")
	}

	// Unknown route
	_, _, found = FindRouteByOreAndTarget("nebula", "ore_unobtanium", "crimson")
	if found {
		t.Error("expected not to find route for unknown ore")
	}

	// Case insensitivity
	_, _, found = FindRouteByOreAndTarget("NEBULA", "SILICON_ORE", "CRIMSON")
	if !found {
		t.Error("expected case-insensitive match")
	}

	// Empty params
	_, _, found = FindRouteByOreAndTarget("", "", "")
	if found {
		t.Error("expected not to find route for empty params")
	}
}

func TestGetReturnLeg(t *testing.T) {
	// Valid return leg
	route := knownTradeRoutes[0] // First route should have a valid return index
	returnLeg, ok := GetReturnLeg(route)
	if !ok {
		t.Fatal("expected to find return leg")
	}
	if returnLeg.ReturnRouteIndex != 0 {
		t.Errorf("return leg's return index should point back, got %d", returnLeg.ReturnRouteIndex)
	}

	// Invalid return index
	noReturn := TradeRoute{ReturnRouteIndex: -1}
	_, ok = GetReturnLeg(noReturn)
	if ok {
		t.Error("expected no return leg for index -1")
	}

	// Out of bounds
	outOfBounds := TradeRoute{ReturnRouteIndex: 9999}
	_, ok = GetReturnLeg(outOfBounds)
	if ok {
		t.Error("expected no return leg for out-of-bounds index")
	}
}

func TestKnownTradeRoutes_Integrity(t *testing.T) {
	// Verify all return route indices are valid and point to routes
	// that point back (round-trip consistency)
	for i, route := range knownTradeRoutes {
		if route.ReturnRouteIndex < 0 || route.ReturnRouteIndex >= len(knownTradeRoutes) {
			t.Errorf("route %d (%s) has invalid return index %d", i, route.Name, route.ReturnRouteIndex)
			continue
		}
		returnRoute := knownTradeRoutes[route.ReturnRouteIndex]
		if returnRoute.ReturnRouteIndex != i {
			t.Errorf("route %d (%s) -> return route %d (%s) does not point back (points to %d)",
				i, route.Name, route.ReturnRouteIndex, returnRoute.Name, returnRoute.ReturnRouteIndex)
		}
	}

	// Verify all routes have non-empty required fields
	for i, route := range knownTradeRoutes {
		if route.Name == "" {
			t.Errorf("route %d has empty name", i)
		}
		if route.BuyEmpire == "" {
			t.Errorf("route %d (%s) has empty buy empire", i, route.Name)
		}
		if route.SellEmpire == "" {
			t.Errorf("route %d (%s) has empty sell empire", i, route.Name)
		}
		if route.OreID == "" {
			t.Errorf("route %d (%s) has empty ore ID", i, route.Name)
		}
		if route.Margin() <= 0 {
			t.Errorf("route %d (%s) has non-positive margin: %.2f", i, route.Name, route.Margin())
		}
	}
}
