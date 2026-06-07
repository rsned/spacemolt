package main

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// paxItem is the common shape used to render passengers grouped by destination
// then class, for both list_passengers (aboard) and list_station_passengers
// (waiting). Fare/Ticks are only meaningful (and shown) for aboard passengers.
type paxItem struct {
	Name        string
	Class       string
	Citizenship string
	CitizenID   string
	DestName    string
	DestID      string
	Fare        int
	Ticks       int
}

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
	name  int
	cit   int // citizenship text width (without brackets); 0 collapses the column
	citID int
	fare  int
	ticks int
}

// renderPaxByDestination renders passengers grouped by destination (alphabetical
// by name) and, within each destination, by class in classOrder. Passengers are
// listed by name. When showFare is true each line also shows fare and ticks.
func renderPaxByDestination(items []paxItem, classOrder []string, showFare bool) string {
	// Group by destination id (canonical/unique); track the display name per id.
	groups := map[string][]paxItem{}
	destName := map[string]string{}
	for _, p := range items {
		groups[p.DestID] = append(groups[p.DestID], p)
		if _, ok := destName[p.DestID]; !ok {
			destName[p.DestID] = p.DestName
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

	// Global column widths so columns line up across all groups.
	var cols paxCols
	for _, p := range items {
		cols.name = max(cols.name, len(p.Name))
		cols.cit = max(cols.cit, len(p.Citizenship))
		cols.citID = max(cols.citID, len(p.CitizenID))
		cols.fare = max(cols.fare, len(strconv.Itoa(p.Fare)))
		cols.ticks = max(cols.ticks, len(strconv.Itoa(p.Ticks)))
	}

	var b strings.Builder
	for _, d := range dests {
		grp := groups[d]
		fmt.Fprintf(&b, "\n%s (%d)\n", paxDestLabel(destName[d], d), len(grp))

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
				renderPaxLine(&b, p, cols, showFare)
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
//	    <name>  [<citizenship>]  <citizen_id>   <fare> cr  <ticks>t
//
// The citizenship column is omitted entirely when no passenger has one (the
// aboard manifest carries no citizenship). Fare/ticks are right-aligned and
// only shown when showFare is true.
func renderPaxLine(b *strings.Builder, p paxItem, cols paxCols, showFare bool) {
	fmt.Fprintf(b, "    %-*s", cols.name, p.Name)
	if cols.cit > 0 {
		cit := ""
		if p.Citizenship != "" {
			cit = "[" + p.Citizenship + "]"
		}
		// cols.cit is the citizenship text width; +2 accounts for the brackets.
		fmt.Fprintf(b, "  %-*s", cols.cit+2, cit)
	}
	if showFare {
		// citizen_id is followed by the fare/ticks columns, so pad it; then
		// append the right-aligned fare/ticks.
		fmt.Fprintf(b, "  %-*s", cols.citID, p.CitizenID)
		fmt.Fprintf(b, "   %*s cr  %*st", cols.fare, strconv.Itoa(p.Fare), cols.ticks, strconv.Itoa(p.Ticks))
	} else {
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
		}
	}
	b.WriteString(renderPaxByDestination(items, paxClassOrderAboard, true))
	return b.String()
}

// formatStationPassengers renders a list_station_passengers response: passengers
// waiting at a station, grouped by destination (alphabetical) then class.
func formatStationPassengers(raw []byte) string {
	raw = unwrapActionResult(raw)
	type waitingPax struct {
		CitizenID       string `json:"citizen_id"`
		Name            string `json:"name"`
		Citizenship     string `json:"citizenship"`
		Class           string `json:"class"`
		Destination     string `json:"destination"`
		DestinationName string `json:"destination_name"`
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

	items := make([]paxItem, len(resp.Waiting))
	for i, w := range resp.Waiting {
		items[i] = paxItem{
			Name: w.Name, Class: w.Class, Citizenship: w.Citizenship,
			CitizenID: w.CitizenID, DestName: w.DestinationName, DestID: w.Destination,
		}
	}
	b.WriteString(renderPaxByDestination(items, paxClassOrderStation, false))
	return b.String()
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
