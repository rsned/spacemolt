// Command battle-export fetches a battle and writes a normalized replay model
// as JSON, for the holotable visualizer to draw.
//
// It exists because battle reads require a logged-in session: the API is
// CORS-open to any origin, but an anonymous session is rejected, so a static
// page cannot fetch a battle for itself. This tool holds the credentials, and
// the page loads the file it writes. The eventual in-client view skips this
// entirely — it already has a player session.
//
//	bin/battle-export --agent miner-9 --battle <battle_id> --out battle.json
//
// Design: kb/docs/superpowers/specs/2026-08-16-battle-holotable-visualizer-design.md
package main

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/battlereplay"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// maxLimit is the server's cap on ticks per get_battle_log page.
const maxLimit = 200

// settleDelay lets a reply land in the client's raw-JSON cache before it is
// read back. The client's Submit terminates on the ok/action frame, but the
// cache write is what this tool reads, so it waits rather than racing it.
const settleDelay = 2 * time.Second

// contentionBackoff outwaits the client's session-contention detector, which
// aborts the run if two connections die within 30s of each other.
const contentionBackoff = 35 * time.Second

func main() {
	agent := flag.String("agent", "", "agent id to authenticate as (required)")
	battleID := flag.String("battle", "", "battle id to export (required)")
	out := flag.String("out", "", "output path (default: battle_<id>.json)")
	limit := flag.Int("limit", maxLimit, "ticks per page (1..200). Lower it for battles with "+
		"hundreds of participants: a page over 10MB exceeds the WebSocket read limit, and each "+
		"oversized frame costs a reconnect")
	pretty := flag.Bool("pretty", false, "indent the output JSON")
	raw := flag.String("raw-out", "", "also write the unmodified log pages here, for fixtures")
	gz := flag.Bool("gzip", false, "gzip the output (a 373-participant battle is ~24MB raw, ~5MB gzipped)")
	flag.Parse()

	if *agent == "" || *battleID == "" {
		fmt.Fprintln(os.Stderr, "usage: battle-export --agent <id> --battle <battle_id> [--out path]")
		os.Exit(2)
	}
	outPath := *out
	if outPath == "" {
		outPath = fmt.Sprintf("battle_%s.json", *battleID)
	}

	logger := log.New(os.Stderr, "[battle-export] ", log.LstdFlags)
	ctx := context.Background()

	client, _, err := game.InitializeAgent(*agent, logger, ctx, false)
	if err != nil {
		logger.Fatalf("login as %s: %v", *agent, err)
	}
	defer client.Close() //nolint:errcheck
	time.Sleep(settleDelay)

	summary, err := fetchSummary(ctx, client, *battleID)
	if err != nil {
		// A missing summary is not fatal: everything needed to draw the battle
		// is in the log itself. Losing the system NAME is the only real cost.
		logger.Printf("summary unavailable (%v); continuing from the log alone", err)
	}

	if *limit < 1 || *limit > maxLimit {
		logger.Fatalf("--limit must be between 1 and %d, got %d", maxLimit, *limit)
	}
	pages, err := fetchLog(ctx, client, *battleID, *limit, logger)
	if err != nil {
		logger.Fatalf("fetch log: %v", err)
	}
	if len(pages) == 0 {
		logger.Fatalf("battle %s returned no log entries", *battleID)
	}

	if *raw != "" {
		if err := writeJSON(*raw, pages, *pretty); err != nil {
			logger.Fatalf("write raw: %v", err)
		}
		logger.Printf("wrote raw pages to %s", *raw)
	}

	model := battlereplay.Adapt(pages, summary)
	if *gz && !strings.HasSuffix(outPath, ".gz") {
		outPath += ".gz"
	}
	if err := writeJSONMaybeGzip(outPath, model, *pretty, *gz); err != nil {
		logger.Fatalf("write model: %v", err)
	}

	var shots, kills int
	for _, f := range model.Frames {
		shots += len(f.Shots)
		kills += len(f.Kills)
	}
	logger.Printf("wrote %s: %d ticks, %d participants, %d shots, %d kills (%s in %s)",
		outPath, model.TickCount, len(model.Participants), shots, kills, model.Outcome, model.SystemName)
}

