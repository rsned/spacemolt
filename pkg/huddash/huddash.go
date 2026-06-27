// Package huddash renders a self-contained hauler dashboard HTML page (inline
// SVG, no JavaScript) from the durable haul_results + fleet_timeseries history
// in market.db plus the live fleet-status.json. It is the Phase-2 generator for
// docs/superpowers/specs/2026-06-26-hauler-dashboard-design.md.
package huddash

import (
	"fmt"
	"html"
	"slices"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/market"
	"github.com/rsned/spacemolt/pkg/overmind/balances"
)

// Period is a time-bucketing granularity for the per-period charts.
type Period struct {
	Name string
	Dur  time.Duration
}

// ParsePeriod maps hour|half_day|day to a Period.
func ParsePeriod(s string) (Period, error) {
	switch s {
	case "hour":
		return Period{"hour", time.Hour}, nil
	case "half_day":
		return Period{"half_day", 12 * time.Hour}, nil
	case "day":
		return Period{"day", 24 * time.Hour}, nil
	default:
		return Period{}, fmt.Errorf("unknown period %q (want hour|half_day|day)", s)
	}
}

// bucketStart floors t to this period's boundary in UTC.
func (p Period) bucketStart(t time.Time) time.Time {
	return t.UTC().Truncate(p.Dur)
}

// label renders a bucket-start time compactly for the chosen granularity.
func (p Period) label(t time.Time) string {
	switch p.Name {
	case "hour":
		return t.UTC().Format("15:04")
	case "half_day":
		return t.UTC().Format("01-02 15h")
	default:
		return t.UTC().Format("01-02")
	}
}

// AgentData is one hauler's window-filtered history plus its current status.
type AgentData struct {
	AgentID string
	Status  balances.LiveRecord    // current credits/location/fuel/cargo (may be zero if absent)
	HasStat bool                   // whether Status was found in fleet-status.json
	Hauls   []market.HaulResult    // within window
	Series  []market.FleetSnapshot // within window
}

// Input is the fully-assembled render model.
type Input struct {
	GeneratedAt time.Time
	Period      Period
	Window      time.Duration
	Agents      []AgentData // sorted by agent id
}

// haulBucket groups the hauls whose sold_at falls in one period bucket.
type haulBucket struct {
	Start time.Time
	Hauls []market.HaulResult
}

func groupHaulsByPeriod(hauls []market.HaulResult, p Period) []haulBucket {
	byKey := map[int64]*haulBucket{}
	var order []int64
	for _, h := range hauls {
		t, err := time.Parse(time.RFC3339, h.SoldAt)
		if err != nil {
			continue
		}
		start := p.bucketStart(t)
		key := start.Unix()
		bk := byKey[key]
		if bk == nil {
			bk = &haulBucket{Start: start}
			byKey[key] = bk
			order = append(order, key)
		}
		bk.Hauls = append(bk.Hauls, h)
	}
	slices.Sort(order)
	out := make([]haulBucket, 0, len(order))
	for _, k := range order {
		out = append(out, *byKey[k])
	}
	return out
}

// phaseTicks returns the four per-phase tick durations, clamped to >=0 (a zero or
// missing leg stamp must not produce a negative segment).
func phaseTicks(h market.HaulResult) [4]float64 {
	clamp := func(a, b int64) float64 {
		if d := a - b; d > 0 {
			return float64(d)
		}
		return 0
	}
	return [4]float64{
		clamp(h.ArrivedSrcTick, h.ClaimedTick),
		clamp(h.BoughtTick, h.ArrivedSrcTick),
		clamp(h.ArrivedDstTick, h.BoughtTick),
		clamp(h.SoldTick, h.ArrivedDstTick),
	}
}

// fleetCreditsPerJump is Σ realized_profit / Σ jumps over every agent's window hauls.
func fleetCreditsPerJump(agents []AgentData) float64 {
	var profit, jumps float64
	for _, a := range agents {
		for _, h := range a.Hauls {
			profit += h.RealizedProfit
			jumps += float64(h.JumpsTraveled)
		}
	}
	if jumps == 0 {
		return 0
	}
	return profit / jumps
}

