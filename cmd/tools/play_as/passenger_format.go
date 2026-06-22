package main

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// paxItem is the common shape used to render passengers grouped by destination
// then class, for both list_passengers (aboard) and list_station_passengers
// (waiting). Fare/Ticks are the booked values (aboard); EstFare is the quoted
// estimate (station waiting list). Each is only shown in its respective view.
type paxItem struct {
	Name        string
	Class       string
	Citizenship string
	CitizenID   string
	DestName    string
	DestID      string
	DestSystem  string
	Fare        int
	Ticks       int
	EstFare     int
	// Jumps is the number of jumps from the player's current system to this
	// passenger's destination system: -1 when unknown (KB/state unavailable or
	// the destination is unreachable), 0 for a same-system delivery, >0 otherwise.
	Jumps int
}

// paxFareMode selects which fare column (if any) a passenger view renders.
type paxFareMode int

const (
	paxFareNone     paxFareMode = iota // no fare column
	paxFareEstimate                    // estimated fare only (station waiting list)
	paxFareBooked                      // booked fare + ticks remaining (aboard manifest)
)

// paxDestLabel renders a destination group header as "Name [id]". The id is
// what load_passenger requires, so it must be visible in the listing. Falls
// back gracefully when either field is missing.
func paxDestLabel(name, id string) string {
	switch {
	case name == "" && id == "":
		return "(unknown destination)"
	case name == "":
		return id
	case id == "":
		return name
	default:
		return name + " [" + id + "]"
	}
}

// paxPerHop returns the estimated fare amortized over the jump distance to the
// passenger's destination (estimated_fare ÷ jumps), and ok=false when there is
// no meaningful per-jump value: a missing fare quote, an unknown distance
// (Jumps < 0), or a same-system delivery (Jumps == 0, no jumps to divide by).
func paxPerHop(p paxItem) (int, bool) {
	if p.EstFare <= 0 || p.Jumps <= 0 {
		return 0, false
	}
	return p.EstFare / p.Jumps, true
}

// Class display orders. The station roster lists economy first (you scan it to
// gauge demand); the aboard manifest lists first class first (highest-value
// cabins on top). Classes not in the chosen order are appended alphabetically.
var (
	paxClassOrderStation = []string{"economy", "business", "first"}
	paxClassOrderAboard  = []string{"first", "business", "economy"}
)

// paxCols holds the global column widths used to align passenger lines across
// all destination groups.
type paxCols struct {
	name   int
	cit    int // citizenship text width (without brackets); 0 collapses the column
	citID  int
	fare   int
	ticks  int
	perHop int // per-jump fare width; 0 collapses the column (station view only)
}

// renderPaxByDestination renders passengers grouped by destination (alphabetical
// by name) and, within each destination, by class in classOrder. Passengers are
// listed by name. fareMode selects which fare column (if any) each line shows.
func renderPaxByDestination(items []paxItem, classOrder []string, fareMode paxFareMode) string {
	// Group by destination id (canonical/unique); track the display name, system,
	// and jump distance per id (all shared by passengers to that destination).
	groups := map[string][]paxItem{}
	destName := map[string]string{}
	destSystem := map[string]string{}
	destJumps := map[string]int{}
	for _, p := range items {
		groups[p.DestID] = append(groups[p.DestID], p)
		if _, ok := destName[p.DestID]; !ok {
			destName[p.DestID] = p.DestName
		}
		if destSystem[p.DestID] == "" {
			destSystem[p.DestID] = p.DestSystem
		}
		if _, ok := destJumps[p.DestID]; !ok {
			destJumps[p.DestID] = p.Jumps
		}
	}
	dests := make([]string, 0, len(groups))
	for d := range groups {
		dests = append(dests, d)
	}
	// Alphabetical by display name, with the id as a stable tiebreaker.
	slices.SortFunc(dests, func(a, c string) int {
		if n := strings.Compare(destName[a], destName[c]); n != 0 {
			return n
		}
		return strings.Compare(a, c)
	})

	// Global column widths so columns line up across all groups. The fare width
	// tracks whichever fare value the mode will actually print.
	var cols paxCols
	for _, p := range items {
		cols.name = max(cols.name, len(p.Name))
		cols.cit = max(cols.cit, len(p.Citizenship))
		cols.citID = max(cols.citID, len(p.CitizenID))
		switch fareMode {
		case paxFareBooked:
			cols.fare = max(cols.fare, len(strconv.Itoa(p.Fare)))
			cols.ticks = max(cols.ticks, len(strconv.Itoa(p.Ticks)))
		case paxFareEstimate:
			cols.fare = max(cols.fare, len(strconv.Itoa(p.EstFare)))
			if ph, ok := paxPerHop(p); ok {
				cols.perHop = max(cols.perHop, len(strconv.Itoa(ph)))
			}
		case paxFareNone:
		}
	}

	var b strings.Builder
	for _, d := range dests {
		grp := groups[d]
		label := paxDestLabel(destName[d], d)
		if sys := destSystem[d]; sys != "" {
			label += " · " + sys
		}
		// Append the jump distance from the player's current system when known.
		switch j := destJumps[d]; {
		case j > 0:
			label += fmt.Sprintf(" · %d jump%s", j, plural(j))
		case j == 0:
			label += " · same system"
		}
		fmt.Fprintf(&b, "\n%s (%d)\n", label, len(grp))

		byClass := map[string][]paxItem{}
		for _, p := range grp {
			byClass[p.Class] = append(byClass[p.Class], p)
		}

		seen := map[string]bool{}
		emit := func(cls string) {
			list := byClass[cls]
			if len(list) == 0 {
				return
			}
			seen[cls] = true
			slices.SortFunc(list, func(a, c paxItem) int {
				return strings.Compare(a.Name, c.Name)
			})
			classLabel := cls
			if classLabel == "" {
				classLabel = "(unspecified)"
			}
			fmt.Fprintf(&b, "  %s:\n", classLabel)
			for _, p := range list {
				renderPaxLine(&b, p, cols, fareMode)
			}
		}
		for _, cls := range classOrder {
			emit(cls)
		}
		// Any classes the server introduces beyond the known three.
		var others []string
		for cls := range byClass {
			if !seen[cls] {
				others = append(others, cls)
			}
		}
		slices.Sort(others)
		for _, cls := range others {
			emit(cls)
		}
	}
	return b.String()
}

