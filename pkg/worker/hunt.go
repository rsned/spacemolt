package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/galaxy"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/spar"
)

const (
	// huntDefaultFleeHull is the hull fraction below which the pass gives up.
	// A cheap loss is not a free one: the Prospector is legacy and its
	// replacement, while armed, is slightly worse.
	huntDefaultFleeHull = 0.35
	// huntMaxEngagements caps one pass so a pathological board or an
	// unkillable creature cannot loop forever.
	huntMaxEngagements = 12
	// huntMaxBattleTicks bounds ONE engagement, in server ticks (the fight
	// loop runs at game.SleepTick, one action per tick). A captured kill of a
	// 70-hull grazer with a starter Prospect took 8 ticks, so this is roughly
	// 3x a normal fight: generous headroom for a real engagement, while still
	// bounding a chase that is going nowhere.
	huntMaxBattleTicks = 24
	// huntNoProgressTicks is the real give-up signal, and it is a PROGRESS
	// bound rather than a liveness one. Progress means either the numeric
	// range closed below anything seen so far, or the quarry lost hull. After
	// this many consecutive ticks with neither, the chase is not going to be
	// won: the quarry is outrunning us (zone_distance never falls) or kiting
	// us (it falls and reopens while the quarry's hull never drops).
	huntNoProgressTicks = 6
	// huntDisengageTicks bounds the break-off. Escaping is not one command:
	// a captured combat_state shows flee_counter 0 of flee_required 3, so the
	// stance has to be held for several ticks while the server counts it out.
	// The pass keeps polling with advancing disabled until the escape
	// completes, the battle ends, or this bound — abandoning a quarry by
	// simply moving on would leave the worker a participant in an unresolved
	// fight, still in range and still being shot.
	huntDisengageTicks = 8

	// huntRepairAtHull is the hull fraction below which the pass repairs while
	// it is docked reading the board.
	//
	// Hull is the one pool that does not come back on its own: shields
	// regenerate, hull needs a station or a Combat Support ship with a repair
	// kit. So hull loss accumulates ACROSS passes, and a fleet that never
	// repairs degrades monotonically until something kills it.
	//
	// This is deliberately far ABOVE huntDefaultFleeHull. The flee threshold is
	// an emergency; this is a top-up, and its whole point is to start each pass
	// near full rather than to rescue one that ended badly. 0.9 tolerates the
	// ordinary case — one engagement fought with the shields already down costs
	// roughly 12 hull, which on the live Prospect's 95 is about 13% — so a pass
	// that spent hull repairs on the next one, and a pass that did not spends
	// nothing.
	//
	// It costs no travel: the pass is already docked at the board station. That
	// is the entire reason the rule is shaped as "repair where you stand"
	// rather than as a return-to-station leg.
	huntRepairAtHull = 0.9

	// huntShieldReadyFrac is the shield fraction the pass wants before opening
	// the NEXT engagement. Shields are renewable and hull is not — incoming
	// damage lands on shields first (a captured battle_update shows shield 96%
	// beside hull 100% after taking fire), and shields regenerate between
	// fights while nothing in this executor repairs hull. So the cheap way to
	// keep hull off the table is to arrive at each fight with a full-enough
	// bar, not to fight until the hull gate fires.
	//
	// The arithmetic, on the operator's damage numbers and a live Prospect
	// reporting max_shield 50, shield_recharge 1: a Belt-Grazer deals 4/tick
	// against 1/tick of recharge, so a fight drains ~3/tick net; a kill takes
	// ~4 ticks in reach, so ~12-15 shield per engagement. Half a bar is more
	// than one engagement's worth, so a pass that waits at this line never
	// exposes hull to a difficulty-1 quarry. Raising it costs wall-clock for
	// no gain; lowering it starts spending hull, which does not grow back.
	//
	// The fraction is the constant; the pool and the rate are NOT. Both are
	// read per ship from the wire (Shield/MaxShield/ShieldRecharge) — the live
	// Prospect reports max_hull 95 where the class catalogue says 100, so any
	// hardcoded maximum is wrong by construction.
	huntShieldReadyFrac = 0.5
	// huntShieldWaitTicks bounds that wait. At the Prospect's reported
	// shield_recharge of 1 this recovers 40% of its bar, enough to climb from
	// nearly flat to past the threshold; the wait also ends early the moment
	// the shield stops rising, so a ship that cannot regenerate does not sit
	// here.
	huntShieldWaitTicks = 20

	// huntStanceFlee / huntStanceFire are the battle stances this pass uses.
	huntStanceFlee = "flee"
	huntStanceFire = "fire"

	// huntWreckTypeCreature is the wreck type a killed creature leaves.
	// Corroborating only: victim_id is the identity that matters.
	huntWreckTypeCreature = "creature"

	// huntPOITypeBelt / huntPOITypeStation are KB pois.type values.
	huntPOITypeBelt    = "asteroid_belt"
	huntPOITypeStation = "station"

	// huntBeltResource is the ore whose presence marks a grazer belt. The
	// mission dialog states the mechanic: "Grazers gather where the iron is
	// still rich."
	huntBeltResource = "iron_ore"

	// huntPolicedMinLevel is the systems.police_level at or above which a
	// system counts as POLICED. The tiers are Lawless 0, Frontier 30, Low
	// Security 55, High Security 80, Maximum Security 100 — so this admits
	// every empire tier and excludes only Lawless, which is 404 of the known
	// systems.
	//
	// This is a safety threshold before it is an economic one. A pirate ambush
	// mid-hunt against a starter hull with one laser is a lost ship, and the
	// hull damage that survives it does not grow back. Note the filter reads
	// police_level and never security_status: that field is display text and
	// is empty for some systems.
	huntPolicedMinLevel = 30

	// huntPOISearchLimit bounds the nearest-systems scan for a hunting ground.
	huntPOISearchLimit = 25

	// huntMaxGrounds is how many hunting grounds one pass will try before
	// giving up and holding the mission. Each failed ground costs a real
	// journey, so this is small; the next pass sweeps again.
	huntMaxGrounds = 3
)

// huntWildlifePOITypes are the KB pois.type values that hold wildlife, IN
// PREFERENCE ORDER. The mission board only exists at a station, so a pass
// reads the board docked and then travels out to the best of these it can
// reach — best by resource tier first and this order second, see
// huntLocalWildlifePOI. Captured kills so far are at a gas cloud (market_prime_gas_plume) and
// a cryobelt (gold_run_cryobelt), in different systems — wildlife is not
// confined to one POI type or one system.
//
// The order is not cosmetic, and the reason is now settled rather than
// suspected: kill_creature IS scoped to a species (huntMissionSpecies), and a
// species lives where it lives. The First Hunt chain wants belt_grazer, which
// is a belt animal, so a pass that flew to the nearest gas cloud would hunt
// all day without advancing — which is exactly what the operator observed.
// asteroid_belt is therefore tried first. nebula is here
// because nebula_drift_hunt is on the mission allowlist (hunt_gate.go) and the
// KB holds 55 nebula POIs — without it, a pass that accepts that mission
// cannot travel to where its quarry lives.
var huntWildlifePOITypes = []string{huntPOITypeBelt, "gas_cloud", "ice_field", "nebula"}

// huntSpeciesPOIType says where each quarry species lives, so the POI search
// and the species filter cannot disagree — flying to a nebula for a mission
// that counts only belt_grazers is a guaranteed wasted pass, and forcing a
// sift_ray mission onto a belt is the same mistake mirrored.
//
// Each entry is derived from the mission that wants it: first_hunt's objective
// reads "Hunt 3 Belt-Grazers at an asteroid belt" and cracking_the_shell's
// "Hunt 3 Slag-Tortoises at an asteroid belt"; ice_field_thinning and
// nebula_drift_hunt name their grounds in their ids. A species that is not
// here searches every wildlife POI type, as before.
var huntSpeciesPOIType = map[string]string{
	"belt_grazer":   huntPOITypeBelt,
	"slag_tortoise": huntPOITypeBelt,
	"rime_grazer":   "ice_field",
	"sift_ray":      "nebula",
}

// huntQuarryRoles are the creature roles a wildlife pass may engage. `role` is
// the stable classifier — species vary by POI and system, and one captured
// belt held eight of them — so this filters on role and never on a species
// allowlist.
//
// The excluded value is the point: a captured list put a 280-hull PREDATOR
// (Tempest-Eel) alongside 45-hull grazers, all at full hull. The entire safety
// case for a difficulty-1 wildlife fleet is that its quarry does not
// meaningfully fight back, and a predator is not that. An unknown or missing
// role is refused too: not hunting is the safe failure.
var huntQuarryRoles = map[string]bool{"grazer": true, "scavenger": true}

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
	// (huntWildlifeMissions in hunt_gate.go) AND is the switch both
	// no-predator enforcement points hang off. It is a *bool, not a bool,
	// because it is a safety interlock rather than a preference: nil means
	// "unset" and resolves to huntWildlifeOnlyDefault (true), so a caller who
	// never heard of this field still gets the interlock. A plain bool cannot
	// express "unset", and its zero value is the unsafe state — a forgotten
	// field would silently admit a 280-hull predator as quarry.
	//
	// Set it to a pointer to false to deliberately opt out.
	WildlifeOnly *bool
	// FleeAtHull is the hull fraction below which the pass retreats rather
	// than engaging another creature (0 -> huntDefaultFleeHull).
	FleeAtHull float64
	// sleep is the post-accept settle delay (nil -> craftPollSleepFunc, the
	// ctx-aware real sleep). Mirrors MissionDeps.sleep; tests inject a
	// zero-delay stand-in so the suite doesn't accumulate real waits.
	sleep func(ctx context.Context, d time.Duration) error
	// tickSleep paces the in-battle policy loop (0 -> game.SleepTick, one
	// action per server tick). Injected as ~0 by tests for the same reason as
	// sleep: a multi-tick chase would otherwise cost the suite minutes.
	tickSleep time.Duration
}

