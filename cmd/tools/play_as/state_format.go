package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// statePayload is the get_state response as it actually arrives on the wire.
// get_state is undocumented but the server answers it with a fuller live
// snapshot than get_status: system/poi/nearby are nested under "location", and
// cargo / missions / queue / per-skill xp / version are included. Declared
// inline (as elsewhere in this package) so the formatter tracks the live
// payload rather than a typed client struct.
type statePayload struct {
	Player struct {
		ID          string `json:"id"`
		Username    string `json:"username"`
		Empire      string `json:"empire"`
		Credits     int64  `json:"credits"`
		FactionID   string `json:"faction_id"`
		FactionRank string `json:"faction_rank"`
		IsCloaked   bool   `json:"is_cloaked"`
	} `json:"player"`
	Ship struct {
		Name          string  `json:"name"`
		ClassID       string  `json:"class_id"`
		ClassName     string  `json:"class_name"`
		Hull          float64 `json:"hull"`
		MaxHull       float64 `json:"max_hull"`
		Shield        float64 `json:"shield"`
		MaxShield     float64 `json:"max_shield"`
		Armor         float64 `json:"armor"`
		Fuel          float64 `json:"fuel"`
		MaxFuel       float64 `json:"max_fuel"`
		CargoUsed     float64 `json:"cargo_used"`
		CargoCapacity float64 `json:"cargo_capacity"`
		CPUUsed       float64 `json:"cpu_used"`
		CPUCapacity   float64 `json:"cpu_capacity"`
		PowerUsed     float64 `json:"power_used"`
		PowerCapacity float64 `json:"power_capacity"`
		Speed         float64 `json:"speed"`
		WeaponSlots   int     `json:"weapon_slots"`
		UtilitySlots  int     `json:"utility_slots"`
		DefenseSlots  int     `json:"defense_slots"`
	} `json:"ship"`
	Location struct {
		SystemID             string   `json:"system_id"`
		SystemName           string   `json:"system_name"`
		POIID                string   `json:"poi_id"`
		POIName              string   `json:"poi_name"`
		POIType              string   `json:"poi_type"`
		Empire               string   `json:"empire"`
		SecurityStatus       string   `json:"security_status"`
		DockedAt             string   `json:"docked_at"`
		Connections          []string `json:"connections"`
		NearbyPlayerCount    int      `json:"nearby_player_count"`
		NearbyEmpireNPCCount int      `json:"nearby_empire_npc_count"`
		NearbyPirateCount    int      `json:"nearby_pirate_count"`
		OfflineCollapsed     int      `json:"offline_collapsed"`
	} `json:"location"`
	Cargo []struct {
		ItemName string  `json:"item_name"`
		Quantity float64 `json:"quantity"`
	} `json:"cargo"`
	Skills map[string]struct {
		Level       int     `json:"level"`
		XP          float64 `json:"xp"`
		NextLevelXP float64 `json:"next_level_xp"`
	} `json:"skills"`
	Missions struct {
		Active      []json.RawMessage `json:"active"`
		MaxMissions int               `json:"max_missions"`
	} `json:"missions"`
	Queue struct {
		HasPending bool `json:"has_pending"`
	} `json:"queue"`
	Version string `json:"version"`
}

