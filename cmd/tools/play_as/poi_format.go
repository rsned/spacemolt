package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// formatPOI renders a get_poi reply. It was the only query of its family with
// no formatter, so the prompt printed raw JSON while get_nearby and
// get_location rendered tables.
//
// The order is deliberate: identity first, then the three things that decide
// what to do here -- what can be mined and whether the field is worked out,
// what services the base offers, and the all-in fuel price. Wormhole expiry is
// last but never omitted: it is the field whose absence strands a ship on the
// far side.
func formatPOI(raw []byte) string {
	var resp serverapi.GetPOIResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}

	// The transit variant describes a journey and carries no POI. Kind alone is
	// not enough -- a reply predating the v0.531.4 discriminator sets only the
	// flag -- so both are checked.
	if resp.InTransit || resp.Kind == "transit" {
		var t strings.Builder
		t.WriteString("\nIn transit — no POI at this position yet.\n")
		if resp.FromSystem != "" || resp.ToSystem != "" {
			kind := resp.TransitType
			if kind == "" {
				kind = "transit"
			}
			fmt.Fprintf(&t, "  %s: %s → %s\n", kind, resp.FromSystem, resp.ToSystem)
		}
		if resp.TicksRemaining > 0 {
			fmt.Fprintf(&t, "  Arrives in %d tick(s)\n", resp.TicksRemaining)
		}
		if resp.Message != "" {
			fmt.Fprintf(&t, "  %s\n", resp.Message)
		}

		return t.String()
	}

	p := resp.POI
	var b strings.Builder
	name := p.Name
	if name == "" {
		name = "(unnamed)"
	}
	fmt.Fprintf(&b, "\n%s", name)
	if p.Type != "" {
		fmt.Fprintf(&b, "  [%s]", p.Type)
	}
	b.WriteString("\n")
	if p.ID != "" {
		fmt.Fprintf(&b, "  ID:       %s", p.ID)
		if p.SystemID != "" {
			fmt.Fprintf(&b, "   system: %s", p.SystemID)
		}
		b.WriteString("\n")
	}
	if p.Class != "" {
		fmt.Fprintf(&b, "  Class:    %s\n", p.Class)
	}
	if p.HasBase || p.BaseName != "" {
		fmt.Fprintf(&b, "  Base:     %s", p.BaseName)
		if p.BaseID != "" {
			fmt.Fprintf(&b, " (%s)", p.BaseID)
		}
		b.WriteString("\n")
	}
	// v0.623 gave every system and location a written description, and v0.624
	// started returning it here.
	if p.Description != "" {
		fmt.Fprintf(&b, "  %s\n", p.Description)
	}
	if p.Hidden {
		fmt.Fprintf(&b, "  ⚑ HIDDEN POI (reveal difficulty %d)\n", p.RevealDifficulty)
	}

	// Richness ranks a field and arrives on the server's own 2-90 scale, so it
	// is printed raw exactly as explore.go does; remaining_display is the word for
	// whether it is worked out. remaining=0 is NOT depletion -- ore regenerates
	// -- so the display string is shown rather than a computed verdict.
	if len(resp.Resources) > 0 {
		b.WriteString("\n  Resources:\n")
		fmt.Fprintf(&b, "    %-20s %9s  %s\n", "RESOURCE", "RICHNESS", "REMAINING")
		for _, r := range resp.Resources {
			nm := r.Name
			if nm == "" {
				nm = r.ResourceID
			}
			fmt.Fprintf(&b, "    %-20s %9.0f  %s\n", nm, r.Richness, r.RemainingDisplay)
		}
	}

	if len(resp.Services) > 0 {
		fmt.Fprintf(&b, "\n  Services: %s\n", strings.Join(resp.Services, ", "))
	}

	if resp.FuelPriceAllIn > 0 || resp.FuelPrice > 0 {
		fmt.Fprintf(&b, "  Fuel:     %d/unit all-in", resp.FuelPriceAllIn)
		if resp.FuelTaxPerUnit > 0 {
			fmt.Fprintf(&b, " (%d base + %d tax)", resp.FuelPrice, resp.FuelTaxPerUnit)
		}
		b.WriteString("\n")
	}
	if resp.FactionFuelCapacity > 0 {
		fmt.Fprintf(&b, "  Bunker:   %d/%d faction fuel\n", resp.FactionFuelReserve, resp.FactionFuelCapacity)
	}

	if resp.PoliceDrones > 0 {
		fmt.Fprintf(&b, "  Police:   %d drone(s)\n", resp.PoliceDrones)
	}
	if resp.PoliceWarning != "" {
		fmt.Fprintf(&b, "  ⚠ %s\n", resp.PoliceWarning)
	}
	if resp.ActiveBattle != nil {
		b.WriteString("  ⚔ active battle here\n")
	}

	// Expiry decides whether a wormhole is worth routing through or about to
	// strand the ship on the far side. It is a STRING in the spec, not a tick
	// count.
	if resp.WormholeDestination != "" || resp.WormholeDestinationID != "" {
		dest := resp.WormholeDestination
		if dest == "" {
			dest = resp.WormholeDestinationID
		}
		fmt.Fprintf(&b, "\n  Wormhole → %s", dest)
		if resp.WormholeDestinationID != "" && resp.WormholeDestination != "" {
			fmt.Fprintf(&b, " (%s)", resp.WormholeDestinationID)
		}
		b.WriteString("\n")
		if resp.WormholeExpiresIn != "" {
			fmt.Fprintf(&b, "  Expires:  %s\n", resp.WormholeExpiresIn)
		} else {
			b.WriteString("  Expires:  not reported\n")
		}
	}

	return b.String()
}
