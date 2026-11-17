package worker

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// huntNeedTestNow is the fixed clock every need test runs on.
var huntNeedTestNow = time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)

func huntAgo(d time.Duration) string { return rfc(huntNeedTestNow.Add(-d)) }

// huntNeedFakeKB adds the SQLiteKB-only wildlife queries (huntNeedKB) to a
// MemoryKB, serving them from fixed rows.
type huntNeedFakeKB struct {
	knowledge.Base
	byName    map[string]string
	drops     map[string][]string
	species   []knowledge.WildlifeSpecies
	sightings []knowledge.WildlifeSighting
	coverage  []knowledge.WildlifeCoverage // newest first
}

func (k *huntNeedFakeKB) SpeciesByDisplayName(context.Context) (map[string]string, error) {
	return k.byName, nil
}

func (k *huntNeedFakeKB) SpeciesDroppingItem(_ context.Context, itemID string) ([]string, error) {
	return k.drops[itemID], nil
}

func (k *huntNeedFakeKB) GetWildlifeSpecies(context.Context) ([]knowledge.WildlifeSpecies, error) {
	return k.species, nil
}

func (k *huntNeedFakeKB) GetWildlifeSightings(_ context.Context, species string, _ int) ([]knowledge.WildlifeSighting, error) {
	var out []knowledge.WildlifeSighting
	for _, s := range k.sightings {
		if s.Species == species {
			out = append(out, s)
		}
	}
	return out, nil
}

func (k *huntNeedFakeKB) GetWildlifeCoverage(_ context.Context, systemID, poiID string, limit int) ([]knowledge.WildlifeCoverage, error) {
	var out []knowledge.WildlifeCoverage
	for _, c := range k.coverage {
		if c.SystemID == systemID && c.POIID == poiID && len(out) < limit {
			out = append(out, c)
		}
	}
	return out, nil
}

// huntNeedKBFixture is huntKB's galaxy (test_system with haven_station, policed
// pollux with pollux_outer_belt) plus the delivery target levy (levy_base) one
// jump beyond pollux, the gauntlet hull class, and a field guide in which
// belt_grazer is a grazer recently sighted at pollux_outer_belt.
func huntNeedKBFixture(t *testing.T) *huntNeedFakeKB {
	t.Helper()
	ctx := context.Background()
	kb := knowledge.NewMemoryKB()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(kb.RememberSystem(ctx, knowledge.System{
		ID: "test_system", Name: "Test", PoliceLevel: 55, LastVisitedTick: 1,
		Connections: []knowledge.SystemConnection{{SystemID: "pollux"}},
	}))
	must(kb.RememberPOI(ctx, knowledge.POI{ID: "haven_station", SystemID: "test_system", Type: "station"}))
	must(kb.RememberBase(ctx, knowledge.SpaceBase{ID: "haven_station", POIID: "haven_station", PublicAccess: true}))
	must(kb.RememberSystem(ctx, knowledge.System{
		ID: "pollux", Name: "Pollux", PoliceLevel: 30, LastVisitedTick: 1,
		Connections: []knowledge.SystemConnection{{SystemID: "test_system"}, {SystemID: "levy"}},
	}))
	must(kb.RememberPOI(ctx, knowledge.POI{ID: "pollux_outer_belt", SystemID: "pollux", Type: "asteroid_belt"}))
	must(kb.RememberSystem(ctx, knowledge.System{
		ID: "levy", Name: "Levy", PoliceLevel: 80, LastVisitedTick: 1,
		Connections: []knowledge.SystemConnection{{SystemID: "pollux"}},
	}))
	must(kb.RememberPOI(ctx, knowledge.POI{ID: "levy_base", SystemID: "levy", Type: "station"}))
	must(kb.StoreShipClasses(ctx, []knowledge.ShipClassDef{{ID: "gauntlet", Category: "Combat", Tier: 2}}))

	return &huntNeedFakeKB{
		Base:    kb,
		byName:  map[string]string{knowledge.NormalizeCreatureName("Belt Grazer"): "belt_grazer"},
		species: []knowledge.WildlifeSpecies{{Species: "belt_grazer", Role: "grazer"}},
		sightings: []knowledge.WildlifeSighting{{
			Species: "belt_grazer", SystemID: "pollux", POIID: "pollux_outer_belt",
			Source: knowledge.WildlifeSourceNearby, ObservedCount: 4, ObservedUTC: huntAgo(time.Hour),
		}},
	}
}

