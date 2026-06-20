package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/faction"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/worker"
)

// currentTick returns the best available game tick: from the global clock if
// running, otherwise from the client state's last server-reported value.
func currentTick(state *game.State) int64 {
	if globalClock != nil {
		return globalClock.Tick()
	}
	if state == nil {
		return 0
	}
	return state.GetTick()
}

// formatMissionDiffValue formats a mission diff value for human-readable output.
// For JSON fields like "objectives", it parses and pretty-prints the JSON.
// For other fields, it returns the value with simple quoting.
func formatMissionDiffValue(field, oldValue, newValue string) (string, string) {
	// For objectives field, try to parse and format as JSON
	if field == "objectives" {
		return formatObjectivesDiff(oldValue, newValue)
	}

	// For other fields, use simple quoting
	return fmt.Sprintf("%q", oldValue), fmt.Sprintf("%q", newValue)
}

// formatObjectivesDiff formats mission objectives JSON arrays for human-readable output.
// It parses the JSON arrays and shows them in a condensed format, or returns an error
// marker if parsing fails.
func formatObjectivesDiff(oldJSON, newJSON string) (string, string) {
	oldFormatted, err := formatObjectivesJSON(oldJSON)
	if err != nil {
		oldFormatted = fmt.Sprintf("[invalid JSON: %v]", err)
	}

	newFormatted, err := formatObjectivesJSON(newJSON)
	if err != nil {
		newFormatted = fmt.Sprintf("[invalid JSON: %v]", err)
	}

	return oldFormatted, newFormatted
}

// formatObjectivesJSON parses a mission objectives JSON array and returns a
// human-readable string showing the count and type of objectives.
func formatObjectivesJSON(jsonStr string) (string, error) {
	var objectives []map[string]any
	if err := json.Unmarshal([]byte(jsonStr), &objectives); err != nil {
		return "", err
	}

	if len(objectives) == 0 {
		return "[]", nil
	}

	// Count objectives by type
	typeCounts := make(map[string]int)
	for _, obj := range objectives {
		if objType, ok := obj["type"].(string); ok {
			typeCounts[objType]++
		}
	}

	// Build a concise description
	var sb strings.Builder
	fmt.Fprintf(&sb, "[%d objectives: ", len(objectives))
	first := true
	for objType, count := range typeCounts {
		if !first {
			sb.WriteString(", ")
		}
		fmt.Fprintf(&sb, "%d %s", count, objType)
		first = false
	}
	sb.WriteString("]")

	return sb.String(), nil
}

// --- Thin wrappers delegating to pkg/worker ---

// kbUpdateSystem fetches the current system data and saves it to the knowledge base.
func kbUpdateSystem(client game.GameClient, ctx context.Context) error {
	return worker.KBUpdateSystem(ctx, client, globalKB, globalAgentID)
}

// kbUpdatePOI fetches current POI data and saves it to the knowledge base, then
// preserves the play_as intel-file side effect (write the raw get_poi payload to
// an intel file for the faction intel terminal).
func kbUpdatePOI(client game.GameClient, ctx context.Context) error {
	if err := worker.KBUpdatePOI(ctx, client, globalKB, globalAgentID); err != nil {
		return err
	}
	// Preserve the play_as intel-file side effect. The client still holds the raw
	// "poi" JSON from the GetPOI that worker.KBUpdatePOI performed. This is a
	// best-effort side effect: a missing/malformed payload is silently skipped
	// since the KB write (the function's contract) already succeeded.
	rawJSON := client.GetRawJSON("poi")
	var poiResp serverapi.GetPOIResponse
	if rawJSON != nil && json.Unmarshal(rawJSON, &poiResp) == nil {
		state := client.GetState()
		if path, err := saveIntelPOI(state.System.ID, poiResp.POI.ID, rawJSON); err != nil {
			fmt.Printf("Warning: failed to write intel file: %v\n", err)
		} else if path != "" {
			fmt.Printf("Saved intel: %s\n", path)
		}
	}
	return nil
}

