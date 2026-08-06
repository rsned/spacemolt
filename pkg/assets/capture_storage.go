package assets

import (
	"context"
	"log"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// CaptureStorage records what one agent holds at every base, discovered from
// view_storage's agent-global hint.
//
// The hint enumerates every base with holdings and is returned by ANY
// view_storage call -- including one aimed at a base where the agent holds
// nothing (verified live 2026-08-06). So base discovery is one seed call plus
// one targeted call per base, never a sweep of all 64 stations.
//
// Failure policy matches CaptureProfile: a source failure degrades to "less
// captured this pass" and returns nil. Only a store write propagates. The one
// rule that matters more than the rest: an unparseable hint must NEVER delete,
// because an empty sweep is indistinguishable from "sold everything".
func CaptureStorage(ctx context.Context, client game.GameClient, st *Store, agentID string, now time.Time) error {
	if st == nil || client == nil {
		return nil
	}
	if err := client.GetStatus(ctx); err != nil {
		return nil //nolint:nilerr // a source failure must never fail the pass
	}
	state := client.GetState()
	if state == nil || state.Player.ID == "" {
		return nil
	}
	playerID := state.Player.ID

	// Seed. An undocked agent MUST supply a station id: bare view_storage
	// answers "You must be docked or provide a station_id to view storage."
	seedStation := ""
	if state.Player.DockedAtBase == "" {
		seedStation = state.Player.HomeBase
		if seedStation == "" {
			return nil // undocked with no home base: nothing to aim the seed at
		}
	}
	seed, ok := fetchStorage(ctx, client, seedStation)
	if !ok {
		return nil
	}

	stored, _, err := st.LoadStorage(ctx, playerID, nil)
	if err != nil {
		return err
	}
	storedByBase := make(map[string]StorageBase, len(stored))
	for _, b := range stored {
		storedByBase[b.BaseID] = b
	}

	hint, hintOK := ParseStorageHint(seed.hint)
	sweep := hint.Bases
	allowBaseDeletion := true
	if !hintOK {
		// Fall back to what we already knew and delete nothing. Logged loudly:
		// a hint format change would otherwise silently freeze the ledger.
		log.Printf("assets: %s: unparseable storage hint %q; falling back to %d known base(s), no deletion",
			agentID, seed.hint, len(stored))
		sweep = make([]string, 0, len(stored))
		for _, b := range stored {
			sweep = append(sweep, b.BaseID)
		}
		allowBaseDeletion = false
	}

	final := make([]StorageBase, 0, len(sweep))
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
		got, ok := fetchStorage(ctx, client, baseID)
		if !ok {
			// Carry the stored rows forward: a failed query is not evidence of
			// an empty base. storedByBase is keyed by the response form the
			// previous capture wrote, and a hit here means baseID IS that
			// form, so marking observed under baseID stays consistent.
			if prev, had := storedByBase[baseID]; had {
				final = append(final, prev)
				observed[baseID] = true
			}
			log.Printf("assets: %s: storage query failed at %s; carrying previous rows forward", agentID, baseID)

			continue
		}
		// Station ids are dual-named in this game (base id vs poi id form).
		// observed and final must key on the RESPONSE's base id, matching
		// storedByBase and the seed-union check below -- keying on the
		// requested baseID instead lets a form mismatch either delete this
		// base (carry-forward lookup misses next pass) or duplicate it
		// (the seed union re-appends it), the same class of bug that hit
		// browse_ships and owned_ships.
		if got.base.BaseID != baseID {
			log.Printf("assets: %s: storage base id form mismatch: requested %q, response named %q",
				agentID, baseID, got.base.BaseID)
		}
		final = append(final, got.base)
		observed[got.base.BaseID] = true
	}

	// The seed response is live data already in hand. The hint enumerates bases
	// with ITEMS, so a base holding only credits (or only a parked ship) is
	// legitimately absent from it -- and without this, such a base is never
	// swept and gets DELETED while we are looking straight at its contents.
	// Directly-observed data outranks a base list parsed from prose.
	//
	// Guarded on non-empty so a genuinely emptied dock still converges to zero:
	// "I sold everything at my home station" must still clear the row.
	if !observed[seed.base.BaseID] && (len(seed.base.Items) > 0 || seed.base.Credits > 0) {
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

	// Truncation cross-check. The hint's leading count is the total across every
	// listed base (verified: databot's "920 items" equals the exact sum of its
	// ten quantities), so a short sweep means bases were omitted from the hint.
	if hintOK && hint.HasTotal {
		var sum float64
		for _, b := range final {
			for _, it := range b.Items {
				sum += it.Quantity
			}
		}
		if sum < hint.Total {
			log.Printf("assets: %s: swept %.0f of %.0f hinted items across %d base(s) -- the hint may be truncated",
				agentID, sum, hint.Total, len(final))
		}
	}

	return st.ReplaceStorage(ctx, playerID, final, now)
}

// storageFetch is one decoded view_storage response.
type storageFetch struct {
	base StorageBase
	hint string
}

// fetchStorage issues one view_storage call and decodes it. stationID == ""
// means "the current dock". ok=false covers both a call failure and an
// undecodable body -- callers must treat it as "not observed", never as "empty".
func fetchStorage(ctx context.Context, client game.GameClient, stationID string) (storageFetch, bool) {
	var err error
	if stationID == "" {
		err = client.ViewStorage(ctx)
	} else {
		err = client.ViewStorageAt(ctx, stationID)
	}
	if err != nil {
		return storageFetch{}, false
	}
	base, hint, ok, derr := StorageFrom(client.GetRawJSON("storage"))
	if derr != nil || !ok {
		return storageFetch{}, false
	}

	return storageFetch{base: base, hint: hint}, true
}
