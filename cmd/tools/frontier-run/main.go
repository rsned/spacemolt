// Command frontier-run builds and reports on the frontier full-explore ledger.
//
//	frontier-run generate  # every frontier system in the KB into a fresh run
//	frontier-run status    # counts, per-wedge progress, what is incomplete
//	frontier-run assign    # a proposed ship -> compass direction table
//	frontier-run relics    # the relics that responded to interact_poi
//
// Spec: docs/superpowers/specs/2026-11-18-frontier-full-explore-design.md
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"math"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/sweep"
)

// defaultRoster is the operator's eight explorers for the run.
const defaultRoster = "explorer-1,explorer-2,explorer-5,explorer-6,explorer-8,explorer-9,explorer-11,explorer-12"

func main() {
	kbPath := flag.String("kb", "data/spacemolt-knowledge.db", "knowledge DB")
	runPath := flag.String("run", "data/sweep/frontier-run.json", "frontier run ledger")
	ships := flag.String("ships", defaultRoster, "assign: the ships, comma-separated")
	reserves := flag.String("reserves", "", "assign: the ships among them with the longest range (they take the remotest directions)")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: frontier-run [flags] generate|status|assign|relics") //nolint:errcheck
		os.Exit(2)
	}
	if err := run(flag.Arg(0), *kbPath, *runPath, split(*ships), split(*reserves)); err != nil {
		fmt.Fprintln(os.Stderr, "frontier-run:", err) //nolint:errcheck
		os.Exit(1)
	}
}

func split(csv string) []string {
	var out []string
	for _, s := range strings.Split(csv, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func run(cmd, kbPath, runPath string, ships, reserves []string) error {
	ctx := context.Background()
	if cmd == "status" {
		return status(os.Stdout, runPath)
	}
	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: kbPath, WAL: true})
	if err != nil {
		return err
	}
	defer kb.Close() //nolint:errcheck
	switch cmd {
	case "generate":
		if err := generate(ctx, kb, runPath, time.Now()); err != nil {
			return err
		}
		return status(os.Stdout, runPath)
	case "assign":
		r, err := sweep.LoadFrontierRun(runPath)
		if err != nil {
			return err
		}
		stations, err := stationPoints(ctx, kb)
		if err != nil {
			return err
		}
		printAssign(os.Stdout, assign(r, stations, ships, reserves))
		return nil
	case "relics":
		return relics(ctx, os.Stdout, kb)
	}
	return fmt.Errorf("unknown command %q", cmd)
}