// kbUpdateStation fetches base details, market listings, and ship listings at the
// current station and saves them to the knowledge base.
func kbUpdateStation(client game.GameClient, ctx context.Context) error {
	return worker.KBUpdateStation(ctx, client, globalKB)
}

// kbUpdateFacilities fetches facility details via 'facility list' and saves enriched
// data (description, active, maintenance, service, recipe_id) to the knowledge base.
func kbUpdateFacilities(client game.GameClient, ctx context.Context) error {
	return worker.KBUpdateFacilities(ctx, client, globalKB)
}

// kbUpdateAll runs update_system, update_poi, and (if docked) update_station,
// update_facilities, and update_missions.
func kbUpdateAll(client game.GameClient, ctx context.Context) error {
	if err := worker.KBUpdateAll(ctx, client, globalKB, globalAgentID); err != nil {
		return err
	}
	state := client.GetState()
	if state.Doc {
		if err := kbUpdateMissions(client, ctx); err != nil {
			fmt.Printf("Warning: update_missions: %v\n", err)
		}
	}
	return nil
}

// kbUpdateMissions fetches the mission board at the current station and upserts
// each hand-authored (non-procedural) entry into the knowledge-base mission
// catalog. Procedural missions (empty template_id) are counted and skipped.
func kbUpdateMissions(client game.GameClient, ctx context.Context) error {
	if globalKB == nil {
		return fmt.Errorf("knowledge base not configured (use --db-path)")
	}

	state := client.GetState()
	if !state.Doc {
		fmt.Println("(Not docked — skipping missions update)")
		return nil
	}

	if err := client.GetMissions(ctx); err != nil {
		return fmt.Errorf("get_missions: %w", err)
	}
	time.Sleep(game.SleepQuick)

	raw := client.GetRawJSON("missions")
	if len(raw) == 0 {
		return fmt.Errorf("get_missions returned no data")
	}

	var resp serverapi.GetMissionsResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return fmt.Errorf("parse get_missions response: %w", err)
	}

	baseID := resp.BaseID
	if baseID == "" {
		baseID = state.CurrentPOI
	}
	systemID := state.System.ID
	tick := currentTick(state)

	var inserted, unchanged, changed, skipped int
	for _, entry := range resp.Missions {
		if entry.TemplateID == "" {
			skipped++
			continue
		}
		res, err := globalKB.UpsertMissionTemplate(ctx, entry, baseID, systemID, tick)
		if err != nil {
			fmt.Printf("Warning: upsert %s: %v\n", entry.MissionID, err)
			continue
		}
		switch {
		case res.Inserted:
			inserted++
		case len(res.Diffs) > 0:
			changed++
			fmt.Printf("Mission template %q changed at %s:\n", entry.TemplateID, baseID)
			for _, d := range res.Diffs {
				oldFormatted, newFormatted := formatMissionDiffValue(d.Field, d.OldValue, d.NewValue)
				fmt.Printf("  %s: %s -> %s\n", d.Field, oldFormatted, newFormatted)
				fmt.Fprintf(os.Stderr, "mission template %s changed at base %s: field=%s old=%q new=%q\n",
					entry.TemplateID, baseID, d.Field, d.OldValue, d.NewValue)
			}
		default:
			unchanged++
		}
	}

	fmt.Printf("update_missions: %d new, %d unchanged, %d changed, %d procedural skipped\n",
		inserted, unchanged, changed, skipped)
	return nil
}

