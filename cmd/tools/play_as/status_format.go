package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/mattn/go-runewidth"
)

// statusPayload is the get_status "player" object as it actually arrives on the
// wire. It deliberately differs from game.Player: skills is a flat level map,
// and standings / completed_missions / revealed_pois are not modelled there.
// Declared inline (as elsewhere in this package) so the formatter tracks the
// live payload rather than the typed client struct.
type statusPayload struct {
	Player struct {
		ID            string         `json:"id"`
		Username      string         `json:"username"`
		Empire        string         `json:"empire"`
		Credits       int64          `json:"credits"`
		CurrentSystem string         `json:"current_system"`
		CurrentPOI    string         `json:"current_poi"`
		FactionID     string         `json:"faction_id"`
		FactionRank   string         `json:"faction_rank"`
		Skills        map[string]int `json:"skills"`
		Standings     map[string]struct {
			Baseline          int `json:"baseline"`
			OutstandingBounty int `json:"outstanding_bounty"`
			Reputation        int `json:"reputation"`
		} `json:"standings"`
		CompletedMissions map[string]string `json:"completed_missions"`
		RevealedPOIs      map[string]bool   `json:"revealed_pois"`
		Stats             struct {
			CreditsEarned          int64 `json:"credits_earned"`
			CreditsSpent           int64 `json:"credits_spent"`
			CreditsGifted          int64 `json:"credits_gifted"`
			ExchangeCreditsEarned  int64 `json:"exchange_credits_earned"`
			ExchangeItemsBought    int64 `json:"exchange_items_bought"`
			ExchangeItemsSold      int64 `json:"exchange_items_sold"`
			TradesCompleted        int64 `json:"trades_completed"`
			OreMined               int64 `json:"ore_mined"`
			ItemsCrafted           int64 `json:"items_crafted"`
			FacilitiesBuilt        int64 `json:"facilities_built"`
			SystemsExplored        int64 `json:"systems_explored"`
			JumpsCompleted         int64 `json:"jumps_completed"`
			DistanceTraveled       int64 `json:"distance_traveled"`
			WormholesTraversed     int64 `json:"wormholes_traversed"`
			DeepCorePOIsDiscovered int64 `json:"deep_core_pois_discovered"`
			ScansPerformed         int64 `json:"scans_performed"`
			TimesDocked            int64 `json:"times_docked"`
			DamageDealt            int64 `json:"damage_dealt"`
			DamageTaken            int64 `json:"damage_taken"`
			PiratesDestroyed       int64 `json:"pirates_destroyed"`
			ShipsLost              int64 `json:"ships_lost"`
			DeathsByPirate         int64 `json:"deaths_by_pirate"`
			WreckItemsLooted       int64 `json:"wreck_items_looted"`
			GiftsSent              int64 `json:"gifts_sent"`
			GiftsReceived          int64 `json:"gifts_received"`
			ChatMessagesSent       int64 `json:"chat_messages_sent"`
			CustomsEvaded          int64 `json:"customs_evaded"`
			TimePlayed             int64 `json:"time_played"`
			MissionsCompleted      int64 `json:"missions_completed"`
			MissionsAccepted       int64 `json:"missions_accepted"`
			MissionsAbandoned      int64 `json:"missions_abandoned"`
		} `json:"stats"`
	} `json:"player"`
	Ship struct {
		Name           string `json:"name"`
		ClassID        string `json:"class_id"`
		CrewCapacity   int    `json:"crew_capacity"`
		MarineCapacity int    `json:"marine_capacity"`
		Personnel      *struct {
			FitCrew        int `json:"fit_crew"`
			FitMarines     int `json:"fit_marines"`
			InjuredCrew    int `json:"injured_crew"`
			InjuredMarines int `json:"injured_marines"`
		} `json:"personnel"`
	} `json:"ship"`
}