// huntCausticNeed is a Station Need board entry for 10 caustic_sac, dropped by
// belt-grazers, delivered to levy_base (not the station whose board it is on).
func huntCausticNeed(difficulty int) serverapi.MissionBoardEntry {
	return serverapi.MissionBoardEntry{
		MissionID: "creature_levy_caustic_sac", Type: "delivery", Title: "Station Need: Caustic Sac",
		Difficulty:  difficulty,
		Description: "The Levy is short of caustic sacs.\nDropped by: Belt-Grazer.",
		Rewards:     &serverapi.MissionRewards{Credits: 23000},
		Objectives: []serverapi.MissionObjective{{
			Type: "deliver_item", ItemID: "caustic_sac", Quantity: 10,
			TargetBaseID: "levy_base", SystemID: "levy",
		}},
	}
}

func huntCausticActive(difficulty int) serverapi.ActiveMission {
	return serverapi.ActiveMission{
		MissionID: "hex-caustic", TemplateID: "creature_levy_caustic_sac", Type: "delivery",
		Title: "Station Need: Caustic Sac", Difficulty: difficulty,
		Description: "The Levy is short of caustic sacs.\nDropped by: Belt-Grazer.",
		Objectives: []serverapi.ActiveMissionObjective{{
			Type: "deliver_item", ItemID: "caustic_sac", Required: 10,
			TargetBase: "levy_base", SystemID: "levy",
		}},
	}
}

// huntNeedFake is huntFake with a hold that tracks items and carcasses that
// carry creature parts: each carcass holds 4 caustic_sac and 3 hide_scrap.
type huntNeedFake struct {
	*huntFake
	loots []string // item:qty, in order
}

func (f *huntNeedFake) GetWrecks(ctx context.Context) error {
	for i := range f.wrecks {
		f.wrecks[i].Cargo = []serverapi.CargoItem{
			{ItemID: "hide_scrap", Quantity: 3, Size: 1},
			{ItemID: "caustic_sac", Quantity: 4, Size: 1},
		}
	}
	return f.huntFake.GetWrecks(ctx)
}

func (f *huntNeedFake) LootWreck(ctx context.Context, wreckID, itemID string, quantity float64) error {
	f.loots = append(f.loots, fmt.Sprintf("%s:%.0f", itemID, quantity))
	if err := f.huntFake.LootWreck(ctx, wreckID, itemID, quantity); err != nil {
		return err
	}
	for i := range f.state.Ship.Cargo {
		if f.state.Ship.Cargo[i].ItemID == itemID {
			f.state.Ship.Cargo[i].Quantity += quantity
			return nil
		}
	}
	f.state.Ship.Cargo = append(f.state.Ship.Cargo, game.CargoItem{ItemID: itemID, Quantity: quantity})
	return nil
}

func newHuntNeedFake(t *testing.T, classID string, opts huntFakeOpts) *huntNeedFake {
	t.Helper()
	f := newHuntFake(t, opts)
	f.state.Ship.ClassID = classID
	f.raw["missions"] = boardJSON(t, huntCausticNeed(2))
	if opts.activeMissions == nil {
		f.actives = []serverapi.ActiveMission{huntCausticActive(2)}
	}
	return &huntNeedFake{huntFake: f}
}

func huntNeedDeps(f *huntNeedFake, kb knowledge.Base, out *strings.Builder) HuntDeps {
	d := huntDeps(f.huntFake, out)
	d.Client = f
	d.KB = kb
	d.NowFn = func() time.Time { return huntNeedTestNow }
	return d
}

