package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// Remote freight (v0.724.0): `shipping action=list_all` reads the board of
// every charted station from one dock, so a carrier with nothing to do here
// can fly to where a run is posted. A contract is still accepted at its
// origin, so this file only chooses where to go; the ordinary docked pass at
// the origin does the accepting, with every check it already has.
const (
	// freightRemoteMaxEmptyHops caps the unpaid leg to a run's origin. The
	// listing can be taken by another carrier while we fly, and the longer
	// the flight the likelier that is.
	freightRemoteMaxEmptyHops = 8

	// freightRemoteMaxPages bounds the board read (20 listings a page).
	freightRemoteMaxPages = 5

	// freightRemoteScanEvery spaces the scans of a worker that stays dry: one
	// scan is a profile read, up to freightRemoteMaxPages board pages and a
	// route lookup per station named.
	freightRemoteScanEvery = 10 * time.Minute
)

// freightRemote is a posted run worth flying to.
type freightRemote struct {
	ContractID   string
	OriginBaseID string
	OriginSystem string
	EmptyHops    int // from here to the origin, unpaid
	RunHops      int // origin to destination
	Net          float64
	PerJump      float64 // Net over every jump flown, the empty leg included
}

// freightRemoteInputs are the per-pass facts freightRemoteCandidate needs.
type freightRemoteInputs struct {
	CargoFree   float64
	FuelCostFor func(jumps int) float64
	// Held is the chain already aboard; a ship carrying freight does not go
	// looking for more elsewhere.
	Held []chainStop
	// Locate resolves a base to its system and the jumps to it from here;
	// ok=false is unroutable.
	Locate func(baseID string) (system string, hops int, ok bool)
	// Strongholds are the systems this ship must not fly to.
	Strongholds map[string]bool
	Now         time.Time
}

// noteRemoteChase records a flight to origin for remote freight.
func (s *missionRunState) noteRemoteChase(origin string, at time.Time) {
	if s == nil {
		return
	}
	if s.remoteChased == nil {
		s.remoteChased = make(map[string]time.Time)
	}
	s.remoteChased[origin] = at
}

// remoteChasedRecently reports whether origin was flown to inside the window.
func (s *missionRunState) remoteChasedRecently(origin string, now time.Time) bool {
	if s == nil {
		return false
	}
	at, ok := s.remoteChased[origin]
	return ok && now.Sub(at) < missionParkWindow
}