// statusCrewLine renders the v0.572.0 personnel block as
// "58/60 fit (2 injured) · marines 6/6", or "" when the frame carries no
// personnel data so pre-0.572 output is unchanged.
func statusCrewLine(sh *struct {
	Name           string `json:"name"`
	ClassID        string `json:"class_id"`
	CrewCapacity   int    `json:"crew_capacity"`
	MarineCapacity int    `json:"marine_capacity"`
	Personnel      *struct {
		FitCrew        int `json:"fit_crew"`
		FitMarines     int `json:"fit_marines"`
		InjuredCrew    int `json:"injured_crew"`
		InjuredMarines int `json:"injured_marines"`
	} `json:"personnel"`
}) string {
	if sh.Personnel == nil {
		return ""
	}
	pe := sh.Personnel
	crew := fmt.Sprintf("%d/%d fit", pe.FitCrew, sh.CrewCapacity)
	if pe.InjuredCrew > 0 {
		crew += fmt.Sprintf(" (%d injured)", pe.InjuredCrew)
	}
	marines := fmt.Sprintf("marines %d/%d", pe.FitMarines, sh.MarineCapacity)
	if pe.InjuredMarines > 0 {
		marines += fmt.Sprintf(" (%d injured)", pe.InjuredMarines)
	}
	return crew + " · " + marines
}

// Column geometry for an 80-column terminal: a full-width header box on top,
// then a Stats / Missions stack on the left beside a Reputation / Skills stack
// on the right.
//
// Missions sits on the left because reputation is no longer a short list: the
// server keeps a separate standing per pirate stronghold on top of the five
// empires, so the Reputation box grew from 6 rows to 14 and pushed the right
// column well past the left. Missions is a fixed 4 rows, which makes it the
// obvious counterweight to move.
const (
	statusTopW   = 80
	statusLeftW  = 39
	statusGap    = 1
	statusRightW = 40
)

