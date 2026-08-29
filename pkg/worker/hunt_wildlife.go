package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
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

// huntNearbyCreatures reads the creature list at a POI the pass has just
// reached, and re-reads it once when the first look comes back empty.
//
// A get_nearby issued in the same second as an arrival reads ZERO creatures at
// a POI that is in fact populated. Live 2026-08-28, craftsman-1, both POIs it
// reached that pass:
//
//	alrescha_emission_nebula  arrived 17:20:55  ->  0 creatures
//	                                 17:21:01  -> 18 creatures
//	alrescha_ice_fields       arrived 17:21:25  ->  0 creatures
//	                                 17:21:31  ->  8 creatures
//
// This is an arrival race, not the ordinary churn of creatures wandering.
// Cause confirmed by the server team 2026-08-28: wildlife is materialised at a
// POI only while a pilot is there, and that step runs LATE in the tick, while
// the arrival confirmation goes out at the START of the same tick. A client
// that queries the instant it arrives reads the location before the herd is
// placed. The first read is therefore not a sample to be averaged with the
// second — it is invalid, and the second is the only reading that means
// anything.
//
// A server fix is planned that places the wildlife before announcing the
// arrival. This code needs no revisit when it lands: the re-read is conditional
// on an empty first look, so a fixed server simply stops triggering it and the
// extra call disappears on its own.
//
// For the hunt fleet that mis-read is worse than a hole in the field guide.
// huntFindQuarry treats an empty list as "this ground holds nothing", abandons
// a perfectly good hunting ground and flies to the next one — so the race made
// the fleet reject the belts its missions needed, at the cost of a real journey
// each time.
//
// The re-read is conditional on the first being empty rather than unconditional
// (as the operator's explore pass does it). Every observed failure has the
// shape 0 -> N, so emptiness is the whole signal; and the hunt fleet walks far
// more POIs than an operator session, where an unconditional second call would
// double nearby volume against a shared per-IP rate limiter for nothing.
//
// The wait is a full SleepTick because the tick boundary is what the reading
// waits on: a shorter pause can land inside the same tick and re-read the same
// nothing. Both looks are captured, so the coverage row still records that the
// first one happened and found none.
func huntNearbyCreatures(ctx context.Context, deps HuntDeps, out io.Writer, poi string) (serverapi.GetNearbyResponse, error) {
	nearby, err := huntReadNearby(ctx, deps, out, poi)
	if err != nil || len(nearby.Creatures) > 0 {
		return nearby, err
	}

	time.Sleep(game.SleepTick)

	// A failed re-read is not a reason to fail the pass: the first look stands
	// on its own, having found nothing, which the caller already handles.
	second, rerr := huntReadNearby(ctx, deps, out, poi)
	if rerr != nil {
		fmt.Fprintf(out, "hunt: re-reading %s: %v; keeping the empty first look\n", poi, rerr) //nolint:errcheck
		return nearby, nil
	}
	if len(second.Creatures) > 0 {
		fmt.Fprintf(out, "hunt: %s read empty on arrival, %d creature(s) on re-read\n", //nolint:errcheck
			poi, len(second.Creatures))
	}
	return second, nil
}

// huntReadNearby issues one get_nearby, files what it saw, and returns it.
func huntReadNearby(ctx context.Context, deps HuntDeps, out io.Writer, poi string) (serverapi.GetNearbyResponse, error) {
	if err := deps.Client.GetNearby(ctx); err != nil {
		return serverapi.GetNearbyResponse{}, fmt.Errorf("get_nearby: %w", err)
	}
	var nearby serverapi.GetNearbyResponse
	if raw := deps.Client.GetRawJSON("nearby"); len(raw) > 0 {
		if err := json.Unmarshal(raw, &nearby); err != nil {
			return serverapi.GetNearbyResponse{}, fmt.Errorf("parse nearby: %w", err)
		}
	}
	// File everything seen, not just what this pass may shoot. The field guide
	// wants the herd we are walking away from as much as the one we engage, and
	// this reply is the only wildlife headcount that names a POI.
	huntCaptureWildlife(ctx, deps, out, nearby, poi)
	return nearby, nil
}
