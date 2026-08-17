package battlereplay

import (
	"context"
	"fmt"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// AttackRecorder is the subset of the KB that stores creature offence.
type AttackRecorder interface {
	UpsertWildlifeAttacks(ctx context.Context, rows []knowledge.WildlifeAttack) error
	SpeciesByDisplayName(ctx context.Context) (map[string]string, error)
}

// attackRecorder narrows a Base, returning nil when the KB cannot store attacks
// (nil, in-memory, or a mock). As everywhere else in wildlife capture, that is
// "nothing to do" rather than an error: a session without a SQLite KB must
// still be able to fight.
func attackRecorder(kb knowledge.Base) AttackRecorder {
	if kb == nil {
		return nil
	}
	rec, ok := kb.(AttackRecorder)
	if !ok {
		return nil
	}

	return rec
}

// CaptureWildlifeAttacks reads a finished battle and records what every
// creature in it was shooting with.
//
// This is the only source for a species' offence, and the reason the table
// exists: get_nearby gives hull and role, scan gives a danger phrase, and
// neither says what a Rainbow Leviathan actually hits for or with what damage
// type. A resistance fit can only be chosen against a damage type.
//
// speciesByCreatureID is the caller's own mapping, normally from the get_nearby
// entry for the quarry it engaged. Creatures missing from it are resolved
// against the names already in wildlife_species, so a battle nobody chose — an
// ambush, a death — still yields data. Whatever cannot be resolved either way
// is skipped rather than filed under a guessed species.
//
// Returns the number of attack rows written.
func CaptureWildlifeAttacks(ctx context.Context, kb knowledge.Base, client game.GameClient, battleID string, speciesByCreatureID map[string]string, logf Logf) (int, error) {
	rec := attackRecorder(kb)
	if rec == nil || client == nil || battleID == "" {
		return 0, nil
	}

	m, err := FetchModel(ctx, client, battleID, logf)
	if err != nil {
		return 0, fmt.Errorf("capture wildlife attacks %s: %w", battleID, err)
	}

	byID, err := ResolveCreatureSpecies(ctx, rec, m, speciesByCreatureID)
	if err != nil {
		return 0, err
	}
	if len(byID) == 0 {
		// No creature in this battle could be named. That is the normal result
		// for a player-versus-player or player-versus-pirate fight, not a
		// failure.
		return 0, nil
	}

	attacks := WildlifeAttacks(m, byID)
	if len(attacks) == 0 {
		return 0, nil
	}
	if err := rec.UpsertWildlifeAttacks(ctx, attacks); err != nil {
		return 0, fmt.Errorf("record wildlife attacks %s: %w", battleID, err)
	}

	return len(attacks), nil
}

// ResolveCreatureSpecies decides, for every creature participant in a battle,
// which species it was.
//
// known wins where it has an entry, because it came from a get_nearby that
// typed the creature directly. The rest fall back to the display-name index,
// which only ever resolves to a species the guide already holds.
func ResolveCreatureSpecies(ctx context.Context, rec AttackRecorder, m *ReplayModel, known map[string]string) (map[string]string, error) {
	if m == nil {
		return nil, nil
	}

	out := make(map[string]string, len(m.Participants))
	var unresolved []Participant
	for _, p := range m.Participants {
		if p.Kind != "creature" {
			continue
		}
		if s := known[p.PlayerID]; s != "" {
			out[p.PlayerID] = s

			continue
		}
		unresolved = append(unresolved, p)
	}
	if len(unresolved) == 0 {
		return out, nil
	}

	byName, err := rec.SpeciesByDisplayName(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve creature species: %w", err)
	}
	for _, p := range unresolved {
		if s := byName[knowledge.NormalizeCreatureName(p.Username)]; s != "" {
			out[p.PlayerID] = s
		}
	}

	return out, nil
}