// formatGetStatus renders a get_status response as a compact two-column
// dashboard sized for an 80-column terminal. Returns "" if the payload can't be
// parsed (caller falls back to JSON). Handles both the flat OK payload and an
// action_result-wrapped frame.
func formatGetStatus(raw []byte) string {
	var s statusPayload
	if err := json.Unmarshal(unwrapActionResult(raw), &s); err != nil {
		return ""
	}
	p := s.Player
	if p.ID == "" {
		return ""
	}

	// --- header box (full width) ---
	hInner := statusTopW - 4
	faction := p.FactionID
	if faction == "" {
		faction = "(none)"
	} else if p.FactionRank != "" {
		faction = fmt.Sprintf("%s (%s)", faction, p.FactionRank)
	}
	ship := s.Ship.Name
	if s.Ship.ClassID != "" {
		ship = fmt.Sprintf("%s [%s]", ship, s.Ship.ClassID)
	}
	headerFields := []string{
		statusField("ID", p.ID, hInner),
		statusField("Empire", p.Empire, hInner),
		statusField("Faction", faction, hInner),
		statusField("Credits", commaInt(p.Credits)+" cr", hInner),
		statusField("Location", p.CurrentSystem+" / "+p.CurrentPOI, hInner),
		statusField("Ship", ship, hInner),
	}
	if crew := statusCrewLine(&s.Ship); crew != "" {
		headerFields = append(headerFields, statusField("Crew", crew, hInner))
	}
	header := boxLines(p.Username, statusTopW, headerFields)

	// --- left column: Stats, then Missions ---
	st := p.Stats
	lInner := statusLeftW - 4
	statRows := []string{
		statusKV("Credits earned", commaInt(st.CreditsEarned), lInner),
		statusKV("Credits spent", commaInt(st.CreditsSpent), lInner),
		statusKV("Credits gifted", commaInt(st.CreditsGifted), lInner),
		statusKV("Exchange credits", commaInt(st.ExchangeCreditsEarned), lInner),
		statusKV("Exchange bought", commaInt(st.ExchangeItemsBought), lInner),
		statusKV("Exchange sold", commaInt(st.ExchangeItemsSold), lInner),
		statusKV("Trades completed", commaInt(st.TradesCompleted), lInner),
		statusKV("Ore mined", commaInt(st.OreMined), lInner),
		statusKV("Items crafted", commaInt(st.ItemsCrafted), lInner),
		statusKV("Facilities built", commaInt(st.FacilitiesBuilt), lInner),
		statusKV("Systems explored", commaInt(st.SystemsExplored), lInner),
		statusKV("Jumps completed", commaInt(st.JumpsCompleted), lInner),
		statusKV("Distance traveled", commaInt(st.DistanceTraveled), lInner),
		statusKV("Wormholes", commaInt(st.WormholesTraversed), lInner),
		statusKV("Deep-core POIs", commaInt(st.DeepCorePOIsDiscovered), lInner),
		statusKV("Scans performed", commaInt(st.ScansPerformed), lInner),
		statusKV("Times docked", commaInt(st.TimesDocked), lInner),
		statusKV("Damage dealt", commaInt(st.DamageDealt), lInner),
		statusKV("Damage taken", commaInt(st.DamageTaken), lInner),
		statusKV("Pirates destroyed", commaInt(st.PiratesDestroyed), lInner),
		statusKV("Ships lost", commaInt(st.ShipsLost), lInner),
		statusKV("Deaths by pirate", commaInt(st.DeathsByPirate), lInner),
		statusKV("Wreck items looted", commaInt(st.WreckItemsLooted), lInner),
		statusKV("Gifts sent", commaInt(st.GiftsSent), lInner),
		statusKV("Gifts received", commaInt(st.GiftsReceived), lInner),
		statusKV("Chat messages", commaInt(st.ChatMessagesSent), lInner),
		statusKV("Customs evaded", commaInt(st.CustomsEvaded), lInner),
		statusKV("Time played", fmtPlaytime(st.TimePlayed), lInner),
	}
	stats := boxLines("Stats", statusLeftW, statRows)

	missions := boxLines("Missions", statusLeftW, []string{
		statusKV("Completed", commaInt(st.MissionsCompleted), lInner),
		statusKV("Accepted", commaInt(st.MissionsAccepted), lInner),
		statusKV("Abandoned", commaInt(st.MissionsAbandoned), lInner),
		statusKV("Hidden revealed", commaInt(int64(len(p.RevealedPOIs))), lInner),
	})

	left := make([]string, 0, len(stats)+len(missions)+1)
	left = append(left, stats...)
	left = append(left, "")
	left = append(left, missions...)

	// --- right column: Reputation / Skills ---
	rInner := statusRightW - 4

	repRows := make([]string, 0, len(p.Standings))
	for _, name := range sortedStandings(p.Standings, p.Empire) {
		v := p.Standings[name]
		val := fmt.Sprintf("%d (base %d)", v.Reputation, v.Baseline)
		if v.OutstandingBounty > 0 {
			val += fmt.Sprintf(" !%d", v.OutstandingBounty)
		}
		repRows = append(repRows, statusKV(name, val, rInner))
	}
	rep := boxLines("Reputation", statusRightW, repRows)

	skillRows := make([]string, 0, len(p.Skills))
	for _, name := range sortedSkills(p.Skills) {
		skillRows = append(skillRows, statusKV(name, fmt.Sprintf("%d", p.Skills[name]), rInner))
	}
	skills := boxLines("Skills", statusRightW, skillRows)

	right := make([]string, 0, len(rep)+len(skills)+1)
	right = append(right, rep...)
	right = append(right, "")
	right = append(right, skills...)

	var b strings.Builder
	for _, line := range header {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	b.WriteString(sideBySide(left, right))
	return b.String()
}

// boxLines renders a titled box of the given outer width: a top border carrying
// the title, one bordered line per body row (each padded/truncated to fit), and
// a bottom border. Body rows are assumed to already be width-4 wide.
func boxLines(title string, width int, body []string) []string {
	inner := width - 4
	lines := make([]string, 0, len(body)+2)

	prefix := "┌─ " + title + " "
	fill := max(width-runewidth.StringWidth(prefix)-1, 0) // -1 for the closing ┐
	lines = append(lines, prefix+strings.Repeat("─", fill)+"┐")
	for _, row := range body {
		lines = append(lines, "│ "+padTrunc(row, inner)+" │")
	}
	lines = append(lines, "└"+strings.Repeat("─", width-2)+"┘")
	return lines
}

// sideBySide joins two pre-rendered boxed columns horizontally, padding the
// shorter one with blank space so the right column stays aligned.
func sideBySide(left, right []string) string {
	n := max(len(left), len(right))
	var b strings.Builder
	for i := range n {
		l := ""
		if i < len(left) {
			l = left[i]
		}
		r := ""
		if i < len(right) {
			r = right[i]
		}
		// TrimRight keeps rows past the end of the right column from trailing
		// the left column's padding into empty space.
		b.WriteString(strings.TrimRight(padRight(l, statusLeftW)+strings.Repeat(" ", statusGap)+r, " "))
		b.WriteByte('\n')
	}
	return b.String()
}

// statusField formats a fixed-label header line: a left label padded to a column
// then the value, the whole thing padded/truncated to width.
func statusField(label, value string, width int) string {
	return padTrunc(fmt.Sprintf("%-9s%s", label, value), width)
}

// statusKV lays out "label … value" with the value right-aligned within width.
func statusKV(label, value string, width int) string {
	gap := width - runewidth.StringWidth(label) - runewidth.StringWidth(value)
	if gap < 1 {
		// Truncate the label so the value always stays flush right.
		label = truncate(label, width-runewidth.StringWidth(value)-1)
		gap = 1
	}
	return label + strings.Repeat(" ", gap) + value
}

// sortedStandings returns standing faction keys with the player's own empire
// first, then the rest alphabetically.
func sortedStandings(m map[string]struct {
	Baseline          int `json:"baseline"`
	OutstandingBounty int `json:"outstanding_bounty"`
	Reputation        int `json:"reputation"`
}, empire string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i] == empire {
			return true
		}
		if keys[j] == empire {
			return false
		}
		return keys[i] < keys[j]
	})
	return keys
}

