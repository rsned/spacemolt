package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/rsned/spacemolt/pkg/galaxy"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

const (
	// huntDefaultFleeHull is the hull fraction below which the pass gives up.
	// A cheap loss is not a free one: the Prospector is legacy and its
	// replacement, while armed, is slightly worse.
	huntDefaultFleeHull = 0.35
	// huntMaxEngagements caps one pass so a pathological board or an
	// unkillable creature cannot loop forever.
	huntMaxEngagements = 12
	// huntMaxBattlePolls bounds how many free get_battle_status polls one
	// engagement may take before the pass gives up on it and moves to the
	// next creature, so a battle that never resolves (server hiccup, a
	// target that despawns mid-fight) cannot hang the whole pass.
	huntMaxBattlePolls = 90
)

// HuntDeps are the injected collaborators for one Hunt pass.
type HuntDeps struct {
	Client  game.GameClient
	KB      knowledge.Base
	Out     io.Writer // nil -> io.Discard
	AgentID string
	// NowFn returns the current wall-clock time (nil -> time.Now); injected
	// for deterministic tests, mirroring Haul/Missions' Now field.
	NowFn func() time.Time
	// MaxDifficulty caps admissible mission difficulty (0 -> huntDefaultMaxDifficulty).
	MaxDifficulty int
	// WildlifeOnly restricts accepted missions to the wildlife-cull allowlist
	// (huntWildlifeMissions in hunt_gate.go).
	WildlifeOnly bool
	// FleeAtHull is the hull fraction below which the pass retreats rather
	// than engaging another creature (0 -> huntDefaultFleeHull).
	FleeAtHull float64
}

// huntNow returns the current time via deps.NowFn, or time.Now if unset.
func huntNow(deps HuntDeps) time.Time {
	if deps.NowFn != nil {
		return deps.NowFn()
	}
	return time.Now()
}

