package battlereplay

import (
	"compress/gzip"
	"encoding/json"
	"os"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// loadGolden reads the captured reference battle: a2619bbe328676445828b4e1007fe9aa,
// Node Beta, 11v30 plus a station, 30 ticks, 42 participants, 10,293 damage.
// Fetched live 2026-08-16 via get_battle_log(limit=200) + get_battle_summary.
func loadGolden(t *testing.T) ([]serverapi.GetBattleLogResponse, *serverapi.BattleSummaryResponse) {
	t.Helper()
	f, err := os.Open("testdata/log_a2619bbe.json.gz")
	if err != nil {
		t.Fatalf("open log fixture: %v", err)
	}
	defer f.Close() //nolint:errcheck
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gunzip: %v", err)
	}
	defer zr.Close() //nolint:errcheck
	var page serverapi.GetBattleLogResponse
	if err := json.NewDecoder(zr).Decode(&page); err != nil {
		t.Fatalf("decode log: %v", err)
	}

	sf, err := os.ReadFile("testdata/summary_a2619bbe.json")
	if err != nil {
		t.Fatalf("read summary fixture: %v", err)
	}
	var sum serverapi.BattleSummaryResponse
	if err := json.Unmarshal(sf, &sum); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	return []serverapi.GetBattleLogResponse{page}, &sum
}

// TestAdaptGoldenBattleShape pins the counts the spec's acceptance criteria
// name. If the wire shape drifts, this fails loudly rather than the renderer
// silently drawing an emptier battle.
func TestAdaptGoldenBattleShape(t *testing.T) {
	pages, sum := loadGolden(t)
	m := Adapt(pages, sum)

	if m.Schema != SchemaVersion {
		t.Errorf("schema = %d, want %d", m.Schema, SchemaVersion)
	}
	if m.BattleID != "a2619bbe328676445828b4e1007fe9aa" {
		t.Errorf("battle id = %q", m.BattleID)
	}
	if m.TickCount != 30 || m.TotalTicks != 30 {
		t.Errorf("ticks = %d (total %d), want 30/30", m.TickCount, m.TotalTicks)
	}
	if len(m.Participants) != 42 {
		t.Errorf("participants = %d, want 42", len(m.Participants))
	}
	if m.SystemID != "node_beta" || m.SystemName != "Node Beta" {
		t.Errorf("system = %q/%q, want node_beta/Node Beta", m.SystemID, m.SystemName)
	}
	if !m.HasStation {
		t.Error("has_station must survive from the summary — a station fought here")
	}
	if m.Status != "completed" || m.Outcome != "victory" || m.WinningSide != 2 {
		t.Errorf("status=%q outcome=%q winner=%d, want completed/victory/2", m.Status, m.Outcome, m.WinningSide)
	}
	if m.TotalDamage != 10293 {
		t.Errorf("total damage = %d, want 10293", m.TotalDamage)
	}

	var shots, kills, moves, chatter int
	for _, f := range m.Frames {
		shots += len(f.Shots)
		kills += len(f.Kills)
		moves += len(f.Moves)
		chatter += len(f.Chatter)
	}
	if kills != 14 {
		t.Errorf("kills = %d, want 14", kills)
	}
	if moves != 405 {
		t.Errorf("zone moves = %d, want 405", moves)
	}
	if chatter != 840 {
		t.Errorf("autopilot chatter = %d, want 840", chatter)
	}
	// 371 attacks expand to at least one shot each — multi-weapon attacks
	// yield more, and every attack must produce at least one drawable shot.
	if shots < 371 {
		t.Errorf("shots = %d, want >= 371 (one per attack minimum)", shots)
	}
}

// TestAdaptMarksDestroyed covers the derivation the renderer cannot make for
// itself: snapshots simply STOP for a dead ship, so without kill-derived
// destruction a renderer cannot tell "destroyed" from "not yet engaged".
func TestAdaptMarksDestroyed(t *testing.T) {
	pages, sum := loadGolden(t)
	m := Adapt(pages, sum)

	var destroyed int
	for _, p := range m.Participants {
		if p.DestroyedAtTick == 0 {
			continue
		}
		destroyed++
		if p.KilledBy == "" {
			t.Errorf("%s destroyed at %d with no killer credited", p.Username, p.DestroyedAtTick)
		}
		if p.DestroyedAtTick < p.FirstTick {
			t.Errorf("%s destroyed at %d before it appeared at %d", p.Username, p.DestroyedAtTick, p.FirstTick)
		}
	}
	if destroyed != 14 {
		t.Errorf("destroyed participants = %d, want 14 (matches ships_destroyed)", destroyed)
	}

	// A destroyed ship must be drawn ON the tick it dies (the explosion frame)
	// and never after it.
	for _, p := range m.Participants {
		if p.DestroyedAtTick == 0 {
			continue
		}
		for _, f := range m.Frames {
			present := false
			for _, s := range f.Ships {
				if s.PlayerID == p.PlayerID {
					present = true
					break
				}
			}
			if f.Tick == p.DestroyedAtTick && !present {
				t.Errorf("%s must still be drawn on its death tick %d", p.Username, f.Tick)
			}
			if f.Tick > p.DestroyedAtTick && present {
				t.Errorf("%s still drawn at tick %d, destroyed at %d", p.Username, f.Tick, p.DestroyedAtTick)
			}
		}
	}
}

