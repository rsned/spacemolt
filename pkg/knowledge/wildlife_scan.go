package knowledge

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// CreatureScan is what a scan reply says about a creature, after unpacking the
// display string the server hides most of it in.
type CreatureScan struct {
	// Name is the display name ("Slag-Tortoise"), Role the bracketed role
	// ("grazer"), Traits the verbatim text after the em dash ("harmless prey,
	// ranchable stock").
	Name   string
	Role   string
	Traits string
	// Danger is the first trait, which is where the threat descriptor sits in
	// every sample measured so far. The rest of Traits is kept because the
	// vocabulary is unknown and a second trait already exists (ranchable stock).
	Danger string
	// Ranchable reports whether revealed_info listed "ranchable".
	Ranchable bool
	Revealed  []string
	Hull      int
}

// scanTraitSeparator is the EM DASH the server puts between role and traits.
// It is not a hyphen and not an en dash: "Slag-Tortoise [grazer — harmless
// prey]". Splitting on "-" would cut the species name in half.
const scanTraitSeparator = "—"

// ParseCreatureScan unpacks a creature scan's username field.
//
// scan does not return species, role or danger as fields — it packs them into
// the display name as "Name [role — trait, trait]" and reports which of them the
// scanner tier unlocked in revealed_info. Verified live on 2026-08-17 against six
// species; a reply without the bracket yields just the name, which is what a
// scan of a ship or an NPC looks like.
func ParseCreatureScan(username string, revealed []string, hull int) CreatureScan {
	out := CreatureScan{Hull: hull, Revealed: revealed}
	for _, r := range revealed {
		if strings.EqualFold(strings.TrimSpace(r), "ranchable") {
			out.Ranchable = true
		}
	}

	name, rest, found := strings.Cut(username, "[")
	out.Name = strings.TrimSpace(name)
	if !found {
		return out
	}
	rest = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(rest), "]"))

	role, traits, hasTraits := strings.Cut(rest, scanTraitSeparator)
	out.Role = strings.TrimSpace(role)
	if !hasTraits {
		// A bracket with no separator is all role, no traits — recorded as such
		// rather than guessed at.
		return out
	}
	out.Traits = strings.TrimSpace(traits)
	if first, _, ok := strings.Cut(out.Traits, ","); ok {
		out.Danger = strings.TrimSpace(first)
	} else {
		out.Danger = out.Traits
	}

	return out
}

// CaptureWildlifeScan records what a scan revealed about a species.
//
// species must be supplied by the caller, normally from the get_nearby entry for
// the creature that was scanned: the reply carries a display name ("Slag-
// Tortoise") but never the species id, and slugging the name would invent a
// second phantom species the first time the server changed its capitalisation.
//
// This is the only field in the guide that costs a tick — scan is a mutation —
// which is why danger is stored per species rather than per creature.
func CaptureWildlifeScan(ctx context.Context, kb Base, species string, s CreatureScan, now time.Time) error {
	rec := wildlifeRecorder(kb)
	if rec == nil || species == "" {
		return nil
	}
	// A scan that revealed nothing about traits leaves danger alone rather than
	// stamping danger_scanned_utc, so the species stays on the scan work list.
	if s.Danger == "" && s.Traits == "" {
		return nil
	}

	stamp := now.UTC().Format(time.RFC3339)
	if err := rec.UpsertWildlifeSpecies(ctx, []WildlifeSpecies{{
		Species:          species,
		Name:             s.Name,
		Role:             s.Role,
		MaxHull:          s.Hull,
		Danger:           s.Danger,
		DangerScannedUTC: stamp,
		ScanTraits:       s.Traits,
		ScanRevealed:     strings.Join(s.Revealed, ","),
		Ranchable:        s.Ranchable,
		LastSeenUTC:      stamp,
	}}); err != nil {
		return fmt.Errorf("capture wildlife scan %s: %w", species, err)
	}

	return nil
}
