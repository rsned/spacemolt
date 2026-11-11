package game

import (
	"fmt"
	"strings"
)

// formatPlayerDied renders the operational log line for a player_died push:
// cause, killer, ship lost, wreck, respawn and insurance.
func formatPlayerDied(p map[string]any, w WreckSite) string {
	str := func(k string) string { s, _ := p[k].(string); return s }
	num := func(k string) int { f, _ := p[k].(float64); return int(f) }

	parts := []string{"💀 SHIP DESTROYED"}
	if c := str("cause"); c != "" {
		parts = append(parts, "cause="+c)
	}
	if k := str("killer_name"); k != "" {
		parts = append(parts, "killer="+k)
	}
	if s := str("ship_lost"); s != "" {
		parts = append(parts, "ship="+s)
	}
	where := w.POIName
	if where == "" {
		where = w.POIID
	}
	if sys := w.SystemName; sys != "" || w.SystemID != "" {
		if sys == "" {
			sys = w.SystemID
		}
		where = strings.TrimSpace(where + " (" + sys + ")")
	}
	if where != "" {
		parts = append(parts, "wreck="+where)
	}
	if w.RespawnBase != "" {
		parts = append(parts, "respawn="+w.RespawnBase)
	}
	if v := num("insurance_payout"); v > 0 {
		parts = append(parts, fmt.Sprintf("insurance=%d", v))
	}
	if v := num("clone_cost"); v > 0 {
		parts = append(parts, fmt.Sprintf("clone_cost=%d", v))
	}

	return strings.Join(parts, " ")
}
