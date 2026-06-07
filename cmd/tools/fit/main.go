// Command fit is an offline ship-fitting calculator. It reads the catalog JSON
// snapshots and answers two questions:
//
//	fit check --ship <id> --module <id> [--count N] [--eng_skill_level 0]
//	fit ships --module <id> --count N            [--eng_skill_level 0]
//
// Global flag --catalog-dir defaults to data/game-api/latest.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/rsned/spacemolt/pkg/fitting"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "check":
		runCheck(os.Args[2:])
	case "ships":
		runShips(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `fit - offline ship-fitting calculator

Usage:
  fit check --ship <id> --module <id> [--count N] [--eng_skill_level 0] [--catalog-dir DIR]
  fit ships --module <id> --count N            [--eng_skill_level 0] [--catalog-dir DIR]
`)
}

func runCheck(args []string) {
	fs := flag.NewFlagSet("check", flag.ExitOnError)
	ship := fs.String("ship", "", "ship id (required)")
	module := fs.String("module", "", "module id (required)")
	count := fs.Int("count", 0, "optional target count to pass/fail against")
	eng := fs.Int("eng_skill_level", 0, "Engineering skill level")
	dir := fs.String("catalog-dir", "data/game-api/latest", "catalog directory")
	_ = fs.Parse(args)

	if *ship == "" || *module == "" {
		fmt.Fprintln(os.Stderr, "check: --ship and --module are required")
		os.Exit(2)
	}

	cat := mustLoad(*dir)
	s, ok := cat.Ship(*ship)
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown ship %q\n", *ship)
		os.Exit(1)
	}
	m, ok := cat.Module(*module)
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown module %q\n", *module)
		os.Exit(1)
	}

	res := fitting.MaxFit(s, m, cat.Engineering(*eng))
	fmt.Print(formatCheck(s.Name, m.Name, res))
	if *count > 0 {
		if res.MaxCount >= *count {
			fmt.Printf("=> YES, fits %d (max %d)\n", *count, res.MaxCount)
		} else {
			fmt.Printf("=> NO, only %d fit (wanted %d)\n", res.MaxCount, *count)
		}
	}
}

func runShips(args []string) {
	fs := flag.NewFlagSet("ships", flag.ExitOnError)
	module := fs.String("module", "", "module id (required)")
	count := fs.Int("count", 1, "minimum count required")
	eng := fs.Int("eng_skill_level", 0, "Engineering skill level")
	dir := fs.String("catalog-dir", "data/game-api/latest", "catalog directory")
	_ = fs.Parse(args)

	if *module == "" {
		fmt.Fprintln(os.Stderr, "ships: --module is required")
		os.Exit(2)
	}

	cat := mustLoad(*dir)
	m, ok := cat.Module(*module)
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown module %q\n", *module)
		os.Exit(1)
	}

	fits := fitting.ShipsThatFit(cat.Ships(), m, *count, cat.Engineering(*eng))
	fmt.Print(formatShips(m.Name, *count, fits))
}

func mustLoad(dir string) *fitting.Catalog {
	cat, err := fitting.LoadCatalog(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load catalog: %v\n", err)
		os.Exit(1)
	}
	return cat
}