// freightRemoteCandidate reads every charted board and returns the run that
// pays most per jump flown, or nil and why not. Like freightCandidate, a
// failure is only ever "nothing to chase".
func freightRemoteCandidate(ctx context.Context, deps MissionDeps, in freightRemoteInputs, _ io.Writer) (*freightRemote, string) {
	if freightPackagesFit(in.CargoFree) < 1 {
		return nil, fmt.Sprintf("hold has %.0f free, a package needs %.0f", in.CargoFree, freightPackageFootprint)
	}
	if len(in.Held) > 0 {
		return nil, fmt.Sprintf("carrying %d package(s)", len(in.Held))
	}
	if deps.State != nil {
		if !deps.State.remoteScanAt.IsZero() && in.Now.Sub(deps.State.remoteScanAt) < freightRemoteScanEvery {
			return nil, "remote boards read recently"
		}
		deps.State.remoteScanAt = in.Now
	}

	if err := deps.Client.ShippingProfile(ctx); err != nil {
		return nil, fmt.Sprintf("shipping profile: %v", err)
	}
	var prof serverapi.ShippingProfileResponse
	if raw := deps.Client.GetRawJSON("shipping_profile"); len(raw) > 0 {
		if err := json.Unmarshal(raw, &prof); err != nil {
			return nil, fmt.Sprintf("decode shipping profile: %v", err)
		}
	}
	if prof.DebtBlocksAcceptance {
		return nil, "freight debt blocks acceptance"
	}
	if prof.Profile.ActiveContracts > 0 {
		return nil, fmt.Sprintf("%d active contract(s) unaccounted for", prof.Profile.ActiveContracts)
	}

	listings, err := freightRemoteBoard(ctx, deps)
	if err != nil {
		return nil, err.Error()
	}
	if len(listings) == 0 {
		return nil, "no freight posted at any charted station"
	}

	floor := effectiveFreightFloor(prof.Progression.CurrentTier, deps.FreightBootstrap, deps.State.bootstrapSpent(), freightProbationBudget)
	type place struct {
		system string
		hops   int
		ok     bool
	}
	places := map[string]place{}
	locate := func(baseID string) place {
		if p, seen := places[baseID]; seen {
			return p
		}
		p := place{}
		if in.Locate != nil && baseID != "" {
			p.system, p.hops, p.ok = in.Locate(baseID)
		}
		places[baseID] = p
		return p
	}

	var best *freightRemote
	// Why each run was passed over, tallied: a line per run would be eighty
	// lines a scan.
	passed := map[string]int{}
	for _, l := range listings {
		c := l.Contract
		if !l.Eligible {
			passed["not eligible"]++
			continue
		}
		if !prof.Capacity.LiabilityUnlimited && prof.Capacity.AggregateLiabilityLimit > 0 &&
			c.ReservedExposure > prof.Capacity.RemainingAggregateLiability {
			passed["over the liability limit"]++
			continue
		}
		if c.RouteHops <= 0 || c.DestinationBaseID == "" {
			passed["no route length given"]++
			continue
		}
		if deps.State.remoteChasedRecently(c.OriginBaseID, in.Now) {
			passed["origin already flown to this window"]++
			continue
		}
		origin := locate(c.OriginBaseID)
		if !origin.ok {
			// Unroutable, or this dock: the router returns no route to the
			// station we are standing in, and the local pass judged its board.
			passed["origin here or unroutable"]++
			continue
		}
		if origin.hops == 0 {
			passed["origin here or unroutable"]++
			continue
		}
		if origin.hops > freightRemoteMaxEmptyHops {
			passed[fmt.Sprintf("origin over %d jumps away", freightRemoteMaxEmptyHops)]++
			continue
		}
		if in.Strongholds[origin.system] {
			passed["origin is a pirate stronghold"]++
			continue
		}
		if l.DeadlineTicks > 0 && float64(c.RouteHops)*freightTicksPerHop*freightDeadlineSlack > float64(l.DeadlineTicks) {
			passed["deadline too tight"]++
			continue
		}
		if freightExpiresBefore(c.ListingExpiresAt, in.Now, origin.hops) {
			passed["listing expires before arrival"]++
			continue
		}
		fuel := 0.0
		if in.FuelCostFor != nil {
			fuel = in.FuelCostFor(origin.hops + c.RouteHops)
		}
		net := float64(c.BaseReward) - fuel
		if net < floor {
			passed["below the net floor"]++
			continue
		}
		perJump := net / float64(origin.hops+c.RouteHops)
		if best != nil && perJump <= best.PerJump {
			continue
		}
		// The destination is routed last: it costs a server lookup, and only
		// a run that would become the best needs one.
		dest := locate(c.DestinationBaseID)
		if !dest.ok {
			passed["destination unroutable"]++
			continue
		}
		if in.Strongholds[dest.system] {
			passed["destination is a pirate stronghold"]++
			continue
		}
		best = &freightRemote{
			ContractID: c.ID, OriginBaseID: c.OriginBaseID, OriginSystem: origin.system,
			EmptyHops: origin.hops, RunHops: c.RouteHops, Net: net, PerJump: perJump,
		}
	}
	if best == nil {
		why := make([]string, 0, len(passed))
		for _, reason := range slices.Sorted(maps.Keys(passed)) {
			why = append(why, fmt.Sprintf("%d %s", passed[reason], reason))
		}
		return nil, fmt.Sprintf("none of %d remote run(s) is worth the flight: %s", len(listings), strings.Join(why, ", "))
	}
	return best, ""
}