func TestHuntPickNeedPrefersRewardPerJump(t *testing.T) {
	t.Parallel()
	jumps := map[string]int{"near": 1, "mid": 3, "far": 20, "t_near": 1, "t_mid": 2}
	jumpsTo := func(s string) int {
		if j, ok := jumps[s]; ok {
			return j
		}
		return -1
	}
	needs := []huntNeed{
		{MissionID: "small_close", Reward: 6000, GroundSystem: "near", SystemID: "t_near"}, // 6000/3 = 2000
		{MissionID: "big_mid", Reward: 24000, GroundSystem: "mid", SystemID: "t_mid"},      // 24000/6 = 4000
		{MissionID: "huge_far", Reward: 60000, GroundSystem: "near", SystemID: "far"},      // 60000/22 ~ 2727
	}
	got, ok := huntPickNeed(needs, jumpsTo, huntNearbyJumps)
	if !ok || got.MissionID != "big_mid" {
		t.Fatalf("picked %q ok=%v, want big_mid (best reward per jump)", got.MissionID, ok)
	}
}

func TestHuntPickNeedSkipsBeyondRadius(t *testing.T) {
	t.Parallel()
	jumps := map[string]int{"close": 2, "beyond": 7, "target": 1}
	jumpsTo := func(s string) int { return jumps[s] }
	needs := []huntNeed{
		{MissionID: "rich_beyond", Reward: 90000, GroundSystem: "beyond", SystemID: "target"},
		{MissionID: "modest_close", Reward: 3000, GroundSystem: "close", SystemID: "target"},
	}
	got, ok := huntPickNeed(needs, jumpsTo, 6)
	if !ok || got.MissionID != "modest_close" {
		t.Fatalf("picked %q ok=%v, want modest_close (rich_beyond's ground is 7 jumps, radius 6)", got.MissionID, ok)
	}
	if _, ok := huntPickNeed(needs[:1], jumpsTo, 6); ok {
		t.Error("a need whose only ground is beyond the radius must not be picked")
	}
	if _, ok := huntPickNeed([]huntNeed{{MissionID: "no_ground", Reward: 1, SystemID: "target"}}, jumpsTo, 6); ok {
		t.Error("a need with no ground must not be picked")
	}
}

func TestHuntNeedRadiusWidensWhenDry(t *testing.T) {
	t.Parallel()
	kb := huntNeedKBFixture(t)
	f := newHuntNeedFake(t, "gauntlet", huntFakeOpts{})
	f.raw["missions"] = boardJSON(t) // nothing on the board
	var log strings.Builder
	deps := huntNeedDeps(f, kb, &log)
	path := huntRoamPath(deps.AgentsDir, deps.AgentID)

	if r := huntNeedRadius(0); r != huntNearbyJumps {
		t.Fatalf("radius with no dry passes = %d, want %d", r, huntNearbyJumps)
	}
	for range 2 {
		if _, ok := huntNeedJob(context.Background(), deps, &log, "pirate-6", serverapi.GetMissionsResponse{}); ok {
			t.Fatal("an empty board must not produce a job")
		}
	}
	r, err := loadHuntRoam(path)
	if err != nil {
		t.Fatal(err)
	}
	if r.DryPasses != 2 {
		t.Fatalf("dry passes = %d after two dry searches, want 2", r.DryPasses)
	}
	if got := huntNeedRadius(r.DryPasses); got <= huntNearbyJumps {
		t.Errorf("radius after two dry passes = %d, want > %d", got, huntNearbyJumps)
	}

	// A pass that finds a job (here an ordinary kill mission) resets it.
	k := newHuntFake(t, huntFakeOpts{
		board:     []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 1}},
		creatures: huntGrazers("c1"),
	})
	kdeps := huntDeps(k, &log)
	kdeps.AgentsDir = deps.AgentsDir
	if herr := Hunt(context.Background(), kdeps); herr != nil {
		t.Fatal(herr)
	}
	r, err = loadHuntRoam(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := huntNeedRadius(r.DryPasses); r.DryPasses != 0 || got != huntNearbyJumps {
		t.Errorf("after a found job: dry passes %d radius %d, want 0 and %d", r.DryPasses, got, huntNearbyJumps)
	}
}

