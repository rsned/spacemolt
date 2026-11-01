package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// CraftQueueFile is the craft queue capture in data/agents/<agent-id>/. The
// line dashboard reads it for a line's queue backlog.
const CraftQueueFile = "craft-queue.json"

// craftQueueJob is one queued job as the dashboard reads it.
type craftQueueJob struct {
	Recipe        string `json:"recipe"`
	RunsRemaining int    `json:"runs_remaining"`
	ETATicks      int    `json:"eta_ticks"`
	Position      int    `json:"position"`
	FacilityID    string `json:"facility_id"`
	BaseID        string `json:"base_id"`
	Status        string `json:"status"`
}

type craftQueueFile struct {
	CapturedAt time.Time       `json:"captured_at"`
	Jobs       []craftQueueJob `json:"jobs"`
}

// CaptureCraftQueue lists this agent's queued craft jobs (`craft` with no
// recipe, action=queue) and writes them to agentDir/CraftQueueFile. A
// facility's backlog is the sum over our agents with jobs on it; hand crafts
// queue per agent.
//
// An empty listing is written: no jobs is an observation. A reply that is not
// a queue listing is refused and the last good capture is kept, because
// GetRawJSON("_last") can hand back another command's reply.
func CaptureCraftQueue(ctx context.Context, client game.GameClient, agentDir string, now time.Time) (int, error) {
	if err := client.RawCommand(ctx, "craft", map[string]any{"action": "queue"}); err != nil {
		return 0, fmt.Errorf("capture_craft_queue: %w", err)
	}
	var listing serverapi.CraftQueueListing
	if err := json.Unmarshal(unwrapActionResult(client.GetRawJSON("_last")), &listing); err != nil {
		return 0, fmt.Errorf("capture_craft_queue: decode: %w", err)
	}
	if listing.Action != "queue" && listing.Action != "job_list" {
		return 0, fmt.Errorf("capture_craft_queue: reply is not a queue listing (action %q)", listing.Action)
	}

	out := craftQueueFile{CapturedAt: now.UTC(), Jobs: make([]craftQueueJob, 0, len(listing.Jobs))}
	for _, j := range listing.Jobs {
		out.Jobs = append(out.Jobs, craftQueueJob{
			Recipe: j.Recipe, RunsRemaining: j.RunsRemaining, ETATicks: j.ETATicks,
			Position: j.Position, FacilityID: j.FacilityID, BaseID: j.BaseID, Status: j.Status,
		})
	}
	raw, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return 0, fmt.Errorf("capture_craft_queue: %w", err)
	}
	path := filepath.Join(agentDir, CraftQueueFile)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil { //nolint:gosec // operator-readable by design
		return 0, fmt.Errorf("capture_craft_queue: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return 0, fmt.Errorf("capture_craft_queue: %w", err)
	}

	return len(out.Jobs), nil
}