// TestAdaptCarriesStateForward covers snapshot sparsity: the server emits rows
// only for participants present that tick (15 of 42 on the first tick here), so
// a naive renderer would blink ships in and out. State must persist between
// snapshots.
func TestAdaptCarriesStateForward(t *testing.T) {
	pages, sum := loadGolden(t)
	raw := pages[0].Entries
	m := Adapt(pages, sum)

	if len(raw[0].Snapshots) >= len(m.Participants) {
		t.Skipf("fixture no longer sparse on tick 1 (%d snapshots); test is vacuous", len(raw[0].Snapshots))
	}

	// Ship count per frame must never shrink except by death.
	deathsBy := map[int64]int{}
	for _, p := range m.Participants {
		if p.DestroyedAtTick > 0 {
			deathsBy[p.DestroyedAtTick]++
		}
	}
	prev := 0
	for i, f := range m.Frames {
		if i > 0 && len(f.Ships) < prev-deathsBy[m.Frames[i-1].Tick] {
			t.Errorf("tick %d: ships dropped %d -> %d with only %d deaths the tick before",
				f.Tick, prev, len(f.Ships), deathsBy[m.Frames[i-1].Tick])
		}
		prev = len(f.Ships)
	}

	// Every drawn ship must resolve to a participant, or the renderer has
	// nothing to draw it with.
	known := map[string]bool{}
	for _, p := range m.Participants {
		known[p.PlayerID] = true
	}
	for _, f := range m.Frames {
		for _, s := range f.Ships {
			if !known[s.PlayerID] {
				t.Fatalf("tick %d draws unknown participant %q", f.Tick, s.PlayerID)
			}
		}
	}
}

// TestAdaptBoundsAndZones checks the layout facts a renderer needs up front:
// the sides face each other along x, and the range bands come back ordered
// far-to-near.
func TestAdaptBoundsAndZones(t *testing.T) {
	pages, sum := loadGolden(t)
	m := Adapt(pages, sum)

	if !(m.Bounds.XMin < m.Bounds.XMax && m.Bounds.YMin < m.Bounds.YMax) {
		t.Fatalf("degenerate bounds: %+v", m.Bounds)
	}
	want := []string{"outer", "mid", "inner", "engaged"}
	if len(m.Zones) != len(want) {
		t.Fatalf("zones = %v, want %v", m.Zones, want)
	}
	for i, z := range want {
		if m.Zones[i] != z {
			t.Errorf("zones = %v, want %v (far to near)", m.Zones, want)
			break
		}
	}

	// The engagement axis: the two sides should occupy different x ranges.
	minX := map[int]float64{}
	maxX := map[int]float64{}
	side := map[string]int{}
	for _, p := range m.Participants {
		side[p.PlayerID] = p.SideID
	}
	for _, f := range m.Frames {
		for _, s := range f.Ships {
			sd := side[s.PlayerID]
			if _, ok := minX[sd]; !ok {
				minX[sd], maxX[sd] = s.X, s.X
			}
			minX[sd] = min(minX[sd], s.X)
			maxX[sd] = max(maxX[sd], s.X)
		}
	}
	if len(minX) != 2 {
		t.Fatalf("expected 2 sides, got %d", len(minX))
	}
	if minX[1] >= minX[2] {
		t.Errorf("sides should separate along x: side1 min %.2f, side2 min %.2f", minX[1], minX[2])
	}
}

// TestShotKindMapping pins the weapon-to-visual mapping, including the one that
// is easy to get backwards: an EMPTY ammo string means a beam (no ammo
// consumed), not "unknown".
func TestShotKindMapping(t *testing.T) {
	for _, tc := range []struct {
		ammo, damageType string
		want             ShotKind
	}{
		{"", "energy", ShotBeam},
		{"", "", ShotBeam},
		{"missile", "explosive", ShotMissile},
		{"scrap_missiles", "kinetic", ShotMissile},
		{"scrap_torpedoes", "explosive", ShotMissile},
		{"railgun", "kinetic", ShotProjectile},
		{"autocannon", "kinetic", ShotProjectile},
		{"scrap_shot", "kinetic", ShotProjectile},
		{"void_core_pack", "void", ShotVoid},
		{"em_charge_pack", "energy", ShotBeam},
		{"", "void", ShotVoid},
		{"", "explosive", ShotExplosive},
		{"", "kinetic", ShotProjectile},
	} {
		if got := shotKind(tc.ammo, tc.damageType); got != tc.want {
			t.Errorf("shotKind(%q, %q) = %q, want %q", tc.ammo, tc.damageType, got, tc.want)
		}
	}
}

