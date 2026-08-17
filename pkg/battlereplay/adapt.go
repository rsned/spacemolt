package battlereplay

import (
	"math"
	"sort"
	"strings"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// zoneOrder ranks range bands from farthest to closest, so a renderer can lay
// them out as ordered lanes. Unknown bands sort last, alphabetically, rather
// than being dropped — a new server-side zone must show up rather than vanish.
var zoneOrder = map[string]int{"outer": 0, "mid": 1, "inner": 2, "engaged": 3}

// Adapt builds a ReplayModel from get_battle_log pages plus an optional
// get_battle_summary. Pages may arrive in any order and may overlap; entries
// are sorted and de-duplicated by tick.
//
// summary may be nil: everything essential is derivable from the log itself,
// and the summary only adds naming and headline totals.
func Adapt(pages []serverapi.GetBattleLogResponse, summary *serverapi.BattleSummaryResponse) ReplayModel {
	m := ReplayModel{Schema: SchemaVersion}

	entries := mergeEntries(pages)
	for _, p := range pages {
		if p.BattleID != "" {
			m.BattleID = p.BattleID
		}
		if p.Status != "" {
			m.Status = p.Status
		}
		if p.TotalTicks > m.TotalTicks {
			m.TotalTicks = p.TotalTicks
		}
	}

	parts := map[string]*Participant{}
	// live tracks the last known state of every ship still in the battle, so a
	// tick that omits a snapshot carries the previous one forward instead of
	// dropping the ship out of the frame.
	live := map[string]ShipState{}
	bounds := Bounds{XMin: math.Inf(1), XMax: math.Inf(-1), YMin: math.Inf(1), YMax: math.Inf(-1)}
	zones := map[string]bool{}
	sawPosition := false

	for _, e := range entries {
		if m.SystemID == "" {
			m.SystemID = e.SystemID
		}

		for _, s := range e.Snapshots {
			p := parts[s.PlayerID]
			if p == nil {
				p = &Participant{PlayerID: s.PlayerID, FirstTick: e.Tick}
				parts[s.PlayerID] = p
			}
			// Identity fields are refreshed rather than set once: a later
			// snapshot may fill in something an earlier one left blank.
			if s.Username != "" {
				p.Username = s.Username
			}
			if s.Kind != "" {
				p.Kind = s.Kind
			}
			if s.ShipClass != "" {
				p.ShipClass = strings.ToLower(s.ShipClass)
			}
			if s.FactionID != "" {
				p.FactionID = s.FactionID
			}
			if s.SideID != 0 {
				p.SideID = s.SideID
			}
			p.MaxHull = max(p.MaxHull, s.MaxHull)
			p.MaxShield = max(p.MaxShield, s.MaxShield)
			p.MaxFuel = max(p.MaxFuel, s.MaxFuel)
			if len(s.Modules) > 0 && len(p.Modules) == 0 {
				for _, mod := range s.Modules {
					p.Modules = append(p.Modules, Module{Name: mod.Name, Category: mod.Category})
				}
			}
			p.LastTick = e.Tick

			live[s.PlayerID] = ShipState{
				PlayerID: s.PlayerID, X: s.X, Y: s.Y, Zone: s.Zone,
				Hull: s.Hull, Shield: s.Shield, Fuel: s.Fuel,
				Stance: s.Stance, TargetID: s.TargetID, AutoPilot: s.AutoPilot,
				DamageDealt: s.DamageDealt, DamageTaken: s.DamageTaken, KillCount: s.KillCount,
			}
			if s.Zone != "" {
				zones[s.Zone] = true
			}
			bounds.XMin = math.Min(bounds.XMin, s.X)
			bounds.XMax = math.Max(bounds.XMax, s.X)
			bounds.YMin = math.Min(bounds.YMin, s.Y)
			bounds.YMax = math.Max(bounds.YMax, s.Y)
			sawPosition = true
		}

		f := Frame{Tick: e.Tick}
		for _, a := range e.Attacks {
			f.Shots = append(f.Shots, shotsFrom(a)...)
		}
		for _, z := range e.ZoneMoves {
			f.Moves = append(f.Moves, Move{PlayerID: z.PlayerID, From: z.OldZone, To: z.NewZone, Reason: z.Reason})
		}
		for _, r := range e.Regen {
			if r.ShieldRegen == 0 && r.ArmorRepair == 0 && r.RemoteRepair == 0 {
				continue
			}
			f.Repairs = append(f.Repairs, Repair{
				PlayerID: r.PlayerID, ShieldRegen: r.ShieldRegen,
				ArmorRepair: r.ArmorRepair, RemoteRepair: r.RemoteRepair,
			})
		}
		for _, c := range e.AutoPilot {
			f.Chatter = append(f.Chatter, Chatter{PlayerID: c.PlayerID, Reason: c.Reason, ChosenTarget: c.ChosenTarget})
		}

		// Kills are applied AFTER this frame's ships are emitted, so the victim
		// is still drawn on the tick it dies — that is the tick the explosion
		// plays on.
		for _, k := range e.Kills {
			f.Kills = append(f.Kills, Kill{KillerID: k.KillerID, VictimID: k.VictimID})
		}

		f.Ships = statesFrom(live)
		m.Frames = append(m.Frames, f)

		for _, k := range e.Kills {
			if p := parts[k.VictimID]; p != nil {
				p.DestroyedAtTick = e.Tick
				p.KilledBy = k.KillerID
			}
			delete(live, k.VictimID)
		}

		if e.BattleEnded != nil {
			if m.Outcome == "" {
				m.Outcome = e.BattleEnded.Outcome
			}
			if m.WinningSide == 0 {
				m.WinningSide = e.BattleEnded.WinningSide
			}
			if m.TotalDamage == 0 {
				m.TotalDamage = e.BattleEnded.TotalDamage
			}
		}
	}

	if len(m.Frames) > 0 {
		m.StartTick = m.Frames[0].Tick
		m.EndTick = m.Frames[len(m.Frames)-1].Tick
		m.TickCount = len(m.Frames)
	}
	if m.TotalTicks == 0 {
		m.TotalTicks = m.TickCount
	}
	if sawPosition {
		m.Bounds = bounds
		m.Centre = Point{X: (bounds.XMin + bounds.XMax) / 2, Y: (bounds.YMin + bounds.YMax) / 2}
	}
	m.Zones = sortedZones(zones)

	m.Participants = make([]Participant, 0, len(parts))
	for _, p := range parts {
		m.Participants = append(m.Participants, *p)
	}
	sort.Slice(m.Participants, func(i, j int) bool {
		a, b := m.Participants[i], m.Participants[j]
		if a.SideID != b.SideID {
			return a.SideID < b.SideID
		}
		if a.Username != b.Username {
			return a.Username < b.Username
		}
		return a.PlayerID < b.PlayerID
	})

	m.Sides = buildSides(m.Frames, m.Participants, m.Centre, m.WinningSide)
	applySummary(&m, summary)
	return m
}

// buildSides summarises where each side sits around the table.
//
// The table is RADIAL, not a linear axis: zones are rings around the centre and
// each side is assigned a bearing, with advance and retreat running inward and
// outward along it. So a side is described by its mean bearing (which arc it
// holds) and mean radius (how far in it has pressed) — not by a position on an
// axis, and emphatically not by a two-side "left versus right" rule, since
// three- and four-sided battles occur.
func buildSides(frames []Frame, parts []Participant, centre Point, winner int) []Side {
	sideOf := make(map[string]int, len(parts))
	counts := map[int]int{}
	for _, p := range parts {
		sideOf[p.PlayerID] = p.SideID
		counts[p.SideID]++
	}

	// Bearings are averaged as unit vectors, not as degrees: a side straddling
	// the +x axis has bearings near both 0° and 360°, and averaging those
	// numerically would place it at 180° — the exact opposite of where it is.
	type acc struct {
		sinSum, cosSum, radSum float64
		n                      int
	}
	stats := map[int]*acc{}
	for _, f := range frames {
		for _, s := range f.Ships {
			sd := sideOf[s.PlayerID]
			a := stats[sd]
			if a == nil {
				a = &acc{}
				stats[sd] = a
			}
			dx, dy := s.X-centre.X, s.Y-centre.Y
			a.radSum += math.Hypot(dx, dy)
			if dx != 0 || dy != 0 {
				ang := math.Atan2(dy, dx)
				a.sinSum += math.Sin(ang)
				a.cosSum += math.Cos(ang)
			}
			a.n++
		}
	}

	out := make([]Side, 0, len(counts))
	for sd, n := range counts {
		s := Side{SideID: sd, Count: n, Won: winner != 0 && sd == winner}
		if a := stats[sd]; a != nil && a.n > 0 {
			s.RadiusMean = a.radSum / float64(a.n)
			deg := math.Atan2(a.sinSum, a.cosSum) * 180 / math.Pi
			if deg < 0 {
				deg += 360
			}
			s.BearingMean = deg
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SideID < out[j].SideID })
	return out
}

// applySummary layers the optional summary over the log-derived model. The log
// wins on anything both describe, because the log is the thing being drawn;
// the summary only fills gaps and adds names the log has no field for.
func applySummary(m *ReplayModel, s *serverapi.BattleSummaryResponse) {
	if s == nil {
		return
	}
	if m.BattleID == "" {
		m.BattleID = s.BattleID
	}
	if m.SystemID == "" {
		m.SystemID = s.SystemID
	}
	m.SystemName = s.SystemName
	m.HasStation = s.HasStation
	if m.Status == "" {
		m.Status = s.Status
	}
	if m.Outcome == "" {
		m.Outcome = s.Outcome
	}
	if m.WinningSide == 0 {
		m.WinningSide = s.WinningSide
	}
	if m.TotalDamage == 0 {
		m.TotalDamage = s.TotalDamage
	}
	if m.StartTick == 0 {
		m.StartTick = s.StartTick
	}

	// Faction identity per side exists only in the summary — snapshots carry a
	// participant's faction, not the side's tag. Sides the summary names but the
	// log never showed are appended, so a side that joined and died before any
	// snapshot still appears in the roster.
	for _, ss := range s.Sides {
		found := false
		for i := range m.Sides {
			if m.Sides[i].SideID != ss.SideID {
				continue
			}
			found = true
			if m.Sides[i].FactionID == "" {
				m.Sides[i].FactionID = ss.FactionID
			}
			if m.Sides[i].FactionTag == "" {
				m.Sides[i].FactionTag = ss.FactionTag
			}
			if m.Sides[i].Count == 0 {
				m.Sides[i].Count = len(ss.Participants)
			}
		}
		if !found {
			m.Sides = append(m.Sides, Side{
				SideID: ss.SideID, FactionID: ss.FactionID, FactionTag: ss.FactionTag,
				Count: len(ss.Participants), Won: s.WinningSide != 0 && ss.SideID == s.WinningSide,
			})
		}
	}
	sort.Slice(m.Sides, func(i, j int) bool { return m.Sides[i].SideID < m.Sides[j].SideID })

	// Re-flag the winner last: buildSides runs before this, so when the winning
	// side came from the SUMMARY rather than a battle_ended entry, the flags set
	// during the build were computed against a zero value.
	for i := range m.Sides {
		m.Sides[i].Won = m.WinningSide != 0 && m.Sides[i].SideID == m.WinningSide
	}
}

// mergeEntries flattens pages into one tick-ordered run, keeping the LAST entry
// seen for a duplicated tick (a re-fetch of a live battle returns a more
// complete version of the tick it already sent).
func mergeEntries(pages []serverapi.GetBattleLogResponse) []serverapi.BattleLogEntry {
	byTick := map[int64]serverapi.BattleLogEntry{}
	for _, p := range pages {
		for _, e := range p.Entries {
			byTick[e.Tick] = e
		}
	}
	out := make([]serverapi.BattleLogEntry, 0, len(byTick))
	for _, e := range byTick {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Tick < out[j].Tick })
	return out
}

