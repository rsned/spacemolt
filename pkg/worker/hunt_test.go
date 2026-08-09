package worker

import (
	"context"
	"encoding/json"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/spar"
)

// The three constants below are VERBATIM captures from the live server
// (2026-08-08, craftsman-1), stored under
// .superpowers/sdd/2026-08-08-wildlife-hunt-fleet/. They are pasted rather
// than hand-composed on purpose: an invented get_nearby payload has already
// put two field names into a struct on this branch that the server never
// sends, and the test built on it passed against the wrong type.
//
// The only edits the fake makes are to the identifiers — the engaged
// creature's id, so a test can run several engagements — never to the shape.
const (
	// huntSelfID is the capturing agent's real player id, so killer_id in the
	// wreck captures and our own participant id in the battle capture match
	// without editing either.
	huntSelfID = "a50924913cef881c5e4d14257589d9ba"

	// huntRealBattleStatusReply is THE get_battle_status reply — note it has
	// no "action" key, which is why the raw store and the parse are shape-gated.
	huntRealBattleStatusReply = `{"battle_id":"43598fe6756f61668661c9aeb8e68b8a","combat_state":{"can_escape":true,"effective_speed":1,"em_disrupted":false,"flee_counter":0,"flee_required":3,"max_weapon_reach":3,"warp_disrupted":false,"webbed":false},"is_participant":true,"participants":[{"auto_pilot":false,"hull_pct":100,"is_npc":true,"kind":"creature","player_id":"crt_cfbce87e4800ee17fa8e12b9e6adb8cb","ship_name":"grazer","side_id":1,"username":"Hollow Pilgrim","zone":"outer","zone_distance":6},{"auto_pilot":true,"hull_pct":100,"kind":"player","player_id":"a50924913cef881c5e4d14257589d9ba","shield_pct":100,"ship_class":"prospect","side_id":2,"stance":"fire","target_id":"crt_cfbce87e4800ee17fa8e12b9e6adb8cb","username":"Arthur 'Artificer' Artis","zone":"outer","zone_distance":6}],"sides":[{"player_count":1,"side_id":1},{"faction_id":"e727c0e918d994c72db2978fe5b18edc","faction_name":"Crafting Collective","faction_tag":"CRFT","player_count":1,"side_id":2}],"system_id":"gold_run"}`

	// huntRealWreckOurs is the Drift-Ray carcass; the fake rewrites victim_id
	// to whichever creature the pass just engaged.
	huntRealWreckOurs = `{"cargo":[{"item_id":"crystallized_biogas","quantity":1,"size":1}],"created_at":"2026-08-09T00:23:13.142752742Z","expire_tick":1565621,"expires_at":"2026-08-09T00:53:13.142752742Z","id":"f80b0e98d825bfed35c3d1efbd9a2eaa","killer_id":"a50924913cef881c5e4d14257589d9ba","killer_name":"Arthur 'Artificer' Artis","modules":[],"poi_id":"market_prime_gas_plume","salvage_value":5,"ship_class":"","system_id":"market_prime","type":"creature","victim_id":"crt_d439a40cf658db0487e1be6bbe26a215","victim_name":"Drift-Ray"}`

	// huntRealWreckDecoy is the Frost-Moth carcass, used UNMODIFIED as a
	// wreck that is not ours: same type, same killer_id, different victim_id.
	// That is exactly the case killer_id and type cannot tell apart.
	huntRealWreckDecoy = `{"cargo":[{"item_id":"crystallized_biogas","quantity":1,"size":1}],"created_at":"2026-08-09T00:36:03.106837197Z","expire_tick":1565698,"expires_at":"2026-08-09T01:06:03.106837197Z","id":"78f7f0cb82e4f4ab27376594c319c73a","killer_id":"a50924913cef881c5e4d14257589d9ba","killer_name":"Arthur 'Artificer' Artis","modules":[],"poi_id":"gold_run_cryobelt","salvage_value":5,"ship_class":"","system_id":"gold_run","type":"creature","victim_id":"crt_1504baf945ab8e7faff70e1f4c7d146c","victim_name":"Frost-Moth"}`
)

// boardMission is the shorthand newHuntFake uses to build a combat board
// entry with a single kill_creature objective.
type boardMission struct {
	id         string
	difficulty int
	quantity   int
	targetID   string // objective target_id, when the mission names its quarry
}

// huntCreature describes one creature in the get_nearby fixture.
type huntCreature struct {
	id       string
	species  string
	role     string // "" -> grazer
	hull     int
	inCombat bool
}

// huntFakeOpts configures newHuntFake.
type huntFakeOpts struct {
	board     []boardMission
	creatures []huntCreature
	// activeMissions overrides the default (one active instance per board
	// entry with a hex-ish id); noActive models the accept not having landed.
	activeMissions []serverapi.ActiveMission
	noActive       bool
	// objectiveStuck freezes the server's kill_creature counter at zero while
	// carcasses still appear — the live failure where a pass kills the wrong
	// species cleanly and the mission never advances.
	objectiveStuck bool
	// battleHullPct is our own hull percentage the battle picture reports
	// during the FIRST fight. 0 means full hull throughout.
	battleHullPct int
	// shipHullFrac is the hull fraction get_status reports from the start of
	// the pass (the between-engagement gate reads Ship.Hull, which only
	// get_status refreshes). 0 means full.
	shipHullFrac float64
	// neverCloses models a quarry that outruns us: advance never reduces
	// zone_distance, so the range never reaches weapon reach.
	neverCloses bool
	// noWreck suppresses the carcass, so a kill can never be confirmed.
	noWreck bool
	// lingerInBattle keeps State.InBattle set after the fight picture goes
	// non-participant, modelling the disengage exit that stops while the
	// server still considers us a combatant (can_escape=false).
	lingerInBattle bool
	// cargoCapacity/cargoUsed size the hold for the loot clamp.
	cargoCapacity float64
	cargoUsed     float64
	// startPOI/unsetDocked place the worker; the default is docked at the
	// station whose board it reads.
	startPOI    string
	unsetDocked bool
}

// huntFake wraps the shared fakeClient (dispatch_test.go) with a battle
// simulator seeded from the REAL get_battle_status capture, because the defect
// this suite exists to catch lives entirely inside a fight: a canned "you are
// not a participant" reply resolves every engagement instantly and proves
// nothing about closing range, breaking off, or confirming a kill.
//
// The simulation is literal about the mechanics the executor depends on, and
// every number in it comes off the wire: the fight opens at zone_distance 6
// against max_weapon_reach 3; `advance` closes one unit per tick and moves
// both participants (the capture shows the two sides sharing a distance);
// damage lands only within reach with a target set and the fire stance; the
// flee stance takes flee_required counts to complete; and a dead creature
// leaves a carcass keyed by victim_id.
type huntFake struct {
	*fakeClient
	opts huntFakeOpts

	huntCalls     int
	fled          bool
	battleActions []string
	lootCalls     []string
	salvageCalls  []string

	// actives is the active-mission list the fake re-serves on every
	// get_active_missions, with the kill_creature counter recomputed from the
	// carcasses so far — the server's count, which is not the same number as
	// the pass's confirmed kills once objectiveStuck is set.
	actives []serverapi.ActiveMission

	quarryID   string
	battle     serverapi.GetBattleStatusResponse
	battleLive bool
	fleeing    bool
	wrecks     []serverapi.Wreck
}