// huntWildlifeOnly resolves the wildlife-only interlock: unset means on.
func huntWildlifeOnly(deps HuntDeps) bool {
	if deps.WildlifeOnly == nil {
		return huntWildlifeOnlyDefault
	}
	return *deps.WildlifeOnly
}

// huntNow returns the current time via deps.NowFn, or time.Now if unset.
func huntNow(deps HuntDeps) time.Time {
	if deps.NowFn != nil {
		return deps.NowFn()
	}
	return time.Now()
}

// huntOutcome is how one engagement ended.
type huntOutcome int

const (
	// huntResolved: the server reports the battle over. Whether that means a
	// kill is decided by the carcass, never assumed.
	huntResolved huntOutcome = iota
	// huntFled: own hull fell below the flee threshold; the whole pass ends.
	huntFled
	// huntGaveUp: the chase made no progress, or spent its tick budget. The
	// quarry is abandoned (already removed from the local pool) and the pass
	// moves to the next creature.
	huntGaveUp
	// huntBattleError: a command failed mid-fight. Treated like gave-up.
	huntBattleError
)

// Hunt performs one wildlife-hunt pass: dock at a station and read the mission
// board, accept the first admissible combat mission (hunt_gate.go's
// huntAdmissible), travel out to a wildlife-bearing POI, hunt creatures there
// until its kill_creature objective is met or the pass must retreat, loot each
// carcass, then complete the mission. Mirrors Haul/Missions' resilience
// contract: mid-run failures log and return nil so the worker idles and
// retries; the pass never kills the worker loop.
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
	if deps.sleep == nil {
		deps.sleep = craftPollSleepFunc
	}
	if deps.tickSleep == 0 {
		deps.tickSleep = game.SleepTick
	}
	maxDifficulty := deps.MaxDifficulty
	if maxDifficulty <= 0 {
		maxDifficulty = huntDefaultMaxDifficulty
	}
	fleeAtHull := deps.FleeAtHull
	if fleeAtHull <= 0 {
		fleeAtHull = huntDefaultFleeHull
	}
	wildlifeOnly := huntWildlifeOnly(deps)
	who := deps.AgentID
	if who == "" {
		who = "hunt"
	}

	// Step 1: get to a board. A mission board only exists at a station, so the
	// pass starts docked — exactly as Missions does (mission.go's
	// needRecovery block). Wildlife is somewhere else entirely: creatures live
	// at asteroid belts, gas clouds and ice fields, which is why step 3 below
	// undocks and travels. A worker parked at a belt from last pass therefore
	// fails the dock here and recovers to a station, spending one pass to get
	// back on a board.
	if err := deps.Client.GetStatus(ctx); err != nil {
		fmt.Fprintf(out, "hunt: get_status: %v\n", err) //nolint:errcheck
		return nil
	}
	state := deps.Client.GetState()
	if state == nil || state.System.ID == "" {
		fmt.Fprintln(out, "hunt: current position unknown; skipping") //nolint:errcheck
		return nil
	}
	needRecovery := state.CurrentPOI == ""
	if !needRecovery && !state.Doc {
		if err := deps.Client.Dock(ctx); err != nil {
			fmt.Fprintf(out, "hunt: dock failed: %v; recovering to a station\n", err) //nolint:errcheck
			needRecovery = true
		}
	}
	if needRecovery {
		if err := huntRecoverToStation(ctx, deps, out, state.System.ID); err != nil {
			fmt.Fprintf(out, "hunt: reposition failed: %v; retry next pass\n", err) //nolint:errcheck
		}
		return nil // re-read the board fresh at the new station next pass
	}

	// Docked, and this is the only place in the pass that is: hull does not
	// grow back, so top it up here or carry the damage into every pass that
	// follows.
	huntRepairAtDock(ctx, deps, out, who, deps.Client.GetState())

	// What the agent has already COMPLETED is what lets a chain continuation
	// over the difficulty cap through. Read once per pass, before anything is
	// scored, and treated as empty on any failure.
	earned := huntEarnedContinuations(ctx, deps, out)

	// Step 2: continue a mission this agent already holds, or accept a new one
	// off the board.
	job, ok := huntSelectJob(ctx, deps, out, who, maxDifficulty, wildlifeOnly, earned)
	if !ok {
		return nil
	}

	// Whatever ends the pass — the objective met, the cap, a flee, an error —
	// report what the server counted, once. Deferred rather than repeated at
	// each exit because the exits that matter most for this signal are the
	// unhappy ones.
	kills, attempts := 0, 0
	reported := false
	report := func() {
		if reported {
			return
		}
		reported = true
		huntReportObjectiveProgress(ctx, deps, out, who, job.activeID, job.baseline, kills)
	}
	defer report()

	target := job.target

	// Steps 4-5: travel out to a hunting ground and read what is there. The
	// board is at a station and the quarry is not; without this leg the pass
	// reads "no creatures at this POI" at a station forever.
	quarry, ok := huntFindQuarry(ctx, deps, out, who, job, wildlifeOnly)
	if !ok {
		return nil
	}

	// Steps 6-7: hunt until the objective is met, the engagement cap is hit,
	// the pool of huntable creatures runs out, or the hull drops below the
	// flee threshold.
	for kills < target && attempts < huntMaxEngagements {
		// The between-engagement hull gate. It reads a value that only
		// get_status refreshes: parseBattleStatusData populates BattleState
		// and InBattle but never Ship.Hull, so without this free query the
		// threshold would be judged against whatever the last async
		// state_update happened to leave behind.
		if err := deps.Client.GetStatus(ctx); err != nil {
			fmt.Fprintf(out, "hunt: get_status before engagement: %v; held for next pass\n", err) //nolint:errcheck
			return nil
		}
		st := deps.Client.GetState()
		if st != nil && st.Ship.MaxHull > 0 && st.Ship.Hull/st.Ship.MaxHull < fleeAtHull {
			frac := st.Ship.Hull / st.Ship.MaxHull
			fmt.Fprintf(out, "hunt[%s]: hull %.0f%% below flee threshold %.0f%% after %d/%d kill(s); breaking off\n", //nolint:errcheck
				who, frac*100, fleeAtHull*100, kills, target)
			// Only meaningful while a battle is actually in progress: between
			// engagements the previous fight has resolved and the server
			// rejects the stance. The in-fight abort (huntPolicy) is what
			// actually escapes a fight.
			if st.InBattle {
				if ferr := deps.Client.Battle(ctx, "stance", map[string]any{"stance": huntStanceFlee}); ferr != nil {
					fmt.Fprintf(out, "hunt: flee stance failed: %v\n", ferr) //nolint:errcheck
				}
			}
			return nil
		}
		// Never open a second fight from inside the first. Giving up on a
		// quarry disengages, but the disengage is bounded and one of its exits
		// — the server answering can_escape=false — deliberately stops while
		// we are still a participant. Calling hunt from that state is refused,
		// and burning attempts against a refusal is worse than ending the pass
		// and starting the next one clean.
		if st != nil && st.InBattle {
			fmt.Fprintf(out, "hunt: still a participant in an unresolved battle after %d/%d kill(s); %s held for next pass\n", //nolint:errcheck
				kills, target, job.boardID)
			return nil
		}

		// Shields absorb first and grow back; hull does neither. Waiting here
		// is what keeps the whole engagement budget spendable — it costs
		// wall-clock and nothing else.
		huntWaitForShields(ctx, deps, out, who, st)

		c, idx := huntPickQuarry(quarry)
		if idx < 0 {
			fmt.Fprintf(out, "hunt: no huntable creatures remain (%d/%d killed); %s held for next pass\n", kills, target, job.boardID) //nolint:errcheck
			return nil
		}
		quarry = slices.Delete(quarry, idx, idx+1)

		if err := deps.Client.Hunt(ctx, c.CreatureID); err != nil {
			fmt.Fprintf(out, "hunt: engage %s failed: %v\n", c.CreatureID, err) //nolint:errcheck
			attempts++
			continue
		}
		attempts++

		outcome := huntFight(ctx, deps, out, fleeAtHull, c.CreatureID)

		// The carcass is the kill receipt: a wreck whose victim_id is the
		// creature we just engaged is the only evidence this pass has that
		// the fight was won. No wreck, no kill — an engagement that merely
		// ENDED proves nothing, since a battle ends when the quarry escapes
		// too.
		if outcome != huntFled {
			if huntLootCarcass(ctx, deps, out, c.CreatureID) {
				kills++
				fmt.Fprintf(out, "hunt[%s]: %s down (%d/%d)\n", who, c.CreatureID, kills, target) //nolint:errcheck
			} else {
				fmt.Fprintf(out, "hunt: no carcass for %s; not counting a kill\n", c.CreatureID) //nolint:errcheck
			}
		}
		// Fleeing ends the pass by design. A battle command that errored ends
		// it too: the worker may still be a participant in an unresolved
		// fight, and calling hunt on a second creature from inside one is
		// refused — so hold the mission and let the next pass start clean.
		switch outcome {
		case huntFled:
			fmt.Fprintf(out, "hunt: broke off at %d/%d kill(s); %s held for next pass\n", kills, target, job.boardID) //nolint:errcheck
			return nil
		case huntBattleError:
			fmt.Fprintf(out, "hunt: ending pass after a battle error at %d/%d kill(s); %s held for next pass\n", kills, target, job.boardID) //nolint:errcheck
			return nil
		case huntResolved, huntGaveUp:
		}
	}

	if kills < target {
		fmt.Fprintf(out, "hunt: %s at %d/%d kill(s); held for next pass\n", job.boardID, kills, target) //nolint:errcheck
		return nil
	}

	// Step 8: objective met — complete through the mission path. Report
	// before completing: a completed mission drops off the active list, and
	// the report reads that list.
	report()
	fmt.Fprintf(out, "hunt: %s objective met (%d/%d); completing\n", job.boardID, kills, target) //nolint:errcheck
	if err := deps.Client.CompleteMission(ctx, job.activeID); err != nil {
		fmt.Fprintf(out, "hunt: complete %s failed: %v; held for next pass\n", job.activeID, err) //nolint:errcheck
	}
	return nil
}

