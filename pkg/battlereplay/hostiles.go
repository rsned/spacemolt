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
