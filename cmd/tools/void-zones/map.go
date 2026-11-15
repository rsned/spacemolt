package main

import (
	"fmt"
	"html"
	"sort"
	"strings"

	"github.com/rsned/spacemolt/pkg/sweep"
)

// foundSys is a system the sweep found, placed and labelled on the map.
type foundSys struct {
	ID, Name string
	X, Y     float64
}

// renderMap draws the run as a self-contained HTML page: systems, zones
// (shaded by state), each zone's planned square wave, the part flown so far,
// the systems the sweep found, and a table of zones.
func renderMap(r *sweep.VoidRun, systems []sweep.Point, finds []foundSys) string {
	var svg strings.Builder
	for _, z := range r.Zones {
		cls := "zone"
		switch {
		case z.Done:
			cls = "zone done"
		case z.Claimant != "":
			cls = "zone live"
		}
		fmt.Fprintf(&svg, `<path class="%s" d="%s"><title>%s</title></path>`, cls, cellPath(z.Zone), z.ID)
	}
	for _, z := range r.Zones {
		fmt.Fprintf(&svg, `<polyline class="plan" points="%s"/>`, points(z.Waypoints))
		if n := min(z.Next, len(z.Waypoints)); n >= 2 {
			fmt.Fprintf(&svg, `<polyline class="flown" points="%s"/>`, points(z.Waypoints[:n]))
		}
	}
	for _, p := range systems {
		fmt.Fprintf(&svg, `<circle class="sys" cx="%.0f" cy="%.0f" r="35"/>`, p.X, -p.Y)
	}
	for _, f := range finds {
		name := f.Name
		if name == "" {
			name = f.ID
		}
		fmt.Fprintf(&svg, `<circle class="find" cx="%.0f" cy="%.0f" r="90"><title>%s</title></circle>`, f.X, -f.Y, html.EscapeString(name))
		fmt.Fprintf(&svg, `<text class="flab" x="%.0f" y="%.0f">%s</text>`, f.X+130, -f.Y+50, html.EscapeString(name))
	}
	for _, z := range r.Zones[:min(15, len(r.Zones))] {
		fmt.Fprintf(&svg, `<text class="lab" x="%.0f" y="%.0f">%s</text>`, z.CX, -z.CY+70, strings.TrimPrefix(z.ID, "V"))
	}

	var rows strings.Builder
	done, flown, total := 0, 0, 0
	for _, z := range r.Zones {
		state := "open"
		switch {
		case z.Done:
			state, done = "done", done+1
		case z.Claimant != "":
			state = html.EscapeString(z.Claimant)
		}
		flown += min(z.Next, len(z.Waypoints))
		total += len(z.Waypoints)
		found := append([]string(nil), z.Found...)
		sort.Strings(found)
		fmt.Fprintf(&rows, "<tr><td>%s</td><td>%.2f</td><td>%.0f, %.0f</td><td>%d</td><td>%d / %d</td><td>%s</td><td class=\"found\">%s</td></tr>",
			z.ID, z.Area/1e6, z.CX, z.CY, len(z.Waypoints)/2, z.Next, len(z.Waypoints), state, html.EscapeString(strings.Join(found, ", ")))
	}
	pct := 0.0
	if total > 0 {
		pct = 100 * float64(flown) / float64(total)
	}
	return fmt.Sprintf(mapPage, len(r.Zones), done, pct, len(finds), html.EscapeString(r.Created), svg.String(), rows.String())
}

// cellPath is the zone's cells as one SVG path, merged into row runs.
func cellPath(z sweep.Zone) string {
	g := z.Grid
	rows := map[int][]int{}
	for _, c := range z.Cells {
		rows[c[1]] = append(rows[c[1]], c[0])
	}
	var b strings.Builder
	for j, xs := range rows {
		sort.Ints(xs)
		for k := 0; k < len(xs); {
			e := k
			for e+1 < len(xs) && xs[e+1] == xs[e]+1 {
				e++
			}
			fmt.Fprintf(&b, "M%.0f %.0fh%.0fv%.0fh%.0fz", float64(xs[k])*g, -float64(j+1)*g, float64(e-k+1)*g, g, -float64(e-k+1)*g)
			k = e + 1
		}
	}
	return b.String()
}

// points is an SVG points list with y flipped (map +y is up).
func points(ps []sweep.Point) string {
	parts := make([]string, len(ps))
	for i, p := range ps {
		parts[i] = fmt.Sprintf("%.0f,%.0f", p.X, -p.Y)
	}
	return strings.Join(parts, " ")
}