// sortedSkills returns skill keys ordered by level (highest first), breaking
// ties alphabetically.
func sortedSkills(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if m[keys[i]] != m[keys[j]] {
			return m[keys[i]] > m[keys[j]]
		}
		return keys[i] < keys[j]
	})
	return keys
}

// commaInt formats n with thousands separators (e.g. 1420770 -> "1,420,770").
func commaInt(n int64) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		if neg {
			return "-" + s
		}
		return s
	}
	var parts []string
	for len(s) > 3 {
		parts = append([]string{s[len(s)-3:]}, parts...)
		s = s[:len(s)-3]
	}
	parts = append([]string{s}, parts...)
	out := strings.Join(parts, ",")
	if neg {
		return "-" + out
	}
	return out
}

// fmtPlaytime renders a seconds count as a coarse "Xd Yh" / "Yh Zm" duration.
func fmtPlaytime(sec int64) string {
	days := sec / 86400
	hours := (sec % 86400) / 3600
	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	mins := (sec % 3600) / 60
	return fmt.Sprintf("%dh %dm", hours, mins)
}

// padTrunc pads s with trailing spaces to exactly width DISPLAY CELLS, or
// truncates it (with an ellipsis) if it is longer.
func padTrunc(s string, width int) string {
	n := runewidth.StringWidth(s)
	if n == width {
		return s
	}
	if n < width {
		return s + strings.Repeat(" ", width-n)
	}
	return truncate(s, width)
}

// truncate shortens s to width display cells, ending with "…" when anything
// was cut. The result is always exactly width cells wide.
//
// Everything here counts CELLS, not runes: an emoji is one rune but occupies
// two columns, so rune-based arithmetic overflows the box by one column per
// wide character. Usernames are operator-chosen and have carried emoji, so
// this is a live case, not a hypothetical.
func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if runewidth.StringWidth(s) <= width {
		return s
	}
	// Reserve one cell for the ellipsis, then take runes while they still fit.
	var b strings.Builder
	used, budget := 0, width-1
	for _, r := range s {
		w := runewidth.RuneWidth(r)
		if used+w > budget {
			break
		}
		b.WriteRune(r)
		used += w
	}
	// Stopping before a double-width rune can leave a cell short; pad so the
	// column still lines up.
	out := b.String() + "…"
	if pad := width - runewidth.StringWidth(out); pad > 0 {
		out += strings.Repeat(" ", pad)
	}

	return out
}
