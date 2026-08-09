package worker

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/spar"
)

// huntSelfID is the player id the fake reports, and the id the battle picture
// identifies "us" by (spar.BuildView matches on PlayerID).
const huntSelfID = "me-1"

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
	hull     int
	inCombat bool
}

// huntFakeOpts configures newHuntFake.
type huntFakeOpts struct {
	board     []boardMission
	creatures []huntCreature
	// activeMissions is the get_active_missions reply. Nil means the default
	// (one active instance per board entry, with a hex-ish mission id);
	// an explicitly empty slice models the accept not having landed yet.
	activeMissions []serverapi.ActiveMission
	noActive       bool
	// battleHullPct is the own hull percentage the battle picture reports
	// during the FIRST fight. 0 means full hull throughout.
	battleHullPct int
	// shipHullFrac is the hull fraction get_status reports from the start of
	// the pass (the between-engagement gate reads Ship.Hull, which only
	// get_status refreshes). 0 means full.
	shipHullFrac float64
	// neverCloses models a quarry that outruns us: advance never improves the
	// zone, so the chase makes no progress and never lands a shot.
	neverCloses bool
	// noWreck suppresses the carcass, so a kill can never be confirmed.
	noWreck bool
	// cargoCapacity/cargoUsed size the hold for the loot clamp.
	cargoCapacity float64
	cargoUsed     float64
	// wreckCargo is what each carcass carries.
	wreckCargo []serverapi.CargoItem
	// startPOI/startDocked place the worker; the default is docked at the
	// station whose board it reads.
	startPOI    string
	startDocked bool
	unsetDocked bool
}

// huntFake wraps the shared fakeClient (dispatch_test.go) with a small battle
// simulator, because the defect this suite exists to catch lives entirely
// inside a fight: a canned "you are not a participant" reply resolves every
// engagement instantly and proves nothing about advancing, breaking off, or
// confirming a kill.
//
// The simulation is deliberately literal about the mechanics the executor
// depends on: a fight starts in the outer zone; `advance` closes one zone per
// tick; damage only lands from the engaged zone with the fire stance; a flee
// stance ends the fight a tick later (it auto-retreats over several ticks);
// and a dead creature leaves a carcass keyed by victim_id.
type huntFake struct {
	*fakeClient
	opts huntFakeOpts

	huntCalls     int
	fled          bool
	battleActions []string
	lootCalls     []string
	salvageCalls  []string

	quarryID   string
	battleLive bool
	fleeing    bool
	zone       int
	stance     string
	targetID   string
	enemyHull  int
	selfHull   int
	wrecks     []serverapi.Wreck
}

