package battlereplay

import (
	"strings"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// HostileType names a battle participant that is a hostile (not a player,
// pirate or creature), or "" when it is not one. Frontier Repeaters are
// recognised by a "repeater_" id, a "repeater" kind, or a "Repeater <Class>"
// display name; the class comes from ship_class, else the name's last word
// ("Repeater Pane" -> repeater_pane).
func HostileType(p Participant) string {
	name := strings.TrimSpace(p.Username)
	isRepeater := strings.HasPrefix(p.PlayerID, "repeater_") || p.Kind == "repeater" ||
		(p.Kind != "player" && strings.HasPrefix(strings.ToLower(name), "repeater "))
	if !isRepeater {
		return ""
	}
	class := p.ShipClass
	if class == "" {
		if f := strings.Fields(name); len(f) > 1 {
			class = f[len(f)-1]
		}
	}
	return knowledge.RepeaterHostileType(class)
}

// HostileAttacks aggregates what every hostile in the battle fired.
func HostileAttacks(m *ReplayModel) []knowledge.HostileAttack {
	if m == nil {
		return nil
	}
	byID := make(map[string]string)
	for _, p := range m.Participants {
		if t := HostileType(p); t != "" {
			byID[p.PlayerID] = t
		}
	}
	rows := attacksByType(m, byID)
	out := make([]knowledge.HostileAttack, 0, len(rows))
	for _, r := range rows {
		out = append(out, knowledge.HostileAttack{
			HostileType: r.Species, BattleID: r.BattleID, WeaponName: r.WeaponName,
			DamageType: r.DamageType, ShotKind: r.ShotKind, Shots: r.Shots, Hits: r.Hits,
			DamageTotal: r.DamageTotal, DamageMin: r.DamageMin, DamageMax: r.DamageMax,
		})
	}
	return out
}

// HostileSpecs reads each hostile's class, hull/shield maxima and fitted
// modules from the battle snapshots, so a battle fills hostile_types and
// hostile_loadouts even for a repeater no get_nearby ever saw. Modules are
// counted by name ("Heavy Pulse Laser" x8).
func HostileSpecs(m *ReplayModel) []knowledge.HostileSpec {
	if m == nil {
		return nil
	}
	var out []knowledge.HostileSpec
	for _, p := range m.Participants {
		t := HostileType(p)
		if t == "" {
			continue
		}
		counts := make(map[string]*knowledge.HostileModule)
		var order []string
		for _, mod := range p.Modules {
			if mod.Name == "" {
				continue
			}
			c, ok := counts[mod.Name]
			if !ok {
				c = &knowledge.HostileModule{Name: mod.Name, Category: mod.Category}
				counts[mod.Name] = c
				order = append(order, mod.Name)
			}
			c.Count++
		}
		mods := make([]knowledge.HostileModule, 0, len(order))
		for _, n := range order {
			mods = append(mods, *counts[n])
		}
		out = append(out, knowledge.HostileSpec{
			HostileID: p.PlayerID, HostileType: t, Kind: knowledge.HostileKindRepeater,
			Class: strings.TrimPrefix(t, knowledge.HostileKindRepeater+"_"), Name: p.Username,
			MaxHull: p.MaxHull, MaxShield: p.MaxShield, Modules: mods, BattleID: m.BattleID,
		})
	}
	return out
}