// kbUpdateFaction collects comprehensive faction data for the current agent's
// faction and persists it to the knowledge base.
func kbUpdateFaction(client game.GameClient, ctx context.Context) error {
	if globalKB == nil {
		return fmt.Errorf("knowledge base not configured (use --db-path)")
	}
	sqlite, ok := globalKB.(*knowledge.SQLiteKB)
	if !ok {
		return fmt.Errorf("faction collection requires a SQLite knowledge base")
	}
	c := faction.NewCollector(sqlite, log.Default())
	if err := c.Collect(ctx, client, true); err != nil {
		return fmt.Errorf("faction collection failed: %w", err)
	}
	fmt.Println("Faction data updated.")
	return nil
}

// cmdSeenFactions lists the distinct factions observed on other agents (from
// the seen_players table), showing which are already captured in the factions
// table. With --seed it enqueues them for background faction_info backfill so
// the factions table fills in. Stale/missing factions are fetched; fresh ones
// are skipped by the backfiller.
func cmdSeenFactions(ctx context.Context, args []string) error {
	if globalKB == nil {
		return fmt.Errorf("knowledge base not configured (use --db-path)")
	}
	sqlite, ok := globalKB.(*knowledge.SQLiteKB)
	if !ok {
		return fmt.Errorf("seen_factions requires a SQLite knowledge base")
	}

	seed := false
	for _, a := range args {
		if a == "--seed" || a == "-s" {
			seed = true
		}
	}

	factions, err := sqlite.ListSeenFactions(ctx)
	if err != nil {
		return fmt.Errorf("list seen factions: %w", err)
	}
	if len(factions) == 0 {
		fmt.Println("  (no factions observed yet)")
		return nil
	}

	fmt.Printf("  %-32s | %-6s | %7s | %9s | %-11s | %s\n",
		"Faction ID", "Tag", "Players", "Sightings", "Last Seen", "Status")
	fmt.Printf("  %s-+-%s-+-%s-+-%s-+-%s-+-%s\n",
		strings.Repeat("-", 32), strings.Repeat("-", 6), strings.Repeat("-", 7),
		strings.Repeat("-", 9), strings.Repeat("-", 11), strings.Repeat("-", 18))

	var missing int
	for _, f := range factions {
		status := "MISSING"
		if f.Seeded {
			status = "seeded"
			if f.Name != "" {
				status = "seeded (" + f.Name + ")"
			}
		} else {
			missing++
		}
		fmt.Printf("  %-32s | %-6s | %7d | %9d | %-11s | %s\n",
			f.FactionID, f.FactionTag, f.PlayerCount, f.Sightings,
			f.LastSeen.Format("01-02 15:04"), status)
	}
	fmt.Printf("  %d faction(s); %d not yet in the factions table\n", len(factions), missing)

	if !seed {
		if missing > 0 {
			fmt.Println("  run 'seen_factions --seed' to backfill the missing ones")
		}
		return nil
	}

	if globalFactionBackfiller == nil {
		return fmt.Errorf("faction backfill unavailable (requires the WebSocket client + --db-path)")
	}
	ids := make([]string, len(factions))
	for i, f := range factions {
		ids[i] = f.FactionID
	}
	globalFactionBackfiller.Enqueue(ids...)
	fmt.Printf("  → enqueued %d faction(s) for background backfill (fresh ones are skipped)\n", len(ids))
	return nil
}

// factionSeedPageSize is the per-request page size for the faction_list seed
// scan. The server caps faction_list at 50 entries per request.
const factionSeedPageSize = 50

// factionSeedMaxPages bounds the seed scan. The server returns factions in an
// unstable order, so offset paging overlaps heavily — a clean 0/50/100 sweep
// misses many entries. We instead poll repeatedly (rotating the offset to
// sample different windows) and accumulate distinct ids until coverage reaches
// total_count, capping the request count so a faction never surfaced can't loop
// forever.
const factionSeedMaxPages = 40

// factionSeedDryStreak stops the scan once this many consecutive pages reveal no
// previously-unseen faction — coverage has plateaued and further polling is
// unlikely to surface the remainder.
const factionSeedDryStreak = 6

