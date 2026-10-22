// clerk-sync records each owned Clerk player's id in the matching agent's
// credentials.json. It is a one-shot operator tool; run with --dry-run first.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/rsned/spacemolt/pkg/game/clerk"
)

func main() {
	agents := flag.String("agents", "data/agents", "agents directory")
	dryRun := flag.Bool("dry-run", false, "report what would change without writing")
	flag.Parse()

	key, err := clerk.LoadKey()
	if err != nil {
		if errors.Is(err, clerk.ErrNoKey) {
			fmt.Fprintln(os.Stderr, "set SPACEMOLT_CLERK_API_KEY or ~/.config/spacemolt/clerk_api_key")
		} else {
			fmt.Fprintln(os.Stderr, "load clerk key:", err)
		}
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	players, err := clerk.NewSource(key).ListPlayers(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "list players:", err)
		os.Exit(1)
	}

	res, err := Sync(*agents, players, *dryRun)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sync:", err)
		os.Exit(1)
	}

	if *dryRun {
		fmt.Println("dry run: nothing written")
	}
	report("Written", res.Written)
	report("Unchanged", res.Unchanged)
	report("Unmatched", res.Unmatched)
	report("Orphans", res.Orphans)
	report("Ambiguous", res.Ambiguous)
	if len(res.Ambiguous) > 0 {
		os.Exit(1)
	}
}

func report(label string, items []string) {
	fmt.Printf("%s: %d\n", label, len(items))
	// Written and Unchanged are the bulk case; listing them adds noise.
	if label == "Written" || label == "Unchanged" {
		return
	}
	for _, it := range items {
		fmt.Printf("  %s\n", it)
	}
}
