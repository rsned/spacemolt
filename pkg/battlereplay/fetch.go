package battlereplay

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// MaxLogLimit is the server's cap on ticks per get_battle_log page.
const MaxLogLimit = 200

// settleDelay lets a reply land in the client's raw-JSON cache before it is
// read back. The client's Submit terminates on the ok/action frame, but the
// cache write is what this reads, so it waits rather than racing it.
const settleDelay = game.SleepQuick

// contentionBackoff outwaits the client's session-contention detector, which
// aborts the run if two connections die within 30s of each other.
const contentionBackoff = game.SleepReconnect + game.SleepShort

// Logf is an optional progress sink. A nil Logf is silent, which is what a
// worker capturing a battle in the background wants.
type Logf func(format string, args ...any)

func (l Logf) printf(format string, args ...any) {
	if l != nil {
		l(format, args...)
	}
}

// FetchModel pulls a whole battle and adapts it to a replay model.
//
// The summary is optional in the sense that a battle with a log but no summary
// still yields a usable model; a summary fetch failure is therefore reported
// through logf rather than failing the call. The log is not optional — with no
// entries there is nothing to reconstruct.
func FetchModel(ctx context.Context, client game.GameClient, battleID string, logf Logf) (*ReplayModel, error) {
	pages, err := FetchLog(ctx, client, battleID, MaxLogLimit, logf)
	if err != nil {
		return nil, err
	}
	if len(pages) == 0 {
		return nil, fmt.Errorf("battle %s returned no log entries", battleID)
	}
	summary, err := FetchSummary(ctx, client, battleID)
	if err != nil {
		logf.printf("battle %s: no summary (%v); continuing from the log alone", battleID, err)
		summary = nil
	}
	m := Adapt(pages, summary)

	return &m, nil
}

// FetchLog pages through get_battle_log until the server says there is no more.
//
// Two ceilings apply. The server caps a page at MaxLogLimit ticks, so a long
// battle needs several requests. Separately, the WebSocket client refuses frames
// over 10 MB (pkg/game/client.go SetReadLimit), and a few hundred participants
// produce roughly 50 KB of snapshot per tick — so a 200-tick page of a large
// battle blows the transport limit, the connection drops mid-read, and the
// command times out after reconnecting. Raising the global read limit would
// inflate buffers for every fleet worker to suit one caller, so instead the page
// size halves on failure and never climbs back: a battle that needs 25-tick
// pages keeps them for the rest of the run.
//
// The backoff between retries is not politeness — two connections dying inside
// 30s make the client conclude another session stole the credentials and give up
// entirely.
func FetchLog(ctx context.Context, client game.GameClient, battleID string, startLimit int, logf Logf) ([]serverapi.GetBattleLogResponse, error) {
	var pages []serverapi.GetBattleLogResponse
	tickStart := 0
	limit := startLimit
	if limit <= 0 || limit > MaxLogLimit {
		limit = MaxLogLimit
	}

	for {
		page, err := fetchLogPage(ctx, client, battleID, tickStart, limit)
		if err != nil {
			if limit > 1 {
				limit /= 2
				logf.printf("page from tick %d failed (%v); retrying with limit %d", tickStart, err, limit)
				select {
				case <-ctx.Done():
					return pages, ctx.Err()
				case <-time.After(contentionBackoff):
				}

				continue
			}

			return pages, fmt.Errorf("get_battle_log from tick %d at limit 1: %w", tickStart, err)
		}
		if len(page.Entries) == 0 {
			return pages, nil
		}
		pages = append(pages, *page)
		last := page.Entries[len(page.Entries)-1].Tick
		if !page.HasMore {
			return pages, nil
		}
		next := int(last) + 1
		if next <= tickStart {
			// Defensive: a server that stops advancing would otherwise spin here.
			return pages, fmt.Errorf("log cursor stalled at tick %d", tickStart)
		}
		tickStart = next
	}
}

// fetchLogPage requests one page of the battle log.
func fetchLogPage(ctx context.Context, client game.GameClient, battleID string, tickStart, limit int) (*serverapi.GetBattleLogResponse, error) {
	args := map[string]any{"battle_id": battleID, "limit": limit}
	if tickStart > 0 {
		args["tick_start"] = tickStart
	}
	if err := client.RawCommand(ctx, "get_battle_log", args); err != nil {
		return nil, err
	}
	if err := settle(ctx); err != nil {
		return nil, err
	}

	raw := client.GetRawJSON("_last")
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty reply")
	}
	var page serverapi.GetBattleLogResponse
	if err := json.Unmarshal(raw, &page); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	// A reply for some other command means the page never landed — treat it as a
	// failure so the caller shrinks the page rather than silently truncating.
	if page.BattleID == "" && len(page.Entries) == 0 {
		return nil, fmt.Errorf("reply carried no battle log")
	}

	return &page, nil
}

// FetchSummary reads the battle summary, which carries the outcome and the
// per-side rollup the log itself does not.
func FetchSummary(ctx context.Context, client game.GameClient, battleID string) (*serverapi.BattleSummaryResponse, error) {
	if err := client.RawCommand(ctx, "get_battle_summary", map[string]any{"battle_id": battleID}); err != nil {
		return nil, err
	}
	if err := settle(ctx); err != nil {
		return nil, err
	}
	var s serverapi.BattleSummaryResponse
	if err := json.Unmarshal(client.GetRawJSON("_last"), &s); err != nil {
		return nil, err
	}
	if s.BattleID == "" {
		return nil, fmt.Errorf("no battle_id in summary reply")
	}

	return &s, nil
}

func settle(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(settleDelay):
		return nil
	}
}