// fetchLog pages through the whole battle, shrinking the page size when a page
// is too large to receive.
//
// Two ceilings apply. The server caps a page at maxLimit ticks, so a long battle
// needs several requests. Separately, the WebSocket client refuses frames over
// 10 MB (pkg/game/client.go SetReadLimit), and a few hundred participants
// produce roughly 50 KB of snapshot per tick — so a 200-tick page of a large
// battle blows the transport limit, the connection drops mid-read, and the
// command times out after reconnecting. Raising the global read limit would
// inflate buffers for every fleet worker to suit one tool, so instead the page
// size halves on failure and never climbs back: a battle that needs 25-tick
// pages keeps them for the rest of the run.
func fetchLog(ctx context.Context, client game.GameClient, battleID string, startLimit int, logger *log.Logger) ([]serverapi.GetBattleLogResponse, error) {
	var pages []serverapi.GetBattleLogResponse
	tickStart := 0
	limit := startLimit

	for {
		page, err := fetchPage(ctx, client, battleID, tickStart, limit)
		if err != nil {
			if limit > 1 {
				limit /= 2
				logger.Printf("page from tick %d failed (%v); retrying with limit %d", tickStart, err, limit)
				// Wait out the client's session-contention window before retrying.
				// An oversized frame kills the connection, and two connects dying
				// inside 30s make the client conclude another session stole the
				// credentials and give up entirely.
				time.Sleep(contentionBackoff)
				continue
			}
			return pages, fmt.Errorf("get_battle_log from tick %d at limit 1: %w", tickStart, err)
		}
		if len(page.Entries) == 0 {
			return pages, nil
		}
		pages = append(pages, *page)
		last := page.Entries[len(page.Entries)-1].Tick
		logger.Printf("fetched %d ticks (through %d, limit %d)%s", len(page.Entries), last, limit,
			map[bool]string{true: ", more to come", false: ""}[page.HasMore])

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

// fetchPage requests one page of the battle log.
func fetchPage(ctx context.Context, client game.GameClient, battleID string, tickStart, limit int) (*serverapi.GetBattleLogResponse, error) {
	args := map[string]any{"battle_id": battleID, "limit": limit}
	if tickStart > 0 {
		args["tick_start"] = tickStart
	}
	if err := client.RawCommand(ctx, "get_battle_log", args); err != nil {
		return nil, err
	}
	time.Sleep(settleDelay)

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

func fetchSummary(ctx context.Context, client game.GameClient, battleID string) (*serverapi.BattleSummaryResponse, error) {
	if err := client.RawCommand(ctx, "get_battle_summary", map[string]any{"battle_id": battleID}); err != nil {
		return nil, err
	}
	time.Sleep(settleDelay)
	var s serverapi.BattleSummaryResponse
	if err := json.Unmarshal(client.GetRawJSON("_last"), &s); err != nil {
		return nil, err
	}
	if s.BattleID == "" {
		return nil, fmt.Errorf("no battle_id in summary reply")
	}
	return &s, nil
}

func writeJSON(path string, v any, pretty bool) error {
	return writeJSONMaybeGzip(path, v, pretty, false)
}

// writeJSONMaybeGzip writes v as JSON, optionally gzipped. Gzip earns its place
// here: the frames are highly repetitive (the same player ids, zones and stances
// every tick), so a 24MB model compresses to about 5MB, and a browser fetching
// it over HTTP decompresses transparently.
func writeJSONMaybeGzip(path string, v any, pretty, compress bool) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck

	var w io.Writer = f
	if compress {
		zw := gzip.NewWriter(f)
		defer zw.Close() //nolint:errcheck
		w = zw
	}
	enc := json.NewEncoder(w)
	if pretty {
		enc.SetIndent("", " ")
	}
	return enc.Encode(v)
}
