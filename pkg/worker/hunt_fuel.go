package worker

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/navigation"
)

// Fuel rules for hunters. The autopilot tops up for the way OUT when it
// leaves a dock; nothing looked at the way back, and nothing looked at all
// when a leg started at a station-less ground. On 2026-11-25, a day after
// every hunter was cleared for Lawless space (grounds 8 to 16 jumps out, few
// pumps), four of seven were stranded: pirate-7 and pirate-8 ran dry at
// grounds, pirate-10 docked at a mobile base that sells no fuel, and pirate-6
// could not pay for a tank. The two docked ones then undocked, failed the
// first jump and redocked every 30 seconds for a combined 1,700 sorties.
const (
	// huntFuelPerJumpDefault stands in when no find_route estimate is to
	// hand. A Gauntlet measures 7 a jump.
	huntFuelPerJumpDefault = 8
	// huntFuelReserveJumps is kept beyond the trip out and the trip from
	// there to a pump: jump estimates leave out in-system travel.
	huntFuelReserveJumps = 2
	// huntFuelStrandedJumps: a docked hunter holding less than this many
	// jumps of fuel, and unable to buy more, stays docked.
	huntFuelStrandedJumps = 2
	// huntFuelCreditsPerUnit prices a unit of fuel when weighing a need. The
	// fleet paid 11.6 on average on 2026-11-25 (7 at Ramen's Rest, 13 at Node
	// Beta, 16 to 19 at Solarian stations).
	huntFuelCreditsPerUnit = 13
	// huntNeedRewardMargin is how far a need's reward must clear the fuel for
	// its trip (operator decision 2026-11-25). That day fuel took 72% of
	// hunter income and 27 of 94 needs paid less than they cost.
	huntNeedRewardMargin = 1.5
)

// errHuntOutOfFuelRange marks a destination the tank cannot reach and return
// from, even full.
var errHuntOutOfFuelRange = errors.New("out of fuel range")

// huntFuelPerJump is the last find_route estimate, or the default.
func huntFuelPerJump(deps HuntDeps) int {
	if per, _, _ := parseFuelEstimates(deps.Client); per > 0 {
		return per
	}
	return huntFuelPerJumpDefault
}

// huntNeedFuelCost prices the fuel for one trip of jumps jumps.
func huntNeedFuelCost(deps HuntDeps, jumps int) int {
	return jumps * huntFuelPerJump(deps) * huntFuelCreditsPerUnit
}

// huntNeedPays reports whether reward clears fuelCost by
// huntNeedRewardMargin.
func huntNeedPays(reward, fuelCost int) bool {
	return float64(reward) >= huntNeedRewardMargin*float64(fuelCost)
}

// huntFuelStation reports whether poiID is a pump a hunter can count on: a
// public empire station offering refuel. Player stations, hives and mobile
// bases are left out on purpose. The KB lists refuel for Civitas (a mobile
// base, closed to outsiders) and for Hive Sulafat; the empire field is the
// one thing that separates them from a station that will actually serve.
func huntFuelStation(ctx context.Context, kb knowledge.Base, poiID string) bool {
	if kb == nil || poiID == "" {
		return false
	}
	base, err := kb.GetBaseByPOI(ctx, poiID)
	return err == nil && base != nil && base.PublicAccess && !base.IsOutpost() &&
		base.Empire != "" && base.Services["refuel"]
}

// huntNearestFuelStation is the fuel station fewest jumps from system `from`
// (0 when there is one in it), over every known connection.
func huntNearestFuelStation(ctx context.Context, kb knowledge.Base, from string) (systemID, poiID string, jumps int, ok bool) {
	if kb == nil || from == "" {
		return "", "", 0, false
	}
	systems, err := kb.GetSystems(ctx)
	if err != nil {
		return "", "", 0, false
	}
	conns, err := kb.GetConnections(ctx)
	if err != nil {
		return "", "", 0, false
	}
	ids := make([]string, 0, len(systems))
	for _, s := range systems {
		ids = append(ids, s.ID)
	}
	dist := navigation.BFSJumps(navigation.JumpGraphFromConnections(conns), from, ids)
	dist[from] = 0
	slices.SortFunc(ids, func(a, b string) int {
		return cmp.Or(cmp.Compare(dist[a], dist[b]), strings.Compare(a, b))
	})
	for _, id := range ids {
		if dist[id] >= navigation.RouteInf {
			break
		}
		pois, perr := kb.GetPOIs(ctx, id)
		if perr != nil {
			continue
		}
		for _, p := range pois {
			if p.Type == huntPOITypeStation && huntFuelStation(ctx, kb, p.ID) {
				return id, p.ID, dist[id], true
			}
		}
	}
	return "", "", 0, false
}

// huntFuelNeed is the fuel a leg to systemID wants: the jumps out, the jumps
// from there to the nearest pump, and the reserve. ok is false when the KB
// cannot say (unknown position, no route, no pump known), which never blocks.
func huntFuelNeed(ctx context.Context, deps HuntDeps, from, systemID string) (need, out, back int, ok bool) {
	conns, err := deps.KB.GetConnections(ctx)
	if err != nil {
		return 0, 0, 0, false
	}
	if from != systemID {
		d := navigation.BFSJumps(navigation.JumpGraphFromConnections(conns), from, []string{systemID})[systemID]
		if d >= navigation.RouteInf {
			return 0, 0, 0, false
		}
		out = d
	}
	_, _, back, ok = huntNearestFuelStation(ctx, deps.KB, systemID)
	if !ok {
		return 0, 0, 0, false
	}
	return (out + back + huntFuelReserveJumps) * huntFuelPerJump(deps), out, back, true
}