// renderPaxLine writes a single indented passenger line, padded to the shared
// column widths so columns align across all groups:
//
//	    <name>  [<citizenship>]  <citizen_id>   [~]<fare> cr  [<ticks>t]
//
// The citizenship column is omitted entirely when no passenger has one (the
// aboard manifest carries no citizenship). The fare column depends on fareMode:
// booked fare + ticks (aboard), a ~estimate (station), or nothing.
func renderPaxLine(b *strings.Builder, p paxItem, cols paxCols, fareMode paxFareMode) {
	fmt.Fprintf(b, "    %-*s", cols.name, p.Name)
	if cols.cit > 0 {
		cit := ""
		if p.Citizenship != "" {
			cit = "[" + p.Citizenship + "]"
		}
		// cols.cit is the citizenship text width; +2 accounts for the brackets.
		fmt.Fprintf(b, "  %-*s", cols.cit+2, cit)
	}
	switch fareMode {
	case paxFareBooked:
		// citizen_id is followed by the fare/ticks columns, so pad it; then
		// append the right-aligned booked fare and ticks remaining.
		fmt.Fprintf(b, "  %-*s", cols.citID, p.CitizenID)
		fmt.Fprintf(b, "   %*s cr  %*st", cols.fare, strconv.Itoa(p.Fare), cols.ticks, strconv.Itoa(p.Ticks))
	case paxFareEstimate:
		// citizen_id is followed by the estimated fare, so pad it; the leading
		// ~ marks the fare as a quote, not a booked amount.
		fmt.Fprintf(b, "  %-*s", cols.citID, p.CitizenID)
		fmt.Fprintf(b, "   ~%*s cr", cols.fare, strconv.Itoa(p.EstFare))
		// When the jump distance is known, append the fare amortized per jump —
		// a value-density gauge for comparing destinations. The column is shared
		// (cols.perHop > 0) so same-system rows leave it blank but still align.
		if cols.perHop > 0 {
			if ph, ok := paxPerHop(p); ok {
				fmt.Fprintf(b, "  ~%*d/jump", cols.perHop, ph)
			}
		}
	case paxFareNone:
		// citizen_id is the final column; don't pad it (avoids trailing space).
		fmt.Fprintf(b, "  %s", p.CitizenID)
	}
	b.WriteString("\n")
}