// formatGetState renders a get_state response as a two-column live-operations
// dashboard sized for an 80-column terminal: a header box on top, a Location +
// Ship column on the left, and a Cargo + Skills + Missions column on the right.
// It reuses the boxLines/sideBySide/statusKV geometry from status_format.go.
// Returns "" if the payload can't be parsed so the caller falls back to JSON.
func formatGetState(raw []byte) string {
	var s statePayload
	if err := json.Unmarshal(unwrapActionResult(raw), &s); err != nil {
		return ""
	}
	if s.Player.ID == "" {
		return ""
	}
	p, sh, loc := s.Player, s.Ship, s.Location

	// --- header box (full width) ---
	hInner := statusTopW - 4
	ship := sh.Name
	switch {
	case sh.ClassName != "":
		ship = fmt.Sprintf("%s [%s]", ship, sh.ClassName)
	case sh.ClassID != "":
		ship = fmt.Sprintf("%s [%s]", ship, sh.ClassID)
	}
	faction := p.FactionID
	switch {
	case faction == "":
		faction = "(none)"
	case p.FactionRank != "":
		faction = fmt.Sprintf("%s (%s)", faction, p.FactionRank)
	}
	status := "In space"
	if loc.DockedAt != "" {
		status = "Docked at " + loc.DockedAt
	}
	if p.IsCloaked {
		status += " (cloaked)"
	}
	header := boxLines(p.Username, statusTopW, []string{
		statusField("Ship", ship, hInner),
		statusField("Location", loc.SystemName+" / "+loc.POIName, hInner),
		statusField("Status", status, hInner),
		statusField("Empire", p.Empire, hInner),
		statusField("Faction", faction, hInner),
		statusField("Credits", commaInt(p.Credits)+" cr", hInner),
		statusField("Version", "v"+s.Version, hInner),
	})

	// --- left column: Location + Ship ---
	lInner := statusLeftW - 4
	locRows := []string{
		statusKV("System", loc.SystemName, lInner),
		statusKV("POI", loc.POIName, lInner),
		statusKV("Type", loc.POIType, lInner),
		padTrunc("Security: "+loc.SecurityStatus, lInner),
		statusKV("Players nearby", fmt.Sprintf("%d", loc.NearbyPlayerCount), lInner),
		statusKV("Empire NPCs", fmt.Sprintf("%d", loc.NearbyEmpireNPCCount), lInner),
		statusKV("Pirates", fmt.Sprintf("%d", loc.NearbyPirateCount), lInner),
		statusKV("Offline collapsed", fmt.Sprintf("%d", loc.OfflineCollapsed), lInner),
		padTrunc("Routes: "+strings.Join(loc.Connections, ", "), lInner),
	}
	shipRows := []string{
		statusKV("Hull", fmt.Sprintf("%.0f/%.0f", sh.Hull, sh.MaxHull), lInner),
		statusKV("Shield", fmt.Sprintf("%.0f/%.0f", sh.Shield, sh.MaxShield), lInner),
		statusKV("Armor", fmt.Sprintf("%.0f", sh.Armor), lInner),
		statusKV("Fuel", fmt.Sprintf("%.0f/%.0f", sh.Fuel, sh.MaxFuel), lInner),
		statusKV("Cargo", fmt.Sprintf("%.0f/%.0f", sh.CargoUsed, sh.CargoCapacity), lInner),
		statusKV("CPU", fmt.Sprintf("%.0f/%.0f", sh.CPUUsed, sh.CPUCapacity), lInner),
		statusKV("Power", fmt.Sprintf("%.0f/%.0f", sh.PowerUsed, sh.PowerCapacity), lInner),
		statusKV("Speed", fmt.Sprintf("%.0f", sh.Speed), lInner),
		statusKV("Slots W/U/D", fmt.Sprintf("%d/%d/%d", sh.WeaponSlots, sh.UtilitySlots, sh.DefenseSlots), lInner),
		statusKV("Queue pending", yesNo(s.Queue.HasPending), lInner),
	}
	left := boxLines("Location", statusLeftW, locRows)
	left = append(left, "")
	left = append(left, boxLines("Ship", statusLeftW, shipRows)...)

	// --- right column: Cargo + Skills + Missions ---
	rInner := statusRightW - 4
	cargoRows := make([]string, 0, len(s.Cargo))
	for _, c := range s.Cargo {
		cargoRows = append(cargoRows, statusKV(c.ItemName, fmt.Sprintf("x%.0f", c.Quantity), rInner))
	}
	if len(cargoRows) == 0 {
		cargoRows = append(cargoRows, padTrunc("(empty)", rInner))
	}
	cargoTitle := fmt.Sprintf("Cargo %.0f/%.0f", sh.CargoUsed, sh.CargoCapacity)

	skillRows := make([]string, 0, len(s.Skills))
	for _, name := range sortedStateSkills(s.Skills) {
		sk := s.Skills[name]
		skillRows = append(skillRows, statusKV(name,
			fmt.Sprintf("L%d %.0f/%.0f", sk.Level, sk.XP, sk.NextLevelXP), rInner))
	}

	missionRows := []string{
		statusKV("Active", fmt.Sprintf("%d / %d", len(s.Missions.Active), s.Missions.MaxMissions), rInner),
	}

	right := boxLines(cargoTitle, statusRightW, cargoRows)
	right = append(right, "")
	right = append(right, boxLines("Skills", statusRightW, skillRows)...)
	right = append(right, "")
	right = append(right, boxLines("Missions", statusRightW, missionRows)...)

	var b strings.Builder
	for _, line := range header {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	b.WriteString(sideBySide(left, right))
	return b.String()
}

// sortedStateSkills returns skill keys ordered by level (highest first),
// breaking ties alphabetically — mirrors sortedSkills but for the richer
// per-skill struct in the get_state payload.
func sortedStateSkills(m map[string]struct {
	Level       int     `json:"level"`
	XP          float64 `json:"xp"`
	NextLevelXP float64 `json:"next_level_xp"`
}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if m[keys[i]].Level != m[keys[j]].Level {
			return m[keys[i]].Level > m[keys[j]].Level
		}
		return keys[i] < keys[j]
	})
	return keys
}

// yesNo renders a bool as "yes"/"no" for compact dashboard cells.
func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
