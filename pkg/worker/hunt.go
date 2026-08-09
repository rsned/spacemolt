package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
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
	// loop runs at game.SleepTick, one action per tick). Low-level wildlife
	// flees and reopens the range, so an engagement is a chase, not an
	// exchange: without a per-fight bound a single grazer can hold the pass
	// for as long as it keeps running.
	huntMaxBattleTicks = 30
	// huntNoProgressTicks is the real give-up signal, and it is a PROGRESS
	// bound rather than a liveness one. Progress means either the range
	// closed (a better zone than any reached so far) or the quarry lost hull.
	// After this many consecutive ticks with neither, the chase is not going
	// to be won: the quarry is outrunning us (the zone never improves) or
	// kiting us (the zone closes and reopens while its hull never drops).
	huntNoProgressTicks = 6
	// huntDisengageTicks bounds the break-off. The flee stance auto-retreats
	// over several ticks, so the pass keeps polling with advancing disabled
	// until the server agrees the battle is over — abandoning a quarry by
	// simply moving on would leave the worker a participant in an unresolved
	// fight, still in range and still being shot.
	huntDisengageTicks = 6

	// huntStanceFlee / huntStanceFire are the battle stances this pass uses.
	huntStanceFlee = "flee"
	huntStanceFire = "fire"

	// huntWreckTypeCreature is the wreck type a killed creature leaves.
	// Corroborating only: victim_id is the identity that matters.
	huntWreckTypeCreature = "creature"
)

// huntWildlifePOITypes are the KB pois.type values that hold wildlife. The
// mission board only exists at a station, so a pass reads the board docked and
// then travels out to one of these.
var huntWildlifePOITypes = []string{"asteroid_belt", "gas_cloud", "ice_field"}

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
	// sleep is the post-accept settle delay (nil -> craftPollSleepFunc, the
	// ctx-aware real sleep). Mirrors MissionDeps.sleep; tests inject a
	// zero-delay stand-in so the suite doesn't accumulate real waits.
	sleep func(ctx context.Context, d time.Duration) error
	// tickSleep paces the in-battle policy loop (0 -> game.SleepTick, one
	// action per server tick). Injected as ~0 by tests for the same reason as
	// sleep: a multi-tick chase would otherwise cost the suite minutes.
	tickSleep time.Duration
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

	// Step 3: the counted kill_creature objective is the target. Log the
	// objective's target_id and description verbatim: whether the server
	// scopes kill_creature progress by species is not answerable from any
	// captured payload, and this line is what makes the next live pass answer
	// it.
	target := huntKillQuantity(*chosen)
	targetID, targetDesc := huntObjectiveTarget(*chosen)
	fmt.Fprintf(out, "hunt[%s]: accepted %s (%s) at %s; target %d kill(s), objective target_id=%q desc=%q\n", //nolint:errcheck
		who, chosen.MissionID, chosen.Title, rfc(huntNow(deps)), target, targetID, targetDesc)

	// Step 4: travel out to a POI that can hold wildlife. The board is at a
	// station and the quarry is not; without this leg the pass reads
	// "no creatures at this POI" at a station forever.
	if err := huntTravelToWildlifePOI(ctx, deps, out); err != nil {
		fmt.Fprintf(out, "hunt: reaching a wildlife POI: %v; %s held for next pass\n", err, chosen.MissionID) //nolint:errcheck
		return nil
	}

	// Step 5: an empty belt is a normal outcome (the herd moved, or it's
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

	// Steps 6-7: hunt until the objective is met, the engagement cap is hit,
	// the pool of huntable creatures runs out, or the hull drops below the
	// flee threshold.
	quarry := nearby.Creatures
	kills, attempts := 0, 0
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

		c, idx := huntPickQuarry(quarry, targetID)
		if idx < 0 {
			fmt.Fprintf(out, "hunt: no huntable creatures remain (%d/%d killed); %s held for next pass\n", kills, target, chosen.MissionID) //nolint:errcheck
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
			fmt.Fprintf(out, "hunt: broke off at %d/%d kill(s); %s held for next pass\n", kills, target, chosen.MissionID) //nolint:errcheck
			return nil
		case huntBattleError:
			fmt.Fprintf(out, "hunt: ending pass after a battle error at %d/%d kill(s); %s held for next pass\n", kills, target, chosen.MissionID) //nolint:errcheck
			return nil
		case huntResolved, huntGaveUp:
		}
	}

	if kills < target {
		fmt.Fprintf(out, "hunt: %s at %d/%d kill(s); held for next pass\n", chosen.MissionID, kills, target) //nolint:errcheck
		return nil
	}

	// Step 8: objective met — complete through the mission path.
	fmt.Fprintf(out, "hunt: %s objective met (%d/%d); completing\n", chosen.MissionID, kills, target) //nolint:errcheck
	if err := deps.Client.CompleteMission(ctx, activeID); err != nil {
		fmt.Fprintf(out, "hunt: complete %s failed: %v; held for next pass\n", activeID, err) //nolint:errcheck
	}
	return nil
}

