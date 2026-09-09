package worker

import (
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

// A locked agent must be stopped by any stronghold on the route, including one
// it merely passes THROUGH — the losses that motivated this gate were transit
// kills, not destination kills.
func TestStrongholdsOnRoute(t *testing.T) {
	strongholds := map[string]bool{
		"algol": true, "Algol": true,
		"zaniah": true, "Zaniah": true,
	}
	route := []game.RouteStep{
		{SystemID: "sirius", Name: "Sirius"},
		{SystemID: "algol", Name: "Algol"},
		{SystemID: "nova_terra", Name: "Nova Terra"},
	}

	got := strongholdsOnRoute(route, strongholds)
	if len(got) != 1 || got[0] != "Algol" {
		t.Fatalf("strongholdsOnRoute = %v, want [Algol]", got)
	}
}

// An UNLOCKED agent gets an empty ref set, and the gate must then be a complete
// no-op — stronghold routes are the richest on the board and haul.go already
// depends on being allowed to work them.
func TestStrongholdsOnRouteEmptyRefsIsNoOp(t *testing.T) {
	route := []game.RouteStep{{SystemID: "algol", Name: "Algol"}}
	if got := strongholdsOnRoute(route, nil); got != nil {
		t.Fatalf("strongholdsOnRoute with no refs = %v, want nil", got)
	}
}

// Strongholds are dual-named (base id vs poi/system id), so a ref set that
// knows only one spelling must still catch the step. See the station-id-alias
// trap: matching on id alone silently lets the route through.
func TestStrongholdsOnRouteMatchesNameOnlyRef(t *testing.T) {
	route := []game.RouteStep{{SystemID: "unknown_id_form", Name: "Zaniah"}}
	got := strongholdsOnRoute(route, map[string]bool{"Zaniah": true})
	if len(got) != 1 {
		t.Fatalf("strongholdsOnRoute = %v, want the name-only match", got)
	}
}

// Every stronghold on the route is reported, not just the first, so the log
// line names the whole problem rather than one hop of it.
func TestStrongholdsOnRouteReportsAll(t *testing.T) {
	route := []game.RouteStep{
		{SystemID: "algol", Name: "Algol"},
		{SystemID: "safe", Name: "Safe"},
		{SystemID: "zaniah", Name: "Zaniah"},
	}
	got := strongholdsOnRoute(route, map[string]bool{"algol": true, "zaniah": true})
	if len(got) != 2 {
		t.Fatalf("strongholdsOnRoute = %v, want 2 entries", got)
	}
}

// A step is reported once even when BOTH its id and name are in the ref set,
// which is the normal case because buildStrongholdRefs registers both.
func TestStrongholdsOnRouteNoDuplicatePerStep(t *testing.T) {
	route := []game.RouteStep{{SystemID: "algol", Name: "Algol"}}
	got := strongholdsOnRoute(route, map[string]bool{"algol": true, "Algol": true})
	if len(got) != 1 {
		t.Fatalf("strongholdsOnRoute = %v, want exactly one entry", got)
	}
}

// The error must name the blocking systems: an operator reading a worker log
// needs to know WHICH hop was refused without re-deriving the route.
func TestErrRouteThroughStrongholdMessage(t *testing.T) {
	err := routeStrongholdError("Algol", []string{"Zaniah"})
	if !strings.Contains(err.Error(), "Zaniah") {
		t.Fatalf("error %q does not name the blocking system", err)
	}
	if !strings.Contains(err.Error(), "Algol") {
		t.Fatalf("error %q does not name the destination", err)
	}
}