var huntZones = []string{"outer", "mid", "inner", "engaged"}

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
		hull := c.hull
		if hull == 0 {
			hull = 45
		}
		creatures = append(creatures, serverapi.NearbyCreature{
			CreatureID: c.id, Species: species, Role: "scavenger",
			Hull: hull, MaxHull: 45, InCombat: c.inCombat,
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
	docked := !opts.unsetDocked
	if opts.startDocked {
		docked = true
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
			Doc:        docked,
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
	return &huntFake{fakeClient: f, opts: opts}
}

// huntDeps builds the deps every test uses: zero-delay sleeps so a multi-tick
// chase costs the suite nothing, and a KB holding one station and one belt in
// the worker's system so the station->belt leg can run for real.
func huntDeps(c *huntFake, out io.Writer) HuntDeps {
	return HuntDeps{
		Client: c, KB: huntKB(), Out: out, AgentID: "pirate-6",
		MaxDifficulty: 1, WildlifeOnly: true,
		sleep:     func(context.Context, time.Duration) error { return nil },
		tickSleep: time.Nanosecond,
	}
}

func huntKB() knowledge.Base {
	kb := knowledge.NewMemoryKB()
	ctx := context.Background()
	_ = kb.RememberSystem(ctx, knowledge.System{ID: "test_system", Name: "Test"})
	_ = kb.RememberPOI(ctx, knowledge.POI{ID: "haven_station", SystemID: "test_system", Type: "station"})
	_ = kb.RememberPOI(ctx, knowledge.POI{ID: "commerce_fields", SystemID: "test_system", Type: "asteroid_belt"})
	_ = kb.RememberBase(ctx, knowledge.SpaceBase{ID: "haven_station", POIID: "haven_station", PublicAccess: true})
	return kb
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

func (f *huntFake) Hunt(ctx context.Context, creatureID string) error {
	f.huntCalls++
	f.calls = append(f.calls, "hunt:"+creatureID)
	f.quarryID = creatureID
	f.battleLive = true
	f.fleeing = false
	f.zone = 0
	f.stance = ""
	f.targetID = ""
	f.enemyHull = 100
	f.selfHull = 100
	if f.huntCalls == 1 && f.opts.battleHullPct > 0 {
		f.selfHull = f.opts.battleHullPct
	}
	f.publishBattle()
	return nil
}

// GetBattleStatus advances the simulated fight one tick and republishes the
// battle picture, exactly where the real client's parseBattleStatusData writes
// State.BattleState.
func (f *huntFake) GetBattleStatus(ctx context.Context) error {
	f.calls = append(f.calls, "get_battle_status")
	switch {
	case !f.battleLive:
	case f.fleeing:
		// The flee stance auto-retreats over several ticks; one is enough here.
		f.battleLive = false
	case f.zone == len(huntZones)-1 && f.stance == "fire":
		f.enemyHull -= 60
		if f.enemyHull <= 0 {
			f.enemyHull = 0
			f.battleLive = false
			f.recordCarcass()
		}
	}
	f.publishBattle()
	return nil
}

func (f *huntFake) publishBattle() {
	if !f.battleLive {
		f.state.BattleState = &game.BattleState{IsParticipant: false}
		f.state.InBattle = false
		return
	}
	f.state.InBattle = true
	f.state.BattleState = &game.BattleState{
		BattleID:      "b-" + f.quarryID,
		IsParticipant: true,
		Participants: []game.BattleParticipant{
			{
				PlayerID: huntSelfID, SideID: "1", Zone: huntZones[f.zone],
				Stance: f.stance, TargetID: f.targetID, HullPct: f.selfHull,
			},
			{
				PlayerID: f.quarryID, SideID: "2", Zone: huntZones[f.zone],
				Stance: "fire", HullPct: f.enemyHull,
			},
		},
	}
}

func (f *huntFake) recordCarcass() {
	if f.opts.noWreck {
		return
	}
	cargo := f.opts.wreckCargo
	if cargo == nil {
		cargo = []serverapi.CargoItem{{ItemID: "carapace", Quantity: 3}}
	}
	f.wrecks = append(f.wrecks, serverapi.Wreck{
		ID: "wreck_" + f.quarryID, Type: "creature",
		VictimID: f.quarryID, KillerID: huntSelfID,
		Cargo: cargo, SalvageValue: 5, POIID: "commerce_fields",
	})
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
		if !f.opts.neverCloses && f.zone < len(huntZones)-1 {
			f.zone++
		}
	case "target":
		f.targetID, _ = payload["target_id"].(string)
	case "stance":
		f.stance, _ = payload["stance"].(string)
		if f.stance == "flee" {
			f.fled = true
			f.fleeing = true
		}
	}
	return nil
}

func (f *huntFake) GetWrecks(ctx context.Context) error {
	f.calls = append(f.calls, "get_wrecks")
	// A decoy from another hunter at the same belt: it must never be looted,
	// which is what makes victim_id (not killer_id, not type) the key.
	all := append([]serverapi.Wreck{{
		ID: "wreck_someone_else", Type: "creature", VictimID: "not_our_quarry",
		KillerID: "someone-else", Cargo: []serverapi.CargoItem{{ItemID: "biogas", Quantity: 9}},
	}}, f.wrecks...)
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
// and a nil error. The log assertion is what distinguishes them — the
// empty-belt branch logs before the loop is ever entered, and the loop's own
// fallback logs "no huntable creatures remain".
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
// hunting worker actually takes damage.
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
	// The abort must dominate: no advance may be issued in the fight it has
	// already decided to escape.
	if len(c.battleActions) == 0 || c.battleActions[0] != "stance:flee" {
		t.Errorf("first battle action = %v, want stance:flee before anything that closes the range", c.battleActions)
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

// Closing the range is the whole fight: a battle opens in the outer zone,
// where short-range weapons refuse to fire. The pass must advance, target and
// hold the fire stance, re-evaluated every tick.
func TestHuntAdvancesToCloseTheRange(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{
		board:     []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 1}},
		creatures: huntGrazers("c1"),
	})
	var log strings.Builder
	if err := Hunt(context.Background(), huntDeps(c, &log)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	advances := 0
	for _, a := range c.battleActions {
		if a == "advance" {
			advances++
		}
	}
	if advances != len(huntZones)-1 {
		t.Errorf("advances = %d (%v), want %d — one per zone from outer to engaged",
			advances, c.battleActions, len(huntZones)-1)
	}
	if !called(c, "battle:target:c1") {
		t.Errorf("must target the quarry once in range, actions: %v", c.battleActions)
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
	advances := 0
	for _, a := range c.battleActions {
		if a == "advance" {
			advances++
		}
	}
	if advances > huntNoProgressTicks+1 {
		t.Errorf("advanced %d times against an uncatchable quarry; the progress bound should stop it near %d",
			advances, huntNoProgressTicks)
	}
	if !c.fled {
		t.Error("abandoning a quarry must disengage first, not just move on")
	}
}

// The carcass is the kill receipt AND the cargo. It is matched by victim_id:
// another hunter's wreck at the same belt is neither proof nor ours to take.
func TestHuntLootsItsOwnCarcassOnly(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{
		board:      []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 1}},
		creatures:  huntGrazers("c1"),
		wreckCargo: []serverapi.CargoItem{{ItemID: "carapace", Quantity: 3}, {ItemID: "biogas", Quantity: 2}},
	})
	if err := Hunt(context.Background(), huntDeps(c, io.Discard)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	want := []string{"wreck_c1/carapace", "wreck_c1/biogas"}
	if len(c.lootCalls) != len(want) {
		t.Fatalf("loot calls = %v, want %v", c.lootCalls, want)
	}
	for i, w := range want {
		if c.lootCalls[i] != w {
			t.Errorf("loot call %d = %q, want %q", i, c.lootCalls[i], w)
		}
	}
	for _, got := range c.lootCalls {
		if strings.Contains(got, "someone_else") {
			t.Errorf("looted another hunter's wreck: %v", c.lootCalls)
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
// escapes too.
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
// even when it is the healthiest one on the belt.
func TestHuntSkipsCreaturesAlreadyInCombat(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{
		board: []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 1}},
		creatures: []huntCreature{
			{id: "busy", hull: 45, inCombat: true},
			{id: "free", hull: 20},
		},
	})
	if err := Hunt(context.Background(), huntDeps(c, io.Discard)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if called(c, "hunt:busy") {
		t.Errorf("engaged a creature already in combat: %v", c.calls)
	}
	if !called(c, "hunt:free") {
		t.Errorf("want the free creature engaged, calls: %v", c.calls)
	}
}

// When the objective names its quarry, that species wins over a healthier
// stranger: killing the wrong species may not advance the objective at all.
func TestHuntPrefersTheObjectiveSpecies(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{
		board: []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 1, targetID: "belt_grazer"}},
		creatures: []huntCreature{
			{id: "scarab", species: "ash_scarab", hull: 45},
			{id: "grazer", species: "belt_grazer", hull: 10},
		},
	})
	if err := Hunt(context.Background(), huntDeps(c, io.Discard)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if !called(c, "hunt:grazer") || called(c, "hunt:scarab") {
		t.Errorf("want only the objective's species engaged, calls: %v", c.calls)
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

// The policy is a pure function of the view, so its precedence is testable
// without a fight: the hull abort must win over closing the range.
func TestHuntPolicyAbortDominatesAdvance(t *testing.T) {
	p := &huntPolicy{fleeAtHull: 0.35}
	act := p.Decide(spar.View{
		Self:    game.BattleParticipant{PlayerID: huntSelfID, Zone: "outer", HullPct: 20},
		Enemies: []game.BattleParticipant{{PlayerID: "c1", HullPct: 100}},
	})
	if act.BattleAction != "stance" || act.Payload["stance"] != "flee" {
		t.Fatalf("wounded in the outer zone: got %+v, want a flee stance", act)
	}
	if p.outcome != huntFled {
		t.Errorf("outcome = %v, want huntFled", p.outcome)
	}
}

var errNotAStation = &huntTestErr{"cannot dock here"}

type huntTestErr struct{ s string }

func (e *huntTestErr) Error() string { return e.s }