// huntGroundKB is a one-ground fixture for the depletion tests: belt_grazer
// last sighted at pollux_outer_belt 10h ago.
func huntGroundKB(t *testing.T) *huntNeedFakeKB {
	t.Helper()
	kb := huntNeedKBFixture(t)
	kb.sightings[0].ObservedUTC = huntAgo(10 * time.Hour)
	return kb
}

func huntGround(t *testing.T, kb *huntNeedFakeKB) (string, string) {
	t.Helper()
	need := huntNeed{ItemID: "caustic_sac", Species: []string{"belt_grazer"}}
	hops := map[string]int{"pollux": 1, "levy": 2, "test_system": 0}
	sys, poi, why := huntNeedGround(context.Background(), kb, kb, need, func(s string) int { return hops[s] }, huntNearbyJumps, nil, huntNeedTestNow)
	return sys + "/" + poi, why
}

func TestHuntGroundSkipsDepletedPOI(t *testing.T) {
	t.Parallel()
	kb := huntGroundKB(t)
	empty := func(ago time.Duration) knowledge.WildlifeCoverage {
		return knowledge.WildlifeCoverage{
			SystemID: "pollux", POIID: "pollux_outer_belt", Source: knowledge.WildlifeSourceNearby,
			CreaturesSeen: 0, ObservedUTC: huntAgo(ago),
		}
	}
	kb.coverage = []knowledge.WildlifeCoverage{empty(time.Hour), empty(3 * time.Hour)}
	if got, why := huntGround(t, kb); got != "/" {
		t.Fatalf("two empty looks in the last 6h: got ground %s (%s), want none", got, why)
	}

	kb.coverage = []knowledge.WildlifeCoverage{empty(7 * time.Hour), empty(8 * time.Hour)}
	if got, why := huntGround(t, kb); got != "pollux/pollux_outer_belt" {
		t.Fatalf("the same looks 7h old must expire: got %s (%s)", got, why)
	}

	// One empty look is not enough to call a ground hunted out.
	kb.coverage = []knowledge.WildlifeCoverage{empty(time.Hour)}
	if got, _ := huntGround(t, kb); got != "pollux/pollux_outer_belt" {
		t.Fatalf("a single empty look must not deplete: got %s", got)
	}

	// Looks that saw other creatures but not the species still count as empty.
	other := empty(time.Hour)
	other.CreaturesSeen, other.GameTick = 5, 900
	other2 := empty(2 * time.Hour)
	other2.CreaturesSeen, other2.GameTick = 3, 800
	kb.coverage = []knowledge.WildlifeCoverage{other, other2}
	if got, _ := huntGround(t, kb); got != "/" {
		t.Fatalf("two recent looks without the species: got %s, want none", got)
	}
	// ...but a look whose get_nearby did see the species is not empty.
	kb.sightings = append(kb.sightings, knowledge.WildlifeSighting{
		Species: "belt_grazer", SystemID: "pollux", POIID: "pollux_outer_belt",
		Source: knowledge.WildlifeSourceNearby, GameTick: 800, ObservedUTC: huntAgo(2 * time.Hour),
	})
	if got, _ := huntGround(t, kb); got != "pollux/pollux_outer_belt" {
		t.Fatalf("a look that saw the species is not empty: got %s", got)
	}
}