// Render produces the complete dashboard HTML document.
func Render(in Input) string {
	var b strings.Builder
	b.WriteString("<!doctype html><html lang=\"en\"><head><meta charset=\"utf-8\">")
	b.WriteString(`<meta name="viewport" content="width=device-width,initial-scale=1">`)
	b.WriteString("<title>Hauler Dashboard</title>")
	b.WriteString(styleBlock)
	b.WriteString("</head><body>")

	fmt.Fprintf(&b, `<h1>Hauler Dashboard</h1><p class="meta">generated %s · period <b>%s</b> · window %s · %d haulers</p>`,
		in.GeneratedAt.UTC().Format("2006-01-02 15:04:05Z"), html.EscapeString(in.Period.Name),
		in.Window.String(), len(in.Agents))

	renderSummary(&b, in)

	fleetCPJ := fleetCreditsPerJump(in.Agents)
	for _, a := range in.Agents {
		renderAgent(&b, a, in.Period, fleetCPJ)
	}

	b.WriteString(legendBlock)
	b.WriteString("</body></html>")
	return b.String()
}

func renderSummary(b *strings.Builder, in Input) {
	b.WriteString(`<table class="summary"><thead><tr>`)
	for _, h := range []string{"Agent", "Location", "Credits", "Fuel", "Cargo", "Hauls", "Realized (real)"} {
		fmt.Fprintf(b, "<th>%s</th>", h)
	}
	b.WriteString("</tr></thead><tbody>")
	for _, a := range in.Agents {
		var realized float64
		for _, h := range a.Hauls {
			realized += h.RealizedProfit
		}
		loc := a.Status.System
		if a.Status.POI != "" {
			loc += "/" + a.Status.POI
		}
		fmt.Fprintf(b, `<tr><td><a href="#%s">%s</a></td><td>%s</td><td class="num">%s</td><td class="num">%s</td><td class="num">%s</td><td class="num">%d</td><td class="num">%s</td></tr>`,
			html.EscapeString(a.AgentID), html.EscapeString(a.AgentID), html.EscapeString(loc),
			commas(a.Status.Credits), fuelText(a.Status), cargoText(a.Status),
			len(a.Hauls), commas(realized))
	}
	b.WriteString("</tbody></table>")
}

func renderAgent(b *strings.Builder, a AgentData, p Period, fleetCPJ float64) {
	fmt.Fprintf(b, `<section class="agent" id="%s"><h2>%s</h2>`,
		html.EscapeString(a.AgentID), html.EscapeString(a.AgentID))
	fmt.Fprintf(b, `<p class="sub">fuel %s · cargo %s · %d hauls in window</p>`,
		fuelText(a.Status), cargoText(a.Status), len(a.Hauls))

	buckets := groupHaulsByPeriod(a.Hauls, p)

	// 1. Credit-balance line.
	pts := make([]xyPoint, 0, len(a.Series))
	for _, s := range a.Series {
		t, err := time.Parse(time.RFC3339, s.TS)
		if err != nil {
			continue
		}
		pts = append(pts, xyPoint{X: float64(t.Unix()), Y: s.Credits})
	}

	// 2. Hauls-per-period bars.  3. Credits-per-jump bars.  4. Per-phase stacked bars.
	haulBars := make([]bucketBar, 0, len(buckets))
	cpjBars := make([]bucketBar, 0, len(buckets))
	phaseBars := make([]stackBar, 0, len(buckets))
	for _, bk := range buckets {
		lbl := p.label(bk.Start)
		var profit, jumps float64
		var seg [4]float64
		for _, h := range bk.Hauls {
			profit += h.RealizedProfit
			jumps += float64(h.JumpsTraveled)
			pt := phaseTicks(h)
			for i := range seg {
				seg[i] += pt[i]
			}
		}
		n := float64(len(bk.Hauls))
		cpj := 0.0
		if jumps > 0 {
			cpj = profit / jumps
		}
		for i := range seg {
			seg[i] /= n // average ticks per haul in the bucket
		}
		haulBars = append(haulBars, bucketBar{Label: lbl, Value: n,
			Title: fmt.Sprintf("%s: %d hauls", lbl, len(bk.Hauls))})
		cpjBars = append(cpjBars, bucketBar{Label: lbl, Value: cpj,
			Title: fmt.Sprintf("%s: %s cr/jump (%s over %.0f jumps)", lbl, fmtNum(cpj), commas(profit), jumps)})
		phaseBars = append(phaseBars, stackBar{Label: lbl, Segs: seg,
			Title: fmt.Sprintf("%s: src %.0f / buy %.0f / dst %.0f / sell %.0f ticks",
				lbl, seg[0], seg[1], seg[2], seg[3])})
	}

	b.WriteString(`<div class="charts">`)
	b.WriteString(lineChartSVG("credit balance", pts))
	b.WriteString(barChartSVG("hauls / "+p.Name, haulBars, 0, ""))
	b.WriteString(barChartSVG("credits / jump", cpjBars, fleetCPJ, "fleet "+fmtNum(fleetCPJ)))
	b.WriteString(stackedBarChartSVG("response ticks (src·buy·dst·sell)", phaseBars))
	b.WriteString(`</div></section>`)
}