func newHuntFake(t *testing.T, opts huntFakeOpts) *huntFake {
	t.Helper()

	entries := make([]serverapi.MissionBoardEntry, 0, len(opts.board))
	actives := make([]serverapi.ActiveMission, 0, len(opts.board))
	for _, m := range opts.board {
		entries = append(entries, serverapi.MissionBoardEntry{
			MissionID:  m.id,
			Type:       "combat",
			Title:      m.id,
			Difficulty: m.difficulty,
			Objectives: []serverapi.MissionObjective{{
				Type: "kill_creature", Quantity: m.quantity,
				TargetID: m.targetID, Description: "cull the herd",
			}},
		})
		actives = append(actives, serverapi.ActiveMission{
			MissionID: "hex-" + m.id, TemplateID: m.id, Title: m.id, Type: "combat",
			Objectives: []serverapi.ActiveMissionObjective{{
				Type: "kill_creature", Description: "cull the herd", Required: m.quantity,
			}},
		})
	}
	if opts.activeMissions != nil {
		actives = opts.activeMissions
	}
	if opts.noActive {
		actives = nil
	}

	creatures := make([]serverapi.NearbyCreature, 0, len(opts.creatures))
	for _, c := range opts.creatures {
		species := c.species
		if species == "" {
			species = "ash_scarab"
		}
		role := c.role
		if role == "" {
			role = "grazer"
		}
		hull := c.hull
		if hull == 0 {
			hull = 45
		}
		creatures = append(creatures, serverapi.NearbyCreature{
			CreatureID: c.id, Species: species, Role: role,
			Hull: hull, MaxHull: hull, InCombat: c.inCombat,
		})
	}
	nearbyRaw, err := json.Marshal(serverapi.GetNearbyResponse{
		Creatures: creatures, CreatureCount: len(creatures), POIID: "commerce_fields",
	})
	if err != nil {
		t.Fatalf("marshal nearby fixture: %v", err)
	}

	poi := opts.startPOI
	if poi == "" {
		poi = "haven_station"
	}
	capacity := opts.cargoCapacity
	if capacity == 0 {
		capacity = 100
	}

	f := &fakeClient{
		state: &game.State{
			Player:     game.Player{ID: huntSelfID},
			System:     game.SystemData{ID: "test_system"},
			CurrentPOI: poi,
			Doc:        !opts.unsetDocked,
			Ship: game.Ship{
				Hull: 100, MaxHull: 100,
				CargoCapacity: capacity, CargoUsed: opts.cargoUsed,
			},
		},
		raw: map[string][]byte{
			"missions":        boardJSON(t, entries...),
			"nearby":          nearbyRaw,
			"active_missions": activeJSON(t, actives...),
		},
	}
	return &huntFake{fakeClient: f, opts: opts, actives: actives}
}

// GetActiveMissions re-serves the active list with the server's kill_creature
// counter set from the carcasses this fight has produced. The executor reads
// this number, not its own; objectiveStuck holds it at zero to model the live
// case where confirmed kills do not count for the accepted mission.
func (f *huntFake) GetActiveMissions(ctx context.Context) error {
	if err := f.fakeClient.GetActiveMissions(ctx); err != nil {
		return err
	}
	if f.actives == nil {
		return nil
	}
	counted := len(f.wrecks)
	if f.opts.objectiveStuck {
		counted = 0
	}
	live := make([]serverapi.ActiveMission, len(f.actives))
	copy(live, f.actives)
	for i := range live {
		live[i].Objectives = slices.Clone(live[i].Objectives)
		for j := range live[i].Objectives {
			if live[i].Objectives[j].Type == "kill_creature" {
				live[i].Objectives[j].Current = counted
			}
		}
	}
	b, err := json.Marshal(serverapi.GetActiveMissionsResponse{Missions: live, TotalCount: len(live)})
	if err != nil {
		return err
	}
	f.raw["active_missions"] = b
	return nil
}

// huntDeps builds the deps every test uses: zero-delay sleeps so a multi-tick
// chase costs the suite nothing, and a KB holding one station and one belt in
// the worker's system so the station->belt leg can run for real.
func huntDeps(c *huntFake, out io.Writer) HuntDeps {
	return HuntDeps{
		Client: c, KB: huntKB(), Out: out, AgentID: "pirate-6",
		MaxDifficulty: 1, WildlifeOnly: huntBool(true),
		sleep:     func(context.Context, time.Duration) error { return nil },
		tickSleep: time.Nanosecond,
	}
}

func huntBool(b bool) *bool { return &b }

func huntKB() knowledge.Base {
	kb := knowledge.NewMemoryKB()
	ctx := context.Background()
	_ = kb.RememberSystem(ctx, knowledge.System{ID: "test_system", Name: "Test"})
	_ = kb.RememberPOI(ctx, knowledge.POI{ID: "haven_station", SystemID: "test_system", Type: "station"})
	_ = kb.RememberPOI(ctx, knowledge.POI{ID: "commerce_fields", SystemID: "test_system", Type: "asteroid_belt"})
	_ = kb.RememberBase(ctx, knowledge.SpaceBase{ID: "haven_station", POIID: "haven_station", PublicAccess: true})
	return kb
}

// huntRealReply decodes the captured reply. Decoding it at all is part of what
// these tests assert: the capture sends side_id as a NUMBER, which a
// string-typed field rejected outright, and a reply that will not decode is a
// battle picture that never reaches the executor.
func huntRealReply(t *testing.T) serverapi.GetBattleStatusResponse {
	t.Helper()
	var resp serverapi.GetBattleStatusResponse
	if err := json.Unmarshal([]byte(huntRealBattleStatusReply), &resp); err != nil {
		t.Fatalf("the captured get_battle_status reply must decode: %v", err)
	}
	if len(resp.Participants) != 2 || resp.CombatState == nil {
		t.Fatalf("captured reply decoded to %+v", resp)
	}
	return resp
}

func (f *huntFake) GetStatus(ctx context.Context) error {
	if err := f.fakeClient.GetStatus(ctx); err != nil {
		return err
	}
	if f.opts.shipHullFrac > 0 {
		f.state.Ship.Hull = f.opts.shipHullFrac * f.state.Ship.MaxHull
	}
	return nil
}

func (f *huntFake) Undock(ctx context.Context) error {
	f.state.Doc = false
	return f.fakeClient.Undock(ctx)
}

func (f *huntFake) Travel(ctx context.Context, poi string) (*game.TravelResult, error) {
	res, err := f.fakeClient.Travel(ctx, poi)
	if err == nil {
		f.state.CurrentPOI = poi
	}
	return res, err
}

func (f *huntFake) GetNearby(ctx context.Context) error {
	f.calls = append(f.calls, "get_nearby")
	return nil
}

// self and quarry return pointers into the simulated battle picture.
func (f *huntFake) self() *serverapi.BattleParticipant {
	for i := range f.battle.Participants {
		if f.battle.Participants[i].PlayerID == huntSelfID {
			return &f.battle.Participants[i]
		}
	}
	return nil
}

func (f *huntFake) quarry() *serverapi.BattleParticipant {
	for i := range f.battle.Participants {
		if f.battle.Participants[i].PlayerID != huntSelfID {
			return &f.battle.Participants[i]
		}
	}
	return nil
}

