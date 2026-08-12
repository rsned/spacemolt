package worker

import (
	"context"
	"fmt"
	"io"

	"github.com/rsned/spacemolt/pkg/game"
)

// Surveying player stations is its own operation, not a side effect of hauling.
// A player station may refuse an outsider's dock, and a station that admits you
// may still sell no fuel -- two independent facts, neither readable ahead of
// time, both discoverable only by flying there and trying. Until they are known
// the shuttle must decline every fare bound for an unverified station, which
// costs real money (13,125cr of Blackthorn fares declined on the first pass
// alone).
//
// The probe answers them deliberately, in a planned order, with a ship chosen to
// make being wrong cheap. It is designed around one asymmetry: a partial survey
// that reports fourteen verdicts and parks the ship safely is worth far more
// than a complete one that strands it. Every stop-or-continue decision resolves
// toward stopping.

// ProbeTarget is one station to survey. StationID is the id the access map keys
// on -- the same id a passenger board would name -- and SystemID/POIID are what
// the autopilot needs to fly there.
type ProbeTarget struct {
	StationID string // recorded in the access map (base id, when known)
	SystemID  string
	POIID     string // station POI to dock at; defaults to StationID when empty
	Name      string // display only
}

// poi returns the POI to fly to, defaulting to the station id.
func (t ProbeTarget) poi() string {
	if t.POIID != "" {
		return t.POIID
	}

	return t.StationID
}

// ProbeDeps are the injected collaborators for a survey run.
type ProbeDeps struct {
	Client game.GameClient
	Out    io.Writer // nil -> io.Discard
	Access *StationAccess

	// FuelPerJump is the ship's cost per jump. The survey's whole safety margin
	// is computed from it, so it is required rather than guessed: 0 disables the
	// fuel gate and the probe will refuse to run.
	FuelPerJump float64

	// ApproachFuel is the in-system cost of crossing from the arrival point to
	// the station POI, typically 1-2. It is small per stop and decisive in
	// aggregate: a route planner that counts only jumps under-budgets an
	// 18-stop survey by ~36 fuel, which is most of a starter hull's tank. 0 ->
	// defaultApproachFuel rather than free, because free is the one value that
	// is certainly wrong.
	ApproachFuel float64

	// JumpsTo reports the jump distance from the ship's current system to a
	// target system. Injected because the probe must ask BEFORE committing to a
	// leg, which no in-flight API can answer.
	JumpsTo func(ctx context.Context, systemID string) (int, error)
}

// defaultApproachFuel is the assumed in-system hop cost when none is given. Set
// to the top of the observed 1-2 range: over-reserving parks the ship one stop
// early, under-reserving strands it short of a station it cannot reach.
const defaultApproachFuel = 2

// approachFuel returns the configured in-system reserve, defaulted.
func approachFuel(deps ProbeDeps) float64 {
	if deps.ApproachFuel > 0 {
		return deps.ApproachFuel
	}

	return defaultApproachFuel
}

// ProbeVerdict is what one stop learned.
type ProbeVerdict struct {
	Target    ProbeTarget
	Reached   bool   // the ship arrived in the system
	Docked    bool   // the station admitted us
	FuelKnown bool   // the fuel desk question was answered
	SellsFuel bool   // ...and the answer, when FuelKnown
	Note      string // why a stop ended early, when it did
}