// huntFuelGate runs before every hunter leg. A leg to a pump is the
// autopilot's business. For any other destination the tank must cover the
// way out and on to a pump: short of that it fills up where it is docked, or
// flies to the nearest pump first when it is not, and refuses the leg
// (errHuntOutOfFuelRange) when even that does not cover it.
func huntFuelGate(ctx context.Context, deps HuntDeps, out io.Writer, ap AutopilotDeps, systemID, poiID string) error {
	if deps.KB == nil || huntFuelStation(ctx, deps.KB, poiID) {
		return nil
	}
	st := deps.Client.GetState()
	if st == nil || st.System.ID == "" {
		return nil
	}
	fuel, maxFuel := st.GetFuel()
	if maxFuel <= 0 {
		return nil
	}
	need, outJ, backJ, ok := huntFuelNeed(ctx, deps, st.System.ID, systemID)
	if !ok || int(fuel) >= need {
		return nil
	}
	short := fmt.Errorf("%w: %s wants ~%d fuel (%d jump(s) out, %d on to a pump, %d spare), tank holds %d",
		errHuntOutOfFuelRange, poiID, need, outJ, backJ, huntFuelReserveJumps, int(maxFuel))
	if int(maxFuel) < need {
		return short
	}
	if !st.Doc {
		fsys, fpoi, _, fok := huntNearestFuelStation(ctx, deps.KB, st.System.ID)
		if !fok {
			return short
		}
		fmt.Fprintf(out, "hunt: fuel %d of %d is short of the ~%d this leg wants; refuelling at %s/%s first\n", //nolint:errcheck
			int(fuel), int(maxFuel), need, fsys, fpoi)
		if err := Autopilot(ctx, ap, fsys, fpoi); err != nil {
			return fmt.Errorf("refuel stop at %s: %w", fpoi, err)
		}
		if err := dockIdempotent(ctx, deps.Client); err != nil {
			return fmt.Errorf("dock to refuel at %s: %w", fpoi, err)
		}
	}
	if err := deps.Client.Refuel(ctx); err != nil {
		fmt.Fprintf(out, "hunt: refuel before the leg to %s failed: %v\n", poiID, err) //nolint:errcheck
	}
	if err := deps.Client.GetStatus(ctx); err != nil {
		return fmt.Errorf("get_status after refuelling: %w", err)
	}
	st = deps.Client.GetState()
	if st == nil {
		return nil
	}
	fuel, _ = st.GetFuel()
	// The need is measured again from where the ship now is.
	if need, _, _, ok = huntFuelNeed(ctx, deps, st.System.ID, systemID); ok && int(fuel) < need {
		return fmt.Errorf("%w: %s wants ~%d fuel and the tank has %d after refuelling", errHuntOutOfFuelRange, poiID, need, int(fuel))
	}
	return nil
}

// huntStrandedTried is when this process last tried to buy its way out of
// being stranded.
var huntStrandedTried time.Time

// huntStrandedAtDock reports a docked hunter that cannot leave: under
// huntFuelStrandedJumps of fuel and unable to buy more here. It stays docked,
// where an operator or an assist can reach it, instead of undocking into a
// jump that fails.
func huntStrandedAtDock(ctx context.Context, deps HuntDeps, out io.Writer, who string) bool {
	st := deps.Client.GetState()
	if st == nil || !st.Doc {
		return false
	}
	floor := huntFuelStrandedJumps * huntFuelPerJump(deps)
	fuel, maxFuel := st.GetFuel()
	if maxFuel <= 0 || int(fuel) >= floor {
		return false
	}
	// One refuel attempt per board-poll interval, not one per pass.
	now := huntNow(deps)
	if !huntStrandedTried.IsZero() && now.Sub(huntStrandedTried) < game.SleepMissionBoardPoll {
		return true
	}
	huntStrandedTried = now
	rerr := deps.Client.Refuel(ctx)
	if err := deps.Client.GetStatus(ctx); err == nil {
		if st = deps.Client.GetState(); st != nil {
			fuel, _ = st.GetFuel()
		}
	}
	if int(fuel) >= floor {
		return false
	}
	fmt.Fprintf(out, "hunt[%s]: STRANDED at %s: fuel %d of %d and none to be had here (%v); staying docked, operator action needed\n", //nolint:errcheck
		who, st.CurrentPOI, int(fuel), int(maxFuel), rerr)
	publishActivity(deps.SetActivity, "Hunt · no fuel")
	return true
}

// huntLegRefused returns the POI a "transit to <poi>: ..." error names when
// the leg was refused for reason, "" otherwise.
func huntLegRefused(err, reason error) string {
	if err == nil || !errors.Is(err, reason) {
		return ""
	}
	const marker = "transit to "
	msg := err.Error()
	i := strings.Index(msg, marker)
	if i < 0 {
		return ""
	}
	rest := msg[i+len(marker):]
	if end := strings.IndexAny(rest, ": "); end >= 0 {
		rest = rest[:end]
	}
	return rest
}
