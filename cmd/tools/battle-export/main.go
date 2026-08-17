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
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
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

func main() {
	agent := flag.String("agent", "", "agent id to authenticate as (required)")
	battleID := flag.String("battle", "", "battle id to export (required)")
	out := flag.String("out", "", "output path (default: battle_<id>.json)")
	pretty := flag.Bool("pretty", false, "indent the output JSON")
	raw := flag.String("raw-out", "", "also write the unmodified log pages here, for fixtures")
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

	pages, err := fetchLog(ctx, client, *battleID, logger)
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
	if err := writeJSON(outPath, model, *pretty); err != nil {
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

// fetchLog pages through the whole battle. The server caps a page at maxLimit
// ticks, so a long battle needs several requests; each one resumes from the
// tick after the last one returned.
func fetchLog(ctx context.Context, client game.GameClient, battleID string, logger *log.Logger) ([]serverapi.GetBattleLogResponse, error) {
	var pages []serverapi.GetBattleLogResponse
	tickStart := 0

	for {
		args := map[string]any{"battle_id": battleID, "limit": maxLimit}
		if tickStart > 0 {
			args["tick_start"] = tickStart
		}
		if err := client.RawCommand(ctx, "get_battle_log", args); err != nil {
			return pages, fmt.Errorf("get_battle_log from tick %d: %w", tickStart, err)
		}
		time.Sleep(settleDelay)

		var page serverapi.GetBattleLogResponse
		if err := json.Unmarshal(client.GetRawJSON("_last"), &page); err != nil {
			return pages, fmt.Errorf("decode page from tick %d: %w", tickStart, err)
		}
		if len(page.Entries) == 0 {
			return pages, nil
		}
		pages = append(pages, page)
		last := page.Entries[len(page.Entries)-1].Tick
		logger.Printf("fetched %d ticks (through %d)%s", len(page.Entries), last,
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
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck
	enc := json.NewEncoder(f)
	if pretty {
		enc.SetIndent("", " ")
	}
	return enc.Encode(v)
}