// huntJob is the one mission a pass works on, however it was obtained.
type huntJob struct {
	// boardID is the template-ish id the curated tables and the logs key on;
	// activeID is the hex instance id complete_mission needs. They differ, and
	// completing with the wrong one 404s.
	boardID  string
	activeID string
	title    string
	target   int // kills the objective asks for
	baseline int // kills the server had already counted when this pass started
	required string
	resumed  bool
}

// huntSelectJob picks the mission this pass will work on: a held one first,
// and only then a new one off the board.
//
// Resuming FIRST is the whole point. Eleven exits in this pass end with a
// mission "held for next pass" — a wrong ground, a hull break-off, the
// engagement cap — and until now nothing ever picked one back up. The mission
// kept its active slot, its partial kill count rotted to expiry, and with a
// five-mission allowlist the fleet converged on "no admissible mission on the
// board; idling" while every worker still looked healthy. Missions solves this
// the same way and for the same reason (mission.go's missionResume, run before
// anything is accepted).
func huntSelectJob(ctx context.Context, deps HuntDeps, out io.Writer, who string, maxDifficulty int, wildlifeOnly bool, earned map[string]string) (huntJob, bool) {
	if job, ok := huntResumeJob(ctx, deps, out, who, wildlifeOnly); ok {
		return job, true
	}
	return huntBoardJob(ctx, deps, out, who, maxDifficulty, wildlifeOnly, earned)
}

// huntResumeJob returns an already-accepted hunt mission to continue.
//
// Two rules about which gates apply on the way back in:
//
//   - Gate 2 (wildlife-only) STILL applies. If the operator revokes the
//     category mid-run the mission is left alone rather than resumed, which is
//     what Missions does for smuggling and for the same reason.
//   - Gate 1 (the difficulty cap) does NOT. The mission is already accepted and
//     already holding a slot; refusing to touch it because it is over today's
//     cap would recreate the exact deadlock this function exists to break. The
//     resume line names the difficulty so an over-cap resume is visible.
func huntResumeJob(ctx context.Context, deps HuntDeps, out io.Writer, who string, wildlifeOnly bool) (huntJob, bool) {
	if err := deps.Client.GetActiveMissions(ctx); err != nil {
		fmt.Fprintf(out, "hunt: get_active_missions: %v; cannot resume this pass\n", err) //nolint:errcheck
		return huntJob{}, false
	}
	raw := deps.Client.GetRawJSON("active_missions")
	if len(raw) == 0 {
		return huntJob{}, false
	}
	var resp serverapi.GetActiveMissionsResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		fmt.Fprintf(out, "hunt: parse active missions: %v; cannot resume this pass\n", err) //nolint:errcheck
		return huntJob{}, false
	}
	for _, m := range resp.Missions {
		o, ok := huntKillObjective(m)
		if !ok || o.Completed {
			continue
		}
		boardID := m.TemplateID
		if boardID == "" {
			boardID = m.MissionID
		}
		if wildlifeOnly && !huntWildlifeMissions[boardID] {
			fmt.Fprintf(out, "hunt: not resuming %s (%s): not a wildlife mission and wildlife-only is set\n", boardID, m.Title) //nolint:errcheck
			continue
		}
		target := o.Required
		if target <= 0 {
			target = 1
		}
		required, source := huntRequiredSpecies(serverapi.MissionBoardEntry{MissionID: boardID, Title: m.Title})
		fmt.Fprintf(out, "hunt[%s]: resuming %s (%s) at %d/%d kill(s), difficulty %d, species %q (%s)\n", //nolint:errcheck
			who, boardID, m.Title, o.Current, target, m.Difficulty, required, source)
		return huntJob{
			boardID: boardID, activeID: m.MissionID, title: m.Title,
			target: target, baseline: o.Current, required: required, resumed: true,
		}, true
	}
	return huntJob{}, false
}

// huntBoardJob reads the station board and accepts the first admissible entry,
// logging every refusal with its reason.
func huntBoardJob(ctx context.Context, deps HuntDeps, out io.Writer, who string, maxDifficulty int, wildlifeOnly bool, earned map[string]string) (huntJob, bool) {
	if err := deps.Client.GetMissions(ctx); err != nil {
		fmt.Fprintf(out, "hunt: get_missions: %v\n", err) //nolint:errcheck
		return huntJob{}, false
	}
	raw := deps.Client.GetRawJSON("missions")
	if len(raw) == 0 {
		fmt.Fprintln(out, "hunt: get_missions returned no data") //nolint:errcheck
		return huntJob{}, false
	}
	var board serverapi.GetMissionsResponse
	if err := json.Unmarshal(raw, &board); err != nil {
		fmt.Fprintf(out, "hunt: parse board: %v\n", err) //nolint:errcheck
		return huntJob{}, false
	}
	var chosen *serverapi.MissionBoardEntry
	for i := range board.Missions {
		e := board.Missions[i]
		ok, reason, waived := huntAdmissible(e, maxDifficulty, wildlifeOnly, earned)
		if !ok {
			fmt.Fprintf(out, "hunt: skip %s (%s): %s\n", e.MissionID, e.Title, reason) //nolint:errcheck
			continue
		}
		if waived != "" {
			// Loud on purpose: this is the fleet taking on something harder
			// than its cap, and an invisible exemption is how a fleet ends up
			// fighting what nobody chose for it.
			fmt.Fprintf(out, "hunt[%s]: CHAIN CONTINUATION: admitting %s at difficulty %d over cap %d, earned by completing %s\n", //nolint:errcheck
				who, e.MissionID, e.Difficulty, maxDifficulty, waived)
		}
		chosen = &e
		break
	}
	if chosen == nil {
		fmt.Fprintln(out, "hunt: no admissible mission on the board; idling") //nolint:errcheck
		return huntJob{}, false
	}
	activeID, baseline, ok := huntAcceptMission(ctx, deps, out, *chosen)
	if !ok {
		return huntJob{}, false
	}
	target := huntKillQuantity(*chosen)
	required, source := huntRequiredSpecies(*chosen)
	_, targetDesc := huntObjectiveTarget(*chosen)
	fmt.Fprintf(out, "hunt[%s]: accepted %s (%s) at %s; target %d kill(s), species %q (%s), desc=%q\n", //nolint:errcheck
		who, chosen.MissionID, chosen.Title, rfc(huntNow(deps)), target, required, source, targetDesc)
	return huntJob{
		boardID: chosen.MissionID, activeID: activeID, title: chosen.Title,
		target: target, baseline: baseline, required: required,
	}, true
}

