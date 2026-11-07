package worker

import (
	"context"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
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

func strongholdKB(t *testing.T) knowledge.Base {
	t.Helper()
	kb := knowledge.NewMemoryKB()
	t.Cleanup(func() { _ = kb.Close() })
	if err := kb.RememberSystem(context.Background(), knowledge.System{ID: "barnard_44", Name: "Barnard 44", IsStronghold: true}); err != nil {
		t.Fatal(err)
	}

	return kb
}

// Live 2026-10-13 and 2026-11-06, craftsman-1: an unlocked agent whose session
// had not read its standings yet was refused for a missing unlock.
func TestStrongholdRefsReadsUnreadStandings(t *testing.T) {
	f := &fakeClient{state: &game.State{}}
	f.onGetStatus = func() {
		f.state.Player.Standings = map[string]game.EmpireStanding{"pirate_voss": {Baseline: pirateUnlockBaseline}}
	}

	if refs := strongholdRefsForRoute(context.Background(), strongholdKB(t), f, io.Discard); len(refs) != 0 {
		t.Fatalf("an unlocked agent must not be blocked: %v", refs)
	}
}

// Standings that stay unreadable still block: guessing unlocked costs the ship.
func TestStrongholdRefsFailsClosedWhenStandingsStayUnread(t *testing.T) {
	f := &fakeClient{state: &game.State{}}

	if refs := strongholdRefsForRoute(context.Background(), strongholdKB(t), f, io.Discard); !refs["barnard_44"] {
		t.Fatalf("want barnard_44 blocked: %v", refs)
	}
}

// A locked agent whose standings are known costs no extra server call.
func TestStrongholdRefsTrustsKnownStandings(t *testing.T) {
	st := &game.State{}
	st.Player.Standings = map[string]game.EmpireStanding{"pirate_voss": {Baseline: 0}}
	f := &fakeClient{state: st}

	refs := strongholdRefsForRoute(context.Background(), strongholdKB(t), f, io.Discard)
	if !refs["barnard_44"] {
		t.Fatalf("want barnard_44 blocked: %v", refs)
	}
	if slices.Contains(f.calls, "get_status") {
		t.Fatalf("known standings need no refresh: %v", f.calls)
	}
}
