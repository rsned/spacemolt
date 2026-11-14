package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/mattn/go-isatty"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/worker"
)

// Environmental hazards in play_as. craftsman-1 lost its Concordance at Crush
// Vector Star (radiation 3) on 2026-11-14: the KB knew the hazard, travel said
// nothing, and the one warning that reached the screen came at 25% hull, 40 s
// before death. play_as now warns before a move, on arrival and on every dose,
// and on the server's hazard_warning push leaves the POI on its own.
// Operator: short visits stay allowed (missions, wreck recovery), so nothing
// is refused; the push is the line.

// hazardEscapeOn gates the automatic escape on a hazard_warning push
// (`set_hazard_escape on|off`); on by default.
var hazardEscapeOn atomic.Bool

// hazardEscaping is set while an escape is in flight so a burst of doses
// starts one escape, not one per push.
var hazardEscaping atomic.Bool

func init() { hazardEscapeOn.Store(true) }

func stdoutStyled() bool { return isatty.IsTerminal(os.Stdout.Fd()) }

// hazardBanner is the warning for a POI's hazard: what it is, how hard it
// hits, and for a steady hazard how many ticks the hull (and shield) last.
// Empty when there is no hazard.
func hazardBanner(h *serverapi.POIHazard, where string, hull, maxHull, shield float64, styled bool) string {
	if h == nil || h.Type == "" {
		return ""
	}
	tag := game.AlertTag("HAZARD", styled)
	line := fmt.Sprintf("%s☢️  %s: %s severity %d", tag, where, h.Type, h.Severity)
	pct := h.DosePct()
	switch {
	case h.Steady() && pct > 0 && maxHull > 0:
		dose := float64(pct) / 100 * maxHull
		ticks := int(math.Ceil((hull + shield) / dose))
		line += fmt.Sprintf(" — %d%% of max hull every tick undocked; ~%d tick(s) (~%ds) to hull 0", pct, ticks, ticks*10)
		if h.DosesOnMining() {
			line += ", and on each mine"
		}
	case h.DosesOnMining() && pct > 0:
		line += fmt.Sprintf(" — %d%% of max hull on each mine", pct)
	default:
		line += " — lowers scan power; no hull damage"
	}
	return line
}

// lookupPOIHazard finds ref (id or name) in the current system and returns
// its hazard: from the live system state first, else the knowledge base.
func lookupPOIHazard(kb knowledge.Base, st *game.State, ref string) (*serverapi.POIHazard, string) {
	if st == nil || ref == "" {
		return nil, ""
	}
	for i := range st.System.POIs {
		p := &st.System.POIs[i]
		if p.ID == ref || strings.EqualFold(p.Name, ref) {
			if p.Hazard != nil && p.Hazard.Type != "" {
				return p.Hazard, nonEmptyStr(p.Name, p.ID)
			}
			if kb == nil {
				return nil, ""
			}
			break
		}
	}
	if kb == nil || st.System.ID == "" {
		return nil, ""
	}
	pois, err := kb.GetPOIs(context.Background(), st.System.ID)
	if err != nil {
		return nil, ""
	}
	for _, p := range pois {
		if (p.ID == ref || strings.EqualFold(p.Name, ref)) && p.HazardType != "" && p.HazardSeverity > 0 {
			return &serverapi.POIHazard{Type: p.HazardType, Severity: p.HazardSeverity}, nonEmptyStr(p.Name, p.ID)
		}
	}
	return nil, ""
}

func nonEmptyStr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// arrivalHazard reads poi_hazard from a travel/jump arrival reply.
func arrivalHazard(raw []byte) (*serverapi.POIHazard, string) {
	h, name, _ := arrivalHazardAt(raw)
	return h, name
}

// arrivalHazardAt is arrivalHazard plus the reply's poi_id.
func arrivalHazardAt(raw []byte) (*serverapi.POIHazard, string, string) {
	var r struct {
		POI       string               `json:"poi"`
		POIID     string               `json:"poi_id"`
		POIHazard *serverapi.POIHazard `json:"poi_hazard"`
	}
	if json.Unmarshal(raw, &r) != nil || r.POIHazard == nil || r.POIHazard.Type == "" {
		return nil, "", ""
	}
	return r.POIHazard, nonEmptyStr(r.POI, r.POIID), r.POIID
}