// frontierInputs reads the frontier out of the KB: every system whose
// position did not come from the map API, the lanes between systems, and a
// standing reason never to enter the ones holding a Repeater Monolith or a
// leviathan.
func frontierInputs(ctx context.Context, kb *knowledge.SQLiteKB) ([]sweep.FrontierInput, [][2]string, error) {
	lethal, err := lethalSystems(ctx, kb)
	if err != nil {
		return nil, nil, err
	}
	rows, err := kb.DB().QueryContext(ctx, `
		SELECT id, name, position_x, position_y FROM systems
		WHERE position_source != 'api' AND NOT (position_x = 0 AND position_y = 0)
		ORDER BY id`)
	if err != nil {
		return nil, nil, fmt.Errorf("frontier systems: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	var systems []sweep.FrontierInput
	for rows.Next() {
		var in sweep.FrontierInput
		if err = rows.Scan(&in.ID, &in.Name, &in.X, &in.Y); err != nil {
			return nil, nil, fmt.Errorf("frontier systems: %w", err)
		}
		in.Lethal = lethal[in.ID]
		systems = append(systems, in)
	}
	if err = rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("frontier systems: %w", err)
	}
	conns, err := kb.GetConnections(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("lanes: %w", err)
	}
	lanes := make([][2]string, 0, len(conns))
	for _, c := range conns {
		lanes = append(lanes, [2]string{c.FromSystem, c.ToSystem})
	}
	return systems, lanes, nil
}

// lethalSystems maps a system id to why no explorer should enter it: a
// Repeater Monolith seen there, or a leviathan (explorer-8 was killed by a
// Molt Leviathan at Oxbow, 2026-11-11).
func lethalSystems(ctx context.Context, kb *knowledge.SQLiteKB) (map[string]string, error) {
	out := map[string]string{}
	for _, q := range []string{
		`SELECT system_id, hostile_type, poi_id FROM hostile_sightings
		 WHERE hostile_type LIKE '%monolith%' AND system_id != '' GROUP BY system_id`,
		`SELECT s.system_id, s.species, s.poi_id FROM wildlife_sightings s
		 LEFT JOIN wildlife_species w ON w.species = s.species
		 WHERE (s.species LIKE '%leviathan%' OR w.name LIKE '%leviathan%') AND s.system_id != '' GROUP BY s.system_id`,
	} {
		rows, err := kb.DB().QueryContext(ctx, q)
		if err != nil {
			return nil, fmt.Errorf("lethal systems: %w", err)
		}
		for rows.Next() {
			var sys, what, poi string
			if err = rows.Scan(&sys, &what, &poi); err != nil {
				rows.Close() //nolint:errcheck
				return nil, fmt.Errorf("lethal systems: %w", err)
			}
			if poi != "" {
				what += " at " + poi
			}
			if out[sys] == "" {
				out[sys] = what
			}
		}
		err = rows.Err()
		rows.Close() //nolint:errcheck
		if err != nil {
			return nil, fmt.Errorf("lethal systems: %w", err)
		}
	}
	return out, nil
}

// stationPoints places every system with a station: where a ship can refuel.
func stationPoints(ctx context.Context, kb *knowledge.SQLiteKB) ([]sweep.Point, error) {
	rows, err := kb.DB().QueryContext(ctx, `
		SELECT DISTINCT s.position_x, s.position_y FROM systems s
		JOIN pois p ON p.system_id = s.id WHERE p.type = 'station'`)
	if err != nil {
		return nil, fmt.Errorf("stations: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	var pts []sweep.Point
	for rows.Next() {
		var p sweep.Point
		if err := rows.Scan(&p.X, &p.Y); err != nil {
			return nil, fmt.Errorf("stations: %w", err)
		}
		pts = append(pts, p)
	}
	return pts, rows.Err()
}

// generate writes a fresh run over the KB's frontier. It refuses to replace a
// run that has claims or progress: archive that file first.
func generate(ctx context.Context, kb *knowledge.SQLiteKB, runPath string, now time.Time) error {
	old, err := sweep.LoadFrontierRun(runPath)
	switch {
	case err == nil && old.Active():
		return fmt.Errorf("%s has claims or progress; archive it (move it aside) before generating a new run", runPath)
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return err
	}
	systems, lanes, err := frontierInputs(ctx, kb)
	if err != nil {
		return err
	}
	if len(systems) == 0 {
		return errors.New("the KB holds no frontier systems (position_source != 'api')")
	}
	return sweep.SaveFrontierRun(runPath, sweep.NewFrontierRun(systems, lanes, now))
}

// status prints the run's totals, a line per compass wedge, and every system
// that is claimed or incomplete.
func status(w io.Writer, runPath string) error {
	r, err := sweep.LoadFrontierRun(runPath)
	if err != nil {
		return err
	}
	type tally struct{ todo, claimed, done, incomplete int }
	total := tally{}
	byWedge := map[string]*tally{}
	clusters := map[string]bool{}
	count := func(t *tally, s *sweep.FrontierSystem) {
		switch s.Status {
		case sweep.FrontierTodo:
			t.todo++
		case sweep.FrontierClaimed:
			t.claimed++
		case sweep.FrontierDone:
			t.done++
		case sweep.FrontierIncomplete:
			t.incomplete++
		}
	}
	ids := slices.Sorted(maps.Keys(r.Systems))
	for _, id := range ids {
		s := r.Systems[id]
		clusters[s.Cluster] = true
		if byWedge[s.Wedge] == nil {
			byWedge[s.Wedge] = &tally{}
		}
		count(&total, s)
		count(byWedge[s.Wedge], s)
	}
	fmt.Fprintf(w, "frontier run (generated %s): %d systems in %d clusters — %d todo, %d claimed, %d done, %d incomplete\n", //nolint:errcheck
		r.GeneratedUTC.Format(time.RFC3339), len(ids), len(clusters), total.todo, total.claimed, total.done, total.incomplete)
	for _, wedge := range sweep.Wedges() {
		t := byWedge[wedge]
		if t == nil {
			t = &tally{}
		}
		fmt.Fprintf(w, "  %-2s  %3d todo  %3d claimed  %3d done  %3d incomplete\n", wedge, t.todo, t.claimed, t.done, t.incomplete) //nolint:errcheck
	}
	for _, id := range ids {
		s := r.Systems[id]
		switch s.Status {
		case sweep.FrontierClaimed:
			fmt.Fprintf(w, "  claimed     %-24s %-2s by %s\n", id, s.Wedge, s.Claimant) //nolint:errcheck
		case sweep.FrontierIncomplete:
			skipped := ""
			if len(s.Skipped) > 0 {
				skipped = " [skipped: " + strings.Join(s.Skipped, ", ") + "]"
			}
			fmt.Fprintf(w, "  incomplete  %-24s %-2s %s%s\n", id, s.Wedge, s.Reason, skipped) //nolint:errcheck
		}
	}
	return nil
}

// assignment is one compass wedge's row of the proposed table.
type assignment struct {
	Wedge  string
	Ship   string  // "" when no ship is proposed for the wedge
	Todo   int     // todo systems in the wedge
	Remote float64 // mean distance from a todo system to the nearest station
	Start  string  // the todo system nearest the centre
}

// assign proposes a ship for each wedge that has work: the wedges are ranked
// by how far their systems lie from a refuelling station, the reserves take
// the remotest, and the other ships follow in the order given. Rows come back
// in compass order.
func assign(r *sweep.FrontierRun, stations []sweep.Point, ships, reserves []string) []assignment {
	rows := map[string]*assignment{}
	for _, wedge := range sweep.Wedges() {
		rows[wedge] = &assignment{Wedge: wedge}
	}
	startDist := map[string]float64{}
	for _, id := range slices.Sorted(maps.Keys(r.Systems)) {
		s := r.Systems[id]
		if s.Status != sweep.FrontierTodo {
			continue
		}
		a := rows[s.Wedge]
		a.Todo++
		nearest := math.Inf(1)
		for _, st := range stations {
			nearest = min(nearest, math.Hypot(s.X-st.X, s.Y-st.Y))
		}
		if !math.IsInf(nearest, 1) {
			a.Remote += nearest
		}
		if d := math.Hypot(s.X, s.Y); a.Start == "" || d < startDist[s.Wedge] {
			a.Start, startDist[s.Wedge] = id, d
		}
	}
	var ranked []*assignment
	for _, wedge := range sweep.Wedges() {
		a := rows[wedge]
		if a.Todo > 0 {
			a.Remote /= float64(a.Todo)
			ranked = append(ranked, a)
		}
	}
	slices.SortStableFunc(ranked, func(a, b *assignment) int {
		switch {
		case a.Remote > b.Remote:
			return -1
		case a.Remote < b.Remote:
			return 1
		}
		return 0
	})
	order := slices.Clone(reserves)
	for _, s := range ships {
		if !slices.Contains(reserves, s) {
			order = append(order, s)
		}
	}
	for i, a := range ranked {
		if i < len(order) {
			a.Ship = order[i]
		}
	}
	out := make([]assignment, 0, len(rows))
	for _, wedge := range sweep.Wedges() {
		out = append(out, *rows[wedge])
	}
	return out
}

func printAssign(w io.Writer, rows []assignment) {
	fmt.Fprintf(w, "%-5s %-12s %5s %14s  %s\n", "wedge", "ship", "todo", "mean to fuel", "start at") //nolint:errcheck
	for _, a := range rows {
		ship := a.Ship
		if ship == "" {
			ship = "-"
		}
		fmt.Fprintf(w, "%-5s %-12s %5d %11.0f LY  %s\n", a.Wedge, ship, a.Todo, a.Remote, a.Start) //nolint:errcheck
	}
}

// relics prints the relics that gave something back, then the ones whose
// prompt offered a mission and were left alone.
func relics(ctx context.Context, w io.Writer, kb knowledge.Base) error {
	all, err := kb.RelicInteractions(ctx)
	if err != nil {
		return err
	}
	var responded, missions []knowledge.RelicInteraction
	for _, r := range all {
		switch {
		case r.Responded():
			responded = append(responded, r)
		case r.Outcome == knowledge.RelicMissionSkipped:
			missions = append(missions, r)
		}
	}
	fmt.Fprintf(w, "relics: %d interactions, %d responded, %d offered a mission\n", len(all), len(responded), len(missions)) //nolint:errcheck
	for _, r := range responded {
		got := "data copied"
		if len(r.Items) > 0 {
			var parts []string
			for _, id := range slices.Sorted(maps.Keys(r.Items)) {
				parts = append(parts, fmt.Sprintf("%s x%d", id, r.Items[id]))
			}
			got = strings.Join(parts, ", ")
			if r.DataCopied {
				got += ", data copied"
			}
		}
		fmt.Fprintf(w, "  %-28s %-20s %-12s %s — %s\n", r.POIID, r.SystemID, r.AgentID, got, r.Message) //nolint:errcheck
	}
	if len(missions) > 0 {
		fmt.Fprintln(w, "offered a mission (not interacted with):") //nolint:errcheck
	}
	for _, r := range missions {
		fmt.Fprintf(w, "  %-28s %-20s %-12s %s %s\n", r.POIID, r.SystemID, r.AgentID, r.Prompt, r.MissionJSON) //nolint:errcheck
	}
	return nil
}