// huntObjectiveTarget returns the first kill_creature objective's target id and
// description, for the accept log line.
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
func huntAcceptMission(ctx context.Context, deps HuntDeps, out io.Writer, e serverapi.MissionBoardEntry) (activeID string, ok bool) {
	if err := deps.Client.AcceptMission(ctx, e.MissionID); err != nil {
		fmt.Fprintf(out, "hunt: accept %s failed: %v\n", e.MissionID, err) //nolint:errcheck
		return "", false
	}
	_ = deps.sleep(ctx, game.SleepTick)
	if err := deps.Client.GetActiveMissions(ctx); err != nil {
		fmt.Fprintf(out, "hunt: accept %s: get_active_missions: %v; id unresolved, held for next pass\n", e.MissionID, err) //nolint:errcheck
		return "", false
	}
	raw := deps.Client.GetRawJSON("active_missions")
	if len(raw) > 0 {
		var resp serverapi.GetActiveMissionsResponse
		if err := json.Unmarshal(raw, &resp); err != nil {
			fmt.Fprintf(out, "hunt: accept %s: parse active missions: %v\n", e.MissionID, err) //nolint:errcheck
		} else {
			for _, m := range resp.Missions {
				if m.TemplateID == e.MissionID || m.Title == e.Title {
					return m.MissionID, true
				}
			}
		}
	}
	fmt.Fprintf(out, "hunt: accept %s (%s): could not resolve active mission id; held for next pass (no completion attempted — the board id 404s)\n", e.MissionID, e.Title) //nolint:errcheck
	return "", false
}