// Hunt performs one wildlife-hunt pass: reposition if the worker's location
// is unknown, read the local mission board and accept the first admissible
// combat mission (hunt_gate.go's huntAdmissible), hunt nearby creatures until
// its kill_creature objective is met or the pass must retreat, then complete
// it. Mirrors Haul/Missions' resilience contract: mid-run failures log and
// return nil so the worker idles and retries; the pass never kills the
// worker loop.
func Hunt(ctx context.Context, deps HuntDeps) error {
	out := deps.Out
	if out == nil {
		out = io.Discard
	}
	if deps.Client == nil {
		fmt.Fprintln(out, "hunt: no game client configured; skipping") //nolint:errcheck
		return nil
	}
	// A disconnected worker cannot accept, fight, or complete a mission;
	// running the pass would only spin failing commands against a dead
	// connection. Skip it and let the reconnect gate restore the connection
	// (the standing loop paces retries) — mirrors Haul/Missions.
	if !deps.Client.IsConnected() {
		fmt.Fprintln(out, "hunt: game connection down; skipping pass until reconnected") //nolint:errcheck
		return nil
	}
	maxDifficulty := deps.MaxDifficulty
	if maxDifficulty <= 0 {
		maxDifficulty = huntDefaultMaxDifficulty
	}
	fleeAtHull := deps.FleeAtHull
	if fleeAtHull <= 0 {
		fleeAtHull = huntDefaultFleeHull
	}

	// Step 1: refresh status; if the worker's position is entirely unknown,
	// reposition to the nearest accessible station using the same recovery
	// primitive Haul (haulRecoverIfStranded) and Missions
	// (missionRecoverToStation) use for a stranded worker, rather than
	// writing new travel code.
	//
	// There is no "wildlife POI type" anywhere in the KB or the plan to route
	// toward more specifically — the live capture that grounded Task 1 shows
	// creatures at an ordinary POI (poi_id "commerce_fields"), not a
	// dedicated wildlife location. So "at a POI that can hold wildlife" is
	// read here as "at any known POI"; once there, an empty creature list
	// (step 4 below) is the normal way this pass discovers the herd moved on,
	// not something this step tries to solve.
	if err := deps.Client.GetStatus(ctx); err != nil {
		fmt.Fprintf(out, "hunt: get_status: %v\n", err) //nolint:errcheck
		return nil
	}
	state := deps.Client.GetState()
	if state == nil || state.System.ID == "" {
		fmt.Fprintln(out, "hunt: current position unknown; skipping") //nolint:errcheck
		return nil
	}
	if state.CurrentPOI == "" {
		if err := huntReposition(ctx, deps, out, state.System.ID); err != nil {
			fmt.Fprintf(out, "hunt: reposition failed: %v; retry next pass\n", err) //nolint:errcheck
		}
		return nil
	}

	// Step 2: read the board; accept the first admissible entry, logging
	// every refusal with its reason.
	if err := deps.Client.GetMissions(ctx); err != nil {
		fmt.Fprintf(out, "hunt: get_missions: %v\n", err) //nolint:errcheck
		return nil
	}
	raw := deps.Client.GetRawJSON("missions")
	if len(raw) == 0 {
		fmt.Fprintln(out, "hunt: get_missions returned no data") //nolint:errcheck
		return nil
	}
	var board serverapi.GetMissionsResponse
	if err := json.Unmarshal(raw, &board); err != nil {
		fmt.Fprintf(out, "hunt: parse board: %v\n", err) //nolint:errcheck
		return nil
	}
	var chosen *serverapi.MissionBoardEntry
	for i := range board.Missions {
		e := board.Missions[i]
		ok, reason := huntAdmissible(e, maxDifficulty, deps.WildlifeOnly)
		if !ok {
			fmt.Fprintf(out, "hunt: skip %s (%s): %s\n", e.MissionID, e.Title, reason) //nolint:errcheck
			continue
		}
		chosen = &e
		break
	}
	if chosen == nil {
		fmt.Fprintln(out, "hunt: no admissible mission on the board; idling") //nolint:errcheck
		return nil
	}

	activeID, ok := huntAcceptMission(ctx, deps, out, *chosen)
	if !ok {
		return nil
	}

	// Step 3: the counted kill_creature objective is the target.
	target := huntKillQuantity(*chosen)
	fmt.Fprintf(out, "hunt: accepted %s (%s) at %s; target %d kill(s)\n", chosen.MissionID, chosen.Title, rfc(huntNow(deps)), target) //nolint:errcheck

	// Step 4: an empty belt is a normal outcome (the herd moved, or it's
	// already thinned out) — not an error. The mission stays accepted and is
	// picked up again next pass.
	if err := deps.Client.GetNearby(ctx); err != nil {
		fmt.Fprintf(out, "hunt: get_nearby: %v\n", err) //nolint:errcheck
		return nil
	}
	raw = deps.Client.GetRawJSON("nearby")
	var nearby serverapi.GetNearbyResponse
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &nearby); err != nil {
			fmt.Fprintf(out, "hunt: parse nearby: %v\n", err) //nolint:errcheck
			return nil
		}
	}
	if len(nearby.Creatures) == 0 {
		fmt.Fprintf(out, "hunt: no creatures at this POI; %s held for next pass\n", chosen.MissionID) //nolint:errcheck
		return nil
	}

	// Steps 5-6: hunt until the objective is met, the engagement cap is hit,
	// the pool of huntable creatures runs out, or the hull drops below the
	// flee threshold.
	quarry := nearby.Creatures
	kills, attempts := 0, 0
	for kills < target && attempts < huntMaxEngagements {
		st := deps.Client.GetState()
		if st != nil && st.Ship.MaxHull > 0 {
			frac := st.Ship.Hull / st.Ship.MaxHull
			if frac < fleeAtHull {
				fmt.Fprintf(out, "hunt: hull %.0f%% below flee threshold %.0f%% after %d/%d kill(s); retreating\n", //nolint:errcheck
					frac*100, fleeAtHull*100, kills, target)
				if ferr := deps.Client.Battle(ctx, "stance", map[string]any{"stance": "flee"}); ferr != nil {
					fmt.Fprintf(out, "hunt: flee stance failed: %v\n", ferr) //nolint:errcheck
				}
				return nil
			}
		}

		c, idx := huntPickQuarry(quarry)
		if idx < 0 {
			fmt.Fprintf(out, "hunt: no huntable creatures remain (%d/%d killed); %s held for next pass\n", kills, target, chosen.MissionID) //nolint:errcheck
			return nil
		}
		quarry = append(quarry[:idx], quarry[idx+1:]...)

		if err := deps.Client.Hunt(ctx, c.CreatureID); err != nil {
			fmt.Fprintf(out, "hunt: engage %s failed: %v\n", c.CreatureID, err) //nolint:errcheck
			attempts++
			continue
		}
		attempts++
		huntAwaitResolution(ctx, deps, out, c.CreatureID)
		kills++
		fmt.Fprintf(out, "hunt: %s down (%d/%d)\n", c.CreatureID, kills, target) //nolint:errcheck
	}

	if kills < target {
		fmt.Fprintf(out, "hunt: %s at %d/%d kill(s); held for next pass\n", chosen.MissionID, kills, target) //nolint:errcheck
		return nil
	}

	// Step 7: objective met — complete through the mission path. Looting the
	// carcass wrecks this leaves behind is out of scope (see the plan's
	// Deferred section) and deliberately not attempted here.
	fmt.Fprintf(out, "hunt: %s objective met (%d/%d); completing\n", chosen.MissionID, kills, target) //nolint:errcheck
	if err := deps.Client.CompleteMission(ctx, activeID); err != nil {
		fmt.Fprintf(out, "hunt: complete %s failed: %v; held for next pass\n", activeID, err) //nolint:errcheck
	}
	return nil
}