func (f *huntFake) Hunt(ctx context.Context, creatureID string) error {
	f.huntCalls++
	f.calls = append(f.calls, "hunt:"+creatureID)
	f.quarryID = creatureID

	var resp serverapi.GetBattleStatusResponse
	if err := json.Unmarshal([]byte(huntRealBattleStatusReply), &resp); err != nil {
		return err
	}
	f.battle = resp
	// Point the capture at the creature this test engaged; everything else
	// (zone_distance 6, max_weapon_reach 3, flee_required 3, is_npc, kind,
	// ship_name, the numeric side ids) stays exactly as the server sent it.
	q := f.quarry()
	q.PlayerID = creatureID
	f.self().TargetID = ""
	f.self().Stance = ""
	if f.huntCalls == 1 && f.opts.battleHullPct > 0 {
		f.self().HullPct = f.opts.battleHullPct
	}
	f.battleLive = true
	f.fleeing = false
	f.publishBattle()
	return nil
}

// GetBattleStatus advances the simulated fight one tick and republishes the
// battle picture through the same converter the real client uses.
func (f *huntFake) GetBattleStatus(ctx context.Context) error {
	f.calls = append(f.calls, "get_battle_status")
	switch {
	case !f.battleLive:
	case f.fleeing:
		f.battle.CombatState.FleeCounter++
		if f.battle.CombatState.FleeCounter >= f.battle.CombatState.FleeRequired {
			f.battleLive = false // the escape completed
		}
	case f.inReach() && f.self().Stance == "fire" && f.self().TargetID != "":
		q := f.quarry()
		q.HullPct -= 60
		if q.HullPct <= 0 {
			q.HullPct = 0
			f.battleLive = false
			f.recordCarcass()
		}
	}
	f.publishBattle()
	return nil
}

func (f *huntFake) inReach() bool {
	return f.self().ZoneDistance <= f.battle.CombatState.MaxWeaponReach
}

func (f *huntFake) publishBattle() {
	if !f.battleLive {
		f.state.BattleState = &game.BattleState{IsParticipant: false}
		f.state.InBattle = f.opts.lingerInBattle
		return
	}
	f.state.InBattle = true
	f.state.BattleState = game.BattleStateFromResponse(&f.battle)
}

func (f *huntFake) recordCarcass() {
	if f.opts.noWreck {
		return
	}
	var w serverapi.Wreck
	if err := json.Unmarshal([]byte(huntRealWreckOurs), &w); err != nil {
		panic(err)
	}
	w.VictimID = f.quarryID
	w.ID = "wreck_" + f.quarryID
	f.wrecks = append(f.wrecks, w)
}

func (f *huntFake) Battle(ctx context.Context, action string, payload map[string]any) error {
	label := action
	if s, ok := payload["stance"].(string); ok {
		label += ":" + s
	}
	if t, ok := payload["target_id"].(string); ok {
		label += ":" + t
	}
	f.calls = append(f.calls, "battle:"+label)
	f.battleActions = append(f.battleActions, label)
	switch action {
	case "advance":
		// Both participants share the distance in the capture, so close both.
		if !f.opts.neverCloses {
			for i := range f.battle.Participants {
				if f.battle.Participants[i].ZoneDistance > 0 {
					f.battle.Participants[i].ZoneDistance--
				}
			}
		}
	case "target":
		f.self().TargetID, _ = payload["target_id"].(string)
	case "stance":
		f.self().Stance, _ = payload["stance"].(string)
		if f.self().Stance == "flee" {
			f.fled = true
			f.fleeing = true
		}
	}
	return nil
}

func (f *huntFake) GetWrecks(ctx context.Context) error {
	f.calls = append(f.calls, "get_wrecks")
	var decoy serverapi.Wreck
	if err := json.Unmarshal([]byte(huntRealWreckDecoy), &decoy); err != nil {
		return err
	}
	all := append([]serverapi.Wreck{decoy}, f.wrecks...)
	b, err := json.Marshal(serverapi.GetWrecksResponse{Wrecks: all, Count: len(all)})
	if err != nil {
		return err
	}
	f.raw["wrecks"] = b
	return nil
}

func (f *huntFake) LootWreck(ctx context.Context, wreckID, itemID string, quantity float64) error {
	f.lootCalls = append(f.lootCalls, wreckID+"/"+itemID)
	f.calls = append(f.calls, "loot:"+wreckID+":"+itemID)
	f.state.Ship.CargoUsed += quantity
	return nil
}

func (f *huntFake) SalvageWreck(ctx context.Context, wreckID string) error {
	f.salvageCalls = append(f.salvageCalls, wreckID)
	f.calls = append(f.calls, "salvage:"+wreckID)
	return nil
}

func huntGrazers(ids ...string) []huntCreature {
	out := make([]huntCreature, 0, len(ids))
	for _, id := range ids {
		out = append(out, huntCreature{id: id})
	}
	return out
}

func called(f *huntFake, want string) bool { return strings.Contains(strings.Join(f.calls, " "), want) }

func countAction(f *huntFake, want string) int {
	n := 0
	for _, a := range f.battleActions {
		if a == want {
			n++
		}
	}
	return n
}

// A gated pass must not issue a hunt at all.
func TestHuntRefusesOverCapMission(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{
		board:     []boardMission{{id: "leviathan_bounty", difficulty: 6, quantity: 1}},
		creatures: huntGrazers("c1"),
	})
	var log strings.Builder
	if err := Hunt(context.Background(), huntDeps(c, &log)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if c.huntCalls != 0 {
		t.Errorf("hunt issued %d times against an over-cap mission, want 0", c.huntCalls)
	}
	if !strings.Contains(log.String(), "difficulty 6 over cap 1") {
		t.Errorf("refusal must be logged with its reason, got:\n%s", log.String())
	}
}

// The counted objective drives the loop: 3 grazers means 3 hunts.
func TestHuntKillsToObjectiveCount(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{
		board:     []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 3}},
		creatures: huntGrazers("c1", "c2", "c3", "c4"),
	})
	if err := Hunt(context.Background(), huntDeps(c, io.Discard)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if c.huntCalls != 3 {
		t.Errorf("hunt calls = %d, want 3 (the objective quantity)", c.huntCalls)
	}
}

// No creatures at this POI is a normal outcome, not an error: the herd moved or
// the belt is mined thin. It must not fail the pass.
//
// huntCalls==0 alone does not isolate the empty-belt check: the engagement
// loop's own huntPickQuarry also returns "nothing to hunt" for a nil quarry
// slice, so a version of Hunt with the check deleted still reports zero hunts
// and a nil error. The log assertion is what distinguishes them.
func TestHuntNoCreaturesIsNotAnError(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{
		board:     []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 3}},
		creatures: nil,
	})
	var log strings.Builder
	if err := Hunt(context.Background(), huntDeps(c, &log)); err != nil {
		t.Fatalf("an empty belt must not fail the pass: %v", err)
	}
	if c.huntCalls != 0 {
		t.Error("no creatures means no hunts")
	}
	if !strings.Contains(log.String(), "no creatures at this POI") {
		t.Errorf("must log the empty-belt outcome specifically, got:\n%s", log.String())
	}
}