// huntFindQuarry travels to a hunting ground and reads what is there, moving
// on to the NEXT ground when the one it reached holds none of the required
// species.
//
// The sweep is what keeps a resumed mission from re-flying into the same dead
// end every pass. Ground selection is deterministic given the KB, so without
// it a mission whose nearest qualifying belt happens to hold the wrong species
// would be resumed, flown to that belt, refused, and held — forever. Excluding
// the grounds already tried turns that into a search.
//
// It is bounded by huntMaxGrounds because each failed ground costs a real
// journey. If every ground in the sweep is wrong the mission is held again,
// and the next pass re-runs the sweep: the fleet keeps looking rather than
// giving up, and the WRONG POI lines say where it has been. AbandonMission is
// deliberately NOT called — first_hunt_belt_grazers is a chain mission and
// what abandoning does to a chain is unknown, which is a worse risk than a
// mission that keeps being retried.
func huntFindQuarry(ctx context.Context, deps HuntDeps, out io.Writer, who string, job huntJob, wildlifeOnly bool) ([]serverapi.NearbyCreature, bool) {
	var tried []string
	for range huntMaxGrounds {
		poi, err := huntTravelToWildlifePOI(ctx, deps, out, job.required, tried)
		if err != nil {
			if len(tried) > 0 {
				break // the sweep ran out of grounds; report it as a sweep
			}
			fmt.Fprintf(out, "hunt: reaching a wildlife POI: %v; %s held for next pass\n", err, job.boardID) //nolint:errcheck
			return nil, false
		}
		tried = append(tried, poi)

		if err := deps.Client.GetNearby(ctx); err != nil {
			fmt.Fprintf(out, "hunt: get_nearby: %v\n", err) //nolint:errcheck
			return nil, false
		}
		var nearby serverapi.GetNearbyResponse
		if raw := deps.Client.GetRawJSON("nearby"); len(raw) > 0 {
			if err := json.Unmarshal(raw, &nearby); err != nil {
				fmt.Fprintf(out, "hunt: parse nearby: %v\n", err) //nolint:errcheck
				return nil, false
			}
		}
		if quarry := huntAdmissibleQuarry(nearby.Creatures, wildlifeOnly, job.required, out); len(quarry) > 0 {
			return quarry, true
		}
		// Standing in the wrong place is its own outcome, and the one an
		// operator can act on: the mission is fine, the fleet is simply not
		// where its quarry lives. Say it distinctly, naming the species wanted
		// and what was actually there, and engage nothing — killing the
		// two-thirds of a grazer herd that does not count costs hull and ticks
		// and earns nothing.
		switch {
		case len(nearby.Creatures) == 0:
			fmt.Fprintf(out, "hunt: no creatures at %s; trying another ground\n", poi) //nolint:errcheck
		case job.required != "":
			fmt.Fprintf(out, "hunt[%s]: WRONG POI: %s needs %q and %s holds %s; no engagement attempted\n", //nolint:errcheck
				who, job.boardID, job.required, poi, huntSpeciesSeen(nearby.Creatures))
		default:
			fmt.Fprintf(out, "hunt: nothing huntable among %d creature(s) at %s\n", len(nearby.Creatures), poi) //nolint:errcheck
		}
	}
	fmt.Fprintf(out, "hunt: no quarry found across %d ground(s) %v; %s held for next pass\n", len(tried), tried, job.boardID) //nolint:errcheck
	return nil, false
}

// huntObjectiveTarget returns the first kill_creature objective's target id and
// description, for the accept log line.
//
// NO OBSERVED WILDLIFE MISSION CARRIES A TARGET ID. The operator's raw board
// capture gives the whole objective as
// {"description":"Hunt 3 Belt-Grazers at an asteroid belt","quantity":3,"type":"kill_creature"}
// — no target_id, item_id, species or system_id — and the KB's
// mission_objectives rows agree, for first_hunt, grazer_cull and
// nebula_drift_hunt alike. targetID is therefore empty in production today and
// everything keyed off it is inert. It stays because it costs nothing and a
// later mission may populate the field.
//
// The description is NOT a substitute. It is prose written for a human, and a
// parser over it is precisely the fragile inference this branch has already
// been burned by twice. Species scoping is enforced server-side; the pass logs
// the description verbatim so an operator can read it, and hunts eligible
// wildlife. huntReportObjectiveProgress is what catches the case where that
// turns out not to count.
func huntObjectiveTarget(e serverapi.MissionBoardEntry) (targetID, description string) {
	for _, o := range e.Objectives {
		if o.Type == objectiveKillCreature {
			return o.TargetID, o.Description
		}
	}
	return "", ""
}

// huntAcceptMission accepts board entry e and resolves its real (hex)
// active-mission id via get_active_missions, mirroring mission.go's
// resolveActiveMissionIDs: board ids are template-ish and complete_mission
// 404s with mission_not_found against one. AcceptMission's Submit already
// blocks for the action_result, but the server has been observed needing one
// more tick before get_active_missions reflects the new instance, so settle
// first. An id that still cannot be resolved is held rather than guessed —
// completing with the board id is documented to 404, which would throw away
// the whole hunt at the last step. ok=false means the pass must end.
// baseline is the server's own kill count for the resolved mission at accept
// time, which a resumed instance can start above zero.
func huntAcceptMission(ctx context.Context, deps HuntDeps, out io.Writer, e serverapi.MissionBoardEntry) (activeID string, baseline int, ok bool) {
	if err := deps.Client.AcceptMission(ctx, e.MissionID); err != nil {
		fmt.Fprintf(out, "hunt: accept %s failed: %v\n", e.MissionID, err) //nolint:errcheck
		return "", 0, false
	}
	_ = deps.sleep(ctx, game.SleepTick)
	if err := deps.Client.GetActiveMissions(ctx); err != nil {
		fmt.Fprintf(out, "hunt: accept %s: get_active_missions: %v; id unresolved, held for next pass\n", e.MissionID, err) //nolint:errcheck
		return "", 0, false
	}
	raw := deps.Client.GetRawJSON("active_missions")
	if len(raw) > 0 {
		var resp serverapi.GetActiveMissionsResponse
		if err := json.Unmarshal(raw, &resp); err != nil {
			fmt.Fprintf(out, "hunt: accept %s: parse active missions: %v\n", e.MissionID, err) //nolint:errcheck
		} else {
			for _, m := range resp.Missions {
				if m.TemplateID == e.MissionID || m.Title == e.Title {
					o, _ := huntKillObjective(m)
					return m.MissionID, o.Current, true
				}
			}
		}
	}
	fmt.Fprintf(out, "hunt: accept %s (%s): could not resolve active mission id; held for next pass (no completion attempted — the board id 404s)\n", e.MissionID, e.Title) //nolint:errcheck
	return "", 0, false
}

// huntKillObjective returns the server's own progress on m's kill_creature
// objective. ok=false when the mission carries no such objective.
func huntKillObjective(m serverapi.ActiveMission) (progress serverapi.ActiveMissionObjective, ok bool) {
	for _, o := range m.Objectives {
		if o.Type == objectiveKillCreature {
			return o, true
		}
	}
	return serverapi.ActiveMissionObjective{}, false
}

// huntObjectiveProgress re-reads what the SERVER counts for activeID, which is
// a different number from the carcasses this pass confirmed: the server scopes
// kill_creature to a species it never names in any machine-readable field, so
// a kill we are certain of may still count for nothing.
func huntObjectiveProgress(ctx context.Context, deps HuntDeps, activeID string) (o serverapi.ActiveMissionObjective, err error) {
	if err := deps.Client.GetActiveMissions(ctx); err != nil {
		return o, fmt.Errorf("get_active_missions: %w", err)
	}
	raw := deps.Client.GetRawJSON("active_missions")
	if len(raw) == 0 {
		return o, fmt.Errorf("get_active_missions returned no data")
	}
	var resp serverapi.GetActiveMissionsResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return o, fmt.Errorf("parse active missions: %w", err)
	}
	for _, m := range resp.Missions {
		if m.MissionID != activeID {
			continue
		}
		o, ok := huntKillObjective(m)
		if !ok {
			return o, fmt.Errorf("mission %s carries no %s objective", activeID, objectiveKillCreature)
		}
		return o, nil
	}
	return o, fmt.Errorf("mission %s is no longer in the active list", activeID)
}

// huntReportObjectiveProgress closes every pass that landed at least one
// confirmed kill by saying what the server made of them.
//
// This is the fleet's only warning for the failure the operator has now
// confirmed live: no wildlife mission carries a machine-readable target, the
// species scoping is enforced server-side, and so a pass standing at the wrong
// POI kills the wrong creatures cleanly and forever. huntMaxEngagements bounds
// each pass, but nothing bounds the number of passes — without this line a
// worker looks perfectly healthy while making no progress at all. Say it once,
// loudly, naming the POI, because the POI is the thing an operator can change.
func huntReportObjectiveProgress(ctx context.Context, deps HuntDeps, out io.Writer, who, missionID string, baseline, kills int) {
	if kills == 0 {
		return
	}
	poi := "an unknown POI"
	if st := deps.Client.GetState(); st != nil && st.CurrentPOI != "" {
		poi = st.CurrentPOI
	}
	o, err := huntObjectiveProgress(ctx, deps, missionID)
	if err != nil {
		fmt.Fprintf(out, "hunt[%s]: could not read %s objective progress after %d confirmed kill(s) at %s: %v\n", //nolint:errcheck
			who, missionID, kills, poi, err)
		return
	}
	if o.Current > baseline {
		fmt.Fprintf(out, "hunt[%s]: %s objective at %d/%d after %d confirmed kill(s) at %s\n", //nolint:errcheck
			who, missionID, o.Current, o.Required, kills, poi)
		return
	}
	fmt.Fprintf(out, "hunt[%s]: OBJECTIVE DID NOT ADVANCE: %d confirmed kill(s) at %s left %s on %d/%d — the wildlife at this POI does not count for this mission; move the pass or drop it\n", //nolint:errcheck
		who, kills, poi, missionID, o.Current, o.Required)
}

