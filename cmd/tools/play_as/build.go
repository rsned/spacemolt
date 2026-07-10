package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/rsned/spacemolt/pkg/craftbrain"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// parseBuildArgs parses: build <target> [qty] [--json] [--max-hand-ticks=N]
func parseBuildArgs(args []string) (string, int, bool, error) {
	if len(args) == 0 {
		return "", 0, false, fmt.Errorf("usage: build <target> [qty] [--json]")
	}
	target := args[0]
	qty := 1
	jsonOut := false
	for _, a := range args[1:] {
		switch {
		case a == "--json":
			jsonOut = true
		case strings.HasPrefix(a, "--max-hand-ticks="):
			// Consumed by runBuild, but must be accepted here so a correct
			// spelling never falls through to the unknown-flag error below.
		case strings.HasPrefix(a, "--"):
			// Reject rather than ignore: a typo like --max-hand-tick=50 would
			// otherwise be silently dropped and the plan would quietly use the
			// default time budget, with nothing telling the operator.
			return "", 0, false, fmt.Errorf("unknown flag %q (supported: --json, --max-hand-ticks=N)", a)
		default:
			n, err := strconv.Atoi(a)
			if err != nil {
				return "", 0, false, fmt.Errorf("quantity %q is not a number", a)
			}
			if n < 1 {
				return "", 0, false, fmt.Errorf("quantity must be >= 1, got %d", n)
			}
			qty = n
		}
	}
	return target, qty, jsonOut, nil
}

// runBuild implements: build <target> [qty] [--json]
// Computes the full recursive work to build the target and prints it for
// review. Read-only — it dispatches nothing.
func runBuild(client game.GameClient, ctx context.Context, args []string) error {
	target, qty, jsonOut, err := parseBuildArgs(args)
	if err != nil {
		return err
	}
	if globalKB == nil {
		return fmt.Errorf("build: knowledge DB not available (run with --db-path)")
	}
	sk, ok := globalKB.(*knowledge.SQLiteKB)
	if !ok {
		return fmt.Errorf("build: knowledge DB does not support facility queries")
	}

	// GetState() returns *State; State.System is a SystemData VALUE, not a
	// pointer (pkg/game/types.go:367). Nil-check only the state.
	originSystem := ""
	if st := client.GetState(); st != nil {
		originSystem = st.System.ID
	}

	opts := craftbrain.DefaultOptions()
	for _, a := range args[1:] {
		if v, found := strings.CutPrefix(a, "--max-hand-ticks="); found {
			n, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return fmt.Errorf("--max-hand-ticks %q is not a number", v)
			}
			opts.MaxHandTicks = n
		}
	}

	src := newCraftbrainSource(sk, globalMarketCollector, originSystem)
	plan, err := craftbrain.New(src).Plan(ctx, target, qty, opts)
	if err != nil {
		return err
	}

	if jsonOut {
		out, err := json.MarshalIndent(plan, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(out))
		return nil
	}
	fmt.Print(craftbrain.Format(plan))
	return nil
}