// warnHazardAhead prints the banner for a POI the ship is about to visit.
func warnHazardAhead(client game.GameClient, target string) {
	st := client.GetState()
	h, name := lookupPOIHazard(globalKB, st, target)
	if h == nil {
		return
	}
	fmt.Println(hazardBanner(h, name, st.Hull, st.MaxHull, st.Ship.Shield, stdoutStyled()))
	if h.Steady() {
		fmt.Println("   Short visits are fine; play_as leaves on its own at the first dose warning (set_hazard_escape off to stay).")
	}
}

// warnHazardArrived prints the banner when the arrival reply stored under one
// of rawKeys carries poi_hazard for the POI the ship now sits at.
func warnHazardArrived(client game.GameClient, rawKeys ...string) {
	st := client.GetState()
	if st == nil {
		return
	}
	for _, key := range rawKeys {
		h, name, id := arrivalHazardAt(client.GetRawJSON(key))
		if h == nil || (id != "" && st.CurrentPOI != "" && id != st.CurrentPOI) {
			continue
		}
		fmt.Println(hazardBanner(h, "ARRIVED at "+name, st.Hull, st.MaxHull, st.Ship.Shield, stdoutStyled()))
		return
	}
}

// printRouteHazards flags the route systems that hold known hazard POIs.
func printRouteHazards(route []game.RouteStep) {
	ids := make([]string, 0, len(route))
	for _, s := range route {
		ids = append(ids, s.SystemID)
	}
	for _, line := range routeHazardLines(globalKB, ids, stdoutStyled()) {
		fmt.Println(line)
	}
}

// hazardAutoEscape moves the ship off the POI that just dosed it. It runs on
// the push goroutine's behalf in its own goroutine; one at a time.
func hazardAutoEscape(ctx context.Context, client game.GameClient) {
	if !hazardEscapeOn.Load() || !hazardEscaping.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer hazardEscaping.Store(false)
		st := client.GetState()
		if st == nil || st.Doc || st.InBattle {
			return
		}
		tag := game.AlertTag("HAZARD", stdoutStyled())
		plan := worker.HazardRefuge(st)
		if plan == nil {
			fmt.Printf("\r%s☢️  AUTO-ESCAPE: no hazard-free POI known in %s — MOVE NOW (jump out or travel)\n", tag, st.System.Name)
			return
		}
		for _, step := range plan {
			fmt.Printf("\r%s☢️  AUTO-ESCAPE: %s\n", tag, strings.Join(step, " "))
			var err error
			switch step[0] {
			case "travel":
				_, err = client.Travel(ctx, step[1])
			case "dock":
				err = client.Dock(ctx)
			}
			if err != nil {
				fmt.Printf("\r%s☢️  AUTO-ESCAPE: %s failed: %v — MOVE NOW\n", tag, strings.Join(step, " "), err)
				return
			}
		}
		fmt.Printf("\r%s☢️  AUTO-ESCAPE: clear of the hazard\n", tag)
	}()
}

// routeHazardLines lists each route system holding a known hazard POI, worst
// first within the system. A jump's landing POI is not known in advance, so
// any hazard in a system on the route is worth a look.
func routeHazardLines(kb knowledge.Base, systemIDs []string, styled bool) []string {
	if kb == nil {
		return nil
	}
	var out []string
	for _, sys := range systemIDs {
		pois, err := kb.GetPOIs(context.Background(), sys)
		if err != nil {
			continue
		}
		var hits []knowledge.POI
		for _, p := range pois {
			if p.HazardType != "" && p.HazardSeverity > 0 {
				hits = append(hits, p)
			}
		}
		if len(hits) == 0 {
			continue
		}
		slices.SortStableFunc(hits, func(a, b knowledge.POI) int { return b.HazardSeverity - a.HazardSeverity })
		parts := make([]string, 0, len(hits))
		for _, p := range hits {
			parts = append(parts, fmt.Sprintf("%s %s %d", nonEmptyStr(p.Name, p.ID), p.HazardType, p.HazardSeverity))
		}
		out = append(out, fmt.Sprintf("%s☢️  route system %s: %s", game.AlertTag("HAZARD", styled), sys, strings.Join(parts, "; ")))
	}
	return out
}