func fuelText(s balances.LiveRecord) string {
	if s.MaxFuel <= 0 {
		return "—"
	}
	return fmt.Sprintf("%s/%s", commas(s.Fuel), commas(s.MaxFuel))
}

func cargoText(s balances.LiveRecord) string {
	if s.CargoCapacity <= 0 {
		return "—"
	}
	return fmt.Sprintf("%s/%s", commas(s.CargoUsed), commas(s.CargoCapacity))
}

// commas formats a number with thousands separators (integer credits/units).
func commas(v float64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	s := fmt.Sprintf("%.0f", v)
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}

const styleBlock = `<style>
body{font:13px/1.4 system-ui,sans-serif;margin:1rem;color:#222;background:#fafafa}
h1{font-size:1.4rem;margin:0 0 .2rem}
.meta{color:#666;margin:0 0 1rem}
table.summary{border-collapse:collapse;width:100%;margin-bottom:1.5rem;background:#fff}
table.summary th,table.summary td{border:1px solid #ddd;padding:3px 8px;text-align:left}
table.summary th{background:#f0f0f0}
td.num{text-align:right;font-variant-numeric:tabular-nums}
section.agent{background:#fff;border:1px solid #ddd;border-radius:6px;padding:.6rem .8rem;margin-bottom:1rem}
section.agent h2{font-size:1.05rem;margin:0}
.sub{color:#666;margin:.1rem 0 .5rem}
.charts{display:grid;grid-template-columns:repeat(auto-fit,minmax(300px,1fr));gap:.6rem}
figure.chart{margin:0}
figure.chart figcaption{font-size:11px;color:#555;margin-bottom:2px}
figure.chart svg{width:100%;height:96px;background:#fcfcfc;border:1px solid #eee}
svg .axis{stroke:#bbb;stroke-width:1}
svg .bar{fill:#1c7ed6}
svg .ref{stroke:#e8590c;stroke-width:1;stroke-dasharray:4 2}
svg .ax{font-size:8px;fill:#888}
svg .xlab{font-size:8px;fill:#888;text-anchor:middle}
svg .empty{font-size:10px;fill:#aaa;text-anchor:middle}
.legend{color:#666;font-size:11px;margin-top:1rem}
.legend span{display:inline-block;width:10px;height:10px;margin:0 2px -1px 8px;border-radius:2px}
</style>`

var legendBlock = fmt.Sprintf(`<p class="legend">response phases:`+
	`<span style="background:%s"></span>travel→src`+
	`<span style="background:%s"></span>buy`+
	`<span style="background:%s"></span>travel→dst`+
	`<span style="background:%s"></span>sell</p>`,
	segColors[0], segColors[1], segColors[2], segColors[3])
