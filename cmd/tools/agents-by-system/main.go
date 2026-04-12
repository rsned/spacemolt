// Command agents-by-system reads the latest agent snapshots from
// daily-summary.db, joins them against the spacemolt knowledge base
// (systems + pois), and renders an HTML page grouping agents by the
// empire of the system they are currently in, then by system, then POI.
//
// Grouping order:
//  1. Empire systems, alphabetical by empire name, then system id, then poi id.
//  2. Non-empire (or unknown) systems last, alphabetical by system id.
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"html"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type row struct {
	AgentID    string
	Username   string
	AgentEmpire string
	ShipClass  string
	Docked     bool
	POIType    string
	SystemID   string
	SystemName string
	SysEmpire  string
	POIID      string
	POIName    string
	Location   string // fallback raw "SystemName / poi_id" from snapshot
}

const query = `
WITH latest AS (
    SELECT agent_id, MAX(captured_date) AS d
    FROM snapshots
    GROUP BY agent_id
),
snaps AS (
    SELECT
        s.agent_id,
        json_extract(s.state_json, '$.username')   AS username,
        json_extract(s.state_json, '$.empire')     AS agent_empire,
        json_extract(s.state_json, '$.ship_class') AS ship_class,
        json_extract(s.state_json, '$.docked')     AS docked,
        json_extract(s.state_json, '$.poi_type')   AS poi_type,
        json_extract(s.state_json, '$.location')   AS location
    FROM snapshots s
    JOIN latest l ON l.agent_id = s.agent_id AND l.d = s.captured_date
),
parsed AS (
    SELECT
        sn.*,
        CASE
            WHEN INSTR(sn.location, ' / ') > 0
                THEN SUBSTR(sn.location, 1, INSTR(sn.location, ' / ') - 1)
            ELSE sn.location
        END AS loc_system_name,
        CASE
            WHEN INSTR(sn.location, ' / ') > 0
                THEN SUBSTR(sn.location, INSTR(sn.location, ' / ') + 3)
            ELSE ''
        END AS loc_poi_id
    FROM snaps sn
)
SELECT
    p.agent_id,
    COALESCE(p.username, ''),
    COALESCE(p.agent_empire, ''),
    COALESCE(p.ship_class, ''),
    COALESCE(p.docked, 0),
    COALESCE(p.poi_type, ''),
    COALESCE(sys.id, ''),
    COALESCE(sys.name, p.loc_system_name),
    COALESCE(sys.empire, ''),
    COALESCE(poi.id, p.loc_poi_id),
    COALESCE(poi.name, p.loc_poi_id),
    COALESCE(p.location, '')
FROM parsed p
LEFT JOIN kb.pois poi
    ON poi.id = p.loc_poi_id
LEFT JOIN kb.systems sys
    ON sys.id = COALESCE(
        poi.system_id,
        (SELECT id FROM kb.systems WHERE name = p.loc_system_name LIMIT 1)
    )
ORDER BY p.agent_id;
`

func main() {
	dbPath := flag.String("db", "data/daily-summary.db", "daily-summary SQLite database path")
	kbPath := flag.String("kb", "data/spacemolt-knowledge.db", "shared knowledge base SQLite path")
	outPath := flag.String("output", "data/reports/agents-by-system.html", "HTML output file path")
	flag.Parse()

	logger := log.New(os.Stdout, "[agents-by-system] ", log.LstdFlags)

	db, err := sql.Open("sqlite", *dbPath+"?mode=ro")
	if err != nil {
		logger.Fatalf("open daily-summary db: %v", err)
	}
	defer func() { _ = db.Close() }()

	if _, err := db.Exec(fmt.Sprintf("ATTACH DATABASE '%s' AS kb", *kbPath)); err != nil {
		logger.Fatalf("attach knowledge db: %v", err)
	}

	rows, err := db.Query(query)
	if err != nil {
		logger.Fatalf("query: %v", err)
	}
	defer func() { _ = rows.Close() }()

	var all []row
	for rows.Next() {
		var r row
		var dockedInt int
		if err := rows.Scan(
			&r.AgentID, &r.Username, &r.AgentEmpire, &r.ShipClass,
			&dockedInt, &r.POIType,
			&r.SystemID, &r.SystemName, &r.SysEmpire,
			&r.POIID, &r.POIName, &r.Location,
		); err != nil {
			logger.Fatalf("scan: %v", err)
		}
		r.Docked = dockedInt != 0
		all = append(all, r)
	}
	if err := rows.Err(); err != nil {
		logger.Fatalf("rows: %v", err)
	}
	logger.Printf("loaded %d agent snapshots", len(all))

	if err := os.MkdirAll(filepath.Dir(*outPath), 0o755); err != nil {
		logger.Fatalf("mkdir: %v", err)
	}
	f, err := os.Create(*outPath)
	if err != nil {
		logger.Fatalf("create output: %v", err)
	}
	defer func() { _ = f.Close() }()

	if err := render(f, all); err != nil {
		logger.Fatalf("render: %v", err)
	}
	logger.Printf("wrote %s", *outPath)
}

// systemKey groups rows by system_id (falling back to system name when no id).
type systemKey struct {
	ID     string
	Name   string
	Empire string
}