func TestHuntGroundTriesUnlookedPOI(t *testing.T) {
	t.Parallel()
	kb := huntGroundKB(t)
	ctx := context.Background()
	// A second ground, further out, sighted just as long ago but never looked at.
	if err := kb.RememberPOI(ctx, knowledge.POI{ID: "levy_ice", SystemID: "levy", Type: "ice_field"}); err != nil {
		t.Fatal(err)
	}
	kb.sightings = append(kb.sightings, knowledge.WildlifeSighting{
		Species: "belt_grazer", SystemID: "levy", POIID: "levy_ice",
		Source: knowledge.WildlifeSourceNearby, ObservedUTC: huntAgo(10 * time.Hour),
	})
	// The near ground was looked at an hour ago and held only other creatures.
	kb.coverage = []knowledge.WildlifeCoverage{{
		SystemID: "pollux", POIID: "pollux_outer_belt", Source: knowledge.WildlifeSourceNearby,
		CreaturesSeen: 4, GameTick: 900, ObservedUTC: huntAgo(time.Hour),
	}}
	got, why := huntGround(t, kb)
	if got != "levy/levy_ice" {
		t.Fatalf("got %s (%s), want the never-looked levy_ice over the looked-at-and-missed belt", got, why)
	}
	if !strings.Contains(why, "never looked at") {
		t.Errorf("reason %q should say the ground was never looked at", why)
	}
}

func TestHuntGroundRefusesUnpolicedAndHazardous(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	kb := huntGroundKB(t)
	if err := kb.RememberSystem(ctx, knowledge.System{
		ID: "pollux", Name: "Pollux", PoliceLevel: 0, LastVisitedTick: 1,
		Connections: []knowledge.SystemConnection{{SystemID: "test_system"}, {SystemID: "levy"}},
	}); err != nil {
		t.Fatal(err)
	}
	if got, _ := huntGround(t, kb); got != "/" {
		t.Fatalf("a Lawless ground must never be used for a need, got %s", got)
	}

	kb = huntGroundKB(t)
	if err := kb.RememberPOI(ctx, knowledge.POI{
		ID: "pollux_outer_belt", SystemID: "pollux", Type: "asteroid_belt", HazardType: "radiation", HazardSeverity: hazardAvoidSeverity,
	}); err != nil {
		t.Fatal(err)
	}
	if got, _ := huntGround(t, kb); got != "/" {
		t.Fatalf("a high-severity hazard ground must be refused, got %s", got)
	}
}