// The flee threshold aborts the hunt rather than trading the hull for a kill —
// and it aborts from INSIDE the fight, which is the only window in which a
// hunting worker actually takes damage (a captured kill cost 12 hull).
func TestHuntFleesBelowHullThreshold(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{
		board:         []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 3}},
		creatures:     huntGrazers("c1", "c2", "c3"),
		battleHullPct: 10, // 10% hull once the first fight is joined
	})
	var log strings.Builder
	deps := huntDeps(c, &log)
	deps.FleeAtHull = 0.3
	if err := Hunt(context.Background(), deps); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if c.huntCalls != 1 {
		t.Errorf("hunt calls = %d, want 1 — the pass must stop at the flee threshold", c.huntCalls)
	}
	if !c.fled {
		t.Error("dropping below the flee threshold must issue a flee stance")
	}
	// The abort must dominate: nothing that closes the range may be issued in
	// a fight it has already decided to escape.
	if len(c.battleActions) == 0 || c.battleActions[0] != "stance:flee" {
		t.Errorf("first battle action = %v, want stance:flee before anything that closes the range", c.battleActions)
	}
	if countAction(c, "advance") != 0 {
		t.Errorf("advanced while fleeing: %v", c.battleActions)
	}
}

// The between-engagement gate reads Ship.Hull, which only get_status
// refreshes; a worker that started the pass already wounded never engages.
func TestHuntBreaksOffBeforeFirstEngagement(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{
		board:        []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 3}},
		creatures:    huntGrazers("c1", "c2"),
		shipHullFrac: 0.1,
	})
	var log strings.Builder
	deps := huntDeps(c, &log)
	deps.FleeAtHull = 0.3
	if err := Hunt(context.Background(), deps); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if c.huntCalls != 0 {
		t.Errorf("hunt calls = %d, want 0 — a wounded worker must not engage at all", c.huntCalls)
	}
	if !strings.Contains(log.String(), "below flee threshold") {
		t.Errorf("must log the break-off, got:\n%s", log.String())
	}
}

// Closing the range is the whole fight, and the range is a NUMBER: the capture
// opens at zone_distance 6 against max_weapon_reach 3, so exactly three
// advances are needed before a shot can land.
func TestHuntAdvancesUntilInWeaponReach(t *testing.T) {
	reply := huntRealReply(t)
	wantAdvances := reply.Participants[0].ZoneDistance - reply.CombatState.MaxWeaponReach
	if wantAdvances != 3 {
		t.Fatalf("fixture drift: expected 6-3=3 advances, computed %d", wantAdvances)
	}

	c := newHuntFake(t, huntFakeOpts{
		board:     []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 1}},
		creatures: huntGrazers("c1"),
	})
	var log strings.Builder
	if err := Hunt(context.Background(), huntDeps(c, &log)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if got := countAction(c, "advance"); got != wantAdvances {
		t.Errorf("advances = %d (%v), want %d — one per unit of zone_distance above max_weapon_reach",
			got, c.battleActions, wantAdvances)
	}
	if !called(c, "battle:target:c1") {
		t.Errorf("must target the quarry once in reach, actions: %v", c.battleActions)
	}
	if !called(c, "battle:stance:fire") {
		t.Errorf("must hold the fire stance, actions: %v", c.battleActions)
	}
	if !strings.Contains(log.String(), "c1 down (1/1)") {
		t.Errorf("the fight must end in a confirmed kill, got:\n%s", log.String())
	}
}

// A quarry that outruns us is abandoned on a progress bound, not chased until
// the tick budget runs out — and the pass disengages before moving on rather
// than calling hunt on a second creature mid-battle.
func TestHuntGivesUpOnAQuarryItCannotCatch(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{
		board:       []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 1}},
		creatures:   huntGrazers("c1"),
		neverCloses: true,
	})
	var log strings.Builder
	if err := Hunt(context.Background(), huntDeps(c, &log)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if !strings.Contains(log.String(), "no progress for") || !strings.Contains(log.String(), "outrunning us") {
		t.Errorf("must log the give-up and which failure mode it was, got:\n%s", log.String())
	}
	if got := countAction(c, "advance"); got > huntNoProgressTicks+1 {
		t.Errorf("advanced %d times against an uncatchable quarry; the progress bound should stop it near %d",
			got, huntNoProgressTicks)
	}
	if !c.fled {
		t.Error("abandoning a quarry must disengage first, not just move on")
	}
}

// The carcass is the kill receipt AND the cargo. It is matched by victim_id:
// the decoy here is a REAL second capture with the same type and the same
// killer_id, which is exactly what those two fields cannot tell apart.
func TestHuntLootsItsOwnCarcassOnly(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{
		board:     []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 1}},
		creatures: huntGrazers("c1"),
	})
	if err := Hunt(context.Background(), huntDeps(c, io.Discard)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	want := []string{"wreck_c1/crystallized_biogas"}
	if len(c.lootCalls) != len(want) || c.lootCalls[0] != want[0] {
		t.Fatalf("loot calls = %v, want %v", c.lootCalls, want)
	}
	// 78f7f0cb… is the Frost-Moth capture: another kill's carcass.
	for _, got := range c.lootCalls {
		if strings.HasPrefix(got, "78f7f0cb") {
			t.Errorf("looted a carcass that was not ours: %v", c.lootCalls)
		}
	}
}

// A full hold stops the looting rather than jettisoning to make room, and the
// carcass is salvaged instead so the kill is not a total loss.
func TestHuntLeavesCargoWhenTheHoldIsFull(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{
		board:         []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 1}},
		creatures:     huntGrazers("c1"),
		cargoCapacity: 10,
		cargoUsed:     10,
	})
	var log strings.Builder
	if err := Hunt(context.Background(), huntDeps(c, &log)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if len(c.lootCalls) != 0 {
		t.Errorf("a full hold must loot nothing, got %v", c.lootCalls)
	}
	if !strings.Contains(log.String(), "hold full") {
		t.Errorf("must log the skip, got:\n%s", log.String())
	}
	if len(c.salvageCalls) != 1 || c.salvageCalls[0] != "wreck_c1" {
		t.Errorf("salvage calls = %v, want [wreck_c1] as the fallback", c.salvageCalls)
	}
}

// No carcass, no kill. A battle ending proves nothing: it ends when the quarry
// escapes, and when we die, too.
func TestHuntWithoutACarcassCountsNoKill(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{
		board:     []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 1}},
		creatures: huntGrazers("c1", "c2"),
		noWreck:   true,
	})
	var log strings.Builder
	if err := Hunt(context.Background(), huntDeps(c, &log)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if !strings.Contains(log.String(), "not counting a kill") {
		t.Errorf("an unconfirmed kill must say so, got:\n%s", log.String())
	}
	if called(c, "complete:") {
		t.Errorf("must not complete a mission whose objective was never met: %v", c.calls)
	}
}

// The failure the operator confirmed live: the pass kills cleanly, the
// carcasses are real, and the server's counter never moves because the mission
// is scoped to a species that does not live here. Nothing else in the pass
// looks wrong, so the log line is the entire signal.
func TestHuntWarnsWhenConfirmedKillsDoNotAdvanceTheObjective(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{
		board:          []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 2}},
		creatures:      huntGrazers("c1"),
		objectiveStuck: true,
	})
	var log strings.Builder
	if err := Hunt(context.Background(), huntDeps(c, &log)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if !strings.Contains(log.String(), "OBJECTIVE DID NOT ADVANCE") {
		t.Errorf("a pass that kills without advancing must say so, got:\n%s", log.String())
	}
	// The POI is the thing an operator can change, so it has to be named.
	if !strings.Contains(log.String(), "commerce_fields") {
		t.Errorf("the warning must name the POI hunted, got:\n%s", log.String())
	}
}

