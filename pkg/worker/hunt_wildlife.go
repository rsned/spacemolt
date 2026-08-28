package worker

import (
	"context"
	"fmt"
	"io"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// huntCaptureWildlife files everything a get_nearby reply saw into the wildlife
// field guide. The game ships no catalog for its creatures, so this reply and
// survey_system's census are the only sources there are.
//
// Capture never fails a pass: a KB that cannot store wildlife, or an error while
// storing, is reported and stepped over. A hunt that dies because bookkeeping
// failed would be a worse trade than a gap in the guide.
func huntCaptureWildlife(ctx context.Context, deps HuntDeps, out io.Writer, nearby serverapi.GetNearbyResponse, poiID string) {
	// An empty creature list is NOT a reason to return. CaptureWildlifeNearby
	// writes its coverage row first and unconditionally so a look that found
	// nothing still leaves a trace; returning here skipped that and
	// reintroduced the failure its comment warns about -- a fully surveyed
	// system reading as half unvisited. The hunt fleet walks far more POIs than
	// any operator session, so it lost the most coverage.
	//
	// Wildlife is transient: ashford_ice_shelf reported 0 creatures at 12:41:35
	// and 3 at 12:43:16 on 2026-08-28, 101 seconds apart. An empty reading is
	// real data about a moment, not an absence of habitat, and only the
	// coverage row separates "looked, found none" from "never looked".
	systemID, tick := huntWhere(deps)
	// The reply names its own POI; prefer it over the id the pass travelled to,
	// which can be a tick stale after an interrupted travel.
	if nearby.POIID != "" {
		poiID = nearby.POIID
	}

	n, err := knowledge.CaptureWildlifeNearby(ctx, deps.KB, nearby.Creatures,
		systemID, poiID, huntPOIType(deps, poiID), deps.AgentID, tick)
	if err != nil {
		fmt.Fprintf(out, "hunt: wildlife not recorded at %s: %v\n", poiID, err) //nolint:errcheck
		return
	}
	if n > 0 {
		fmt.Fprintf(out, "hunt: recorded %d creature(s) across %d species at %s\n", //nolint:errcheck
			len(nearby.Creatures), n, poiID)
	}
}

// huntCaptureKill records a killed creature and, when the carcass was found, its
// contents. wreck is nil when no carcass turned up, which records the kill with
// the carcass unread so it cannot be mistaken for a creature that dropped
// nothing.
//
// Damage dealt and taken are not passed: they arrive only on the battle_ended
// push, which nothing writes into State today. fightTicks is measured by the
// policy loop, so the per-species time cost is real while the damage cost waits
// for that push to be wired up.
//
// The battle id IS recorded, and is the handle that makes the rest recoverable:
// the full damage log — per shot, per weapon, per damage type — can be fetched
// afterwards from get_battle_log for any battle whose id was kept, and there is
// no way to enumerate past battles without one.
func huntCaptureKill(ctx context.Context, deps HuntDeps, out io.Writer, wreck *serverapi.Wreck, c serverapi.NearbyCreature, fightTicks int) {
	systemID, tick := huntWhere(deps)
	if err := knowledge.CaptureWildlifeCarcass(ctx, deps.KB, wreck, c, systemID, huntBattleID(deps),
		deps.AgentID, tick, fightTicks, 0, 0); err != nil {
		fmt.Fprintf(out, "hunt: kill of %s not recorded: %v\n", c.CreatureID, err) //nolint:errcheck
	}
}

// huntBattleID reads the id of the battle just fought. Empty is normal — a
// creature that died to a single volley may never have produced a battle push —
// and an empty id simply leaves the kill unjoinable to a damage log.
func huntBattleID(deps HuntDeps) string {
	st := deps.Client.GetState()
	if st == nil {
		return ""
	}

	return st.LastBattleID
}

// huntWhere reads the current system id and game tick out of client state,
// tolerating a nil state.
func huntWhere(deps HuntDeps) (systemID string, tick int64) {
	st := deps.Client.GetState()
	if st == nil {
		return "", 0
	}
	return st.System.ID, st.CurrentTick
}

// huntPOIType resolves a POI id to its type, which becomes the species' habitat
// (belt, gas_cloud, cryobelt, nebula...). It reads the POI list already in
// client state rather than querying: the pass has just travelled there, so the
// system is loaded, and an unknown type is recorded as no habitat rather than a
// guessed one.
func huntPOIType(deps HuntDeps, poiID string) string {
	st := deps.Client.GetState()
	if st == nil {
		return ""
	}
	for _, p := range st.System.POIs {
		if p.ID == poiID {
			return p.Type
		}
	}
	return ""
}
