package worker

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/galaxy"
)

// A pipeline miner is pinned to one POI. When that POI ran dry the server
// answered every mine with "Resources depleted", the pass failed, and after
// three stall-restarts the overmind quarantined the miner: miningbot_0018
// (zinc, Cloverfield belt) and miningbot_0023 (plasma gas, Timberline) sat
// quarantined for days on 2026-11-10/11 with full tanks. A depleted POI
// regrows only while nobody mines it, so the miner now leaves it to rest and
// works the nearest other known source of the same item, delivering to the
// same place, until the rest period is over.

// MineDepletedRest is how long a POI found depleted is left alone before the
// pinned miner tries it again. It matches the resource-POI freshness window.
const MineDepletedRest = 6 * time.Hour

// mineMigrateCandidates bounds how many alternate deposits one pass tries
// when the alternates are themselves depleted.
const mineMigrateCandidates = 3

// isDepletedErr reports whether err is the server refusing to mine because
// the POI's resources are exhausted.
func isDepletedErr(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "depleted")
}

// markResting records poiID as depleted now.
func (d *WorkerDispatch) markResting(poiID string, now time.Time) {
	if d.restingPOIs == nil {
		d.restingPOIs = map[string]time.Time{}
	}
	d.restingPOIs[poiID] = now
}

// isResting reports whether poiID was found depleted within MineDepletedRest.
// An expired entry is dropped so the next pass tries the POI again.
func (d *WorkerDispatch) isResting(poiID string, now time.Time) bool {
	at, ok := d.restingPOIs[poiID]
	if !ok {
		return false
	}
	if now.Sub(at) >= MineDepletedRest {
		delete(d.restingPOIs, poiID)
		return false
	}
	return true
}

// findAlternateMinePOI returns the nearest known deposit of itemID, searched
// from fromSystem, that is not resting, not a blocked stronghold, and not in a
// Lawless system this agent may not mine. Only POIs the KB records as carrying
// itemID qualify: an unsurveyed belt of the right type is no substitute here.
func (d *WorkerDispatch) findAlternateMinePOI(ctx context.Context, fromSystem, itemID string, strongholds map[string]bool, now time.Time) (system, poi string, err error) {
	if d.KB == nil {
		return "", "", fmt.Errorf("no knowledge base configured")
	}
	graph := &galaxy.GalaxyGraph{}
	if err = graph.BuildFromDB(ctx, d.KB); err != nil {
		return "", "", fmt.Errorf("build galaxy graph: %w", err)
	}
	results, err := galaxy.FindNearestByResource(ctx, d.KB, graph, fromSystem, itemID, mineCandidateSlate)
	if err != nil {
		return "", "", fmt.Errorf("find resource %q: %w", itemID, err)
	}
	unlocked := d.pirateUnlocked(ctx)
	for _, r := range results {
		if strongholdBlocked(strongholds, r.SystemID, r.SystemName) {
			continue
		}
		if sys, serr := d.KB.GetSystem(ctx, r.SystemID); serr == nil {
			if blocked, _ := lawlessBlocked(sys, unlocked); blocked {
				continue
			}
		}
		for _, p := range r.POIs {
			if !d.isResting(p.ID, now) {
				return r.SystemID, p.ID, nil
			}
		}
	}

	return "", "", fmt.Errorf("no other known deposit of %s within %d systems", itemID, mineCandidateSlate)
}

// mineWithMigration mines itemID at the pinned (systemID, poiID) unless that
// POI is resting, and moves on to alternates when a POI reports depleted. A
// depleted POI before any alternate is found is an error only when nothing at
// all was mined this pass.
func (d *WorkerDispatch) mineWithMigration(ctx context.Context, itemID string, qty int, systemID, poiID string, anyYield bool, strongholds map[string]bool) error {
	now := time.Now()
	sys, poi := systemID, poiID
	if d.isResting(poiID, now) {
		alt, altPOI, err := d.findAlternateMinePOI(ctx, systemID, itemID, strongholds, now)
		if err != nil {
			return fmt.Errorf("mine_qty_at: %s is resting after depletion and %w", poiID, err)
		}
		sys, poi = alt, altPOI
		fmt.Fprintf(d.Out, "⛏️  mine_qty_at: %s is resting (depleted); mining %s at %s/%s instead\n", poiID, itemID, sys, poi) //nolint:errcheck
	}

	for attempt := 0; ; attempt++ {
		var err error
		why := "depleted"
		if terr := d.autopilotAndUndock(ctx, sys, poi); terr != nil {
			err = fmt.Errorf("mine_qty_at: travel to %s: %w", poi, terr)
			// Only a vanished alternate is skipped; the pinned POI failing
			// to resolve is a config problem the operator must see.
			if poi == poiID || !isUnknownDestinationErr(terr) {
				return err
			}
			why = "no longer exists (Unknown destination)"
		} else {
			err = d.mineLoop(ctx, itemID, qty, poi, anyYield)
			if !isDepletedErr(err) {
				return err
			}
		}
		d.markResting(poi, time.Now())
		if attempt >= mineMigrateCandidates {
			return err
		}
		alt, altPOI, ferr := d.findAlternateMinePOI(ctx, systemID, itemID, strongholds, time.Now())
		if ferr != nil {
			fmt.Fprintf(d.Out, "⛏️  mine_qty_at: %s %s and %v\n", poi, why, ferr) //nolint:errcheck
			return err
		}
		fmt.Fprintf(d.Out, "⛏️  mine_qty_at: %s %s — resting it %s; migrating to %s/%s for %s\n", //nolint:errcheck
			poi, why, MineDepletedRest, alt, altPOI, itemID)
		sys, poi = alt, altPOI
	}
}

// isUnknownDestinationErr reports whether err is the server refusing travel
// to a POI it does not know: a transient POI (a Phantom Glint) that the KB
// still lists after it vanished.
func isUnknownDestinationErr(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unknown destination")
}
