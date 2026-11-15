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

const (
	// intelPageSize is faction_query_intel's maximum limit.
	intelPageSize = 100
	// intelMaxPages bounds one capture (10,000 systems) so a server that keeps
	// reporting a larger total cannot loop forever.
	intelMaxPages = 100
)

// captureFactionIntel copies the faction's whole intel pool (the Intel
// Center's systems, POIs, deposits, lanes and v0.716.0 hazards) into the
// shared KB, one page at a time, so tools and other agents can query it
// without a faction officer's session. Returns the number of systems stored.
//
// Each page is checked against the offset it asked for: GetRawJSON hands back
// the last stored reply, and storing a stale page again under a later offset
// would be harmless but silent, while a reply for the wrong page means the
// query did not land, so the capture stops with an error instead of guessing.
func captureFactionIntel(ctx context.Context, client game.GameClient, store knowledge.FactionIntelStore,
	factionID string, out io.Writer,
) (int, error) {
	if store == nil || factionID == "" {
		return 0, nil
	}
	captured := time.Now().UTC().Format(time.RFC3339)
	stored := 0
	for page := range intelMaxPages {
		offset := page * intelPageSize
		if err := client.FactionQueryIntel(ctx, map[string]any{"limit": intelPageSize, "offset": offset}); err != nil {
			return stored, fmt.Errorf("faction_query_intel offset %d: %w", offset, err)
		}
		var resp serverapi.FactionQueryIntelResponse
		if err := json.Unmarshal(client.GetRawJSON("faction_intel"), &resp); err != nil {
			return stored, fmt.Errorf("decode intel page %d: %w", offset, err)
		}
		if resp.Offset != offset {
			return stored, fmt.Errorf("intel page reply names offset %d, asked for %d", resp.Offset, offset)
		}
		entries, err := resp.IntelEntries()
		if err != nil {
			return stored, err
		}
		if len(entries) == 0 {
			break
		}
		if err := store.StoreFactionIntel(ctx, factionID, entries, resp.CurrentTick, captured); err != nil {
			return stored, err
		}
		stored += len(entries)
		if resp.Total > 0 && offset+len(entries) >= resp.Total {
			break
		}
	}
	fmt.Fprintf(out, "faction intel: %d systems stored for %s\n", stored, factionID) //nolint:errcheck

	return stored, nil
}