// The same report on the happy path states the server's own count, so the two
// numbers can be compared in a log without guessing which one moved.
func TestHuntReportsServerObjectiveProgress(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{
		board:     []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 1}},
		creatures: huntGrazers("c1"),
	})
	var log strings.Builder
	if err := Hunt(context.Background(), huntDeps(c, &log)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if !strings.Contains(log.String(), "objective at 1/1 after 1 confirmed kill(s)") {
		t.Errorf("want the server's own count reported, got:\n%s", log.String())
	}
	if strings.Contains(log.String(), "OBJECTIVE DID NOT ADVANCE") {
		t.Errorf("an advancing objective must not be reported as stuck, got:\n%s", log.String())
	}
	// The read has to happen BEFORE the completion, not after: completing
	// takes the mission off the active list the report reads.
	if lastCall(c, "get_active_missions") > firstCall(c, "complete:") {
		t.Errorf("progress must be read before complete_mission, calls: %v", c.calls)
	}
}

func firstCall(f *huntFake, prefix string) int {
	return slices.IndexFunc(f.calls, func(s string) bool { return strings.HasPrefix(s, prefix) })
}

func lastCall(f *huntFake, prefix string) int {
	last := -1
	for i, s := range f.calls {
		if strings.HasPrefix(s, prefix) {
			last = i
		}
	}
	return last
}

// A pass that confirmed nothing has nothing to compare, and must not spend a
// query saying so.
func TestHuntDoesNotReportProgressWithoutAConfirmedKill(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{
		board:     []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 1}},
		creatures: huntGrazers("c1"),
		noWreck:   true,
	})
	var log strings.Builder
	if err := Hunt(context.Background(), huntDeps(c, &log)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if strings.Contains(log.String(), "OBJECTIVE DID NOT ADVANCE") || strings.Contains(log.String(), "objective at ") {
		t.Errorf("no confirmed kill means no progress report, got:\n%s", log.String())
	}
}

// Completion uses the RESOLVED (hex) active-mission id: complete_mission with
// a board id 404s with mission_not_found.
func TestHuntCompletesWithTheResolvedMissionID(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{
		board:     []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 1}},
		creatures: huntGrazers("c1"),
	})
	if err := Hunt(context.Background(), huntDeps(c, io.Discard)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if !called(c, "complete:hex-first_hunt_belt_grazers") {
		t.Errorf("want complete with the resolved active id, calls: %v", c.calls)
	}
	if called(c, "complete:first_hunt_belt_grazers ") {
		t.Errorf("completed with the board id, calls: %v", c.calls)
	}
}

// An unresolved active id is held, not guessed: completing with the board id
// throws the whole hunt away at the last step.
func TestHuntHoldsWhenTheActiveIDCannotBeResolved(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{
		board:     []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 1}},
		creatures: huntGrazers("c1"),
		noActive:  true,
	})
	var log strings.Builder
	if err := Hunt(context.Background(), huntDeps(c, &log)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if c.huntCalls != 0 {
		t.Errorf("hunt calls = %d, want 0 — nothing to complete means nothing worth killing for", c.huntCalls)
	}
	if !strings.Contains(log.String(), "could not resolve active mission id") {
		t.Errorf("must log why the pass was held, got:\n%s", log.String())
	}
	if called(c, "complete:") {
		t.Errorf("must not complete with an unresolved id, calls: %v", c.calls)
	}
}

// Do not pile onto a fight already underway: an InCombat creature is skipped
// even when it would otherwise be the pick.
func TestHuntSkipsCreaturesAlreadyInCombat(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{
		board: []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 1}},
		creatures: []huntCreature{
			{id: "busy", hull: 20, inCombat: true},
			{id: "free", hull: 45},
		},
	})
	var log strings.Builder
	if err := Hunt(context.Background(), huntDeps(c, &log)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if called(c, "hunt:busy") {
		t.Errorf("engaged a creature already in combat: %v", c.calls)
	}
	if !called(c, "hunt:free") {
		t.Errorf("want the free creature engaged, calls: %v", c.calls)
	}
	if !strings.Contains(log.String(), "already in combat") {
		t.Errorf("must log the refusal, got:\n%s", log.String())
	}
}

// A PREDATOR is never engaged while wildlife-only is in force. The whole
// safety case for a difficulty-1 fleet is that its quarry does not
// meaningfully fight back; the captured Tempest-Eel is a 280-hull predator
// sitting at full hull beside 45-hull grazers, so any "healthiest first" or
// "first in the list" rule walks straight into it.
func TestHuntRefusesToEngageAPredator(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{
		board: []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 1}},
		creatures: []huntCreature{
			{id: "eel", species: "tempest_eel", role: "predator", hull: 280},
			{id: "jelly", species: "bell_jelly", role: "grazer", hull: 45},
		},
	})
	var log strings.Builder
	if err := Hunt(context.Background(), huntDeps(c, &log)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if called(c, "hunt:eel") {
		t.Fatalf("engaged a predator: %v", c.calls)
	}
	if !called(c, "hunt:jelly") {
		t.Errorf("want the grazer engaged, calls: %v", c.calls)
	}
	if !strings.Contains(log.String(), `role "predator" is not huntable wildlife`) {
		t.Errorf("must log the refusal with the role named, got:\n%s", log.String())
	}
}

// The no-predator rule must survive a caller who never heard of the field.
// This test deliberately builds HuntDeps by hand and leaves WildlifeOnly
// unset: with a plain bool, the zero value is the UNSAFE state, and a Task 6
// dispatch that forgot the field would silently make a 280-hull Tempest-Eel
// admissible quarry — which the weakest-first rule would then walk into on a
// belt that held nothing else.
func TestHuntWildlifeOnlyDefaultsOnWhenUnset(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{
		board: []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 1}},
		creatures: []huntCreature{
			{id: "eel", species: "tempest_eel", role: "predator", hull: 280},
		},
	})
	var log strings.Builder
	deps := HuntDeps{
		Client: c, KB: huntKB(), Out: &log, AgentID: "pirate-6",
		MaxDifficulty: 1, // NOTE: WildlifeOnly deliberately not set.
		sleep:         func(context.Context, time.Duration) error { return nil },
		tickSleep:     time.Nanosecond,
	}
	if deps.WildlifeOnly != nil {
		t.Fatal("this test is only meaningful with WildlifeOnly left unset")
	}
	if err := Hunt(context.Background(), deps); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if c.huntCalls != 0 {
		t.Fatalf("engaged a predator with WildlifeOnly unset: %v", c.calls)
	}
	if !strings.Contains(log.String(), `role "predator" is not huntable wildlife`) {
		t.Errorf("the interlock must be on by default and log its refusal, got:\n%s", log.String())
	}
}

