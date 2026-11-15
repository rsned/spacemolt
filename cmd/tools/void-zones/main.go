// Command void-zones builds and reports on the interior void sweep ledger.
//
//	void-zones generate  # find void zones in the KB and write a fresh run
//	void-zones status    # one line per zone: claimant, progress, finds
//	void-zones map --out map.html
//
// Spec: docs/superpowers/specs/2026-11-14-interior-void-sweep-design.md
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/sweep"
)

// laneSpacing is the operator's 100 LY between square-wave lanes.
const laneSpacing = 100

func main() {
	kbPath := flag.String("kb", "data/spacemolt-knowledge.db", "knowledge DB")
	runPath := flag.String("run", "data/sweep/void-run.json", "void run ledger")
	out := flag.String("out", "void_map.html", "map: output HTML file")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: void-zones [flags] generate|status|map") //nolint:errcheck
		os.Exit(2)
	}
	if err := run(flag.Arg(0), *kbPath, *runPath, *out); err != nil {
		fmt.Fprintln(os.Stderr, "void-zones:", err) //nolint:errcheck
		os.Exit(1)
	}
}

func run(cmd, kbPath, runPath, out string) error {
	switch cmd {
	case "generate":
		pts, err := systems(kbPath)
		if err != nil {
			return err
		}
		if err := generate(pts, runPath, time.Now()); err != nil {
			return err
		}
		return status(runPath)
	case "status":
		return status(runPath)
	case "map":
		pts, err := systems(kbPath)
		if err != nil {
			return err
		}
		r, err := sweep.LoadVoidRun(runPath)
		if err != nil {
			return err
		}
		finds, err := foundSystems(kbPath, r)
		if err != nil {
			return err
		}
		return os.WriteFile(out, []byte(renderMap(r, pts, finds)), 0o644)
	}
	return fmt.Errorf("unknown command %q", cmd)
}

// foundSystems places every system the run has found, from the KB.
func foundSystems(kbPath string, r *sweep.VoidRun) ([]foundSys, error) {
	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: kbPath, WAL: true})
	if err != nil {
		return nil, err
	}
	defer kb.Close() //nolint:errcheck
	var out []foundSys
	for _, z := range r.Zones {
		for _, id := range z.Found {
			s, gerr := kb.GetSystem(context.Background(), id)
			if gerr != nil || s == nil {
				continue // not in the KB yet: the table still lists it
			}
			out = append(out, foundSys{ID: id, Name: s.Name, X: s.Position.X, Y: s.Position.Y})
		}
	}
	return out, nil
}

// systems is every KB system with a known position (Sol is the only system
// that really sits at 0,0).
func systems(kbPath string) ([]sweep.Point, error) {
	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: kbPath, WAL: true})
	if err != nil {
		return nil, err
	}
	defer kb.Close() //nolint:errcheck
	all, err := kb.GetSystems(context.Background())
	if err != nil {
		return nil, err
	}
	pts := make([]sweep.Point, 0, len(all))
	for _, s := range all {
		if s.Position.X == 0 && s.Position.Y == 0 && s.ID != "sol" {
			continue
		}
		pts = append(pts, sweep.Point{X: s.Position.X, Y: s.Position.Y})
	}
	return pts, nil
}

// generate writes a fresh run over the void zones among pts. It refuses to
// replace a run that has claims or progress: archive that file first.
func generate(pts []sweep.Point, runPath string, now time.Time) error {
	old, err := sweep.LoadVoidRun(runPath)
	switch {
	case err == nil && old.Active():
		return fmt.Errorf("%s has claims or progress; archive it (move it aside) before generating a new run", runPath)
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return err
	}
	zones := sweep.ComputeZones(pts, sweep.DefaultZoneOpts())
	return sweep.SaveVoidRun(runPath, sweep.NewVoidRun(zones, laneSpacing, now))
}

// status prints one line per zone.
func status(runPath string) error {
	r, err := sweep.LoadVoidRun(runPath)
	if err != nil {
		return err
	}
	done, area := 0, 0.0
	for _, z := range r.Zones {
		state := "open"
		switch {
		case z.Done:
			state, done = "done", done+1
		case z.Claimant != "":
			state = z.Claimant
		}
		area += z.Area
		fmt.Printf("%s  %6.2fM LY²  centre (%6.0f,%6.0f)  lanes %3d  %3d/%-3d  %-12s found %d\n", //nolint:errcheck
			z.ID, z.Area/1e6, z.CX, z.CY, len(z.Waypoints)/2, z.Next, len(z.Waypoints), state, len(z.Found))
	}
	fmt.Printf("%d zones, %.1fM LY², %d done (run created %s)\n", len(r.Zones), area/1e6, done, r.Created) //nolint:errcheck
	return nil
}