// formatListPassengers renders a list_passengers response: passengers currently
// aboard the ship, grouped by destination then class, with a berth-usage header.
func formatListPassengers(raw []byte) string {
	raw = unwrapActionResult(raw)
	type aboardPax struct {
		CitizenID       string `json:"citizen_id"`
		Name            string `json:"name"`
		Citizenship     string `json:"citizenship"`
		Class           string `json:"class"`
		Destination     string `json:"destination"`
		DestinationName string `json:"destination_name"`
		Fare            int    `json:"fare"`
		TicksRemaining  int    `json:"ticks_remaining"`
	}
	var resp struct {
		Passengers     []aboardPax `json:"passengers"`
		Count          int         `json:"count"`
		EconomyBerths  string      `json:"economy_berths"`
		BusinessBerths string      `json:"business_berths"`
		FirstBerths    string      `json:"first_berths"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}
	// Distinguish an error/empty frame from a genuine empty manifest: a real
	// response always carries berth gauges.
	if resp.Passengers == nil && resp.EconomyBerths == "" &&
		resp.BusinessBerths == "" && resp.FirstBerths == "" {
		return ""
	}

	berth := func(s string) string {
		if s == "" {
			return "0/0"
		}
		return s
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Passengers aboard — %d | Berths: eco %s, biz %s, first %s\n",
		len(resp.Passengers), berth(resp.EconomyBerths), berth(resp.BusinessBerths), berth(resp.FirstBerths))
	if len(resp.Passengers) == 0 {
		b.WriteString("\n  (none aboard)\n")
		return b.String()
	}

	items := make([]paxItem, len(resp.Passengers))
	for i, p := range resp.Passengers {
		items[i] = paxItem{
			Name: p.Name, Class: p.Class, Citizenship: p.Citizenship,
			CitizenID: p.CitizenID, DestName: p.DestinationName, DestID: p.Destination,
			Fare: p.Fare, Ticks: p.TicksRemaining,
			Jumps: -1, // aboard manifest carries no origin; jumps are not shown
		}
	}
	b.WriteString(renderPaxByDestination(items, paxClassOrderAboard, paxFareBooked))
	return b.String()
}

// formatStationPassengers renders a list_station_passengers response: passengers
// waiting at a station, grouped by destination (alphabetical) then class.
func formatStationPassengers(raw []byte) string {
	raw = unwrapActionResult(raw)
	type waitingPax struct {
		CitizenID         string `json:"citizen_id"`
		Name              string `json:"name"`
		Citizenship       string `json:"citizenship"`
		Class             string `json:"class"`
		Destination       string `json:"destination"`
		DestinationName   string `json:"destination_name"`
		DestinationSystem string `json:"destination_system"`
		EstimatedFare     int    `json:"estimated_fare"`
	}
	var resp struct {
		Station string       `json:"station"`
		Count   int          `json:"count"`
		Waiting []waitingPax `json:"waiting"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}
	if resp.Station == "" && len(resp.Waiting) == 0 {
		return ""
	}

	station := resp.Station
	if station == "" {
		station = "this station"
	}
	var b strings.Builder
	if len(resp.Waiting) == 0 {
		fmt.Fprintf(&b, "No passengers waiting at %s\n", station)
		return b.String()
	}
	fmt.Fprintf(&b, "Passengers waiting at %s — %d total\n", station, len(resp.Waiting))

	// Resolve jump distances from the player's current system to each distinct
	// destination system (best-effort: needs live state + the KB; empty otherwise).
	destSystems := make([]string, 0, len(resp.Waiting))
	for _, w := range resp.Waiting {
		if w.DestinationSystem != "" {
			destSystems = append(destSystems, w.DestinationSystem)
		}
	}
	jumpsBySystem := stationDestJumps(destSystems)

	items := make([]paxItem, len(resp.Waiting))
	anyFare := false
	for i, w := range resp.Waiting {
		jumps := -1
		if j, ok := jumpsBySystem[w.DestinationSystem]; ok {
			jumps = j
		}
		items[i] = paxItem{
			Name: w.Name, Class: w.Class, Citizenship: w.Citizenship,
			CitizenID: w.CitizenID, DestName: w.DestinationName, DestID: w.Destination,
			DestSystem: w.DestinationSystem, EstFare: w.EstimatedFare, Jumps: jumps,
		}
		if w.EstimatedFare > 0 {
			anyFare = true
		}
	}
	// Older servers omit estimated_fare; only render the fare column when at
	// least one passenger carries a quote (avoids a column of "~0 cr").
	mode := paxFareNone
	if anyFare {
		mode = paxFareEstimate
	}
	b.WriteString(renderPaxByDestination(items, paxClassOrderStation, mode))
	return b.String()
}