// huntAdmissibleQuarry filters a get_nearby creature list down to what this
// pass may engage, logging every refusal with its reason exactly as
// huntAdmissible does for the board. Three rules: never pile onto a fight
// already underway (InCombat — there is no field named IsAggressive on the
// wire, see hunt_gate.go); while wildlife-only is in force, never engage
// anything whose role is not in huntQuarryRoles; and when the mission is
// scoped to a species, engage nothing else.
//
// The species rule is a HARD filter, not a preference. Role cannot stand in
// for it: at one captured belt, slag_tortoise, patina_grazer and belt_grazer
// were all role "grazer" and only belt_grazer counted, so a role-only filter
// engages two wrong targets for every right one — each costing hull, ticks and
// an attempts slot, and earning nothing. required is empty for a mission
// nobody has scoped, and then this rule does not apply at all.
func huntAdmissibleQuarry(creatures []serverapi.NearbyCreature, wildlifeOnly bool, required string, out io.Writer) []serverapi.NearbyCreature {
	keep := make([]serverapi.NearbyCreature, 0, len(creatures))
	for _, c := range creatures {
		switch {
		case c.InCombat:
			fmt.Fprintf(out, "hunt: skip %s (%s): already in combat\n", c.CreatureID, c.Species) //nolint:errcheck
		case wildlifeOnly && !huntQuarryRoles[c.Role]:
			fmt.Fprintf(out, "hunt: skip %s (%s): role %q is not huntable wildlife\n", c.CreatureID, c.Species, c.Role) //nolint:errcheck
		case required != "" && c.Species != required && c.CreatureID != required:
			fmt.Fprintf(out, "hunt: skip %s: species %q does not count for this mission, which needs %q\n", c.CreatureID, c.Species, required) //nolint:errcheck
		default:
			keep = append(keep, c)
		}
	}
	return keep
}

// huntRepairAtDock tops the hull up while the pass is docked at the board
// station, when it is below huntRepairAtHull.
//
// This is the whole repair story, on purpose. The pass already docks to read
// the board, so a repair here costs no travel and no extra leg — it is one
// command at a place the worker is already standing. A mid-pass return to a
// station, and the Combat Support repair-kit path, are deliberately NOT built.
//
// A repair that fails is logged and swallowed, matching the neighbouring
// executors: it may cost credits the agent does not have, or the station may
// not offer the service, and neither is a reason to throw away a pass that can
// still hunt. The call is given its own deadline so a slow or unacknowledged
// reply cannot park a worker at the dock — GameClient.Repair waits on the
// default terminator, whose behaviour against a real repair reply is
// UNVERIFIED (the same shape recently hung Attack for five minutes).
func huntRepairAtDock(ctx context.Context, deps HuntDeps, out io.Writer, who string, st *game.State) {
	if st == nil || st.Ship.MaxHull <= 0 || st.Ship.Hull/st.Ship.MaxHull >= huntRepairAtHull {
		return
	}
	fmt.Fprintf(out, "hunt[%s]: hull %.0f%% (%.0f/%.0f) below %.0f%%; repairing at %s\n", //nolint:errcheck
		who, st.Ship.Hull/st.Ship.MaxHull*100, st.Ship.Hull, st.Ship.MaxHull, huntRepairAtHull*100, st.CurrentPOI)
	rctx, cancel := context.WithTimeout(ctx, game.SleepActionStartTimeout)
	defer cancel()
	if err := deps.Client.Repair(rctx); err != nil {
		fmt.Fprintf(out, "hunt: repair at %s: %v; hunting on the hull we have\n", st.CurrentPOI, err) //nolint:errcheck
		return
	}
	fmt.Fprintf(out, "hunt[%s]: repaired at %s\n", who, st.CurrentPOI) //nolint:errcheck
}

// huntWaitForShields holds the pass between engagements until the shield bar
// is back above huntShieldReadyFrac. It returns nothing: the caller re-reads
// get_status at the top of the next iteration anyway, and handing back a state
// that is one tick stale by the time it is used would be worse than nothing.
//
// It reads Ship.Shield from get_status, NOT shield_pct from the battle
// picture: shield_pct is a battle participant field and there is no battle in
// progress here. get_status is the only source between fights, and it is the
// same free query the hull gate above already spends.
//
// The recovery rate is MEASURED, never computed. The operator reports ~1/tick,
// the wire carries a shield_recharge field, and neither is trusted here: the
// loop simply waits for the number to rise and gives up the moment it stops,
// the same rule that governs max_weapon_reach. Waiting is bounded, and a wait
// that ends without full recovery still engages — an under-shielded engagement
// is a cost, not a hazard, and the hull gate is what makes it safe.
func huntWaitForShields(ctx context.Context, deps HuntDeps, out io.Writer, who string, st *game.State) {
	if st == nil || st.Ship.MaxShield <= 0 || st.Ship.Shield/st.Ship.MaxShield >= huntShieldReadyFrac {
		return
	}
	// The ship's own reported recharge decides whether waiting can work at all.
	// A hull that reports no recharge will not recover no matter how long the
	// pass stands still, and the measured loop below would only discover that
	// after burning a tick.
	if st.Ship.ShieldRecharge <= 0 {
		fmt.Fprintf(out, "hunt[%s]: shields %.0f%% and this hull reports no recharge; engaging as-is\n", //nolint:errcheck
			who, st.Ship.Shield/st.Ship.MaxShield*100)
		return
	}
	cur := st
	// The previous reading is kept as a NUMBER, not as the state it came from:
	// Client.GetState hands back a deep copy, but a caller that returns the
	// same struct twice would make a pointer comparison read every tick as
	// "no recovery" and defeat the wait.
	last := cur.Ship.Shield
	fmt.Fprintf(out, "hunt[%s]: shields %.0f%% (want %.0f%%); waiting to recover before the next engagement\n", //nolint:errcheck
		who, cur.Ship.Shield/cur.Ship.MaxShield*100, huntShieldReadyFrac*100)
	for range huntShieldWaitTicks {
		if err := deps.sleep(ctx, game.SleepTick); err != nil {
			return
		}
		if err := deps.Client.GetStatus(ctx); err != nil {
			fmt.Fprintf(out, "hunt: get_status while waiting on shields: %v\n", err) //nolint:errcheck
			return
		}
		next := deps.Client.GetState()
		if next == nil || next.Ship.MaxShield <= 0 {
			return
		}
		if next.Ship.Shield <= last {
			fmt.Fprintf(out, "hunt[%s]: shields stopped recovering at %.0f%%; engaging as-is\n", //nolint:errcheck
				who, next.Ship.Shield/next.Ship.MaxShield*100)
			return
		}
		last, cur = next.Ship.Shield, next
		if cur.Ship.Shield/cur.Ship.MaxShield >= huntShieldReadyFrac {
			fmt.Fprintf(out, "hunt[%s]: shields back to %.0f%%; engaging\n", who, cur.Ship.Shield/cur.Ship.MaxShield*100) //nolint:errcheck
			return
		}
	}
	fmt.Fprintf(out, "hunt[%s]: shields only %.0f%% after %d ticks; engaging as-is\n", //nolint:errcheck
		who, cur.Ship.Shield/cur.Ship.MaxShield*100, huntShieldWaitTicks)
}

// huntSpeciesSeen lists the distinct species in a get_nearby creature list, for
// the log line that tells an operator what the POI actually holds.
func huntSpeciesSeen(creatures []serverapi.NearbyCreature) string {
	seen := make([]string, 0, len(creatures))
	for _, c := range creatures {
		s := c.Species
		if s == "" {
			s = c.CreatureID
		}
		if !slices.Contains(seen, s) {
			seen = append(seen, s)
		}
	}
	if len(seen) == 0 {
		return "nothing"
	}
	return strings.Join(seen, ", ")
}