// ProbeStations flies the given targets in order, learning access and fuel at
// each, and returns a verdict per attempted stop.
//
// It stops early -- and says so -- the moment the remaining fuel cannot cover
// the next leg. The ship is deliberately left wherever it stops: GSA recovery
// tows a drifting ship back to empire space for a small fee, which is cheaper
// and safer than reserving return fuel on every leg, and it means the survey
// budget buys forward progress rather than a round trip.
func ProbeStations(ctx context.Context, deps ProbeDeps, targets []ProbeTarget) ([]ProbeVerdict, error) {
	out := deps.Out
	if out == nil {
		out = io.Discard
	}
	if deps.Client == nil {
		return nil, fmt.Errorf("probe: no client")
	}
	if deps.FuelPerJump <= 0 {
		return nil, fmt.Errorf("probe: FuelPerJump must be > 0 (the fuel gate is the only thing keeping the ship recoverable)")
	}
	if deps.JumpsTo == nil {
		return nil, fmt.Errorf("probe: JumpsTo required")
	}

	verdicts := make([]ProbeVerdict, 0, len(targets))
	for i, t := range targets {
		fuel, ok := probeFuel(deps.Client)
		if !ok {
			fmt.Fprintln(out, "probe: cannot read fuel; stopping here rather than flying blind") //nolint:errcheck

			return verdicts, nil
		}
		jumps, err := deps.JumpsTo(ctx, t.SystemID)
		if err != nil {
			// An unroutable target is this target's problem, not the survey's:
			// skip it and keep the remaining stops.
			fmt.Fprintf(out, "probe: %s unroutable (%v); skipping\n", t.Name, err) //nolint:errcheck
			verdicts = append(verdicts, ProbeVerdict{Target: t, Note: "unroutable: " + err.Error()})

			continue
		}
		need := float64(jumps)*deps.FuelPerJump + approachFuel(deps)
		if need > fuel {
			fmt.Fprintf(out, //nolint:errcheck
				"probe: stopping at stop %d/%d -- %s is %d jumps (%.0f fuel) and only %.0f remain; parking for recovery\n",
				i+1, len(targets), t.Name, jumps, need, fuel)

			return verdicts, nil
		}

		verdicts = append(verdicts, probeOne(ctx, deps, out, t, jumps, need, fuel))
	}
	fmt.Fprintf(out, "probe: survey complete; %d station(s) attempted\n", len(verdicts)) //nolint:errcheck

	return verdicts, nil
}

// probeOne flies to a single target and records what it learns.
func probeOne(ctx context.Context, deps ProbeDeps, out io.Writer, t ProbeTarget, jumps int, need, fuel float64) ProbeVerdict {
	v := ProbeVerdict{Target: t}
	fmt.Fprintf(out, "probe: -> %s (%s), %d jumps, %.0f fuel of %.0f\n", t.Name, t.SystemID, jumps, need, fuel) //nolint:errcheck

	err := Autopilot(ctx, AutopilotDeps{Client: deps.Client, Out: out}, t.SystemID, t.poi())
	if err != nil {
		// Transit failure teaches nothing about the station -- we never got to
		// ask it anything.
		fmt.Fprintf(out, "probe: transit to %s failed: %v\n", t.Name, err) //nolint:errcheck
		v.Note = "transit failed: " + err.Error()

		return v
	}
	v.Reached = true

	dockErr := deps.Client.Dock(ctx)
	deps.Access.RecordDock(t.StationID, dockErr)
	if dockErr != nil {
		// This is the answer we came for, not a failure of the run.
		fmt.Fprintf(out, "probe: %s REFUSED the dock: %v\n", t.Name, dockErr) //nolint:errcheck
		v.Note = "dock refused: " + dockErr.Error()

		return v
	}
	v.Docked = true
	fmt.Fprintf(out, "probe: %s ADMITTED us\n", t.Name) //nolint:errcheck

	// Docked, so the fuel desk becomes answerable. Refuelling here is also how
	// the survey extends its own range, which is why it is attempted at every
	// stop rather than only when low.
	refuelErr := RefuelAndSync(ctx, deps.Client, out, "probe")
	deps.Access.RecordRefuel(t.StationID, refuelErr)
	if sells, known := deps.Access.Fuel(t.StationID); known {
		v.FuelKnown, v.SellsFuel = true, sells
		if sells {
			fmt.Fprintf(out, "probe: %s sells fuel\n", t.Name) //nolint:errcheck
		} else {
			fmt.Fprintf(out, "probe: %s has NO fuel desk\n", t.Name) //nolint:errcheck
		}
	} else if refuelErr != nil {
		fmt.Fprintf(out, "probe: %s fuel status inconclusive: %v\n", t.Name, refuelErr) //nolint:errcheck
	}

	// Undock so the ship is adrift and eligible for GSA recovery if this turns
	// out to be the last stop. A ship left docked at a station nobody routes to
	// waits forever; a drifting one gets towed home.
	if err := deps.Client.Undock(ctx); err != nil {
		fmt.Fprintf(out, "probe: undock at %s failed: %v\n", t.Name, err) //nolint:errcheck
	}

	return v
}

// probeFuel reads current fuel, reporting ok=false when state is unavailable --
// which must halt the survey rather than be treated as an empty tank.
func probeFuel(client game.GameClient) (float64, bool) {
	s := client.GetState()
	if s == nil {
		return 0, false
	}
	fuel, _ := s.GetFuel()

	return fuel, true
}
