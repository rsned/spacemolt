package assets

import (
	"context"
	"fmt"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// Action-log capture budget. One poll is one WebSocket round-trip and costs no
// tick (get_action_log is a query), so the limit exists to bound how long a
// single scheduled task holds the worker, not to save game resources.
const (
	// actionLogPageSize is the server maximum. Fewer, larger pages means fewer
	// round-trips for the same history.
	actionLogPageSize = 100
	// actionLogPollsPerRun bounds one capture pass. At 100 entries a poll this
	// ingests up to 2,000 events per run — enough that an hourly schedule
	// backfills the busiest agent measured (craftsman-1, 45,016 entries) in
	// about a day, while a caught-up agent stops after a single poll.
	actionLogPollsPerRun = 20
	// actionLogFirstSinceID starts a fresh walk. It cannot be 0: the server
	// documents since_id=0 as "normal newest-first paging", which would return
	// the most recent page and leave the cursor unable to advance backwards.
	// Any positive value below the first real id walks from the beginning,
	// oldest-first.
	actionLogFirstSinceID = 1
)

// ActionLogResult reports what one capture pass did.
type ActionLogResult struct {
	Polls    int
	Fetched  int
	Inserted int
	Pruned   int64
	CaughtUp bool
	Cursor   int64
}

// CaptureActionLog walks an agent's server-side action log forward from its
// stored cursor, appending events and pruning the bulk types inline.
//
// The walk uses since_id, which the server serves oldest-first and answers with
// next_since_id. That direction is what makes the capture gap-free: page-based
// paging shifts under new events (page 2 is not the same 100 entries a minute
// later), so a page cursor either re-reads or skips. A since_id cursor cannot.
//
// The server retains roughly 85 days, so this is a race against expiry for any
// agent whose log has never been captured — hence the backfill budget rather
// than a single newest-page poll.
//
// Failure of a poll ends the pass without error, keeping the cursor at the last
// confirmed position: asset capture must never become a new way for a worker
// pass to fail. Only a store write propagates.
func CaptureActionLog(ctx context.Context, client game.GameClient, st *Store, agentID string, now time.Time) (ActionLogResult, error) {
	var res ActionLogResult
	if st == nil || client == nil {
		return res, nil
	}

	playerID, err := actionLogPlayerID(ctx, client)
	if err != nil || playerID == "" {
		return res, nil //nolint:nilerr // an unidentifiable agent is a source failure, not a pass failure
	}

	cursor, _, err := st.LoadActionLogCursor(ctx, playerID)
	if err != nil {
		return res, err
	}
	since := max(cursor.NextSinceID, actionLogFirstSinceID)

	for range actionLogPollsPerRun {
		payload := map[string]any{
			"since_id":  since,
			"page_size": actionLogPageSize,
		}
		if err := client.GetActionLog(ctx, payload); err != nil {
			break
		}
		page, ok, err := ActionLogFrom(client.GetRawJSON("action_log"))
		if err != nil || !ok {
			break
		}
		res.Polls++
		res.Fetched += len(page.Events)

		inserted, err := st.InsertActionLogEvents(ctx, playerID, page.Events)
		if err != nil {
			return res, err
		}
		res.Inserted += inserted

		next := page.NextSinceID
		if next <= since {
			// No usable cursor in the reply: fall back to the highest id seen.
			// Without this a server that omits next_since_id would re-fetch the
			// same window every poll for the rest of time.
			for _, e := range page.Events {
				if e.EventID > next {
					next = e.EventID
				}
			}
		}
		if next <= since {
			// Nothing new and nothing to advance past: the walk is current.
			cursor.CaughtUp = true
			break
		}
		since = next

		// A short page means the server had nothing more to give.
		if len(page.Events) < actionLogPageSize {
			cursor.CaughtUp = true
			break
		}
		cursor.CaughtUp = false
	}

	cursor.NextSinceID = since
	cursor.EventsStored += int64(res.Inserted)
	res.CaughtUp = cursor.CaughtUp
	res.Cursor = since
	if err := st.SaveActionLogCursor(ctx, playerID, cursor, now); err != nil {
		return res, err
	}

	// Prune after writing the cursor, so a prune failure cannot cost the pass
	// its progress.
	pruned, err := st.PruneActionLog(ctx, playerID, now)
	if err != nil {
		return res, err
	}
	res.Pruned = pruned

	return res, nil
}

// actionLogPlayerID resolves the agent's player id from ambient state, asking
// the server only if login has not populated it yet.
//
// Unlike CaptureProfile this does not need a guaranteed-fresh GetStatus: no
// field of the action log is read out of player state, only the id used to key
// the rows, and that never changes for the life of the account.
func actionLogPlayerID(ctx context.Context, client game.GameClient) (string, error) {
	if state := client.GetState(); state != nil && state.Player.ID != "" {
		return state.Player.ID, nil
	}
	if err := client.GetStatus(ctx); err != nil {
		return "", fmt.Errorf("assets: action log needs a player id: %w", err)
	}
	state := client.GetState()
	if state == nil {
		return "", nil
	}

	return state.Player.ID, nil
}