func render(w *os.File, rows []row) error {
	// Bucket: empire -> systemKey -> poi key -> []row
	type poiGroup struct {
		ID   string
		Name string
		Rows []row
	}
	type sysGroup struct {
		Key  systemKey
		POIs map[string]*poiGroup
	}
	empires := map[string]map[string]*sysGroup{} // empire -> sysID -> sysGroup

	for _, r := range rows {
		empire := strings.ToLower(strings.TrimSpace(r.SysEmpire))
		sysID := r.SystemID
		if sysID == "" {
			sysID = "~" + r.SystemName // stable fallback key
		}
		sg, ok := empires[empire]
		if !ok {
			sg = map[string]*sysGroup{}
			empires[empire] = sg
		}
		g, ok := sg[sysID]
		if !ok {
			g = &sysGroup{
				Key:  systemKey{ID: r.SystemID, Name: r.SystemName, Empire: empire},
				POIs: map[string]*poiGroup{},
			}
			sg[sysID] = g
		}
		poiID := r.POIID
		if poiID == "" {
			poiID = "(deep space)"
		}
		pg, ok := g.POIs[poiID]
		if !ok {
			pg = &poiGroup{ID: poiID, Name: firstNonEmpty(r.POIName, poiID)}
			g.POIs[poiID] = pg
		}
		pg.Rows = append(pg.Rows, r)
	}

	// Ordered empire list: alphabetical empires first, then "" (unknown) last.
	var empireNames []string
	for e := range empires {
		if e != "" {
			empireNames = append(empireNames, e)
		}
	}
	sort.Strings(empireNames)
	if _, ok := empires[""]; ok {
		empireNames = append(empireNames, "")
	}

	var b strings.Builder
	b.WriteString(`<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8">
<title>Agents by System</title>
<style>
body { font-family: -apple-system, system-ui, sans-serif; margin: 2rem; color: #222; }
h1 { margin-bottom: 0.25rem; }
.timestamp { color: #888; font-size: 0.85rem; margin-bottom: 1.5rem; }
h2.empire { margin-top: 2rem; padding: 0.35rem 0.75rem; background: #222; color: #fff; border-radius: 4px; }
h2.empire.unknown { background: #666; }
h3.system { margin: 1rem 0 0.25rem; color: #333; }
h3.system .sysid { color: #888; font-weight: normal; font-size: 0.85em; }
h4.poi { margin: 0.5rem 0 0.25rem 1rem; color: #555; }
h4.poi .poiid { color: #999; font-weight: normal; font-size: 0.85em; }
table { border-collapse: collapse; margin: 0.25rem 0 0.5rem 2rem; }
th, td { text-align: left; padding: 3px 10px; border-bottom: 1px solid #eee; font-size: 0.9rem; }
th { background: #f4f4f4; }
.docked { color: #0a0; font-weight: bold; }
.undocked { color: #c60; }
.count { color: #888; font-size: 0.85em; font-weight: normal; }
</style></head><body>`)
	fmt.Fprintf(&b, "<h1>Agents by System</h1>\n")
	fmt.Fprintf(&b, `<div class="timestamp">Generated %s — %d agents</div>`+"\n",
		time.Now().Format("2006-01-02 15:04:05"), len(rows))

	for _, empire := range empireNames {
		sg := empires[empire]
		empireLabel := titleCase(empire)
		cls := "empire"
		if empire == "" {
			empireLabel = "Non-Empire / Unknown"
			cls = "empire unknown"
		}
		// count agents in this empire
		var empireAgents int
		for _, g := range sg {
			for _, p := range g.POIs {
				empireAgents += len(p.Rows)
			}
		}
		fmt.Fprintf(&b, `<h2 class="%s">%s <span class="count">(%d agents)</span></h2>`+"\n",
			cls, html.EscapeString(empireLabel), empireAgents)

		// Sort systems by id (stable, uses fallback key when id missing)
		var sysIDs []string
		for id := range sg {
			sysIDs = append(sysIDs, id)
		}
		sort.Strings(sysIDs)

		for _, sid := range sysIDs {
			g := sg[sid]
			sysDisplayID := g.Key.ID
			if sysDisplayID == "" {
				sysDisplayID = "(unknown id)"
			}
			fmt.Fprintf(&b, `<h3 class="system">%s <span class="sysid">[%s]</span></h3>`+"\n",
				html.EscapeString(g.Key.Name), html.EscapeString(sysDisplayID))

			// Sort POIs by id
			var poiIDs []string
			for id := range g.POIs {
				poiIDs = append(poiIDs, id)
			}
			sort.Strings(poiIDs)
			for _, pid := range poiIDs {
				pg := g.POIs[pid]
				fmt.Fprintf(&b, `<h4 class="poi">%s <span class="poiid">[%s]</span></h4>`+"\n",
					html.EscapeString(pg.Name), html.EscapeString(pg.ID))
				b.WriteString("<table><tr><th>Agent</th><th>Username</th><th>Home Empire</th><th>Ship</th><th>Docked</th><th>POI Type</th></tr>\n")
				// Sort agents within a POI by agent id for stability
				sort.Slice(pg.Rows, func(i, j int) bool { return pg.Rows[i].AgentID < pg.Rows[j].AgentID })
				for _, r := range pg.Rows {
					dockedCell := `<span class="undocked">—</span>`
					if r.Docked {
						dockedCell = `<span class="docked">docked</span>`
					}
					fmt.Fprintf(&b,
						"<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>\n",
						html.EscapeString(r.AgentID),
						html.EscapeString(r.Username),
						html.EscapeString(r.AgentEmpire),
						html.EscapeString(r.ShipClass),
						dockedCell,
						html.EscapeString(r.POIType),
					)
				}
				b.WriteString("</table>\n")
			}
		}
	}
	b.WriteString("</body></html>\n")

	_, err := w.WriteString(b.String())
	return err
}

func titleCase(s string) string {
	if s == "" {
		return s
	}
	b := []byte(s)
	if b[0] >= 'a' && b[0] <= 'z' {
		b[0] -= 'a' - 'A'
	}
	return string(b)
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
