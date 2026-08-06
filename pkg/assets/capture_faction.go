package assets

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// factionCaptorFreshness bounds which members count toward the designation. A
// member whose profile has not refreshed inside this window is presumed dead
// and cannot hold the captor role hostage.
const factionCaptorFreshness = 24 * time.Hour

// IsFactionCaptor reports whether this player is the designated captor for its
// faction this cycle.
//
// Faction assets are per-faction, so one member's capture covers every member.
// The designation is the member with the lowest player_id among those with a
// fresh profile -- a pure function of data every worker already has, so it needs
// no claim file, no lock and no shared state.
//
// A cold ledger (no fresh rows at all) returns true: bootstrapping by capturing
// is right, because every faction write is an idempotent upsert or a whole-set
// replacement, so a duplicate capture costs two free queries and nothing else.
func (s *Store) IsFactionCaptor(ctx context.Context, playerID, factionID string, now time.Time) (bool, error) {
	if s == nil || s.db == nil || playerID == "" || factionID == "" {
		return false, nil
	}
	cutoff := rfc3339(now.Add(-factionCaptorFreshness))
	// MIN() over zero matching rows returns ONE row containing SQL NULL -- not
	// sql.ErrNoRows -- so this must scan into a NullString. Scanning NULL into a
	// plain string fails with "converting NULL to string is unsupported", which
	// would turn a cold ledger into an error instead of a bootstrap.
	var lowest sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT MIN(player_id) FROM agent_profile
		 WHERE faction_id = ? AND captured_at >= ?`, factionID, cutoff).Scan(&lowest)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("assets: faction captor %s: %w", factionID, err)
	}
	if !lowest.Valid || lowest.String == "" {
		return true, nil // cold ledger: bootstrap
	}

	return lowest.String == playerID, nil
}

// CaptureFaction records the agent's faction treasury and shared storage.
//
// A no-op for unaffiliated agents (most of the fleet) and for members who are
// not this cycle's designated captor. Failure policy matches CaptureProfile:
// source failures degrade silently, only store writes propagate.
func CaptureFaction(ctx context.Context, client game.GameClient, st *Store, agentID string, now time.Time) error {
	if st == nil || client == nil {
		return nil
	}
	if err := client.GetStatus(ctx); err != nil {
		return nil //nolint:nilerr // a source failure must never fail the pass
	}
	state := client.GetState()
	if state == nil || state.Player.ID == "" || state.Player.FactionID == "" {
		return nil
	}
	playerID, factionID := state.Player.ID, state.Player.FactionID

	captor, err := st.IsFactionCaptor(ctx, playerID, factionID, now)
	if err != nil {
		return err
	}
	if !captor {
		return nil
	}

	if err := client.FactionInfo(ctx); err != nil {
		return nil //nolint:nilerr // degrade to no capture this pass
	}
	profile, ok, derr := FactionProfileFrom(client.GetRawJSON("faction_info"))
	if derr != nil || !ok {
		return nil //nolint:nilerr // an undecodable body degrades to no capture this pass
	}
	if err := st.UpsertFactionProfile(ctx, profile, now); err != nil {
		return err
	}

	// Seed the storage sweep from wherever the agent is. Like the agent hint,
	// the faction hint enumerates every base with holdings.
	seedStation := ""
	if state.Player.DockedAtBase == "" {
		seedStation = state.Player.HomeBase
		if seedStation == "" {
			return nil
		}
	}
	seed, ok := fetchFactionStorage(ctx, client, seedStation)
	if !ok {
		return nil
	}

	stored, _, err := st.LoadFactionStorage(ctx, factionID, nil)
	if err != nil {
		return err
	}
	storedByBase := make(map[string]FactionStorageBase, len(stored))
	for _, b := range stored {
		storedByBase[b.BaseID] = b
	}

	hint, hintOK := ParseStorageHint(seed.hint)
	sweep := hint.Bases
	allowBaseDeletion := true
	if !hintOK {
		log.Printf("assets: %s: unparseable faction storage hint %q; falling back to %d known base(s), no deletion",
			agentID, seed.hint, len(stored))
		sweep = make([]string, 0, len(stored))
		for _, b := range stored {
			sweep = append(sweep, b.BaseID)
		}
		allowBaseDeletion = false
	}

	final := make([]FactionStorageBase, 0, len(sweep))
	observed := make(map[string]bool, len(sweep))
	for _, baseID := range sweep {
		if observed[baseID] {
			continue // the hint named this base more than once
		}
		if baseID == seed.base.BaseID {
			final = append(final, seed.base)
			observed[baseID] = true

			continue
		}
		time.Sleep(game.SleepQuick)
		got, ok := fetchFactionStorage(ctx, client, baseID)
		if !ok {
			// storedByBase is keyed by the response form the previous
			// capture wrote, and a hit here means baseID IS that form, so
			// marking observed under baseID stays consistent.
			if prev, had := storedByBase[baseID]; had {
				final = append(final, prev)
				observed[baseID] = true
			}
			log.Printf("assets: %s: faction storage query failed at %s; carrying previous rows forward",
				agentID, baseID)

			continue
		}
		// See the mirrored comment in capture_storage.go: observed and final
		// must key on the RESPONSE's base id, matching storedByBase and the
		// seed-union check below, or a dual-named station form mismatch can
		// silently delete or duplicate this base.
		if got.base.BaseID != baseID {
			log.Printf("assets: %s: faction storage base id form mismatch: requested %q, response named %q",
				agentID, baseID, got.base.BaseID)
		}
		final = append(final, got.base)
		observed[got.base.BaseID] = true
	}

	// Same seed-base rule as CaptureStorage, and it bites harder here: faction
	// storage carries a FUEL BUNKER (fuel_reserve/fuel_capacity) alongside items
	// and credits, so a base holding only a fuel reserve is entirely ordinary
	// and is absent from an item-counting hint. Without this the faction's
	// bunker row is deleted while its contents sit in the seed response.
	if !observed[seed.base.BaseID] &&
		(len(seed.base.Items) > 0 || seed.base.Credits > 0 || seed.base.FuelReserve > 0) {
		final = append(final, seed.base)
		observed[seed.base.BaseID] = true
	}

	if !allowBaseDeletion {
		for _, b := range stored {
			if !observed[b.BaseID] {
				final = append(final, b)
			}
		}
	}

	return st.ReplaceFactionStorage(ctx, factionID, final, now)
}

// factionStorageFetch is one decoded view_faction_storage response.
type factionStorageFetch struct {
	base FactionStorageBase
	hint string
}

func fetchFactionStorage(ctx context.Context, client game.GameClient, stationID string) (factionStorageFetch, bool) {
	var err error
	if stationID == "" {
		err = client.ViewFactionStorage(ctx)
	} else {
		err = client.ViewFactionStorageAt(ctx, stationID)
	}
	if err != nil {
		return factionStorageFetch{}, false
	}
	base, hint, ok, derr := FactionStorageFrom(client.GetRawJSON("faction_storage"))
	if derr != nil || !ok {
		return factionStorageFetch{}, false
	}

	return factionStorageFetch{base: base, hint: hint}, true
}
