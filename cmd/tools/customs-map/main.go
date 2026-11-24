// Command customs-map builds and reports the customs map: which systems
// empire customs inspects (monitored), which see traffic with no
// inspections (clear), and which are unknown. Contraband routes avoid the
// monitored ones.
//
//	customs-map build   rebuild data/overmind/customs-monitored.json
//	customs-map report  print the current map grouped by empire
package main

import (
	"cmp"
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"slices"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/rsned/spacemolt/pkg/customs"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/worker"
)

func main() {
	assetsPath := flag.String("assets", "data/assets.db", "assets database (action_log_events)")
	kbPath := flag.String("kb", "data/spacemolt-knowledge.db", "knowledge base (system empires)")
	mapPath := flag.String("map", customs.DefaultMapPath, "customs map file")
	overridesPath := flag.String("overrides", customs.DefaultOverridesPath, "operator overrides file")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: customs-map [flags] build|report\n") //nolint:errcheck
		flag.PrintDefaults()
	}
	flag.Parse()
	switch flag.Arg(0) {
	case "build":
		build(*assetsPath, *kbPath, *mapPath, *overridesPath)
	case "report":
		report(*mapPath)
	default:
		flag.Usage()
		os.Exit(2)
	}
}

func build(assetsPath, kbPath, mapPath, overridesPath string) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:"+assetsPath+"?mode=ro")
	if err != nil {
		log.Fatalf("open %s: %v", assetsPath, err)
	}
	defer db.Close() //nolint:errcheck
	var kb knowledge.Base
	if sk, kerr := knowledge.NewSQLiteKB(knowledge.Config{DBPath: kbPath, WAL: true}); kerr != nil {
		log.Printf("kb %s unavailable (%v); empires come from inspections only", kbPath, kerr)
	} else {
		defer sk.Close() //nolint:errcheck
		kb = sk
	}
	m, err := worker.RefreshCustomsMap(ctx, db, kb, mapPath, overridesPath, time.Now())
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(worker.CustomsMapSummary(m))
}

func report(mapPath string) {
	m, err := customs.Load(mapPath)
	if err != nil {
		log.Fatalf("%v (run `customs-map build` first)", err)
	}
	fmt.Printf("customs map generated %s, %d-day window\n", m.GeneratedAt.Format(time.RFC3339), m.WindowDays)
	byEmpire := map[string][]string{}
	for id, s := range m.Systems {
		if s.Class == customs.Unknown {
			continue
		}
		e := cmp.Or(s.Empire, "(no empire)")
		byEmpire[e] = append(byEmpire[e], id)
	}
	empires := make([]string, 0, len(byEmpire))
	for e := range byEmpire {
		empires = append(empires, e)
	}
	slices.Sort(empires)
	for _, e := range empires {
		ids := byEmpire[e]
		slices.Sort(ids)
		fmt.Printf("\n%s\n", strings.ToUpper(e))
		for _, class := range []customs.Class{customs.Monitored, customs.Clear} {
			for _, id := range ids {
				s := m.Systems[id]
				if s.Class != class {
					continue
				}
				line := fmt.Sprintf("  %-9s %-22s arrivals=%-5d", class, id, s.Arrivals)
				if s.Inspections > 0 {
					line += fmt.Sprintf(" inspections=%-4d last=%s since=%d", s.Inspections, s.LastInspection.Format("01-02 15:04"), s.ArrivalsSince)
				}
				if s.Seizures > 0 {
					line += fmt.Sprintf(" seizures=%d %v", s.Seizures, s.SeizedItems)
				}
				if s.Reason != "" {
					line += "  [override: " + s.Reason + "]"
				}
				fmt.Println(line)
			}
		}
	}
}