// TestShotsFromExpandsWeapons: an attack firing several weapons becomes several
// shots so each can be drawn in its own style, and an attack with no weapon
// detail still yields one shot rather than vanishing.
func TestShotsFromExpandsWeapons(t *testing.T) {
	a := serverapi.AttackLogEntry{
		AttackerID: "a", TargetID: "b", HitSuccess: true, DamageType: "energy",
		FinalDamage: 90, ShieldDamage: 60, HullDamage: 30, ZoneDistance: 2,
		Weapons: []serverapi.WeaponFireDetail{
			{Name: "Pulse Laser I", Damage: 40},
			{Name: "Rail Battery", AmmoUsed: "railgun", Damage: 50, CritFired: true},
		},
	}
	got := shotsFrom(a)
	if len(got) != 2 {
		t.Fatalf("shots = %d, want 2 (one per weapon)", len(got))
	}
	if got[0].Kind != ShotBeam || got[1].Kind != ShotProjectile {
		t.Errorf("kinds = %q/%q, want beam/projectile", got[0].Kind, got[1].Kind)
	}
	if got[0].Crit || !got[1].Crit {
		t.Error("crit must follow the weapon that actually crit")
	}
	for i, s := range got {
		if s.Damage != 90 || s.ShieldDamage != 60 || s.HullDamage != 30 {
			t.Errorf("shot %d: applied damage must carry the attack totals, got %+v", i, s)
		}
	}
	if got[0].WeaponDamage != 40 || got[1].WeaponDamage != 50 {
		t.Errorf("per-weapon damage = %d/%d, want 40/50", got[0].WeaponDamage, got[1].WeaponDamage)
	}

	bare := shotsFrom(serverapi.AttackLogEntry{AttackerID: "a", TargetID: "b", FinalDamage: 5, DamageType: "void"})
	if len(bare) != 1 || bare[0].Kind != ShotVoid {
		t.Errorf("attack with no weapon detail must still draw one shot, got %+v", bare)
	}
}

// TestAdaptMergesOverlappingPages covers live polling, where a re-fetch returns
// ticks already seen: pages must merge by tick rather than duplicating frames.
func TestAdaptMergesOverlappingPages(t *testing.T) {
	pages, sum := loadGolden(t)
	full := pages[0]

	first := serverapi.GetBattleLogResponse{
		BattleID: full.BattleID, Status: "active", TotalTicks: full.TotalTicks,
		Entries: full.Entries[:20],
	}
	// Overlaps the first page by five ticks, as a poll from a stale cursor does.
	second := serverapi.GetBattleLogResponse{
		BattleID: full.BattleID, Status: full.Status, TotalTicks: full.TotalTicks,
		Entries: full.Entries[15:],
	}

	merged := Adapt([]serverapi.GetBattleLogResponse{second, first}, sum) // out of order on purpose
	whole := Adapt(pages, sum)

	if merged.TickCount != whole.TickCount {
		t.Errorf("merged ticks = %d, want %d (no duplicates, no gaps)", merged.TickCount, whole.TickCount)
	}
	for i := 1; i < len(merged.Frames); i++ {
		if merged.Frames[i].Tick <= merged.Frames[i-1].Tick {
			t.Fatalf("frames out of order at %d: %d then %d", i, merged.Frames[i-1].Tick, merged.Frames[i].Tick)
		}
	}
	if len(merged.Participants) != len(whole.Participants) {
		t.Errorf("merged participants = %d, want %d", len(merged.Participants), len(whole.Participants))
	}
}

// TestAdaptWithoutSummary: the summary is optional, and everything essential to
// draw must come from the log alone.
func TestAdaptWithoutSummary(t *testing.T) {
	pages, _ := loadGolden(t)
	m := Adapt(pages, nil)

	if m.TickCount != 30 || len(m.Participants) != 42 {
		t.Errorf("log-only: ticks=%d participants=%d, want 30/42", m.TickCount, len(m.Participants))
	}
	if m.SystemID != "node_beta" {
		t.Errorf("system id must come from the log entries, got %q", m.SystemID)
	}
	if m.Outcome != "victory" {
		t.Errorf("outcome must come from the battle_ended entry, got %q", m.Outcome)
	}
	if m.SystemName != "" {
		t.Errorf("system NAME has no log field; want empty without a summary, got %q", m.SystemName)
	}
}