// huntPickQuarry selects the next creature to engage from live: creatures
// already InCombat are skipped (do not pile onto a fight already underway —
// there is no field named IsAggressive on the wire, see hunt_gate.go), and
// among the rest the highest hull-fraction survivor is preferred. When the
// objective names a target, creatures matching it by species or creature id
// win over any non-matching one regardless of hull — killing the wrong
// species may not advance the objective at all. Returns idx=-1 when every
// remaining creature is InCombat.
func huntPickQuarry(creatures []serverapi.NearbyCreature, targetID string) (serverapi.NearbyCreature, int) {
	best := -1
	bestFrac := -1.0
	bestMatch := false
	for i, c := range creatures {
		if c.InCombat {
			continue
		}
		frac := 1.0
		if c.MaxHull > 0 {
			frac = float64(c.Hull) / float64(c.MaxHull)
		}
		match := targetID != "" && (c.Species == targetID || c.CreatureID == targetID)
		switch {
		case match && !bestMatch:
			// First objective match seen: it outranks any healthier stranger.
		case match == bestMatch && frac > bestFrac:
			// Same class of candidate, healthier.
		default:
			continue
		}
		bestMatch = match
		bestFrac = frac
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
// It behaves like spar's aggressor — close to the engaged zone, target, hold
// the fire stance, re-evaluated every tick because low-level wildlife flees and
// reopens the range — with three additions the sparring presets have no reason
// to carry: a hull abort that is strictly dominant over closing the range, a
// progress-based give-up for a quarry that cannot be caught, and a bounded
// disengage so breaking off actually breaks contact instead of leaving the
// worker a participant in a fight it has stopped playing.
type huntPolicy struct {
	fleeAtHull float64
	// outcome is set once, when the policy decides to break off; the resolved
	// case never sets it (the loop just ends).
	outcome huntOutcome
	reason  string

	ticks        int
	disengaging  bool
	disengageFor int
	bestZone     int
	bestZoneSet  bool
	lowEnemyHull int
	stallTicks   int
}

func (p *huntPolicy) Name() string { return "hunt" }

func (p *huntPolicy) Decide(v spar.View) spar.Action {
	p.ticks++

	// Breaking off is a multi-tick affair: the flee stance auto-retreats over
	// several ticks, so hold it (issuing nothing that closes the range) until
	// the server ends the battle or the budget runs out.
	if p.disengaging {
		p.disengageFor++
		if p.disengageFor > huntDisengageTicks {
			return spar.Action{Kind: spar.ActionStop}
		}
		if v.Self.Stance != huntStanceFlee {
			return spar.Action{Kind: spar.ActionBattle, BattleAction: "stance", Payload: map[string]any{"stance": huntStanceFlee}}
		}
		return spar.Action{Kind: spar.ActionNoop}
	}

	// The hull abort is evaluated before any zone logic and returns
	// immediately: evaluated after, or alongside, the loop would close the
	// very range it just decided to escape. HullPct 0 is read as "not
	// reported" rather than "destroyed" — a destroyed ship is no longer a
	// participant, which ends the loop anyway.
	if v.Self.HullPct > 0 && float64(v.Self.HullPct)/100 < p.fleeAtHull {
		return p.breakOff(huntFled, fmt.Sprintf("hull %d%% below flee threshold %.0f%%", v.Self.HullPct, p.fleeAtHull*100))
	}

	if p.ticks > huntMaxBattleTicks {
		return p.breakOff(huntGaveUp, fmt.Sprintf("engagement exceeded %d ticks", huntMaxBattleTicks))
	}

	// Progress is either range closed or quarry hull lost. A fleeing quarry
	// can otherwise keep the loop busy indefinitely with neither side dying.
	zone := spar.ZoneRank(v.Self.Zone)
	enemyHull := huntLowestEnemyHull(v)
	progressed := false
	if !p.bestZoneSet || zone > p.bestZone {
		p.bestZone, p.bestZoneSet, progressed = zone, true, true
	}
	if enemyHull >= 0 && (p.lowEnemyHull == 0 || enemyHull < p.lowEnemyHull) {
		p.lowEnemyHull, progressed = enemyHull, true
	}
	if progressed {
		p.stallTicks = 0
	} else {
		p.stallTicks++
	}
	if p.stallTicks >= huntNoProgressTicks {
		// The two failure modes worth telling apart in the log.
		what := "quarry is kiting us (range closes and reopens, its hull never drops)"
		if p.bestZone < spar.ZoneRank(spar.ZoneEngaged) {
			what = "quarry is outrunning us (never closed past zone " + v.Self.Zone + ")"
		}
		return p.breakOff(huntGaveUp, fmt.Sprintf("no progress for %d ticks: %s", p.stallTicks, what))
	}

	if zone < spar.ZoneRank(spar.ZoneEngaged) {
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
	p := &huntPolicy{fleeAtHull: fleeAtHull}
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
func huntTravelToWildlifePOI(ctx context.Context, deps HuntDeps, out io.Writer) error {
	if deps.KB == nil {
		return fmt.Errorf("no knowledge base configured")
	}
	state := deps.Client.GetState()
	if state == nil {
		return fmt.Errorf("no cached state")
	}
	current := state.System.ID

	destSystem, destPOI := current, huntLocalWildlifePOI(ctx, deps.KB, current)
	if destPOI == "" {
		galGraph := &galaxy.GalaxyGraph{}
		if err := galGraph.BuildFromDB(ctx, deps.KB); err != nil {
			return fmt.Errorf("build galaxy graph: %w", err)
		}
		best := -1
		for _, t := range huntWildlifePOITypes {
			near, err := galaxy.FindNearestByPOIType(ctx, deps.KB, galGraph, current, t, 1)
			if err != nil || len(near) == 0 {
				continue
			}
			if best < 0 || near[0].Hops < best {
				// FindNearest leaves NearestResult.POIs nil for POI-type
				// lookups, so the POI id has to come back out of the KB.
				poi := huntLocalWildlifePOI(ctx, deps.KB, near[0].SystemID)
				if poi == "" {
					continue
				}
				best, destSystem, destPOI = near[0].Hops, near[0].SystemID, poi
			}
		}
	}
	if destPOI == "" {
		return fmt.Errorf("no known wildlife POI reachable from %s", current)
	}
	if state.CurrentPOI == destPOI && !state.Doc {
		return nil // already parked on the quarry's doorstep
	}
	if state.Doc {
		if err := deps.Client.Undock(ctx); err != nil {
			return fmt.Errorf("undock: %w", err)
		}
	}
	fmt.Fprintf(out, "hunt: heading to %s/%s to find wildlife\n", destSystem, destPOI) //nolint:errcheck
	if err := Autopilot(ctx, AutopilotDeps{Client: deps.Client, Out: out}, destSystem, destPOI); err != nil {
		return fmt.Errorf("transit to %s: %w", destPOI, err)
	}
	return nil
}

// huntLocalWildlifePOI returns the id of a POI in systemID whose type can hold
// wildlife, or "" when the KB knows of none.
func huntLocalWildlifePOI(ctx context.Context, kb knowledge.Base, systemID string) string {
	pois, err := kb.GetPOIs(ctx, systemID)
	if err != nil {
		return ""
	}
	for _, p := range pois {
		if slices.Contains(huntWildlifePOITypes, p.Type) {
			return p.ID
		}
	}
	return ""
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
