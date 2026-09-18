// Command rescue-clear removes stranded-worker rescue records from the shared
// queue so the owning overmind releases those agents from quarantine.
//
// A quarantined agent is never launched: restoreQuarantine runs before the
// supervisor starts workers, and pollRescues only releases an agent when its
// record is gone ("no record for quarantined %s; releasing"). So a record that
// no longer describes reality — most often a full-fuel "strandee" manufactured
// by a reconnect wedge — parks a healthy agent indefinitely, and restarting the
// overmind does not help.
//
// Removal goes through rescue.Queue.Remove rather than editing the JSON: the
// queue is shared by every fleet's overmind and guarded by a sidecar flock, so
// a hand-edit races four live writers.
//
// Usage:
//
//	rescue-clear -agents trader-3,salvager-1,salvager-9,trader-10
//	rescue-clear -agents hauler-0 -dry-run
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/rsned/spacemolt/pkg/rescue"
)

// clearRecords removes each named agent's record from the queue and appends it
// to the history jsonl, mirroring the overmind's own archiveRescue. An agent
// with no record is reported and skipped rather than treated as an error:
// the common case for that is a record the supervisor archived on its own
// between listing and clearing. Returns the number actually removed.
func clearRecords(q *rescue.Queue, histPath string, agentIDs []string, out io.Writer) (int, error) {
	removed := 0
	for _, id := range agentIDs {
		rec, err := q.Remove(id)
		if err != nil {
			return removed, fmt.Errorf("remove %s: %w", id, err)
		}
		if rec == nil {
			fmt.Fprintf(out, "  %-12s no record in queue (skipped)\n", id) //nolint:errcheck
			continue
		}
		removed++
		//nolint:errcheck // operator-facing progress line; a broken stdout is not worth aborting the clear
		fmt.Fprintf(out, "  %-12s removed (%s @ %s, fuel %.0f/%.0f)\n",
			id, rec.Fleet, rec.System, rec.Fuel, rec.MaxFuel)
		if err := appendHistory(histPath, rec); err != nil {
			// The record is already out of the queue, which is the part that
			// matters; a lost history line must not abort the remaining ids.
			fmt.Fprintf(out, "  %-12s WARNING: history append failed: %v\n", id, err) //nolint:errcheck
		}
	}
	return removed, nil
}

// appendHistory writes rec as one json line to the rescue history archive.
func appendHistory(path string, rec *rescue.Record) error {
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck
	_, err = f.Write(append(line, '\n'))

	return err
}

func main() {
	queuePath := flag.String("queue", "data/overmind/rescue-queue.json", "Shared stranded-worker rescue queue file")
	histPath := flag.String("history", "data/overmind/rescue-history.jsonl", "Archive of completed rescue records")
	agents := flag.String("agents", "", "Comma-separated agent ids whose rescue records to clear (required)")
	dryRun := flag.Bool("dry-run", false, "List the matching records and exit without removing anything")
	flag.Parse()

	ids := splitIDs(*agents)
	if len(ids) == 0 {
		fmt.Fprintln(os.Stderr, "rescue-clear: -agents is required") //nolint:errcheck
		flag.Usage()
		os.Exit(2)
	}

	q := rescue.NewQueue(*queuePath)
	if *dryRun {
		recs, err := q.List()
		if err != nil {
			fmt.Fprintf(os.Stderr, "rescue-clear: list: %v\n", err) //nolint:errcheck
			os.Exit(1)
		}
		want := make(map[string]bool, len(ids))
		for _, id := range ids {
			want[id] = true
		}
		fmt.Printf("dry run — %d record(s) in %s:\n", len(recs), *queuePath)
		for _, r := range recs {
			mark := "keep  "
			if want[r.AgentID] {
				mark = "REMOVE"
			}
			fmt.Printf("  %s %-12s %-6s %-12s fuel %.0f/%.0f status %s\n",
				mark, r.AgentID, r.Fleet, r.System, r.Fuel, r.MaxFuel, r.Status)
		}

		return
	}

	n, err := clearRecords(q, *histPath, ids, os.Stdout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rescue-clear: %v\n", err) //nolint:errcheck
		os.Exit(1)
	}
	fmt.Printf("cleared %d record(s); the owning overmind releases them on its next rescue poll\n", n)
}

// splitIDs parses the comma-separated -agents value, dropping empties so a
// trailing comma or stray space does not become a phantom agent id.
func splitIDs(s string) []string {
	var ids []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			ids = append(ids, p)
		}
	}

	return ids
}