// An unknown or absent role is refused too: not hunting is the safe failure.
func TestHuntRefusesACreatureWithNoRole(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{
		board:     []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 1}},
		creatures: []huntCreature{{id: "mystery", species: "unknown_thing", role: "leviathan", hull: 900}},
	})
	var log strings.Builder
	if err := Hunt(context.Background(), huntDeps(c, &log)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if c.huntCalls != 0 {
		t.Errorf("hunt calls = %d, want 0 for an unrecognised role", c.huntCalls)
	}
	if !strings.Contains(log.String(), "nothing huntable") {
		t.Errorf("must log that the belt held nothing huntable, got:\n%s", log.String())
	}
}

// Among equally admissible quarry the SHORTEST fight wins: identical
// kill_creature credit, fewer ticks, less damage taken.
func TestHuntPicksTheShortestFight(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{
		board: []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 1}},
		creatures: []huntCreature{
			{id: "whale", species: "pilot_whale", hull: 220},
			{id: "jelly", species: "bell_jelly", hull: 45},
		},
	})
	if err := Hunt(context.Background(), huntDeps(c, io.Discard)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if !called(c, "hunt:jelly") || called(c, "hunt:whale") {
		t.Errorf("want the 45-hull jelly engaged rather than the 220-hull whale, calls: %v", c.calls)
	}
}

// When the objective names its quarry, that species wins over a shorter fight:
// killing the wrong species may not advance the objective at all.
func TestHuntPrefersTheObjectiveSpecies(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{
		board: []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 1, targetID: "belt_grazer"}},
		creatures: []huntCreature{
			{id: "scarab", species: "ash_scarab", role: "scavenger", hull: 20},
			{id: "grazer", species: "belt_grazer", hull: 70},
		},
	})
	if err := Hunt(context.Background(), huntDeps(c, io.Discard)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if !called(c, "hunt:grazer") || called(c, "hunt:scarab") {
		t.Errorf("want only the objective's species engaged, calls: %v", c.calls)
	}
}

// The objective's target_id and description are logged verbatim on accept.
// Mission progress has not moved across several live kills and nobody knows
// what kill_creature is scoped to; this line is what makes the next live pass
// answer it.
func TestHuntLogsTheObjectiveTarget(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{
		board:     []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 1, targetID: "belt_grazer"}},
		creatures: huntGrazers("c1"),
	})
	var log strings.Builder
	if err := Hunt(context.Background(), huntDeps(c, &log)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if !strings.Contains(log.String(), `target_id="belt_grazer"`) || !strings.Contains(log.String(), `desc="cull the herd"`) {
		t.Errorf("must log the objective target verbatim, got:\n%s", log.String())
	}
}

// One pass is capped: a board asking for more kills than huntMaxEngagements
// must not chase the whole belt.
func TestHuntStopsAtTheEngagementCap(t *testing.T) {
	ids := make([]string, 0, huntMaxEngagements+3)
	for i := range huntMaxEngagements + 3 {
		ids = append(ids, "c"+string(rune('a'+i)))
	}
	c := newHuntFake(t, huntFakeOpts{
		board:     []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: huntMaxEngagements + 3}},
		creatures: huntGrazers(ids...),
	})
	if err := Hunt(context.Background(), huntDeps(c, io.Discard)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if c.huntCalls != huntMaxEngagements {
		t.Errorf("hunt calls = %d, want %d (the per-pass engagement cap)", c.huntCalls, huntMaxEngagements)
	}
}

// The board is at a station and the quarry is not: a pass that never leaves
// the station finds an empty belt forever.
func TestHuntTravelsFromTheBoardToAWildlifePOI(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{
		board:     []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 1}},
		creatures: huntGrazers("c1"),
	})
	if err := Hunt(context.Background(), huntDeps(c, io.Discard)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if !called(c, "undock") {
		t.Errorf("must undock before leaving the board, calls: %v", c.calls)
	}
	if !called(c, "travel:commerce_fields") {
		t.Errorf("must travel to the belt, calls: %v", c.calls)
	}
	if c.state.CurrentPOI != "commerce_fields" {
		t.Errorf("ended at %q, want the belt", c.state.CurrentPOI)
	}
}

// nebula_drift_hunt is on the mission allowlist and the KB holds 55 nebula
// POIs, so a pass that accepts it has to be able to travel to one.
func TestHuntTravelsToANebula(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	ctx := context.Background()
	_ = kb.RememberSystem(ctx, knowledge.System{ID: "test_system", Name: "Test"})
	_ = kb.RememberPOI(ctx, knowledge.POI{ID: "haven_station", SystemID: "test_system", Type: "station"})
	_ = kb.RememberPOI(ctx, knowledge.POI{ID: "test_nebula", SystemID: "test_system", Type: "nebula"})
	_ = kb.RememberBase(ctx, knowledge.SpaceBase{ID: "haven_station", POIID: "haven_station", PublicAccess: true})

	c := newHuntFake(t, huntFakeOpts{
		board:     []boardMission{{id: "nebula_drift_hunt", difficulty: 1, quantity: 1}},
		creatures: huntGrazers("c1"),
	})
	deps := huntDeps(c, io.Discard)
	deps.KB = kb
	if err := Hunt(ctx, deps); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if !called(c, "travel:test_nebula") {
		t.Errorf("a nebula must be a reachable wildlife POI, calls: %v", c.calls)
	}
}

// POI choice is ranked by huntWildlifePOITypes' order, not by whatever order
// the KB happens to return rows in — MemoryKB iterates a map, so an
// unranked implementation picks at random. The First Hunt chain asks for
// BELT-grazers, so flying to the gas cloud in the same system is a plausible
// reason for live mission progress not moving.
func TestHuntPrefersAnAsteroidBeltOverOtherWildlifePOIs(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	ctx := context.Background()
	_ = kb.RememberSystem(ctx, knowledge.System{ID: "test_system", Name: "Test"})
	_ = kb.RememberPOI(ctx, knowledge.POI{ID: "haven_station", SystemID: "test_system", Type: "station"})
	_ = kb.RememberPOI(ctx, knowledge.POI{ID: "test_cloud", SystemID: "test_system", Type: "gas_cloud"})
	_ = kb.RememberPOI(ctx, knowledge.POI{ID: "test_nebula", SystemID: "test_system", Type: "nebula"})
	_ = kb.RememberPOI(ctx, knowledge.POI{ID: "test_belt", SystemID: "test_system", Type: "asteroid_belt"})

	// Repeated because the map iteration order is what an unranked
	// implementation would be relying on; one draw proves nothing.
	for i := range 25 {
		if got, _ := huntLocalWildlifePOI(ctx, kb, "test_system"); got != "test_belt" {
			t.Fatalf("draw %d: chose %q, want test_belt — the belt outranks the gas cloud and the nebula", i, got)
		}
	}
}

// beltWith is one asteroid belt carrying a single iron seam.
func beltWith(id string, richness, remaining float64) knowledge.POI {
	return knowledge.POI{
		ID: id, SystemID: "test_system", Type: "asteroid_belt",
		Resources: []game.POIResource{{ResourceID: "iron_ore", Richness: richness, Remaining: remaining}},
	}
}