// stationDestJumps returns the jump distance from the player's current system to
// each of the given destination system names, keyed by the name as supplied. It
// is the globals-backed wrapper around destSystemJumps: it pulls the origin from
// live game state and the systems/connections from the knowledge base, returning
// nil (no jumps rendered) whenever any of those is unavailable.
func stationDestJumps(destSystems []string) map[string]int {
	if globalClient == nil || globalKB == nil || len(destSystems) == 0 {
		return nil
	}
	state := globalClient.GetState()
	if state == nil || state.System.ID == "" {
		return nil
	}
	ctx := context.Background()
	systems, err := globalKB.GetSystems(ctx)
	if err != nil {
		return nil
	}
	conns, err := globalKB.GetConnections(ctx)
	if err != nil {
		return nil
	}
	return destSystemJumps(state.System.ID, systems, conns, destSystems)
}

// destSystemJumps computes the number of jumps from originID to each destination
// system named in destSystems, resolving names to ids via systems and walking the
// graph built from conns. The result is keyed by the destination system string
// exactly as it appeared in destSystems; destinations that cannot be resolved or
// reached are omitted (callers then render no jump count for them). It is the
// pure core of stationDestJumps, separated so it can be unit-tested without the
// globals.
func destSystemJumps(originID string, systems []knowledge.System, conns []knowledge.Connection, destSystems []string) map[string]int {
	if originID == "" || len(destSystems) == 0 {
		return nil
	}
	byID := make(map[string]string, len(systems))   // lowercased id -> canonical id
	byName := make(map[string]string, len(systems)) // lowercased name -> canonical id
	for _, s := range systems {
		byID[strings.ToLower(s.ID)] = s.ID
		if s.Name != "" {
			byName[strings.ToLower(s.Name)] = s.ID
		}
	}

	// Resolve each distinct destination to a canonical id, remembering the
	// original string so the returned map keys match what the caller passed.
	nameToID := make(map[string]string, len(destSystems))
	targets := make([]string, 0, len(destSystems))
	for _, name := range destSystems {
		if _, done := nameToID[name]; done {
			continue
		}
		if id, ok := resolveSystemToken(name, byID, byName); ok {
			nameToID[name] = id
			targets = append(targets, id)
		}
	}
	if len(targets) == 0 {
		return nil
	}

	graph := jumpGraphFromConnections(conns)
	dists := bfsJumps(graph, originID, targets)
	out := make(map[string]int, len(nameToID))
	for name, id := range nameToID {
		if d := dists[id]; d < routeInf {
			out[name] = d
		}
	}
	return out
}

// formatLoadPassenger renders a load_passenger action_result.
func formatLoadPassenger(raw []byte) string {
	raw = unwrapActionResult(raw)
	type loadedPax struct {
		Name            string `json:"name"`
		Class           string `json:"class"`
		Destination     string `json:"destination"`
		DestinationName string `json:"destination_name"`
		Fare            int    `json:"fare"`
		TicksRemaining  int    `json:"ticks_remaining"`
	}
	var resp struct {
		Message   string      `json:"message"`
		Loaded    []loadedPax `json:"loaded"`
		Count     int         `json:"count"`
		TotalFare int         `json:"total_fare"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}
	if len(resp.Loaded) == 0 && resp.Message == "" {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Loaded %d passenger(s) — %d cr total\n", len(resp.Loaded), resp.TotalFare)
	for _, p := range resp.Loaded {
		fmt.Fprintf(&b, "  %s [%s] → %s, %d cr (%dt)\n",
			p.Name, p.Class, paxDestLabel(p.DestinationName, p.Destination), p.Fare, p.TicksRemaining)
	}
	if resp.Message != "" {
		fmt.Fprintf(&b, "  %s\n", resp.Message)
	}
	return b.String()
}

// formatUnloadPassenger renders an unload_passenger action_result.
func formatUnloadPassenger(raw []byte) string {
	raw = unwrapActionResult(raw)
	var resp struct {
		Message   string `json:"message"`
		Name      string `json:"name"`
		Delivered bool   `json:"delivered"`
		FarePaid  int    `json:"fare_paid"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}
	if resp.Name == "" && resp.Message == "" {
		return ""
	}

	var b strings.Builder
	if resp.Name != "" {
		if resp.Delivered {
			fmt.Fprintf(&b, "Delivered %s — %d cr\n", resp.Name, resp.FarePaid)
		} else {
			fmt.Fprintf(&b, "%s disembarked — stranded (no fare)\n", resp.Name)
		}
	}
	if resp.Message != "" {
		fmt.Fprintf(&b, "  %s\n", resp.Message)
	}
	return b.String()
}
