package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// handleBulkUploadDroneScript implements the `bulk_upload_drone_script` REPL
// meta-command. It reads a script body from --file, fetches the current drone
// roster via GetDrones, and runs UploadDroneScript for each matching drone.
//
// upload_drone_script is a mutation (1/tick / 10 s), so this command is
// inherently sequential — N drones takes ~N ticks of real time. The REPL
// stays blocked but prints per-drone progress so the operator can see it
// advancing. The roster filter (--type / --status / --name) lets the operator
// avoid wasting ticks on drones the script doesn't apply to.
//
// Usage:
//
//	bulk_upload_drone_script --file <path> [--type <substr>] [--status <substr>]
//	                         [--name <substr>] [--dry_run]
func handleBulkUploadDroneScript(client game.GameClient, ctx context.Context, parts []string) error {
	_, flags := partitionFlags(parts[1:])

	path := flags["file"]
	if path == "" {
		return fmt.Errorf("usage: bulk_upload_drone_script --file <path> [--type T] [--status S] [--name N] [--dry_run]")
	}

	body, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read script %q: %w", path, err)
	}
	script := strings.TrimRight(string(body), "\r\n\t ")
	if script == "" {
		return fmt.Errorf("script file %q is empty (pass an empty script via upload_drone_script if that's intended)", path)
	}

	// Refresh the drone roster: this hits the server, then we parse the raw
	// JSON via the same shape the standard formatter uses.
	if err := client.GetDrones(ctx); err != nil {
		return fmt.Errorf("get_drones: %w", err)
	}
	raw := client.GetRawJSON("drones")
	if len(raw) == 0 {
		return fmt.Errorf("get_drones returned no payload")
	}
	type droneRow struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Type   string `json:"type"`
		Status string `json:"status"`
	}
	var resp struct {
		Drones []droneRow `json:"drones"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return fmt.Errorf("decode drones: %w", err)
	}

	typeFilter := strings.ToLower(flags["type"])
	statusFilter := strings.ToLower(flags["status"])
	nameFilter := strings.ToLower(flags["name"])
	// partitionFlags stores bare flags (`--dry_run`) as "", which collides
	// with map[string]string's zero value for absent keys — use the
	// presence-aware helper so a missing flag stays false.
	dryRun := partitionFlagBool(flags, "dry_run")

	matches := make([]droneRow, 0, len(resp.Drones))
	for _, d := range resp.Drones {
		if typeFilter != "" && !strings.Contains(strings.ToLower(d.Type), typeFilter) {
			continue
		}
		if statusFilter != "" && !strings.Contains(strings.ToLower(d.Status), statusFilter) {
			continue
		}
		if nameFilter != "" && !strings.Contains(strings.ToLower(d.Name), nameFilter) {
			continue
		}
		matches = append(matches, d)
	}

	if len(matches) == 0 {
		fmt.Printf("No drones matched (roster has %d).\n", len(resp.Drones))
		return nil
	}

	// upload_drone_script is rate-limited at 1 mutation per tick; advertise
	// expected wall time so the operator knows what they're committing to.
	expected := time.Duration(len(matches)) * game.SleepTick
	mode := "uploading"
	if dryRun {
		mode = "would upload to"
	}
	fmt.Printf("%s %d/%d drone(s) — script: %s (%d bytes); estimated %s @ 1 tick/drone\n",
		mode, len(matches), len(resp.Drones), path, len(script), expected)

	if dryRun {
		for _, d := range matches {
			label := d.ID
			if d.Name != "" {
				label = fmt.Sprintf("%s (%s)", d.Name, d.ID)
			}
			fmt.Printf("  - %s [%s, %s]\n", label, d.Type, d.Status)
		}
		return nil
	}

	ok, fail := 0, 0
	for i, d := range matches {
		label := d.ID
		if d.Name != "" {
			label = fmt.Sprintf("%s (%s)", d.Name, d.ID)
		}
		fmt.Printf("[%d/%d] uploading to %s [%s, %s]... ", i+1, len(matches), label, d.Type, d.Status)
		if err := client.UploadDroneScript(ctx, d.ID, script); err != nil {
			fmt.Printf("✗ %v\n", err)
			fail++
			// Continue with the rest — partial success is usually still useful.
			continue
		}
		fmt.Println("✓")
		ok++
	}
	fmt.Printf("\nDone: %d succeeded, %d failed.\n", ok, fail)
	return nil
}