// statesFrom renders the live map as a stable, sorted slice so exported models
// diff cleanly between runs.
func statesFrom(live map[string]ShipState) []ShipState {
	out := make([]ShipState, 0, len(live))
	for _, s := range live {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PlayerID < out[j].PlayerID })
	return out
}

func sortedZones(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for z := range set {
		out = append(out, z)
	}
	sort.Slice(out, func(i, j int) bool {
		ri, oki := zoneOrder[out[i]]
		rj, okj := zoneOrder[out[j]]
		if oki != okj {
			return oki // known bands first, unknown ones after
		}
		if oki && ri != rj {
			return ri < rj
		}
		return out[i] < out[j]
	})
	return out
}

// shotsFrom expands one attack into one Shot per weapon fired. A three-weapon
// attack becomes three shots because each weapon can be a different type with
// its own visual treatment.
//
// The applied-damage figures are the ATTACK's totals, repeated on each shot:
// the server resolves resists per attack, not per weapon, so dividing them
// between weapons would invent precision the data does not have. Each shot
// carries its own pre-resist WeaponDamage for relative weighting.
//
// An attack with no weapon detail still yields one shot, so damage that
// resolved is never silently undrawn.
func shotsFrom(a serverapi.AttackLogEntry) []Shot {
	base := Shot{
		FromID: a.AttackerID, ToID: a.TargetID,
		DamageType: a.DamageType, Hit: a.HitSuccess,
		Damage: a.FinalDamage, ShieldDamage: a.ShieldDamage, HullDamage: a.HullDamage,
		ZoneDistance: a.ZoneDistance, Splash: a.Splash,
	}
	if len(a.Weapons) == 0 {
		base.Kind = shotKind("", a.DamageType)
		return []Shot{base}
	}
	out := make([]Shot, 0, len(a.Weapons))
	for _, w := range a.Weapons {
		s := base
		s.WeaponName = w.Name
		s.Ammo = w.AmmoUsed
		s.Crit = w.CritFired
		s.WeaponDamage = w.Damage
		if s.WeaponDamage == 0 {
			s.WeaponDamage = w.BaseDamage
		}
		dt := w.DamageType
		if dt == "" {
			dt = a.DamageType
		}
		s.DamageType = dt
		s.Kind = shotKind(w.AmmoUsed, dt)
		out = append(out, s)
	}
	return out
}

// shotKind maps a weapon fire to its visual family.
//
// Ammo is the primary signal and damage type the fallback, because ammo
// distinguishes the two cases that look most different on screen: a missile
// crossing the gap versus a beam that is simply on. An EMPTY ammo string is
// the beam signature — beams consume no ammo, and in the reference battle 666
// of 840 weapon fires reported no ammo at all.
func shotKind(ammo, damageType string) ShotKind {
	a := strings.ToLower(ammo)
	switch {
	case strings.Contains(a, "missile"), strings.Contains(a, "torpedo"), strings.Contains(a, "rocket"):
		return ShotMissile
	case strings.Contains(a, "railgun"), strings.Contains(a, "autocannon"), strings.Contains(a, "shot"),
		strings.Contains(a, "slug"), strings.Contains(a, "round"):
		return ShotProjectile
	}
	switch strings.ToLower(damageType) {
	case "void":
		return ShotVoid
	case "explosive":
		return ShotExplosive
	case "kinetic":
		return ShotProjectile
	}
	// energy, unknown, or absent: a beam. This is the common case.
	return ShotBeam
}
