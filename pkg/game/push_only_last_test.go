package game

import (
	"io"
	"log"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/internal/protocol"
)

// TestStoreRawJSON_CraftingUpdateDoesNotClobberLast pins the fix for the
// crafting_update "_last" clobber bug: crafting_update is a server-initiated
// push (see the OnCraftingUpdate callback ~client.go:2512), never a reply to
// a client command. Before protocol.TypeCraftingUpdate was added to
// pushOnlyResponseTypes, a crafting_update tick arriving on the read-loop
// goroutine between a craft command's send and the caller's GetRawJSON("_last")
// read would silently overwrite "_last" with the crafting_update payload.
// CraftingUpdateEvent and CraftQueueListing share jobs/job_id/runs_remaining
// JSON tags, so the clobbered payload decodes cleanly as a queue listing and
// the craft verb's absence-means-done heuristic reports a still-running job
// as finished (silent premature success).
func TestStoreRawJSON_CraftingUpdateDoesNotClobberLast(t *testing.T) {
	c := &Client{
		latestRawJSON: make(map[string][]byte),
		debugLogger:   log.New(io.Discard, "", 0),
	}

	// Simulate the craft verb having just captured its command reply under
	// "_last" (e.g. the job-id capture from a "craft" command's ok response).
	c.storeRawJSON(protocol.Response{
		Type: protocol.TypeOK,
		Payload: map[string]any{
			"action": "craft",
			"job_id": "job-abc123",
		},
	})

	before := string(c.GetRawJSON("_last"))
	if !strings.Contains(before, "job-abc123") {
		t.Fatalf("setup failed: _last = %q, want it to contain job-abc123", before)
	}

	// A crafting_update push arrives on the read-loop goroutine (e.g. the
	// next server tick) before the craft verb gets around to reading "_last".
	c.storeRawJSON(protocol.Response{
		Type: protocol.TypeCraftingUpdate,
		Payload: map[string]any{
			"tick": 1300000,
			"jobs": []any{
				map[string]any{"job_id": "job-other-999", "runs_remaining": 3},
			},
		},
	})

	after := string(c.GetRawJSON("_last"))
	if after != before {
		t.Errorf("crafting_update clobbered _last: before=%q after=%q", before, after)
	}
	if strings.Contains(after, "job-other-999") {
		t.Errorf("_last was overwritten with crafting_update payload: %q", after)
	}
}