// huntAcceptMission accepts board entry e and resolves its real (hex)
// active-mission id via get_active_missions, mirroring mission.go's
// resolveActiveMissionIDs: board ids are template-ish and complete_mission
// 404s with mission_not_found against one. Unlike the mission runner this
// does not add a settle sleep before the lookup — HuntDeps carries no
// injectable sleep, and a hunt pass already spends many ticks fighting
// before it ever reaches completion, so a miss here just falls back to the
// board id (a held-for-next-pass retry at worst, never a crash). ok=false
// means the accept itself was refused; the caller logs and ends the pass.
func huntAcceptMission(ctx context.Context, deps HuntDeps, out io.Writer, e serverapi.MissionBoardEntry) (activeID string, ok bool) {
	if err := deps.Client.AcceptMission(ctx, e.MissionID); err != nil {
		fmt.Fprintf(out, "hunt: accept %s failed: %v\n", e.MissionID, err) //nolint:errcheck
		return "", false
	}
	if err := deps.Client.GetActiveMissions(ctx); err != nil {
		fmt.Fprintf(out, "hunt: get_active_missions: %v; using board id %s for completion\n", err, e.MissionID) //nolint:errcheck
		return e.MissionID, true
	}
	raw := deps.Client.GetRawJSON("active_missions")
	if len(raw) == 0 {
		return e.MissionID, true
	}
	var resp serverapi.GetActiveMissionsResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return e.MissionID, true
	}
	for _, m := range resp.Missions {
		if m.TemplateID == e.MissionID || m.Title == e.Title {
			return m.MissionID, true
		}
	}
	return e.MissionID, true
}

// huntPickQuarry selects the next creature to engage from live: creatures
// already InCombat are skipped (do not pile onto a fight already underway —
// there is no field named IsAggressive on the wire, see hunt_gate.go), and
// among the rest the highest hull-fraction survivor is preferred. Returns
// idx=-1 when every remaining creature is InCombat.
func huntPickQuarry(creatures []serverapi.NearbyCreature) (serverapi.NearbyCreature, int) {
	best := -1
	bestFrac := -1.0
	for i, c := range creatures {
		if c.InCombat {
			continue
		}
		frac := 1.0
		if c.MaxHull > 0 {
			frac = float64(c.Hull) / float64(c.MaxHull)
		}
		if frac > bestFrac {
			bestFrac = frac
			best = i
		}
	}
	if best < 0 {
		return serverapi.NearbyCreature{}, -1
	}
	return creatures[best], best
}

// huntAwaitResolution polls get_battle_status — a free query with no tick
// cost — until the reply reports we are no longer a battle participant (the
// fight resolved), or huntMaxBattlePolls is exhausted. Best-effort: any
// get_battle_status error, or an exhausted poll budget, is logged and the
// pass simply moves on to the next creature rather than hanging.
func huntAwaitResolution(ctx context.Context, deps HuntDeps, out io.Writer, creatureID string) {
	for range huntMaxBattlePolls {
		if err := deps.Client.GetBattleStatus(ctx); err != nil {
			fmt.Fprintf(out, "hunt: get_battle_status for %s: %v\n", creatureID, err) //nolint:errcheck
			return
		}
		var resp serverapi.GetBattleStatusResponse
		if raw := deps.Client.GetRawJSON("battle_status"); len(raw) > 0 {
			_ = json.Unmarshal(raw, &resp)
		}
		if !resp.IsParticipant {
			return
		}
		_ = craftPollSleepFunc(ctx, game.SleepQuick)
	}
	fmt.Fprintf(out, "hunt: battle vs %s did not resolve after %d poll(s); moving on\n", creatureID, huntMaxBattlePolls) //nolint:errcheck
}

// huntReposition routes a worker with an entirely unknown position (no
// CurrentPOI) to the nearest known accessible station, reusing the same
// recovery primitive Haul (haulRecoverIfStranded) and Missions
// (missionRecoverToStation) use for a stranded worker rather than writing new
// travel code.
func huntReposition(ctx context.Context, deps HuntDeps, out io.Writer, current string) error {
	if deps.KB == nil {
		return fmt.Errorf("no knowledge base configured")
	}
	galGraph := &galaxy.GalaxyGraph{}
	if err := galGraph.BuildFromDB(ctx, deps.KB); err != nil {
		return fmt.Errorf("build galaxy graph: %w", err)
	}
	near, err := galaxy.FindNearestByPOIType(ctx, deps.KB, galGraph, current, "station", 1)
	if err != nil {
		return fmt.Errorf("nearest station: %w", err)
	}
	if len(near) == 0 {
		return fmt.Errorf("no reachable station known")
	}
	dest := near[0]
	var station string
	if len(dest.POIs) > 0 {
		station = dest.POIs[0].ID
	}
	name := dest.SystemName
	if name == "" {
		name = dest.SystemID
	}
	fmt.Fprintf(out, "hunt: no known position; relocating to %s (%s, %d jump(s))\n", name, dest.SystemID, dest.Hops) //nolint:errcheck
	if err := Autopilot(ctx, AutopilotDeps{Client: deps.Client, Out: out}, dest.SystemID, station); err != nil {
		return fmt.Errorf("relocation transit: %w", err)
	}
	return nil
}