func TestHuntDeliveryStopsAtQuantityAndDelivers(t *testing.T) {
	t.Parallel()
	kb := huntNeedKBFixture(t)
	// A 16-unit hold only reaches 10 sacs if the sacs are looted ahead of the
	// hide_scrap each carcass also drops.
	f := newHuntNeedFake(t, "gauntlet", huntFakeOpts{cargoCapacity: 16, creatures: huntGrazers("c1", "c2", "c3", "c4", "c5")})
	var log strings.Builder
	if err := Hunt(context.Background(), huntNeedDeps(f, kb, &log)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if !called(f.huntFake, "accept:creature_levy_caustic_sac") {
		t.Fatalf("the Station Need was not accepted; log:\n%s", log.String())
	}
	if f.huntCalls != 3 {
		t.Errorf("hunts = %d, want 3 (4 sacs a carcass, stop once 10 are aboard)\n%s", f.huntCalls, log.String())
	}
	if got := cargoQty(f.state, "caustic_sac"); got < 10 {
		t.Errorf("hold has %.0f caustic_sac, want >= 10", got)
	}
	lastLoot := lastCall(f.huntFake, "loot:")
	travel := lastCall(f.huntFake, "travel:levy_base")
	dock := lastCall(f.huntFake, "dock")
	complete := lastCall(f.huntFake, "complete:hex-caustic")
	if travel < 0 || dock < 0 || complete < 0 {
		t.Fatalf("delivery must travel/dock/complete at levy_base; calls %v\nlog:\n%s", f.calls, log.String())
	}
	if lastLoot >= travel || travel >= dock || dock >= complete {
		t.Errorf("want loot < travel:levy_base < dock < complete, got %d %d %d %d", lastLoot, travel, dock, complete)
	}
	if slices.Contains(f.calls[lastLoot:], "travel:haven_station") {
		t.Error("delivered to the accepting station, not the mission's target base")
	}
}

func TestHuntDeliveryMissionPartWinsHoldSpace(t *testing.T) {
	t.Parallel()
	f := newHuntNeedFake(t, "gauntlet", huntFakeOpts{cargoCapacity: 10})
	f.wrecks = []serverapi.Wreck{{ID: "w1", VictimID: "c1", Type: "creature"}}
	var log strings.Builder
	deps := huntNeedDeps(f, huntNeedKBFixture(t), &log)
	need := &huntNeed{ItemID: "caustic_sac", Quantity: 8}

	// Carcass order puts 3 hide_scrap before 4 caustic_sac. 10 hold, 8 sacs
	// still wanted: the sacs come first, and only 2 scraps fit beside the 4
	// sacs still owed.
	if !huntLootCarcass(context.Background(), deps, &log, serverapi.NearbyCreature{CreatureID: "c1"}, 3, need) {
		t.Fatal("carcass not found")
	}
	if want := []string{"caustic_sac:4", "hide_scrap:2"}; !slices.Equal(f.loots, want) {
		t.Fatalf("loots = %v, want %v\n%s", f.loots, want, log.String())
	}

	// Second carcass: 4 sacs aboard, 4 owed, exactly 4 hold left because the
	// first carcass's scraps stopped short of it. The sacs take it all.
	f.loots = nil
	f.wrecks = []serverapi.Wreck{{ID: "w2", VictimID: "c2", Type: "creature"}}
	huntLootCarcass(context.Background(), deps, &log, serverapi.NearbyCreature{CreatureID: "c2"}, 3, need)
	if want := []string{"caustic_sac:4"}; !slices.Equal(f.loots, want) {
		t.Fatalf("loots = %v, want %v\n%s", f.loots, want, log.String())
	}

	// Without a need, carcass order is untouched (today's behaviour).
	g := newHuntNeedFake(t, "gauntlet", huntFakeOpts{cargoCapacity: 5})
	g.wrecks = []serverapi.Wreck{{ID: "w3", VictimID: "c3", Type: "creature"}}
	gdeps := huntNeedDeps(g, huntNeedKBFixture(t), &log)
	huntLootCarcass(context.Background(), gdeps, &log, serverapi.NearbyCreature{CreatureID: "c3"}, 3, nil)
	if want := []string{"hide_scrap:3", "caustic_sac:2"}; !slices.Equal(g.loots, want) {
		t.Fatalf("no-need loots = %v, want %v", g.loots, want)
	}
}

func TestHuntNoNeedsOnNonCombatHull(t *testing.T) {
	t.Parallel()
	kb := huntNeedKBFixture(t)
	f := newHuntNeedFake(t, "prospector", huntFakeOpts{creatures: huntGrazers("c1")})
	var log strings.Builder
	if err := Hunt(context.Background(), huntNeedDeps(f, kb, &log)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if called(f.huntFake, "accept:") || f.huntCalls != 0 {
		t.Fatalf("a non-combat hull took creature work: calls %v", f.calls)
	}
	if !strings.Contains(log.String(), "does not take creature work") {
		t.Errorf("refusal must be logged, got:\n%s", log.String())
	}
}

func TestHuntNeedHuntsOnlyItsSpecies(t *testing.T) {
	t.Parallel()
	kb := huntNeedKBFixture(t)
	f := newHuntNeedFake(t, "gauntlet", huntFakeOpts{creatures: []huntCreature{
		{id: "tortoise", species: "slag_tortoise", hull: 10}, // smaller, would be picked first
		{id: "eel", species: "belt_grazer", role: "predator"},
		{id: "g1"}, {id: "g2"}, {id: "g3"},
	}})
	var log strings.Builder
	if err := Hunt(context.Background(), huntNeedDeps(f, kb, &log)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	for _, c := range f.calls {
		if c == "hunt:tortoise" || c == "hunt:eel" {
			t.Fatalf("engaged %s: only the need's species, and never a predator; calls %v", c, f.calls)
		}
	}
	if f.huntCalls == 0 {
		t.Fatalf("expected the belt_grazers to be hunted; log:\n%s", log.String())
	}
}

func TestHuntResumedNeedHeldWhenNoLongerAdmissible(t *testing.T) {
	t.Parallel()
	kb := huntNeedKBFixture(t)
	f := newHuntNeedFake(t, "gauntlet", huntFakeOpts{
		heldAtStart:    true,
		activeMissions: []serverapi.ActiveMission{huntCausticActive(3)},
		creatures:      huntGrazers("c1"),
	})
	f.raw["missions"] = boardJSON(t)
	var log strings.Builder
	if err := Hunt(context.Background(), huntNeedDeps(f, kb, &log)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if !strings.Contains(log.String(), "holding need hex-caustic") {
		t.Errorf("an over-cap held need must be logged as held, got:\n%s", log.String())
	}
	if called(f.huntFake, "abandon:") || f.huntCalls != 0 {
		t.Errorf("a held need that no longer admits must be neither abandoned nor hunted: %v", f.calls)
	}
}

func TestHuntResumedNeedDeliversWhenAlreadyHeld(t *testing.T) {
	t.Parallel()
	kb := huntNeedKBFixture(t)
	f := newHuntNeedFake(t, "gauntlet", huntFakeOpts{
		heldAtStart:    true,
		activeMissions: []serverapi.ActiveMission{huntCausticActive(2)},
	})
	f.state.Ship.Cargo = []game.CargoItem{{ItemID: "caustic_sac", Quantity: 10}}
	var log strings.Builder
	if err := Hunt(context.Background(), huntNeedDeps(f, kb, &log)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if f.huntCalls != 0 || !called(f.huntFake, "travel:levy_base") || !called(f.huntFake, "complete:hex-caustic") {
		t.Fatalf("a resumed need with the parts aboard must deliver without hunting; calls %v\nlog:\n%s", f.calls, log.String())
	}
}

func TestHuntGroundRadiusBoundsTheChoice(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	kb := huntGroundKB(t) // pollux_outer_belt: sighted 10h ago, never looked at
	// A far ground with a fresh sighting, which outranks the near one by class.
	if err := kb.RememberSystem(ctx, knowledge.System{ID: "faraway", Name: "Faraway", PoliceLevel: 80, LastVisitedTick: 1}); err != nil {
		t.Fatal(err)
	}
	if err := kb.RememberPOI(ctx, knowledge.POI{ID: "faraway_belt", SystemID: "faraway", Type: "asteroid_belt"}); err != nil {
		t.Fatal(err)
	}
	kb.sightings = append(kb.sightings, knowledge.WildlifeSighting{
		Species: "belt_grazer", SystemID: "faraway", POIID: "faraway_belt",
		Source: knowledge.WildlifeSourceNearby, ObservedUTC: huntAgo(time.Hour),
	})
	need := huntNeed{ItemID: "caustic_sac", Species: []string{"belt_grazer"}}
	hops := func(s string) int { return map[string]int{"pollux": 3, "faraway": 20}[s] }

	_, poi, why := huntNeedGround(ctx, kb, kb, need, hops, 6, nil, huntNeedTestNow)
	if poi != "pollux_outer_belt" {
		t.Fatalf("radius 6: got %q (%s), want the near unlooked pollux_outer_belt, not the 20-jump sighted one", poi, why)
	}
	// Re-pick after excluding the near ground: nothing within the radius.
	if _, poi, _ := huntNeedGround(ctx, kb, kb, need, hops, 6, []string{"pollux_outer_belt"}, huntNeedTestNow); poi != "" {
		t.Fatalf("re-pick after exclude returned %q, beyond the radius", poi)
	}
	// With a radius that reaches it, the fresher far ground wins on class.
	if _, poi, _ := huntNeedGround(ctx, kb, kb, need, hops, 24, nil, huntNeedTestNow); poi != "faraway_belt" {
		t.Fatalf("radius 24: got %q, want faraway_belt", poi)
	}
}

func TestHuntTravelToNeedGroundKeepsTheJobRadius(t *testing.T) {
	t.Parallel()
	kb := huntGroundKB(t) // pollux_outer_belt is 1 jump from test_system
	// A second, fresher ground 2 jumps out at levy.
	if err := kb.RememberPOI(context.Background(), knowledge.POI{ID: "levy_ice", SystemID: "levy", Type: "ice_field"}); err != nil {
		t.Fatal(err)
	}
	kb.sightings = append(kb.sightings, knowledge.WildlifeSighting{
		Species: "belt_grazer", SystemID: "levy", POIID: "levy_ice",
		Source: knowledge.WildlifeSourceNearby, ObservedUTC: huntAgo(time.Hour),
	})
	f := newHuntNeedFake(t, "gauntlet", huntFakeOpts{})
	var log strings.Builder
	deps := huntNeedDeps(f, kb, &log)
	need := huntNeed{ItemID: "caustic_sac", Species: []string{"belt_grazer"}}

	need.Radius = 1
	if poi, err := huntTravelToNeedGround(context.Background(), deps, &log, need, nil); err != nil || poi != "pollux_outer_belt" {
		t.Fatalf("radius 1: got %q err %v, want pollux_outer_belt", poi, err)
	}
	f.state.CurrentPOI, f.state.Doc = "haven_station", true
	need.Radius = 0 // unset falls back to huntNearbyJumps, which reaches the fresher levy_ice
	if poi, err := huntTravelToNeedGround(context.Background(), deps, &log, need, nil); err != nil || poi != "levy_ice" {
		t.Fatalf("radius unset: got %q err %v", poi, err)
	}
	// After excluding it, levy_ice (2 jumps) is beyond the job's radius of 1:
	// the re-pick refuses rather than flying further out.
	f.state.CurrentPOI, f.state.Doc = "haven_station", true
	need.Radius = 1
	if poi, err := huntTravelToNeedGround(context.Background(), deps, &log, need, []string{"pollux_outer_belt"}); err == nil {
		t.Fatalf("re-pick after exclude flew to %q", poi)
	}
}

func TestHuntDeliveryWaitsForRepair(t *testing.T) {
	t.Parallel()
	kb := huntNeedKBFixture(t)
	// 36% hull: above the 35% flee floor, so the pass hunts, but well below
	// the repair threshold the delivery leg requires.
	f := newHuntNeedFake(t, "gauntlet", huntFakeOpts{shipHullFrac: 0.36, creatures: huntGrazers("c1", "c2", "c3", "c4")})
	var log strings.Builder
	deps := huntNeedDeps(f, kb, &log)
	if err := Hunt(context.Background(), deps); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if got := cargoQty(f.state, "caustic_sac"); got < 10 {
		t.Fatalf("parts not gathered (%.0f aboard); log:\n%s", got, log.String())
	}
	if called(f.huntFake, "travel:levy_base") || called(f.huntFake, "complete:") {
		t.Fatalf("delivered on a damaged hull: calls %v", f.calls)
	}
	if !strings.Contains(log.String(), "delivery waits for repair") {
		t.Errorf("the wait must be logged, got:\n%s", log.String())
	}

	// Next pass: repaired. The held need resumes with its parts aboard and
	// delivers without hunting again.
	f.opts.shipHullFrac = 0
	f.state.Ship.Hull = f.state.Ship.MaxHull
	hunts := f.huntCalls
	if err := Hunt(context.Background(), deps); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if f.huntCalls != hunts {
		t.Errorf("resumed pass hunted again (%d -> %d)", hunts, f.huntCalls)
	}
	if !called(f.huntFake, "travel:levy_base") || !called(f.huntFake, "complete:hex-caustic") {
		t.Fatalf("repaired pass must deliver; calls %v\nlog:\n%s", f.calls, log.String())
	}
}
