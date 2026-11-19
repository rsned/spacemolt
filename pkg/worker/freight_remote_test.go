package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// remoteListing is a posted run from origin to dest, runHops long.
func remoteListing(id, origin, dest string, reward int64, runHops int) serverapi.ShippingListing {
	return serverapi.ShippingListing{
		Eligible:      true,
		DeadlineTicks: 5000,
		Contract: serverapi.ShipmentContract{
			ID: id, OriginBaseID: origin, DestinationBaseID: dest,
			BaseReward: reward, RouteHops: runHops, Status: "posted",
		},
	}
}

func shippingListAllJSON(t *testing.T, listings ...serverapi.ShippingListing) []byte {
	t.Helper()
	b, err := json.Marshal(serverapi.ShippingListResponse{
		Action: "list_all", Shipments: listings, Total: len(listings), Page: 1, PerPage: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// remoteTestInputs places every base by a table of "system:hops"; a base not
// in the table is unroutable.
func remoteTestInputs(places map[string]string) freightRemoteInputs {
	return freightRemoteInputs{
		CargoFree:   500,
		FuelCostFor: noFuel,
		Now:         time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC),
		Strongholds: map[string]bool{"crix": true},
		Locate: func(baseID string) (string, int, bool) {
			p, ok := places[baseID]
			if !ok {
				return "", 0, false
			}
			sys, hops, _ := strings.Cut(p, ":")
			n := 0
			for _, c := range hops {
				n = n*10 + int(c-'0')
			}
			return sys, n, true
		},
	}
}

func remoteClient(t *testing.T, listings ...serverapi.ShippingListing) *fakeClient {
	t.Helper()
	return &fakeClient{
		state: &game.State{},
		raw: map[string][]byte{
			"shipping_profile":  shippingProfileJSON(t, false, ""),
			"shipping_list_all": shippingListAllJSON(t, listings...),
		},
	}
}

// The best run is the one that pays most per jump flown, the empty leg to its
// origin included: a fat reward far away loses to a good one next door.
func TestFreightRemotePicksBestPerJump(t *testing.T) {
	f := remoteClient(t,
		remoteListing("far", "far_base", "dest_a", 20000, 10), // 8 empty + 10 = 1111/jump
		remoteListing("near", "near_base", "dest_a", 9000, 2), // 1 empty + 2  = 3000/jump
		remoteListing("poor", "near_base", "dest_a", 600, 2),  // 200/jump
	)
	in := remoteTestInputs(map[string]string{"far_base": "vega:8", "near_base": "sol:1", "dest_a": "tau:3"})

	got, reason := freightRemoteCandidate(context.Background(), MissionDeps{Client: f, State: &missionRunState{}}, in, io.Discard)
	if got == nil {
		t.Fatalf("want a remote run, got skip: %s", reason)
	}
	if got.ContractID != "near" || got.OriginBaseID != "near_base" || got.EmptyHops != 1 {
		t.Fatalf("want near from near_base at 1 hop, got %+v", got)
	}
}

func TestFreightRemoteSkipsRunsItShouldNotChase(t *testing.T) {
	ineligible := remoteListing("ineligible", "near_base", "dest_a", 90000, 2)
	ineligible.Eligible, ineligible.IneligibleReason = false, "carrier tier below the risk band"
	tight := remoteListing("tight", "near_base", "dest_a", 90000, 10)
	tight.DeadlineTicks = 100 // 10 hops need ~285 ticks
	f := remoteClient(t,
		ineligible,
		tight,
		remoteListing("here", "this_base", "dest_a", 90000, 2),          // this dock: the local pass judged it
		remoteListing("lost", "unknown_base", "dest_a", 90000, 2),       // unroutable origin
		remoteListing("too_far", "edge_base", "dest_a", 90000, 2),       // empty leg over the cap
		remoteListing("from_hold", "crix_base", "dest_a", 90000, 2),     // stronghold origin
		remoteListing("to_hold", "near_base", "crix_base", 90000, 2),    // stronghold destination
		remoteListing("nowhere", "near_base", "unknown_base", 90000, 2), // unroutable destination
		remoteListing("cheap", "near_base", "dest_a", 100, 2),           // below the net floor
	)
	in := remoteTestInputs(map[string]string{
		"this_base": "here:0", "near_base": "sol:1", "dest_a": "tau:3",
		"edge_base": "rim:9", "crix_base": "crix:2",
	})
	var out bytes.Buffer

	got, reason := freightRemoteCandidate(context.Background(), MissionDeps{Client: f, State: &missionRunState{}}, in, &out)
	if got != nil {
		t.Fatalf("want no remote run, got %+v\n%s", got, out.String())
	}
	if reason == "" {
		t.Fatal("a skip must say why")
	}
}

// An origin already flown to this window is not chased again: if the run was
// gone on arrival, re-reading the same listing would send the ship back.
func TestFreightRemoteSkipsOriginChasedRecently(t *testing.T) {
	f := remoteClient(t, remoteListing("near", "near_base", "dest_a", 9000, 2))
	in := remoteTestInputs(map[string]string{"near_base": "sol:1", "dest_a": "tau:3"})
	st := &missionRunState{}
	st.noteRemoteChase("near_base", in.Now.Add(-5*time.Minute))

	if got, _ := freightRemoteCandidate(context.Background(), MissionDeps{Client: f, State: st}, in, io.Discard); got != nil {
		t.Fatalf("want the chased origin skipped, got %+v", got)
	}
	st.noteRemoteChase("near_base", in.Now.Add(-2*missionParkWindow))
	st.remoteScanAt = time.Time{}
	if got, reason := freightRemoteCandidate(context.Background(), MissionDeps{Client: f, State: st}, in, io.Discard); got == nil {
		t.Fatalf("want the origin chased again once the window passes, got skip: %s", reason)
	}
}

// The scan costs several server calls, so a dry worker repeats it on a
// timer, not on every pass.
func TestFreightRemoteScansOnATimer(t *testing.T) {
	f := remoteClient(t) // nothing posted anywhere
	in := remoteTestInputs(nil)
	deps := MissionDeps{Client: f, State: &missionRunState{}}

	freightRemoteCandidate(context.Background(), deps, in, io.Discard)
	first := len(f.shippingCalls)
	if first == 0 {
		t.Fatal("the first dry pass must scan")
	}
	freightRemoteCandidate(context.Background(), deps, in, io.Discard)
	if len(f.shippingCalls) != first {
		t.Fatalf("a second pass inside the window scanned again: %v", f.shippingCalls)
	}
	in.Now = in.Now.Add(2 * freightRemoteScanEvery)
	freightRemoteCandidate(context.Background(), deps, in, io.Discard)
	if len(f.shippingCalls) == first {
		t.Fatal("no rescan after the window passed")
	}
}

// A ship carrying freight, or without room for a package, never scans.
func TestFreightRemoteNeedsAnEmptyChainAndRoom(t *testing.T) {
	f := remoteClient(t, remoteListing("near", "near_base", "dest_a", 9000, 2))
	in := remoteTestInputs(map[string]string{"near_base": "sol:1", "dest_a": "tau:3"})
	in.CargoFree = 40
	if got, _ := freightRemoteCandidate(context.Background(), MissionDeps{Client: f, State: &missionRunState{}}, in, io.Discard); got != nil {
		t.Fatalf("a full hold chased %+v", got)
	}
	in.CargoFree = 500
	in.Held = []chainStop{{ContractID: "held", DestBaseID: "dest_a", Hops: 2}}
	if got, _ := freightRemoteCandidate(context.Background(), MissionDeps{Client: f, State: &missionRunState{}}, in, io.Discard); got != nil {
		t.Fatalf("a ship holding freight chased %+v", got)
	}
	if len(f.shippingCalls) != 0 {
		t.Fatalf("neither case may talk to /shipping: %v", f.shippingCalls)
	}
}

// A dry pass with a remote run to chase flies to its origin instead of
// counting dry; the next pass reads that station's own board.
func TestMissionFreightOrDryChasesRemoteRun(t *testing.T) {
	f := remoteClient(t, remoteListing("near", "near_base", "dest_a", 9000, 2))
	f.state = missionState(true, 5000, 0)
	f.route = []game.RouteStep{{SystemID: "sol", Jumps: 1}}
	var navTo []string
	st := &missionRunState{}
	deps := MissionDeps{
		Client: f, State: st, EnableFreight: true, AgentID: "fighter-4",
		nav: func(_ context.Context, system, poi string, _ map[string]bool) error {
			navTo = append(navTo, system+"/"+poi)
			return nil
		},
	}
	var out bytes.Buffer

	err := missionFreightOrDry(context.Background(), deps, nil, nil, nil, func() func(int) float64 { return noFuel }, map[string]bool{}, &out)
	if err != nil {
		t.Fatalf("pass failed: %v", err)
	}
	if len(navTo) != 1 || navTo[0] != "sol/near_base" {
		t.Fatalf("want one flight to sol/near_base, got %v\n%s", navTo, out.String())
	}
	if st.dry != 0 {
		t.Fatalf("a chase is not a dry pass, dry = %d", st.dry)
	}
}

// A worker pinned to a station keeps to its own board.
func TestMissionFreightOrDryPinnedWorkerDoesNotChase(t *testing.T) {
	f := remoteClient(t, remoteListing("near", "near_base", "dest_a", 9000, 2))
	f.state = missionState(true, 5000, 0)
	f.route = []game.RouteStep{{SystemID: "sol", Jumps: 1}}
	st := &missionRunState{}
	deps := MissionDeps{
		Client: f, State: st, EnableFreight: true, AgentID: "fighter-2", HomeStation: "treasure_cache_trading_post",
		nav: func(context.Context, string, string, map[string]bool) error {
			t.Fatal("a pinned worker flew off after remote freight")
			return nil
		},
	}

	_ = missionFreightOrDry(context.Background(), deps, nil, nil, nil, func() func(int) float64 { return noFuel }, map[string]bool{}, io.Discard)
	if len(f.shippingCalls) != 0 {
		t.Fatalf("a pinned worker scanned the remote boards: %v", f.shippingCalls)
	}
}

// When nothing is worth the flight the reason says what ruled the board out,
// so one log line shows whether the cap, the floor or the tier is the limit.
func TestFreightRemoteSaysWhyNothingWasWorthIt(t *testing.T) {
	f := remoteClient(t,
		remoteListing("too_far", "edge_base", "dest_a", 90000, 2),
		remoteListing("too_far_2", "edge_base", "dest_a", 90000, 2),
		remoteListing("cheap", "near_base", "dest_a", 100, 2),
	)
	in := remoteTestInputs(map[string]string{"near_base": "sol:1", "dest_a": "tau:3", "edge_base": "rim:9"})

	_, reason := freightRemoteCandidate(context.Background(), MissionDeps{Client: f, State: &missionRunState{}}, in, io.Discard)
	for _, want := range []string{"2 origin over 8 jumps away", "1 below the net floor"} {
		if !strings.Contains(reason, want) {
			t.Errorf("reason %q does not say %q", reason, want)
		}
	}
}

// A dry worker repeats the pass every few seconds; the same reason for not
// chasing is logged once, not every time.
func TestFreightChaseRemoteLogsAReasonOnce(t *testing.T) {
	f := remoteClient(t)
	f.state = missionState(true, 5000, 60) // 40 free: no room for a package
	deps := MissionDeps{Client: f, State: &missionRunState{}, EnableFreight: true, AgentID: "engineer-2"}
	var out bytes.Buffer
	for range 3 {
		freightChaseRemote(context.Background(), deps, nil, nil, &out)
	}
	if n := strings.Count(out.String(), "no remote run to chase"); n != 1 {
		t.Fatalf("want the reason logged once, got %d:\n%s", n, out.String())
	}
}