// freightRemoteBoard pages through action=list_all.
func freightRemoteBoard(ctx context.Context, deps MissionDeps) ([]serverapi.ShippingListing, error) {
	var all []serverapi.ShippingListing
	for page := 1; page <= freightRemoteMaxPages; page++ {
		if err := deps.Client.Shipping(ctx, "list_all", map[string]any{"page": page}); err != nil {
			return nil, fmt.Errorf("shipping list_all: %w", err)
		}
		var board serverapi.ShippingListResponse
		if raw := deps.Client.GetRawJSON("shipping_list_all"); len(raw) > 0 {
			if err := json.Unmarshal(raw, &board); err != nil {
				return nil, fmt.Errorf("decode shipping list_all: %w", err)
			}
		}
		all = append(all, board.Shipments...)
		if len(board.Shipments) == 0 || len(all) >= board.Total {
			break
		}
	}
	return all, nil
}

// freightExpiresBefore reports whether a listing lapses before a ship
// emptyHops away could reach it. An unreadable expiry is not a reason to skip.
func freightExpiresBefore(expiresAt string, now time.Time, emptyHops int) bool {
	if expiresAt == "" {
		return false
	}
	at, err := time.Parse(time.RFC3339Nano, expiresAt)
	if err != nil {
		return false
	}
	flight := time.Duration(float64(emptyHops)*freightTicksPerHop*freightDeadlineSlack) * game.SleepTick
	return at.Before(now.Add(flight))
}

// freightChaseRemote flies a dry, unpinned freight worker to the origin of the
// best remote run. It reports whether the ship was sent: the caller then ends
// the pass, and the next one reads that station's own board.
func freightChaseRemote(ctx context.Context, deps MissionDeps, strongholds map[string]bool, fuelModel func() func(int) float64, out io.Writer) bool {
	if !deps.EnableFreight || deps.HomeStation != "" || deps.State == nil {
		return false
	}
	now := missionNow(deps)
	// Checked here as well as in the scan so that the fuel model, which costs
	// a server probe to build, is not built on a pass that will not scan.
	if !deps.State.remoteScanAt.IsZero() && now.Sub(deps.State.remoteScanAt) < freightRemoteScanEvery {
		return false
	}
	in := freightRemoteInputs{
		CargoFree:   float64(cargoFreeSpace(deps.Client.GetState())),
		Held:        freightChainStops(ctx, deps, nil, out),
		Strongholds: strongholds,
		Now:         now,
		Locate: func(baseID string) (string, int, bool) {
			return missionLocateBase(ctx, deps, baseID)
		},
	}
	if fuelModel != nil {
		in.FuelCostFor = fuelModel()
	}
	run, why := freightRemoteCandidate(ctx, deps, in, out)
	if run == nil {
		if deps.State.shouldLogSkip("freight-remote", why) {
			fmt.Fprintf(out, "freight: no remote run to chase (%s)\n", why) //nolint:errcheck
		}
		return false
	}
	fmt.Fprintf(out, "freight: nothing here; flying %d jump(s) to %s for %s (%d-jump run, net %.0f)\n", //nolint:errcheck
		run.EmptyHops, run.OriginBaseID, run.ContractID, run.RunHops, run.Net)
	deps.State.noteRemoteChase(run.OriginBaseID, now)
	publishActivity(deps.SetActivity, "Flying to freight at "+run.OriginBaseID)
	if err := deps.nav(ctx, run.OriginSystem, run.OriginBaseID, nil); err != nil {
		fmt.Fprintf(out, "freight: flight to %s failed: %v\n", run.OriginBaseID, err) //nolint:errcheck
		return true
	}
	if err := freightSettleDock(ctx, deps, out); err != nil {
		fmt.Fprintf(out, "freight: dock at %s: %v\n", run.OriginBaseID, err) //nolint:errcheck
	}
	return true
}

// missionLocateBase resolves a base to its system and jump distance through
// the server's router, as missionHopsToBase does for the distance alone.
func missionLocateBase(ctx context.Context, deps MissionDeps, baseID string) (system string, hops int, ok bool) {
	if baseID == "" {
		return "", 0, false
	}
	route, err := deps.Client.FindRoute(ctx, baseID)
	if err != nil || len(route) == 0 {
		return "", 0, false
	}
	last := route[len(route)-1]
	if last.SystemID == "" {
		return "", 0, false
	}
	if last.Jumps > 0 {
		return last.SystemID, last.Jumps, true
	}
	return last.SystemID, len(route), true
}