// "Grazers gather where the iron is still rich" — among belts, the rich
// un-worked one wins, and a mined-out one loses to a belt nobody has surveyed.
func TestHuntPrefersTheRichestUnworkedBelt(t *testing.T) {
	ctx := context.Background()
	kb := knowledge.NewMemoryKB()
	_ = kb.RememberSystem(ctx, knowledge.System{ID: "test_system", Name: "Test"})
	_ = kb.RememberPOI(ctx, beltWith("commerce_fields", 75, 0)) // richness 75, mined out
	_ = kb.RememberPOI(ctx, beltWith("thin_belt", 20, 1000))
	_ = kb.RememberPOI(ctx, beltWith("rich_belt", 90, 1000))
	_ = kb.RememberPOI(ctx, knowledge.POI{ID: "unsurveyed_belt", SystemID: "test_system", Type: "asteroid_belt"})

	for i := range 25 {
		got, why := huntLocalWildlifePOI(ctx, kb, "test_system")
		if got != "rich_belt" {
			t.Fatalf("draw %d: chose %q (%s), want rich_belt — richness*remaining is highest there", i, got, why)
		}
		if !strings.Contains(why, "un-worked") {
			t.Fatalf("draw %d: reason %q must say the belt is un-worked", i, why)
		}
	}
}

// A belt with nothing left in it is the last place to hunt: an un-worked gas
// cloud beats it, which is what "when no belt qualifies" means in practice.
func TestHuntSkipsAMinedOutBeltForALiveGasCloud(t *testing.T) {
	ctx := context.Background()
	kb := knowledge.NewMemoryKB()
	_ = kb.RememberSystem(ctx, knowledge.System{ID: "test_system", Name: "Test"})
	_ = kb.RememberPOI(ctx, beltWith("commerce_fields", 75, 0))
	_ = kb.RememberPOI(ctx, knowledge.POI{
		ID: "live_cloud", SystemID: "test_system", Type: "gas_cloud",
		Resources: []game.POIResource{{ResourceID: "helium", Richness: 30, Remaining: 500}},
	})

	for i := range 25 {
		if got, why := huntLocalWildlifePOI(ctx, kb, "test_system"); got != "live_cloud" {
			t.Fatalf("draw %d: chose %q (%s), want live_cloud — commerce_fields has remaining 0", i, got, why)
		}
	}
}

// Unknown richness sits BETWEEN known-rich and known-exhausted: a surveyed
// live belt beats it, and it beats a stripped one.
func TestHuntRanksUnknownRichnessBetweenRichAndExhausted(t *testing.T) {
	ctx := context.Background()
	unknown := knowledge.POI{ID: "unsurveyed_belt", SystemID: "test_system", Type: "asteroid_belt"}

	richKB := knowledge.NewMemoryKB()
	_ = richKB.RememberSystem(ctx, knowledge.System{ID: "test_system"})
	_ = richKB.RememberPOI(ctx, unknown)
	_ = richKB.RememberPOI(ctx, beltWith("rich_belt", 90, 1000))

	exhaustedKB := knowledge.NewMemoryKB()
	_ = exhaustedKB.RememberSystem(ctx, knowledge.System{ID: "test_system"})
	_ = exhaustedKB.RememberPOI(ctx, unknown)
	_ = exhaustedKB.RememberPOI(ctx, beltWith("commerce_fields", 75, 0))

	for i := range 25 {
		if got, _ := huntLocalWildlifePOI(ctx, richKB, "test_system"); got != "rich_belt" {
			t.Fatalf("draw %d: chose %q over a known-rich belt, want rich_belt", i, got)
		}
		got, why := huntLocalWildlifePOI(ctx, exhaustedKB, "test_system")
		if got != "unsurveyed_belt" {
			t.Fatalf("draw %d: chose %q over an unsurveyed belt, want unsurveyed_belt — a mined-out POI is worse than an unknown one", i, got)
		}
		if !strings.Contains(why, "unknown") {
			t.Fatalf("draw %d: reason %q must say the richness is unknown", i, why)
		}
	}
}

// A worker parked at a belt (undocked, no board) recovers to a station rather
// than reading a board that isn't there.
func TestHuntRecoversToAStationWhenUndocked(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{
		board:       []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 1}},
		creatures:   huntGrazers("c1"),
		startPOI:    "commerce_fields",
		unsetDocked: true,
	})
	c.dockErr = errNotAStation
	var log strings.Builder
	if err := Hunt(context.Background(), huntDeps(c, &log)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if !called(c, "travel:haven_station") {
		t.Errorf("must relocate to a station, calls: %v", c.calls)
	}
	if called(c, "get_missions") {
		t.Errorf("must not read a board it cannot be docked at, calls: %v", c.calls)
	}
}

// --- policy-level tests: the fight controller is a pure function of the view ---

// huntView builds a spar.View from the captured reply, applying overrides.
func huntView(t *testing.T, mutate func(*serverapi.GetBattleStatusResponse)) spar.View {
	t.Helper()
	resp := huntRealReply(t)
	if mutate != nil {
		mutate(&resp)
	}
	v, ok := spar.BuildView(game.BattleStateFromResponse(&resp), huntSelfID)
	if !ok {
		t.Fatal("BuildView could not find self in the captured participants")
	}
	return v
}

// The hull abort must win over closing the range, or the loop closes the very
// range it decided to escape.
func TestHuntPolicyAbortDominatesAdvance(t *testing.T) {
	p := &huntPolicy{fleeAtHull: 0.35}
	v := huntView(t, func(r *serverapi.GetBattleStatusResponse) {
		r.Participants[1].HullPct = 20 // ours; still at zone_distance 6, far out of reach
	})
	act := p.Decide(v)
	if act.BattleAction != "stance" || act.Payload["stance"] != "flee" {
		t.Fatalf("wounded and out of reach: got %+v, want a flee stance", act)
	}
	if p.outcome != huntFled {
		t.Errorf("outcome = %v, want huntFled", p.outcome)
	}
}

// Range is numeric: at zone_distance 6 vs max_weapon_reach 3 the policy
// advances; at 3 it stops advancing and fights.
func TestHuntPolicyUsesNumericRangeNotTheZoneLabel(t *testing.T) {
	far := huntView(t, nil)
	if act := (&huntPolicy{}).Decide(far); act.BattleAction != "advance" {
		t.Errorf("zone_distance 6 > reach 3: got %+v, want advance", act)
	}
	// Same zone STRING ("outer"), but now inside weapon reach. A zone-ladder
	// implementation would keep advancing here; the numeric one must not.
	near := huntView(t, func(r *serverapi.GetBattleStatusResponse) {
		for i := range r.Participants {
			r.Participants[i].ZoneDistance = r.CombatState.MaxWeaponReach
		}
		r.Participants[1].TargetID = ""
	})
	if near.Self.Zone != "outer" {
		t.Fatalf("fixture drift: expected the zone label to stay %q", "outer")
	}
	act := (&huntPolicy{}).Decide(near)
	if act.BattleAction == "advance" {
		t.Errorf("in reach at zone_distance %d: got advance, want target/fire", near.Self.ZoneDistance)
	}
}