const mapPage = `<title>Inner Void Map</title>
<meta name="claude:viewport-fit-sides" content="cover">
<style>
/* layout: one wide star chart, then the zone ledger beneath it */
:root{--bg:#f3f5f8;--fg:#18202b;--muted:#5d6878;--line:#cfd6e0;--zone:#7aa7d9;--live:#d9822b;--done:#3d9a62;--sys:#5d6878;--find:#c2185b;
--display:"Saira Semi Condensed",system-ui,sans-serif;--body:"IBM Plex Sans",system-ui,sans-serif;--mono:"IBM Plex Mono",ui-monospace,monospace}
@media (prefers-color-scheme:dark){:root:not([data-theme="light"]){--bg:#0d1219;--fg:#e3e8ef;--muted:#93a0b2;--line:#2a3442;--zone:#2d5687;--live:#f0a250;--done:#5cc489;--sys:#8794a6;--find:#ff6fae;color-scheme:dark}}
:root[data-theme="dark"]{--bg:#0d1219;--fg:#e3e8ef;--muted:#93a0b2;--line:#2a3442;--zone:#2d5687;--live:#f0a250;--done:#5cc489;--sys:#8794a6;--find:#ff6fae;color-scheme:dark}
body{background:var(--bg);color:var(--fg);font-family:var(--body);line-height:1.5}
main{max-width:1080px;margin:0 auto;padding-block:28px 48px;padding-left:max(16px,env(safe-area-inset-left,0px));padding-right:max(16px,env(safe-area-inset-right,0px));display:grid;gap:20px}
h1{font-family:var(--display);font-weight:600;font-size:2rem;margin:0}
p{margin:0;max-width:68ch;color:var(--muted)}
.facts{display:flex;flex-wrap:wrap;gap:6px 28px;font-size:.9rem} .facts b{font-family:var(--mono)}
.legend{display:flex;flex-wrap:wrap;gap:8px 20px;font-size:.85rem;color:var(--muted)}
.legend span{display:inline-flex;align-items:center;gap:6px} .sw{width:14px;height:14px;border-radius:3px;display:inline-block}
svg{width:100%%;max-width:100%%;height:auto;display:block}
.zone{fill:var(--zone);opacity:.45} .zone.live{fill:var(--live);opacity:.5} .zone.done{fill:var(--done);opacity:.5}
.plan{fill:none;stroke:var(--muted);stroke-width:8;stroke-opacity:.35}
.flown{fill:none;stroke:var(--live);stroke-width:22}
.sys{fill:var(--sys)}
.find{fill:var(--find);stroke:var(--bg);stroke-width:24}
.flab{fill:var(--find);font:600 170px var(--body)}
.lab{fill:var(--fg);font:600 200px var(--mono);text-anchor:middle}
.tbl{overflow-x:auto}
table{border-collapse:collapse;width:100%%;font-size:.88rem;font-variant-numeric:tabular-nums}
th,td{padding:6px 10px;border-bottom:1px solid var(--line);text-align:right;white-space:nowrap}
th{font-weight:600;color:var(--muted);font-size:.75rem;text-transform:uppercase;letter-spacing:.06em}
td:first-child,th:first-child{text-align:left;font-family:var(--mono);font-weight:600}
td.found{text-align:left;white-space:normal}
</style>
<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=IBM+Plex+Mono:wght@400;600&family=IBM+Plex+Sans:wght@400;600&family=Saira+Semi+Condensed:wght@600&display=swap">
<main>
<header style="display:grid;gap:8px"><h1>Inner Void Map</h1>
<p>Empty space inside 7,150 LY: every patch more than 350 LY from any known system. Explorers fly each zone in a square wave with lanes 100 LY apart, one zone each, largest first.</p></header>
<div class="facts"><span><b>%d</b> zones</span><span><b>%d</b> done</span><span><b>%.0f%%</b> of legs flown</span><span><b>%d</b> found</span><span>run created <b>%s</b></span></div>
<div class="legend"><span><i class="sw" style="background:var(--zone)"></i>open zone</span><span><i class="sw" style="background:var(--live)"></i>being flown</span><span><i class="sw" style="background:var(--done)"></i>done</span><span><i class="sw" style="background:var(--sys);border-radius:50%%"></i>known system</span><span><i class="sw" style="background:var(--find);border-radius:50%%"></i>found by the sweep</span></div>
<svg viewBox="-9200 -9200 18400 18400" role="img" aria-label="Galaxy map with void zones and flown lanes">%s</svg>
<div class="tbl"><table><thead><tr><th>Zone</th><th>Area M LY²</th><th>Centre</th><th>Lanes</th><th>Legs flown</th><th>Status</th><th>Systems found</th></tr></thead><tbody>%s</tbody></table></div>
</main>`
