package knowledge

import (
	"context"
	"fmt"
	"slices"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// WildlifeRecorder is the subset of the KB that stores wildlife observations.
// SQLiteKB implements it; the in-memory KB and mocks do not, which is why every
// capture entry point takes a Base and degrades to a no-op when the concrete
// store is something else.
type WildlifeRecorder interface {
	UpsertWildlifeSpecies(ctx context.Context, rows []WildlifeSpecies) error
	RecordWildlifeSightings(ctx context.Context, rows []WildlifeSighting) error
	RecordWildlifeKill(ctx context.Context, k WildlifeKill) error
}

// wildlifeRecorder narrows a Base to a WildlifeRecorder, returning nil when the
// KB cannot store wildlife (nil, in-memory, or a mock). Callers treat nil as
// "nothing to do" rather than an error: capture is opportunistic, and a session
// running without a SQLite KB must still be able to hunt.
func wildlifeRecorder(kb Base) WildlifeRecorder {
	if kb == nil {
		return nil
	}
	rec, ok := kb.(WildlifeRecorder)
	if !ok {
		return nil
	}
	return rec
}

// WildlifeFromNearby folds a get_nearby creature list into one species row and
// one sighting row per distinct species.
//
// Individuals are deliberately not stored. A crt_ id lives only until something
// eats it and the herd respawns with fresh ids, so a row per individual would
// grow without bound while answering no question a per-species count cannot.
// What the individuals contribute is the aggregate: how many were present, how
// many were branded, how many were already fighting, and the max_hull that is a
// species constant.
func WildlifeFromNearby(creatures []serverapi.NearbyCreature, systemID, poiID, poiType, agentID string, tick int64) ([]WildlifeSpecies, []WildlifeSighting) {
	if len(creatures) == 0 {
		return nil, nil
	}

	type agg struct {
		name     string
		role     string
		maxHull  int
		count    int
		branded  int
		inCombat int
	}
	bySpecies := make(map[string]*agg, len(creatures))
	order := make([]string, 0, len(creatures))

	for _, c := range creatures {
		// Species is the join key everywhere. A creature that arrives without
		// one cannot be filed, and inventing a key from the display name would
		// produce a second, permanent phantom species the moment the server
		// changed capitalisation.
		if c.Species == "" {
			continue
		}
		a, ok := bySpecies[c.Species]
		if !ok {
			a = &agg{}
			bySpecies[c.Species] = a
			order = append(order, c.Species)
		}
		if c.Name != "" {
			a.name = c.Name
		}
		if c.Role != "" {
			a.role = c.Role
		}
		a.maxHull = max(a.maxHull, c.MaxHull)
		a.count++
		if c.Branded {
			a.branded++
		}
		if c.InCombat {
			a.inCombat++
		}
	}

	species := make([]WildlifeSpecies, 0, len(order))
	sightings := make([]WildlifeSighting, 0, len(order))
	for _, key := range order {
		a := bySpecies[key]
		var habitats []string
		if poiType != "" {
			habitats = []string{poiType}
		}
		species = append(species, WildlifeSpecies{
			Species:  key,
			Name:     a.name,
			Role:     a.role,
			MaxHull:  a.maxHull,
			Habitats: habitats,
		})
		sightings = append(sightings, WildlifeSighting{
			Species:       key,
			SystemID:      systemID,
			POIID:         poiID,
			Source:        WildlifeSourceNearby,
			ObservedCount: a.count,
			Branded:       a.branded,
			InCombat:      a.inCombat,
			GameTick:      tick,
			AgentID:       agentID,
		})
	}
	return species, sightings
}

// WildlifeFromSurvey folds a survey_system census into species and sighting
// rows. The census is system-wide, so no sighting gets a POI id — knowing a
// system holds 40 Belt-Grazers does not say which belt they are on.
//
// The bloom fields are copied onto every row of the survey rather than kept
// once: bloom is a property of the system at that moment, and denormalising it
// here is what lets a single query correlate a species' count against the bloom
// that drove it.
func WildlifeFromSurvey(resp serverapi.SurveySystemResponse, agentID string, tick int64) ([]WildlifeSpecies, []WildlifeSighting) {
	if len(resp.Wildlife) == 0 {
		return nil, nil
	}
	species := make([]WildlifeSpecies, 0, len(resp.Wildlife))
	sightings := make([]WildlifeSighting, 0, len(resp.Wildlife))
	for _, w := range resp.Wildlife {
		if w.Species == "" {
			continue
		}
		species = append(species, WildlifeSpecies{
			Species: w.Species,
			Name:    w.Name,
			Role:    w.Role,
			// A census reports no hull; leaving MaxHull zero lets the merge
			// keep whatever get_nearby already established.
		})
		sightings = append(sightings, WildlifeSighting{
			Species:        w.Species,
			SystemID:       resp.SystemID,
			Source:         WildlifeSourceSurvey,
			ObservedCount:  w.Estimate,
			Abundance:      w.Abundance,
			Ranched:        w.Ranched,
			BloomStatus:    resp.BloomStatus,
			BloomIntensity: resp.BloomIntensity,
			GameTick:       tick,
			AgentID:        agentID,
		})
	}
	return species, sightings
}

// CaptureWildlifeNearby stores the field-guide and census rows implied by a
// get_nearby reply. A KB that cannot hold wildlife is not an error.
func CaptureWildlifeNearby(ctx context.Context, kb Base, creatures []serverapi.NearbyCreature, systemID, poiID, poiType, agentID string, tick int64) (int, error) {
	rec := wildlifeRecorder(kb)
	if rec == nil {
		return 0, nil
	}
	species, sightings := WildlifeFromNearby(creatures, systemID, poiID, poiType, agentID, tick)
	if len(sightings) == 0 {
		return 0, nil
	}
	if err := rec.UpsertWildlifeSpecies(ctx, species); err != nil {
		return 0, fmt.Errorf("capture wildlife species: %w", err)
	}
	if err := rec.RecordWildlifeSightings(ctx, sightings); err != nil {
		return 0, fmt.Errorf("capture wildlife sightings: %w", err)
	}
	return len(sightings), nil
}

// CaptureWildlifeSurvey stores the census carried by a survey_system reply.
func CaptureWildlifeSurvey(ctx context.Context, kb Base, resp serverapi.SurveySystemResponse, agentID string, tick int64) (int, error) {
	rec := wildlifeRecorder(kb)
	if rec == nil {
		return 0, nil
	}
	species, sightings := WildlifeFromSurvey(resp, agentID, tick)
	if len(sightings) == 0 {
		return 0, nil
	}
	if err := rec.UpsertWildlifeSpecies(ctx, species); err != nil {
		return 0, fmt.Errorf("capture survey wildlife species: %w", err)
	}
	if err := rec.RecordWildlifeSightings(ctx, sightings); err != nil {
		return 0, fmt.Errorf("capture survey wildlife sightings: %w", err)
	}
	return len(sightings), nil
}

// CaptureWildlifeCarcass records a killed creature and the contents of its
// carcass, given the get_wrecks entry for that carcass.
//
// wreck.Cargo is the drop roll as the server rolled it, which is why this takes
// the wreck rather than a loot result: what an agent carries away is capped by
// its remaining hold and then by salvage, so looted quantities would make the
// drop table a function of cargo space. wreck may be nil for a kill whose
// carcass was never found, which records the kill with CarcassRead false so it
// stays out of the drop denominator.
//
// species/role/maxHull come from the get_nearby entry for the creature that was
// engaged: a creature wreck carries victim_name but an EMPTY ship_class, so the
// carcass alone cannot name the species.
func CaptureWildlifeCarcass(ctx context.Context, kb Base, wreck *serverapi.Wreck, creature serverapi.NearbyCreature, systemID, battleID, agentID string, tick int64, durationTicks, damageDealt, damageTaken int) error {
	rec := wildlifeRecorder(kb)
	if rec == nil || creature.CreatureID == "" {
		return nil
	}

	k := WildlifeKill{
		CreatureID:    creature.CreatureID,
		GameTick:      tick,
		Species:       creature.Species,
		CreatureName:  creature.Name,
		Role:          creature.Role,
		MaxHull:       creature.MaxHull,
		SystemID:      systemID,
		BattleID:      battleID,
		DurationTicks: durationTicks,
		DamageDealt:   damageDealt,
		DamageTaken:   damageTaken,
		AgentID:       agentID,
	}
	if wreck != nil {
		k.WreckID = wreck.ID
		k.SalvageValue = wreck.SalvageValue
		k.POIID = wreck.POIID
		if wreck.SystemID != "" {
			k.SystemID = wreck.SystemID
		}
		// Reading the carcass is what makes this kill count in the drop
		// denominator — including when it turns out to be empty.
		k.CarcassRead = true
		for _, c := range wreck.Cargo {
			if c.ItemID == "" {
				continue
			}
			k.Drops = append(k.Drops, WildlifeDrop{ItemID: c.ItemID, Quantity: c.Quantity})
		}
		slices.SortFunc(k.Drops, func(a, b WildlifeDrop) int {
			switch {
			case a.ItemID < b.ItemID:
				return -1
			case a.ItemID > b.ItemID:
				return 1
			default:
				return 0
			}
		})
	}

	if err := rec.RecordWildlifeKill(ctx, k); err != nil {
		return fmt.Errorf("capture wildlife carcass: %w", err)
	}
	// The kill is also a sighting of the species at that place, and the only
	// one that will ever be recorded for a creature killed on sight.
	if creature.Species != "" {
		if err := rec.UpsertWildlifeSpecies(ctx, []WildlifeSpecies{{
			Species: creature.Species,
			Name:    creature.Name,
			Role:    creature.Role,
			MaxHull: creature.MaxHull,
		}}); err != nil {
			return fmt.Errorf("capture wildlife carcass species: %w", err)
		}
	}
	return nil
}