// seedFactionsFromList polls faction_list until it has seen every faction (or
// coverage plateaus) and seeds the factions table with the lightweight header
// fields each entry carries (name, tag, leader, member_count, owned_bases,
// colors). It accumulates distinct faction ids across pages because the server
// orders faction_list non-deterministically, so a single offset sweep does not
// enumerate the full set. New factions are inserted with a stale captured_utc so
// a later faction_info backfill still enriches them; existing rows have only
// those columns refreshed, never their richer faction_info-sourced columns.
func seedFactionsFromList(ctx context.Context, client game.GameClient) error {
	if globalKB == nil {
		return fmt.Errorf("knowledge base not configured (use --db-path)")
	}
	sqlite, ok := globalKB.(*knowledge.SQLiteKB)
	if !ok {
		return fmt.Errorf("faction_list --seed requires a SQLite knowledge base")
	}

	seen := map[string]bool{}
	var inserted, refreshed, total int
	dry := 0
	offset := 0

	for range factionSeedMaxPages {
		if err := client.FactionList(ctx, factionSeedPageSize, offset); err != nil {
			return fmt.Errorf("faction_list (offset %d): %w", offset, err)
		}
		raw := client.GetRawJSON("_last")
		if len(raw) == 0 {
			return fmt.Errorf("faction_list (offset %d): empty response", offset)
		}
		var resp struct {
			Factions []struct {
				ID             string `json:"id"`
				Name           string `json:"name"`
				Tag            string `json:"tag"`
				LeaderUsername string `json:"leader_username"`
				MemberCount    int    `json:"member_count"`
				OwnedBases     int    `json:"owned_bases"`
				PrimaryColor   string `json:"primary_color"`
				SecondaryColor string `json:"secondary_color"`
			} `json:"factions"`
			TotalCount int `json:"total_count"`
		}
		if err := json.Unmarshal(raw, &resp); err != nil {
			return fmt.Errorf("faction_list (offset %d): decode: %w", offset, err)
		}
		if resp.TotalCount > 0 {
			total = resp.TotalCount
		}

		newThisPage := 0
		for _, f := range resp.Factions {
			if seen[f.ID] {
				continue
			}
			seen[f.ID] = true
			newThisPage++
			isNew, err := sqlite.UpsertFactionListEntry(ctx, knowledge.FactionListEntry{
				FactionID:      f.ID,
				Name:           f.Name,
				Tag:            f.Tag,
				LeaderUsername: f.LeaderUsername,
				MemberCount:    f.MemberCount,
				OwnedBases:     f.OwnedBases,
				PrimaryColor:   f.PrimaryColor,
				SecondaryColor: f.SecondaryColor,
			})
			if err != nil {
				return fmt.Errorf("seed faction %s: %w", f.ID, err)
			}
			if isNew {
				inserted++
			} else {
				refreshed++
			}
		}

		// Done once we've covered the whole set.
		if total > 0 && len(seen) >= total {
			break
		}
		// Stop if coverage has stalled across several consecutive pages.
		if newThisPage == 0 {
			dry++
			if dry >= factionSeedDryStreak {
				break
			}
		} else {
			dry = 0
		}

		// Rotate the offset window to sample a different slice next time; wrap
		// once we've walked past the end of the (unstably ordered) set.
		offset += factionSeedPageSize
		if total > 0 && offset >= total {
			offset = 0
		}

		// Space out page requests so a full scan doesn't hammer the server.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(game.SleepQuick):
		}
	}

	if total > 0 && len(seen) < total {
		fmt.Printf("  ⚠ seeded %d of %d faction(s) (%d new, %d refreshed) — server did not surface the rest within %d pages; re-run to pick up stragglers\n",
			len(seen), total, inserted, refreshed, factionSeedMaxPages)
		return nil
	}
	fmt.Printf("  → seeded %d faction(s): %d new, %d refreshed\n", len(seen), inserted, refreshed)
	return nil
}
