// Command mission-bind records that a mission template is offered at exactly
// one station.
//
// A mission's location rows say where it has been SEEN on a board, which
// cannot express where it can be TAKEN. The authoritative signal is the
// accept_mission refusal ("This mission is only available at X"), and play_as
// records that automatically. But a refusal only happens while the mission is
// unaccepted: once an agent takes it, the constraint we learned has no path
// into the database. frontier_extension is exactly that case — refused at one
// station on 2026-09-18, then accepted at Alpha Centauri Colonial Station
// before the column existed to hold it.
//
// This tool closes that gap by hand. It resolves the display name to a base id
// the same way the refusal path does — the resolved id is stored, never the
// name — and refuses to write anything if either the station or the mission is
// unknown.
//
// Usage:
//
//	mission-bind -mission frontier_extension -station "Alpha Centauri Colonial Station" -tick 1917226
//	mission-bind -mission frontier_extension -station "..." -show
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// binder is the slice of the knowledge base this tool needs, so the unit
// tests exercise the real SQLiteKB rather than a mock of it.
type binder interface {
	ResolveStationName(ctx context.Context, name string) (baseID, systemID string, err error)
	SetMissionExclusiveBaseInSystem(ctx context.Context, missionID, baseID, systemID, source string, tick int64) error
}

// bind resolves stationName to a base id and records it as missionID's only
// giver. Resolution happens first and its failure is returned untouched, so an
// unknown station never reaches the write — recording a name we could not
// resolve would be worse than recording nothing, because a wrong exclusive
// binding tells every future agent to fly to the wrong place.
func bind(ctx context.Context, kb binder, missionID, stationName, source string, tick int64, out io.Writer) error {
	baseID, systemID, err := kb.ResolveStationName(ctx, stationName)
	if err != nil {
		return fmt.Errorf("resolve %q: %w", stationName, err)
	}
	fmt.Fprintf(out, "resolved %q -> base %s (system %s)\n", stationName, baseID, systemID) //nolint:errcheck

	if err := kb.SetMissionExclusiveBaseInSystem(ctx, missionID, baseID, systemID, source, tick); err != nil {
		return fmt.Errorf("bind %s: %w", missionID, err)
	}
	fmt.Fprintf(out, "bound %s -> %s (source %s, tick %d)\n", missionID, baseID, source, tick) //nolint:errcheck

	return nil
}

func main() {
	dbPath := flag.String("db-path", "data/spacemolt-knowledge.db", "Knowledge base path")
	mission := flag.String("mission", "", "Mission template id (required)")
	station := flag.String("station", "", "Station display name as the server reported it (required unless -show)")
	source := flag.String("source", "operator", "Provenance stamped on the binding")
	tick := flag.Int64("tick", 0, "Game tick the constraint was observed at")
	show := flag.Bool("show", false, "Print the current binding and exit without writing")
	flag.Parse()

	if *mission == "" {
		fmt.Fprintln(os.Stderr, "mission-bind: -mission is required") //nolint:errcheck
		flag.Usage()
		os.Exit(2)
	}

	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: *dbPath})
	if err != nil {
		fmt.Fprintf(os.Stderr, "mission-bind: open kb: %v\n", err) //nolint:errcheck
		os.Exit(1)
	}
	defer kb.Close() //nolint:errcheck

	ctx := context.Background()
	if *show {
		b, err := kb.MissionExclusiveBase(ctx, *mission)
		if err != nil {
			fmt.Fprintf(os.Stderr, "mission-bind: %v\n", err) //nolint:errcheck
			os.Exit(1)
		}
		if b == nil {
			fmt.Printf("%s: no exclusive binding recorded\n", *mission)

			return
		}
		fmt.Printf("%s: base=%s system=%s source=%s tick=%d seen=%s\n",
			*mission, b.BaseID, b.SystemID, b.Source, b.Tick, b.SeenAt)

		return
	}

	if *station == "" {
		fmt.Fprintln(os.Stderr, "mission-bind: -station is required") //nolint:errcheck
		os.Exit(2)
	}

	if err := bind(ctx, kb, *mission, *station, *source, *tick, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "mission-bind: %v\n", err) //nolint:errcheck
		if errors.Is(err, knowledge.ErrMissionUnknown) {
			fmt.Fprintln(os.Stderr, "  (the template has no row yet — it must be seen on a board first)") //nolint:errcheck
		}
		os.Exit(1)
	}
}
