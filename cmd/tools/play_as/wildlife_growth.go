package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// globalWildlifeGrowthDir is the base directory for the TEMPORARY per-creature
// observation dump. Empty disables collection. See appendCreatureObservations
// for why this is a file rather than a KB table.
var globalWildlifeGrowthDir = "data/wildlife-growth"

// creatureObservation is one creature as seen in one get_nearby reply.
//
// It is a flat record on purpose: this is a raw collection feeding a schema
// decision, so it keeps every per-individual field the payload offers rather
// than the subset today's questions need.
type creatureObservation struct {
	ObservedUTC string `json:"observed_utc"`
	GameTick    int64  `json:"game_tick"`
	AgentID     string `json:"agent_id"`
	SystemID    string `json:"system_id"`
	POIID       string `json:"poi_id"`
	CreatureID  string `json:"creature_id"`
	Species     string `json:"species"`
	Name        string `json:"name"`
	Role        string `json:"role"`
	Hull        int    `json:"hull"`
	MaxHull     int    `json:"max_hull"`
	InCombat    bool   `json:"in_combat,omitempty"`
	Branded     bool   `json:"branded,omitempty"`
}

// appendCreatureObservations dumps one JSONL line per creature in a get_nearby
// reply, so per-individual hull can be tracked over time.
//
// WHY THIS IS A FILE AND NOT A KB TABLE -- it is temporary scaffolding.
// Creature hull stopped being a species constant on 2026-10-14: three Young
// Cloudwhales read 1368/1433/1408 at one POI and are expected to climb as they
// mature. wildlife_species holds ONE max_hull per species (ratcheted up, so it
// converges on the largest individual ever seen) and wildlife_sightings records
// neither hull nor creature_id, so the per-individual numbers are discarded
// today. The right permanent shape depends on facts nobody has yet -- what the
// growth stages are, whether the name prefix is the only marker, whether hull
// rises smoothly or steps -- and a KB migration cannot be edited once applied.
// So: collect raw, learn the shape, then design the table.
//
// Append-only and keyed by nothing: duplicate observations of the same creature
// in the same tick are kept, because the explore loop deliberately looks twice
// per POI and the duplicate is itself evidence about sampling.
//
// An empty dir disables collection entirely.
func appendCreatureObservations(dir string, creatures []serverapi.NearbyCreature, systemID, poiID, agentID string, tick int64) error {
	if dir == "" || len(creatures) == 0 {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("wildlife growth dir: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)

	f, err := os.OpenFile(filepath.Join(dir, "observations.jsonl"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open wildlife growth log: %w", err)
	}
	defer f.Close() //nolint:errcheck

	enc := json.NewEncoder(f)
	for _, c := range creatures {
		if err := enc.Encode(creatureObservation{
			ObservedUTC: now,
			GameTick:    tick,
			AgentID:     agentID,
			SystemID:    systemID,
			POIID:       poiID,
			CreatureID:  c.CreatureID,
			Species:     c.Species,
			Name:        c.Name,
			Role:        c.Role,
			Hull:        c.Hull,
			MaxHull:     c.MaxHull,
			InCombat:    c.InCombat,
			Branded:     c.Branded,
		}); err != nil {
			return fmt.Errorf("write creature observation: %w", err)
		}
	}
	return nil
}