// huntPickQuarry selects the next creature to engage from an already-filtered
// pool: the SHORTEST FIGHT wins, the creature with the least hull left to chew
// through.
//
// Species is NOT considered here. It is a hard filter applied upstream in
// huntAdmissibleQuarry, so everything in this pool already counts for the
// mission and there is nothing left to prefer. Keeping the rule in one place
// is the point — a filter plus a preference over the same field is two places
// to get it wrong.
//
// NOTE — this deviates from the brief, which said to prefer full-hull ones,
// and the deviation was ratified in review.
//
// The strongest argument is that a long fight costs PERMANENT damage. Incoming
// damage lands on shields, which regenerate, but a quarry big enough that the
// fight outlasts the shield pool starts eating hull — and hull comes back only
// at a station. That is exactly where the operator's 82% hull came from: not a
// belt-grazer, but a ~120hp creature that outlived the shields before dying.
// A 50-point bar at ~3/tick net covers ~16 in-reach ticks, so at 18 damage a
// tick anything past roughly 290 hull is buying credits-and-a-dock worth of
// damage. Shortest-fight is the rule that keeps every engagement inside the
// renewable pool.
//
// The revenue side agrees: BOTH captured carcasses — different species, different systems
// — carry an identical single crystallized_biogas and salvage_value 5, so the
// loot does not scale with the quarry. A bigger creature is strictly more cost
// for identical revenue. On the cost side, a captured kill of a 70-hull grazer
// took 8 ticks and 12 hull, so the 220-hull Pilot-Whale seen beside a 45-hull
// Bell-Jelly is roughly three times both for the same one kill_creature tick —
// and with a hard huntMaxEngagements and a hull abort that ends the pass,
// ticks and damage per kill are what cap kills per pass. The brief's rule has
// no surviving purpose either: "don't engage something already being fought"
// is covered separately by the InCombat skip. Flip the comparison to revert.
func huntPickQuarry(creatures []serverapi.NearbyCreature) (serverapi.NearbyCreature, int) {
	best := -1
	bestHull := 0
	for i, c := range creatures {
		if best >= 0 && c.Hull >= bestHull {
			continue
		}
		bestHull = c.Hull
		best = i
	}
	if best < 0 {
		return serverapi.NearbyCreature{}, -1
	}
	return creatures[best], best
}

// huntPolicy is the per-tick fight controller, implementing spar.Policy so the
// engagement runs on spar.RunPolicyLoop rather than a second hand-rolled
// advance loop.
//
// It behaves like spar's aggressor — close the range, target, hold the fire
// stance, re-evaluated every tick because low-level wildlife flees and reopens
// the range — with four additions the sparring presets have no reason to
// carry: a hull abort that is strictly dominant over closing the range, a
// progress-based give-up for a quarry that cannot be caught, a bounded
// disengage that waits for the server's flee counter instead of walking away
// mid-escape, and a predator guard.
//
// Range is NUMERIC, not a zone-string ladder. A captured get_battle_status
// reply carries per-participant zone_distance (6) and combat_state
// max_weapon_reach (3): in range means distance <= reach. The zone label is a
// coarse name over that number. max_weapon_reach is read from the wire on
// every poll because it varies with the weapon fit.
type huntPolicy struct {
	fleeAtHull   float64
	wildlifeOnly bool
	// outcome is set once, when the policy decides to break off; the resolved
	// case never sets it (the loop just ends).
	outcome huntOutcome
	reason  string

	ticks        int
	disengaging  bool
	disengageFor int
	bestDist     int
	bestDistSet  bool
	lowEnemyHull int
	stallTicks   int
}

func (p *huntPolicy) Name() string { return "hunt" }

func (p *huntPolicy) Decide(v spar.View) spar.Action {
	p.ticks++

	// Breaking off is a multi-tick commitment: the server counts flee_counter
	// up to flee_required (3 in the capture) before the escape completes, so
	// hold the stance — issuing nothing that closes the range — until it
	// lands, the battle ends, or the bound runs out.
	if p.disengaging {
		p.disengageFor++
		if cs := v.CombatState; cs != nil {
			if cs.FleeRequired > 0 && cs.FleeCounter >= cs.FleeRequired {
				p.reason += fmt.Sprintf("; escaped after %d/%d flee count(s)", cs.FleeCounter, cs.FleeRequired)
				return spar.Action{Kind: spar.ActionStop}
			}
			if !cs.CanEscape {
				// The server says escape is not possible. Nothing this loop
				// can issue changes that; stop and let the pass end rather
				// than spending ticks on a stance that cannot complete.
				p.reason += "; server reports escape impossible (can_escape=false)"
				return spar.Action{Kind: spar.ActionStop}
			}
		}
		if p.disengageFor > huntDisengageTicks {
			p.reason += fmt.Sprintf("; escape unconfirmed after %d tick(s)", huntDisengageTicks)
			return spar.Action{Kind: spar.ActionStop}
		}
		if v.Self.Stance != huntStanceFlee {
			return spar.Action{Kind: spar.ActionBattle, BattleAction: "stance", Payload: map[string]any{"stance": huntStanceFlee}}
		}
		return spar.Action{Kind: spar.ActionNoop}
	}

	// Second line of defence on the no-predators rule. get_nearby's role field
	// is the first (huntAdmissibleQuarry); a battle participant carries the
	// same classifier as ship_name, so anything that got past the filter — or
	// wandered in — is caught here before a shot is exchanged.
	if p.wildlifeOnly {
		for _, e := range v.Enemies {
			if e.IsNPC && e.ShipName != "" && !huntQuarryRoles[e.ShipName] {
				return p.breakOff(huntFled, fmt.Sprintf("%s is a %s, not huntable wildlife", e.Username, e.ShipName))
			}
		}
	}

	// The hull abort is evaluated before any range logic and returns
	// immediately: evaluated after, or alongside, the loop would close the
	// very range it just decided to escape. A captured kill cost 12 hull
	// against a 70-hull grazer, so this gate is load-bearing. HullPct 0 is
	// read as "not reported" rather than "destroyed" — a destroyed ship is no
	// longer a participant, which ends the loop anyway.
	if v.Self.HullPct > 0 && float64(v.Self.HullPct)/100 < p.fleeAtHull {
		return p.breakOff(huntFled, fmt.Sprintf("hull %d%% below flee threshold %.0f%%", v.Self.HullPct, p.fleeAtHull*100))
	}

	if p.ticks > huntMaxBattleTicks {
		return p.breakOff(huntGaveUp, fmt.Sprintf("engagement exceeded %d ticks", huntMaxBattleTicks))
	}

	// Progress is either the numeric range falling below anything seen so far,
	// or the quarry losing hull. A fleeing quarry can otherwise keep the loop
	// busy indefinitely with neither side dying.
	dist := v.Self.ZoneDistance
	enemyHull := huntLowestEnemyHull(v)
	rng := huntRangeStatus(v)
	closedThisTick := false
	if dist > 0 && (!p.bestDistSet || dist < p.bestDist) {
		p.bestDist, p.bestDistSet, closedThisTick = dist, true, true
	}
	progressed := closedThisTick
	if enemyHull >= 0 && (p.lowEnemyHull == 0 || enemyHull < p.lowEnemyHull) {
		p.lowEnemyHull, progressed = enemyHull, true
	}
	if progressed {
		p.stallTicks = 0
	} else {
		p.stallTicks++
	}
	if p.stallTicks >= huntNoProgressTicks {
		// Three failure modes, and they need different reason strings because
		// the log is the only diagnosis a live pass leaves behind. Reporting a
		// kite when the truth is "we never closed" points the next reader in
		// exactly the wrong direction.
		var what string
		switch rng {
		case huntRangeUnknown:
			what = fmt.Sprintf("weapon reach unknown (no combat_state on the reply); closed to distance %d and stalled there", dist)
		case huntRangeOut:
			what = fmt.Sprintf("quarry is outrunning us (never closed inside weapon reach; distance %d, best %d)", dist, p.bestDist)
		case huntRangeIn:
			what = "quarry is kiting us (range closes and reopens, its hull never drops)"
		}
		return p.breakOff(huntGaveUp, fmt.Sprintf("no progress for %d ticks: %s", p.stallTicks, what))
	}

	// Out of reach: close. Reach unknown: keep closing for as long as closing
	// still works, then fight from wherever that left us — declaring
	// "in range" on absent data guarantees an engagement that can never land a
	// shot, and combat_state has exactly one capture behind it.
	if rng == huntRangeOut || (rng == huntRangeUnknown && (closedThisTick || !p.bestDistSet)) {
		return spar.Action{Kind: spar.ActionBattle, BattleAction: "advance"}
	}
	if v.Self.TargetID == "" && len(v.Enemies) > 0 && v.Enemies[0].PlayerID != "" {
		return spar.Action{Kind: spar.ActionBattle, BattleAction: "target", Payload: map[string]any{"target_id": v.Enemies[0].PlayerID}}
	}
	if v.Self.Stance != huntStanceFire {
		return spar.Action{Kind: spar.ActionBattle, BattleAction: "stance", Payload: map[string]any{"stance": huntStanceFire}}
	}
	return spar.Action{Kind: spar.ActionNoop}
}

// huntRangeStatus classifies the engagement range from the wire:
// zone_distance against combat_state.max_weapon_reach, both re-read every
// tick and neither ever hardcoded (max_weapon_reach varies with the fit).
//
// The unknown case is kept distinct from the in-range case on purpose. Folding
// it into "in range" is safe but yields nothing — the engagement can never
// land a shot — and it makes the give-up reason claim the quarry kited us when
// in truth we never closed.
type huntRangeState int

const (
	huntRangeIn      huntRangeState = iota // weapons reach
	huntRangeOut                           // too far to fire
	huntRangeUnknown                       // the reply did not say
)

