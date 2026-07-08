package main

import (
	"encoding/json"
	"os"
	"sort"
	"strings"

	"github.com/rsned/spacemolt/pkg/game"
)

// metaCommands are REPL-only commands not present in the OpenAPI spec.
var metaCommands = []string{
	"help", "exit", "quit", "set_format", "set_debug", "loop", "mbox", "nearest",
	"update_market", "find_item", "show_system",
}

// loadCompletionCommands returns a sorted, deduplicated list of command
// names to offer for tab completion. It reads command names from the
// OpenAPI document at openapiPath (paths are of the form "/<command>") and
// merges in the REPL meta-commands.
//
// If the file cannot be read or parsed, it returns just the meta-commands
// so completion degrades gracefully rather than failing REPL startup.
func loadCompletionCommands(openapiPath string) []string {
	set := make(map[string]struct{}, 200)
	for _, c := range metaCommands {
		set[c] = struct{}{}
	}

	data, err := os.ReadFile(openapiPath)
	if err == nil {
		var doc struct {
			Paths map[string]any `json:"paths"`
		}
		if json.Unmarshal(data, &doc) == nil {
			for p := range doc.Paths {
				name := strings.TrimPrefix(p, "/")
				if name == "" || strings.ContainsAny(name, "/{") {
					continue
				}
				set[name] = struct{}{}
			}
		}
	}

	out := make([]string, 0, len(set))
	for c := range set {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// makeCompleter returns a liner completion function that completes commands
// and, for a handful of navigation commands, their first argument based on
// current system state from the game client.
//
//   - Bare first word: case-insensitive prefix match against commands.
//   - "dock <prefix>":   station-type POIs in the current system.
//   - "travel <prefix>": every POI ID in the current system.
//   - "jump <prefix>":   connected system IDs/names.
//
// Once a second space is present (i.e. a second argument is being typed),
// completion is suppressed so free-form arguments aren't clobbered.
func makeCompleter(commands []string, client game.GameClient) func(string) []string {
	return func(line string) []string {
		head, rest, hasSpace := strings.Cut(line, " ")
		if !hasSpace {
			prefix := strings.ToLower(line)
			var matches []string
			for _, c := range commands {
				if strings.HasPrefix(c, prefix) {
					matches = append(matches, c)
				}
			}
			return matches
		}

		cmd := strings.ToLower(head)
		if strings.Contains(rest, " ") {
			return nil
		}

		targets := targetCandidates(cmd, client)
		if targets == nil {
			return nil
		}
		sort.Strings(targets)

		prefix := strings.ToLower(rest)
		completed := make([]string, 0, len(targets))
		for _, t := range targets {
			if strings.HasPrefix(strings.ToLower(t), prefix) {
				// liner replaces the whole line, so rebuild "<cmd> <target>".
				completed = append(completed, cmd+" "+t)
			}
		}
		return completed
	}
}

// targetCandidates returns argument candidates for the given command using
// current game state, or nil if the command has no argument completion.
func targetCandidates(cmd string, client game.GameClient) []string {
	if client == nil {
		return nil
	}
	state := client.GetState()
	if state == nil {
		return nil
	}
	switch cmd {
	case "mbox":
		return []string{"list", "show", "search", "mark-read", "delete", "restore", "backfill", "sources"}
	case "nearest":
		return []string{"station", "asteroid", "black_market", "base", "enemy", "gas_giant", "nebula", "outpost", "planet", "ruin", "star", "wreck"}
	case "dock":
		var out []string
		for _, poi := range state.System.POIs {
			if poi.Type == "station" || poi.Type == "outpost" {
				out = append(out, poi.ID)
			}
		}
		return out
	case "travel":
		out := make([]string, 0, len(state.System.POIs))
		for _, poi := range state.System.POIs {
			out = append(out, poi.ID)
		}
		return out
	case "jump":
		// SystemIDs only — system Names may contain spaces which break
		// liner's longest-common-prefix calculation and clobber typed input.
		out := make([]string, 0, len(state.System.Connections))
		for _, conn := range state.System.Connections {
			if conn.SystemID != "" {
				out = append(out, conn.SystemID)
			}
		}
		return out
	}
	return nil
}