// Breaking off is not one command: the server counts flee_counter up to
// flee_required (3) before the escape lands. Walking away at the first tick
// abandons the ship mid-escape, still a participant and still being shot.
func TestHuntPolicyHoldsTheFleeUntilTheCounterCompletes(t *testing.T) {
	p := &huntPolicy{fleeAtHull: 0.35}
	// Tick 1: wounded -> break off.
	if act := p.Decide(huntView(t, func(r *serverapi.GetBattleStatusResponse) {
		r.Participants[1].HullPct = 10
	})); act.Payload["stance"] != "flee" {
		t.Fatalf("tick 1: want a flee stance, got %+v", act)
	}
	// Ticks 2-3: the counter is still short of flee_required; keep holding.
	for count := 1; count < 3; count++ {
		act := p.Decide(huntView(t, func(r *serverapi.GetBattleStatusResponse) {
			r.Participants[1].HullPct = 10
			r.Participants[1].Stance = "flee"
			r.CombatState.FleeCounter = count
		}))
		if act.Kind == spar.ActionStop {
			t.Fatalf("stopped at flee_counter %d of %d — the escape had not completed",
				count, 3)
		}
	}
	// The counter reaches flee_required: now the loop may end.
	act := p.Decide(huntView(t, func(r *serverapi.GetBattleStatusResponse) {
		r.Participants[1].HullPct = 10
		r.Participants[1].Stance = "flee"
		r.CombatState.FleeCounter = r.CombatState.FleeRequired
	}))
	if act.Kind != spar.ActionStop {
		t.Errorf("flee_counter reached flee_required: got %+v, want stop", act)
	}
	if !strings.Contains(p.reason, "escaped after 3/3") {
		t.Errorf("reason = %q, want it to record the completed escape", p.reason)
	}
}

// can_escape is the server's verdict, not something to assume: holding a
// stance that cannot complete just spends ticks under fire.
func TestHuntPolicyStopsWhenTheServerSaysEscapeIsImpossible(t *testing.T) {
	p := &huntPolicy{fleeAtHull: 0.35}
	if act := p.Decide(huntView(t, func(r *serverapi.GetBattleStatusResponse) {
		r.Participants[1].HullPct = 10
	})); act.Payload["stance"] != "flee" {
		t.Fatalf("tick 1: want a flee stance, got %+v", act)
	}
	act := p.Decide(huntView(t, func(r *serverapi.GetBattleStatusResponse) {
		r.Participants[1].HullPct = 10
		r.Participants[1].Stance = "flee"
		r.CombatState.CanEscape = false
	}))
	if act.Kind != spar.ActionStop {
		t.Errorf("can_escape=false: got %+v, want stop", act)
	}
	if !strings.Contains(p.reason, "can_escape=false") {
		t.Errorf("reason = %q, want it to name can_escape", p.reason)
	}
}

// The second line of defence on the no-predators rule: the battle payload
// carries the role as ship_name, so anything that wandered in is caught before
// a shot is exchanged.
func TestHuntPolicyBreaksOffFromAPredatorParticipant(t *testing.T) {
	p := &huntPolicy{fleeAtHull: 0.35, wildlifeOnly: true}
	act := p.Decide(huntView(t, func(r *serverapi.GetBattleStatusResponse) {
		r.Participants[0].ShipName = "predator"
		r.Participants[0].Username = "Tempest-Eel"
	}))
	if act.Payload["stance"] != "flee" {
		t.Fatalf("a predator in the battle: got %+v, want a flee stance", act)
	}
	if p.outcome != huntFled || !strings.Contains(p.reason, "not huntable wildlife") {
		t.Errorf("outcome=%v reason=%q, want a wildlife refusal", p.outcome, p.reason)
	}
}

// A pass must never open a second fight from inside the first. The disengage
// is bounded and one of its exits — the server answering can_escape=false —
// stops while we are still a participant; calling hunt from there is refused.
func TestHuntDoesNotEngageWhileStillInABattle(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{
		board:          []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 2}},
		creatures:      huntGrazers("c1", "c2"),
		lingerInBattle: true,
	})
	var log strings.Builder
	if err := Hunt(context.Background(), huntDeps(c, &log)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if c.huntCalls != 1 {
		t.Errorf("hunt calls = %d, want 1 — the second engagement must not start while still a participant", c.huntCalls)
	}
	if !strings.Contains(log.String(), "still a participant in an unresolved battle") {
		t.Errorf("must log why the pass stopped, got:\n%s", log.String())
	}
}

// huntDecideN drives the policy n times against views built by mutate(tick),
// returning every action.
func huntDecideN(t *testing.T, p *huntPolicy, n int, mutate func(tick int, r *serverapi.GetBattleStatusResponse)) []spar.Action {
	t.Helper()
	acts := make([]spar.Action, 0, n)
	for tick := range n {
		acts = append(acts, p.Decide(huntView(t, func(r *serverapi.GetBattleStatusResponse) { mutate(tick, r) })))
	}
	return acts
}

// max_weapon_reach is read from the wire, never assumed to be the captured 3.
// A fit that reaches 8 is already in range at distance 6 and must not advance;
// one that reaches 1 must keep closing.
func TestHuntPolicyReadsWeaponReachFromTheWire(t *testing.T) {
	longReach := huntView(t, func(r *serverapi.GetBattleStatusResponse) {
		r.CombatState.MaxWeaponReach = 8 // zone_distance stays at the captured 6
	})
	if act := (&huntPolicy{}).Decide(longReach); act.BattleAction == "advance" {
		t.Errorf("reach 8 vs distance 6 is in range: got advance, want target/fire")
	}
	shortReach := huntView(t, func(r *serverapi.GetBattleStatusResponse) {
		r.CombatState.MaxWeaponReach = 1
	})
	if act := (&huntPolicy{}).Decide(shortReach); act.BattleAction != "advance" {
		t.Errorf("reach 1 vs distance 6: got %+v, want advance", act)
	}
}

// With combat_state absent there is no reach to compare against. Declaring
// "in range" would be safe but yield nothing — the engagement could never land
// a shot. Keep closing while closing still works, then fight.
func TestHuntPolicyKeepsClosingWhenWeaponReachIsUnknown(t *testing.T) {
	p := &huntPolicy{}
	// Ticks 0-2 the distance keeps falling; from tick 3 it sticks.
	acts := huntDecideN(t, p, 5, func(tick int, r *serverapi.GetBattleStatusResponse) {
		r.CombatState = nil
		d := 6 - tick
		if d < 3 {
			d = 3
		}
		for i := range r.Participants {
			r.Participants[i].ZoneDistance = d
		}
	})
	for i := range 4 {
		if acts[i].BattleAction != "advance" {
			t.Errorf("tick %d: got %+v, want advance while the distance is still falling", i, acts[i])
		}
	}
	if acts[4].BattleAction == "advance" {
		t.Errorf("tick 4: distance stopped falling, so advancing is pointless; got %+v", acts[4])
	}
}

// ...and it must say so. Reporting a kite when the truth is "we never knew the
// reach" points the next reader in exactly the wrong direction.
func TestHuntPolicyNamesUnknownReachInTheGiveUpReason(t *testing.T) {
	p := &huntPolicy{}
	huntDecideN(t, p, huntNoProgressTicks+2, func(_ int, r *serverapi.GetBattleStatusResponse) {
		r.CombatState = nil // distance never moves either
	})
	if !strings.Contains(p.reason, "weapon reach unknown") {
		t.Errorf("reason = %q, want it to name the absent combat_state", p.reason)
	}
	if strings.Contains(p.reason, "kiting") {
		t.Errorf("reason = %q, must not blame a kite when the reach was never reported", p.reason)
	}
}

var errNotAStation = &huntTestErr{"cannot dock here"}

type huntTestErr struct{ s string }

func (e *huntTestErr) Error() string { return e.s }