func huntRangeStatus(v spar.View) huntRangeState {
	dist := v.Self.ZoneDistance
	if dist <= 0 {
		// No distance reported: nothing to steer on, so let the server
		// arbitrate an out-of-range shot rather than advancing blind.
		return huntRangeIn
	}
	if v.CombatState == nil || v.CombatState.MaxWeaponReach <= 0 {
		return huntRangeUnknown
	}
	if dist <= v.CombatState.MaxWeaponReach {
		return huntRangeIn
	}
	return huntRangeOut
}

// breakOff records why the fight is being abandoned and starts the disengage.
func (p *huntPolicy) breakOff(o huntOutcome, reason string) spar.Action {
	p.outcome, p.reason, p.disengaging = o, reason, true
	return spar.Action{Kind: spar.ActionBattle, BattleAction: "stance", Payload: map[string]any{"stance": huntStanceFlee}}
}

// huntLowestEnemyHull returns the lowest living enemy hull percentage, or -1
// when the view holds no enemies.
func huntLowestEnemyHull(v spar.View) int {
	low := -1
	for _, e := range v.Enemies {
		if low < 0 || e.HullPct < low {
			low = e.HullPct
		}
	}
	return low
}

// huntFight drives one engagement to its end on spar.RunPolicyLoop — the
// per-tick advance/target/fire loop that already exists and is unit-tested
// (pkg/spar/match_test.go), rather than a second copy of it here. Returns how
// the engagement ended; it never reports a kill, because the battle ending is
// not evidence of one.
func huntFight(ctx context.Context, deps HuntDeps, out io.Writer, fleeAtHull float64, creatureID string) huntOutcome {
	selfID := ""
	if st := deps.Client.GetState(); st != nil {
		selfID = st.Player.ID
	}
	if selfID == "" {
		fmt.Fprintf(out, "hunt: own player id unknown; cannot drive the fight vs %s\n", creatureID) //nolint:errcheck
		return huntBattleError
	}
	p := &huntPolicy{fleeAtHull: fleeAtHull, wildlifeOnly: huntWildlifeOnly(deps)}
	if err := spar.RunPolicyLoop(ctx, deps.Client, selfID, p, deps.tickSleep); err != nil {
		fmt.Fprintf(out, "hunt: fight vs %s ended on error after %d tick(s): %v\n", creatureID, p.ticks, err) //nolint:errcheck
		return huntBattleError
	}
	if p.reason != "" {
		fmt.Fprintf(out, "hunt: broke off vs %s after %d tick(s): %s\n", creatureID, p.ticks, p.reason) //nolint:errcheck
		return p.outcome
	}
	return huntResolved
}

// huntLootCarcass looks for the carcass of the creature just engaged and
// empties it. It doubles as the kill check: get_wrecks is free, and a wreck
// whose victim_id is this creature is the most direct proof the pass has that
// the fight was won. Returns whether that proof was found.
//
// victim_id is the primary key, not killer_id or type: killer_id alone cannot
// tell this pass's second kill from its first, and another hunter's carcass at
// the same belt matches a type-only filter.
func huntLootCarcass(ctx context.Context, deps HuntDeps, out io.Writer, creatureID string) bool {
	if err := deps.Client.GetWrecks(ctx); err != nil {
		fmt.Fprintf(out, "hunt: get_wrecks after %s: %v\n", creatureID, err) //nolint:errcheck
		return false
	}
	raw := deps.Client.GetRawJSON("wrecks")
	if len(raw) == 0 {
		return false
	}
	var resp serverapi.GetWrecksResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		fmt.Fprintf(out, "hunt: parse wrecks: %v\n", err) //nolint:errcheck
		return false
	}
	idx := slices.IndexFunc(resp.Wrecks, func(w serverapi.Wreck) bool { return w.VictimID == creatureID })
	if idx < 0 {
		return false
	}
	w := resp.Wrecks[idx]
	if w.Type != "" && w.Type != huntWreckTypeCreature {
		fmt.Fprintf(out, "hunt: carcass %s for %s has type %q, not %q\n", w.ID, creatureID, w.Type, huntWreckTypeCreature) //nolint:errcheck
	}

	// The hold is the only limit that matters here. Never jettison to make
	// room: a hunt's cargo is worth less than whatever is already aboard was
	// worth carrying.
	free := 0.0
	if st := deps.Client.GetState(); st != nil {
		free = st.Ship.CargoCapacity - st.Ship.CargoUsed
	}
	looted := 0.0
	for _, item := range w.Cargo {
		qty := min(item.Quantity, free-looted)
		if qty <= 0 {
			fmt.Fprintf(out, "hunt: hold full; leaving %.0f %s on carcass %s\n", item.Quantity, item.ItemID, w.ID) //nolint:errcheck
			break
		}
		if err := deps.Client.LootWreck(ctx, w.ID, item.ItemID, qty); err != nil {
			fmt.Fprintf(out, "hunt: loot %.0f %s from %s: %v\n", qty, item.ItemID, w.ID, err) //nolint:errcheck
			continue
		}
		looted += qty
		fmt.Fprintf(out, "hunt: looted %.0f %s from carcass %s\n", qty, item.ItemID, w.ID) //nolint:errcheck
	}
	// Salvage is the fallback for cargo the hold cannot take, and it consumes
	// the wreck — so only ever after the loot attempt, and only when nothing
	// came out.
	if looted == 0 && len(w.Cargo) > 0 && w.SalvageValue > 0 {
		if err := deps.Client.SalvageWreck(ctx, w.ID); err != nil {
			fmt.Fprintf(out, "hunt: salvage %s: %v\n", w.ID, err) //nolint:errcheck
		} else {
			fmt.Fprintf(out, "hunt: salvaged carcass %s for %d\n", w.ID, w.SalvageValue) //nolint:errcheck
		}
	}
	return true
}

// huntTravelToWildlifePOI moves the pass from the station board out to a POI
// that can hold wildlife (huntWildlifePOITypes), preferring one in the current
// system and otherwise routing to the nearest system that has one.
func huntTravelToWildlifePOI(ctx context.Context, deps HuntDeps, out io.Writer, required string, exclude []string) (string, error) {
	if deps.KB == nil {
		return "", fmt.Errorf("no knowledge base configured")
	}
	state := deps.Client.GetState()
	if state == nil {
		return "", fmt.Errorf("no cached state")
	}
	current := state.System.ID

	var destSystem, destPOI, why string
	if poiType, scoped := huntSpeciesPOIType[required]; scoped {
		// The mission names its quarry, so the ground is determined: search
		// only where that species lives, under the rules for that ground.
		destSystem, destPOI, why = huntFindGround(ctx, deps.KB, current, poiType, exclude)
		if destPOI == "" {
			return "", fmt.Errorf("no %s reachable from %s that suits %s", poiType, current, required)
		}
	} else {
		// Unscoped: any wildlife POI will do, nearest and least-worked first,
		// and the system we are already in is free.
		destSystem = current
		destPOI, why = huntLocalWildlifePOI(ctx, deps.KB, current, exclude)
		if destPOI == "" {
			for _, t := range huntWildlifePOITypes {
				sys, poi, reason := huntFindGround(ctx, deps.KB, current, t, exclude)
				if poi != "" {
					destSystem, destPOI, why = sys, poi, reason
					break
				}
			}
		}
	}
	if destPOI == "" {
		return "", fmt.Errorf("no known wildlife POI reachable from %s", current)
	}
	if state.CurrentPOI == destPOI && !state.Doc {
		return destPOI, nil // already parked on the quarry's doorstep
	}
	if state.Doc {
		if err := deps.Client.Undock(ctx); err != nil {
			return "", fmt.Errorf("undock: %w", err)
		}
	}
	fmt.Fprintf(out, "hunt: heading to %s/%s (%s) to find wildlife\n", destSystem, destPOI, why) //nolint:errcheck
	if err := Autopilot(ctx, AutopilotDeps{Client: deps.Client, Out: out}, destSystem, destPOI); err != nil {
		return "", fmt.Errorf("transit to %s: %w", destPOI, err)
	}
	return destPOI, nil
}

// huntResourceTier classes a candidate POI by what the KB knows about its
// resources. Lower is better. Unknown sits BETWEEN rich and exhausted: a POI
// nobody has surveyed may well be un-worked, so it must not be ranked with the
// ones we know are stripped, and must not outrank one we know is rich.
//
// score is the tie-break within a tier: richness * remaining summed over the
// live resources, so "rich AND un-worked" beats rich-but-thin and thick-but-poor
// alike. Both factors come off poi_resources unchanged; no scale is assumed
// beyond the two being comparable across rows of the same table.
const (
	huntResourcesLive = iota
	huntResourcesUnknown
	huntResourcesExhausted
)

// huntResourceTierNames label the tiers in the travel log line, so an operator
// reading "gas_cloud, mined out" knows the pass had nothing better to fly to.
var huntResourceTierNames = [...]string{"un-worked", "richness unknown", "mined out"}

