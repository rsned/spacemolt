package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// backfillImportBatch bounds one transaction's worth of rows. The dump is
// append-only and already tens of thousands of lines, so it is imported in
// batches rather than one statement per row or one transaction for the lot.
const backfillImportBatch = 500

// backfillCreatureObservations imports data/wildlife-growth/observations.jsonl
// into wildlife_observations, returning the rows parsed and the lines skipped.
//
// It exists because the JSONL predates the table (migration 62) and holds the
// only lifecycle data we have -- the calf/young/adult cloudwhale ladder and the
// one mid-combat hull reading among them. Starting the table empty would
// discard it.
//
// The import is an UPSERT keyed (creature_id, game_tick), so re-running it is
// idempotent and it can be run again after the dump grows. A malformed line is
// counted and skipped rather than aborting: the dump is written by an append
// that can be interrupted, so a truncated final line is expected, not
// exceptional.
func backfillCreatureObservations(ctx context.Context, kb knowledge.Base, dir string) (imported, skipped int, err error) {
	if kb == nil {
		return 0, 0, fmt.Errorf("backfill: no knowledge base configured (use --db-path)")
	}
	rec, ok := kb.(wildlifeObservationRecorder)
	if !ok {
		return 0, 0, fmt.Errorf("backfill: knowledge base does not record wildlife observations")
	}

	path := filepath.Join(dir, "observations.jsonl")
	f, openErr := os.Open(path)
	if openErr != nil {
		return 0, 0, fmt.Errorf("backfill: %w", openErr)
	}
	defer f.Close() //nolint:errcheck

	flush := func(batch []knowledge.WildlifeObservation) error {
		if len(batch) == 0 {
			return nil
		}

		return rec.RecordWildlifeObservations(ctx, batch)
	}

	batch := make([]knowledge.WildlifeObservation, 0, backfillImportBatch)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var o creatureObservation
		if err := json.Unmarshal([]byte(line), &o); err != nil {
			skipped++

			continue
		}
		if o.CreatureID == "" {
			skipped++

			continue
		}
		imported++
		batch = append(batch, knowledge.WildlifeObservation{
			CreatureID:  o.CreatureID,
			GameTick:    o.GameTick,
			ObservedUTC: o.ObservedUTC,
			Species:     o.Species,
			Name:        o.Name,
			Role:        o.Role,
			Hull:        o.Hull,
			MaxHull:     o.MaxHull,
			InCombat:    o.InCombat,
			Branded:     o.Branded,
			SystemID:    o.SystemID,
			POIID:       o.POIID,
			AgentID:     o.AgentID,
		})
		if len(batch) >= backfillImportBatch {
			if err := flush(batch); err != nil {
				return imported, skipped, fmt.Errorf("backfill import: %w", err)
			}
			batch = batch[:0]
		}
	}
	if err := sc.Err(); err != nil {
		return imported, skipped, fmt.Errorf("backfill read %s: %w", path, err)
	}
	if err := flush(batch); err != nil {
		return imported, skipped, fmt.Errorf("backfill import: %w", err)
	}

	return imported, skipped, nil
}