func huntResourceTier(p knowledge.POI) (tier int, score float64) {
	if len(p.Resources) == 0 {
		return huntResourcesUnknown, 0
	}
	live := false
	for _, r := range p.Resources {
		if r.Remaining > 0 {
			live = true
			score += r.Richness * r.Remaining
		}
	}
	if !live {
		return huntResourcesExhausted, 0
	}
	return huntResourcesLive, score
}

// huntHasStation reports whether the KB knows of a station POI in systemID.
func huntHasStation(pois []knowledge.POI) bool {
	return slices.ContainsFunc(pois, func(p knowledge.POI) bool { return p.Type == huntPOITypeStation })
}

// huntCarriesResource reports whether a POI's KB resources include resourceID
// with anything left in it.
func huntCarriesResource(p knowledge.POI, resourceID string) bool {
	return slices.ContainsFunc(p.Resources, func(r game.POIResource) bool {
		return r.ResourceID == resourceID && r.Remaining > 0
	})
}

// huntGroundIn picks the best POI of poiType in systemID, or reports that this
// system is not a hunting ground at all.
//
// For an ASTEROID BELT the rule is the operator's, and it is predictive rather
// than reactive:
//
//	a POLICED, STATION-LESS system whose belt carries iron_ore.
//
// Grazers gather where the iron is still rich; a belt stays rich only while
// nobody mines it; and nobody mines iron_ore in a station-less system because
// the round trip — travel, jump, jump, travel, and back — is not worth iron's
// price. So the ABSENCE of a station is what predicts a rich belt, where
// richness data can only report one after the fact. Policed is the safety half:
// no surprise pirate attack mid-hunt, and hull lost to an ambush is permanent.
//
// Note what this implies for the pass: the board station's own system always
// has a station, so a belt hunt ALWAYS leaves the system it read the board in.
// That is the rule working, not a bug.
//
// For the other wildlife types the clauses do not apply — an ice field is not
// mined for iron — and only the policed preference carries over, which the
// caller relaxes if nothing policed is in range. Within a system, ties break on
// remaining first and richness second: both belts qualify, take the fuller one.
func huntGroundIn(ctx context.Context, kb knowledge.Base, systemID, poiType string, requirePoliced bool, exclude []string) (poiID, why string, ok bool) {
	if requirePoliced {
		sys, err := kb.GetSystem(ctx, systemID)
		// An unvisited system carries a map-import police_level of 0, so it
		// fails this test rather than being trusted — the safe direction.
		if err != nil || sys == nil || sys.PoliceLevel < huntPolicedMinLevel {
			return "", "", false
		}
	}
	pois, err := kb.GetPOIs(ctx, systemID)
	if err != nil {
		return "", "", false
	}
	belt := poiType == huntPOITypeBelt
	if belt && huntHasStation(pois) {
		return "", "", false // a station here means the belt is worked out
	}
	var (
		best          knowledge.POI
		bestRemaining float64
		bestRichness  float64
		found         bool
	)
	for _, p := range pois {
		if p.Type != poiType || slices.Contains(exclude, p.ID) {
			continue
		}
		if belt && !huntCarriesResource(p, huntBeltResource) {
			continue
		}
		remaining, richness := huntResourceTotals(p)
		if found && (remaining < bestRemaining || (remaining == bestRemaining && richness <= bestRichness)) {
			continue
		}
		best, bestRemaining, bestRichness, found = p, remaining, richness, true
	}
	if !found {
		return "", "", false
	}
	if belt {
		return best.ID, fmt.Sprintf("policed station-less %s, %.0f iron left", poiType, bestRemaining), true
	}
	policed := "policed"
	if !requirePoliced {
		policed = "unpoliced"
	}
	return best.ID, fmt.Sprintf("%s %s", policed, poiType), true
}

// huntResourceTotals sums what a POI has left and how rich it is, over the
// resources that still have something in them.
func huntResourceTotals(p knowledge.POI) (remaining, richness float64) {
	for _, r := range p.Resources {
		if r.Remaining > 0 {
			remaining += r.Remaining
			richness += r.Richness
		}
	}
	return remaining, richness
}

// huntFindGround walks outwards from the pass's current system for the nearest
// system that is a hunting ground for poiType.
//
// Nearest-qualifying wins rather than richest-anywhere, which is a deliberate
// departure from a global "order by remaining desc": every system that passes
// the belt rule is predicted rich by that rule, so the remaining tonnage is a
// tiebreak, while hops are fuel, wall-clock and one more chance to be jumped in
// a system we did not choose.
//
// The policed requirement is relaxed on a second pass for non-belt grounds, so
// a nebula hunt is not left with nowhere to go. It is NEVER relaxed for belts:
// there, policed and station-less are the rule itself.
func huntFindGround(ctx context.Context, kb knowledge.Base, from, poiType string, exclude []string) (systemID, poiID, why string) {
	graph := &galaxy.GalaxyGraph{}
	if err := graph.BuildFromDB(ctx, kb); err != nil {
		return "", "", ""
	}
	near, err := galaxy.FindNearestByPOIType(ctx, kb, graph, from, poiType, huntPOISearchLimit)
	if err != nil {
		return "", "", ""
	}
	for _, requirePoliced := range []bool{true, false} {
		for _, cand := range near {
			// FindNearest leaves NearestResult.POIs nil for POI-type lookups,
			// so the POI id has to come back out of the KB.
			poi, reason, ok := huntGroundIn(ctx, kb, cand.SystemID, poiType, requirePoliced, exclude)
			if ok {
				return cand.SystemID, poi, reason
			}
		}
		if poiType == huntPOITypeBelt {
			break
		}
	}
	return "", "", ""
}

// huntLocalWildlifePOI returns the id of a POI in systemID whose type can hold
// wildlife, or "" when the KB knows of none. why is a short human-readable
// account of why that one won, for the travel log.
//
// Ranking, in order:
//
//  1. Resource tier. The mission dialog states the mechanic outright —
//     "Grazers gather where the iron is still rich, so a quiet, un-worked belt
//     holds far more than a busy, mined-out one" — so a stripped POI is the
//     last place to go looking. commerce_fields is the live example: richness
//     75, remaining 0, and the operator's capture there found scavengers
//     instead of grazers.
//  2. huntWildlifePOITypes' order, so an asteroid belt beats a gas cloud among
//     equally-worked candidates.
//  3. richness * remaining, highest first.
//
// Tier leads type deliberately: a rich gas cloud is a better bet than a belt
// with nothing left in it, which is what "when no belt qualifies, fall back to
// the other types" means in practice. Nothing here hard-fails on missing
// poi_resources rows — that is what the unknown tier is for.
func huntLocalWildlifePOI(ctx context.Context, kb knowledge.Base, systemID string, exclude []string) (poiID, why string) {
	pois, err := kb.GetPOIs(ctx, systemID)
	if err != nil {
		return "", ""
	}
	var (
		best      string
		bestType  = len(huntWildlifePOITypes)
		bestTier  int
		bestScore float64
		bestName  string
	)
	for _, p := range pois {
		rank := slices.Index(huntWildlifePOITypes, p.Type)
		if rank < 0 || slices.Contains(exclude, p.ID) {
			continue
		}
		tier, score := huntResourceTier(p)
		better := best == ""
		switch {
		case best == "":
		case tier != bestTier:
			better = tier < bestTier
		case rank != bestType:
			better = rank < bestType
		default:
			better = score > bestScore
		}
		if better {
			best, bestType, bestTier, bestScore, bestName = p.ID, rank, tier, score, p.Type
		}
	}
	if best == "" {
		return "", ""
	}
	return best, fmt.Sprintf("%s, %s", bestName, huntResourceTierNames[bestTier])
}

// huntRecoverToStation routes a worker that is not at a dockable station to
// the nearest known accessible one, reusing the same recovery primitive Haul
// (haulRecoverIfStranded) and Missions (missionRecoverToStation) use rather
// than writing new travel code. It docks on arrival: the next thing a pass
// does is read a board, which only exists at a station one is docked at.
func huntRecoverToStation(ctx context.Context, deps HuntDeps, out io.Writer, current string) error {
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
	// FindNearest never populates NearestResult.POIs for a POI-type lookup,
	// so the station POI id has to be resolved from the KB — passing the
	// empty POIs[0].ID would autopilot to the system and stop in open space.
	station, serr := missionStationPOI(ctx, deps.KB, dest.SystemID)
	if serr != nil || station == "" {
		return fmt.Errorf("no accessible station POI known in %s (err=%v)", dest.SystemID, serr)
	}
	name := dest.SystemName
	if name == "" {
		name = dest.SystemID
	}
	fmt.Fprintf(out, "hunt: relocating to %s (%s/%s, %d jump(s))\n", name, dest.SystemID, station, dest.Hops) //nolint:errcheck
	if err := Autopilot(ctx, AutopilotDeps{Client: deps.Client, Out: out}, dest.SystemID, station); err != nil {
		return fmt.Errorf("relocation transit: %w", err)
	}
	if err := deps.Client.Dock(ctx); err != nil {
		return fmt.Errorf("dock at %s: %w", station, err)
	}
	return nil
}
