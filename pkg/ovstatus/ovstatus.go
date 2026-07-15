// Package ovstatus renders a live status page for one or more overminds.
//
// Each overmind writes a balances.StatusFile (captured_at + workers[]) on every
// heartbeat tick. This package reads those files on demand and renders a single
// HTML page: a table of contents listing every overmind, then one alphabetical
// worker table per overmind showing name, credits, fleet role, current
// task/status, and last position. The page is re-rendered on every request, so a
// manual browser refresh always reflects the newest snapshot the overminds have
// written.
package ovstatus

import (
	"fmt"
	"html"
	"sort"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/overmind/balances"
)

// Source identifies one overmind's live status file.
type Source struct {
	// Name is the human-readable overmind label shown on the page (e.g. "Haul").
	Name string
	// Path is the path to that overmind's status JSON file.
	Path string
}

// HaulStats bundles the haul-fleet efficiency data rendered onto the page: an
// optional fleet panel and a per-agent lifetime line map. A nil *HaulStats (or
// nil fields) renders the page exactly as before.
type HaulStats struct {
	Panel    *EffPanel                // nil -> no panel
	Lifetime map[string]AgentLifetime // agent_id -> per-worker line; absent -> no line
}

// EffPanel is the fleet efficiency headline: windowed gross/fuel/net cr/jump and
// a per-agent net/jump ranking.
type EffPanel struct {
	WindowLabel  string
	Hauls        int
	GrossPerJump float64
	FuelPerJump  float64
	NetPerJump   float64
	Agents       []PanelAgent // ranked NetPerJump desc (caller sorts)
}

// PanelAgent is one agent's windowed net cr/jump for the panel ranking.
type PanelAgent struct {
	AgentID    string
	Hauls      int
	NetPerJump float64
}

// AgentLifetime is one worker's lifetime stats line (gross avg, no fuel term).
type AgentLifetime struct {
	Hauls      int
	Jumps      int64
	AvgPerJump float64
}

// PerJumpMetrics returns gross, fuel, and net credits-per-jump for a haul
// aggregate. fuelPerJump is fuel units burned per jump; fuelCrPerUnit is the
// credit price per fuel unit; their product is the (constant) fuel cr/jump.
// When sumJumps <= 0 all three are 0 (no hauls, no divide).
func PerJumpMetrics(sumProfit float64, sumJumps int64, fuelPerJump, fuelCrPerUnit float64) (gross, fuelCr, net float64) {
	if sumJumps <= 0 {
		return 0, 0, 0
	}
	gross = sumProfit / float64(sumJumps)
	fuelCr = fuelPerJump * fuelCrPerUnit
	net = gross - fuelCr
	return gross, fuelCr, net
}

// staleAfter is how long since a worker's last heartbeat before it is flagged
// stale on the page. Heartbeats land roughly every 30s, so a couple of minutes
// of silence is a real signal something is wrong.
const staleAfter = 2 * time.Minute

// section is one rendered overmind: its label, snapshot metadata, and sorted
// workers. anchor is the in-page id used by the table of contents.
type section struct {
	Name       string
	Anchor     string
	CapturedAt time.Time
	HasFile    bool
	LoadErr    string
	Workers    []balances.LiveRecord
}

// Render reads every source and returns a complete HTML document. refresh is the
// auto-refresh interval in seconds (a meta-refresh); now is the reference time
// used for relative "last seen" text and staleness. Sources that fail to load
// are rendered with an inline error rather than failing the whole page.
func Render(sources []Source, hs *HaulStats, refresh int, now time.Time) string {
	sections := make([]section, 0, len(sources))
	for i, src := range sources {
		sec := section{Name: src.Name, Anchor: anchorID(src.Name, i)}
		sf, err := balances.ReadStatus(src.Path)
		switch {
		case err != nil:
			sec.LoadErr = err.Error()
		case sf.CapturedAt == "" && len(sf.Workers) == 0:
			// ReadStatus returns a zero StatusFile when the file is absent.
			sec.HasFile = false
		default:
			sec.HasFile = true
			if t, perr := time.Parse(time.RFC3339, sf.CapturedAt); perr == nil {
				sec.CapturedAt = t
			}
			sec.Workers = append(sec.Workers, sf.Workers...)
			sort.Slice(sec.Workers, func(a, b int) bool {
				return strings.ToLower(sec.Workers[a].AgentID) < strings.ToLower(sec.Workers[b].AgentID)
			})
		}
		sections = append(sections, sec)
	}
	return renderDoc(sections, hs, refresh, now)
}

func renderDoc(sections []section, hs *HaulStats, refresh int, now time.Time) string {
	var b strings.Builder
	b.WriteString("<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n")
	b.WriteString("<meta charset=\"utf-8\">\n")
	b.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	if refresh > 0 {
		fmt.Fprintf(&b, "<meta http-equiv=\"refresh\" content=\"%d\">\n", refresh)
	}
	b.WriteString("<title>Overmind Fleet Status</title>\n")
	b.WriteString(styleBlock)
	b.WriteString("</head>\n<body>\n")

	fmt.Fprintf(&b, "<header><h1>Overmind Fleet Status</h1>"+
		"<div class=\"meta\">Rendered %s · auto-refresh %s "+
		"<button onclick=\"location.reload()\">Refresh now</button></div></header>\n",
		html.EscapeString(now.UTC().Format("2006-01-02 15:04:05 UTC")), refreshLabel(refresh))

	// Table of contents.
	b.WriteString("<nav class=\"toc\"><strong>Overminds:</strong> ")
	for i, sec := range sections {
		if i > 0 {
			b.WriteString(" · ")
		}
		count := len(sec.Workers)
		fmt.Fprintf(&b, "<a href=\"#%s\">%s</a> <span class=\"count\">(%d)</span>",
			sec.Anchor, html.EscapeString(sec.Name), count)
	}
	b.WriteString("</nav>\n")

	if hs != nil && hs.Panel != nil {
		renderEffPanel(&b, hs.Panel)
	}

	for _, sec := range sections {
		renderSection(&b, sec, hs, now)
	}

	b.WriteString("</body>\n</html>\n")
	return b.String()
}

func renderSection(b *strings.Builder, sec section, hs *HaulStats, now time.Time) {
	fmt.Fprintf(b, "<section id=\"%s\">\n", sec.Anchor)
	fmt.Fprintf(b, "<h2>%s", html.EscapeString(sec.Name))
	if sec.HasFile {
		fmt.Fprintf(b, " <span class=\"subtle\">%d agents</span>", len(sec.Workers))
	}
	b.WriteString("</h2>\n")

	switch {
	case sec.LoadErr != "":
		fmt.Fprintf(b, "<p class=\"err\">Failed to read status file: %s</p>\n", html.EscapeString(sec.LoadErr))
		b.WriteString("</section>\n")
		return
	case !sec.HasFile:
		b.WriteString("<p class=\"subtle\">No status file yet — overmind not running or has not written a snapshot.</p>\n")
		b.WriteString("</section>\n")
		return
	}

	capturedTxt := "unknown"
	if !sec.CapturedAt.IsZero() {
		capturedTxt = fmt.Sprintf("%s (%s)", sec.CapturedAt.UTC().Format("15:04:05 UTC"), relTime(sec.CapturedAt, now))
	}
	fmt.Fprintf(b, "<p class=\"subtle\">Snapshot captured %s</p>\n", html.EscapeString(capturedTxt))

	if len(sec.Workers) == 0 {
		b.WriteString("<p class=\"subtle\">No workers in snapshot.</p>\n</section>\n")
		return
	}

	b.WriteString("<table>\n<thead><tr>" +
		"<th>Name</th><th class=\"num\">Credits</th><th>Role</th>" +
		"<th>Task / Status</th><th>Position</th><th>Last seen</th>" +
		"</tr></thead>\n<tbody>\n")
	for _, w := range sec.Workers {
		renderRow(b, w, hs, now)
	}
	b.WriteString("</tbody>\n</table>\n</section>\n")
}

func renderRow(b *strings.Builder, w balances.LiveRecord, hs *HaulStats, now time.Time) {
	stale := isStale(w.LastSeen, now)
	cls := ""
	if !w.Healthy || stale {
		cls = " class=\"warn\""
	}
	fmt.Fprintf(b, "<tr%s>", cls)
	fmt.Fprintf(b, "<td>%s</td>", html.EscapeString(nameText(w)))
	fmt.Fprintf(b, "<td class=\"num\">%s</td>", html.EscapeString(formatCredits(w.Credits)))
	fmt.Fprintf(b, "<td>%s</td>", html.EscapeString(roleText(w)))
	fmt.Fprintf(b, "<td>%s</td>", html.EscapeString(statusText(w, stale)))
	fmt.Fprintf(b, "<td>%s</td>", html.EscapeString(positionText(w)))
	fmt.Fprintf(b, "<td>%s</td>", html.EscapeString(lastSeenText(w.LastSeen, now)))
	b.WriteString("</tr>\n")
	if hs != nil {
		if lt, ok := hs.Lifetime[w.AgentID]; ok {
			renderLifetimeLine(b, lt)
		}
	}
}

// renderEffPanel renders the fleet efficiency headline above the per-overmind
// sections.
func renderEffPanel(b *strings.Builder, p *EffPanel) {
	b.WriteString("<section class=\"effpanel\">\n")
	fmt.Fprintf(b, "<h2>Haul fleet efficiency <span class=\"subtle\">(%s)</span></h2>\n", html.EscapeString(p.WindowLabel))
	if p.Hauls == 0 {
		fmt.Fprintf(b, "<p class=\"subtle\">No hauls in the last %s.</p>\n</section>\n", html.EscapeString(p.WindowLabel))
		return
	}
	fmt.Fprintf(b, "<p class=\"effhead\">gross %s − fuel %s = <strong>NET %s cr/jump</strong> · %s hauls</p>\n",
		formatCredits(p.GrossPerJump), formatCredits(p.FuelPerJump), formatCredits(p.NetPerJump), formatCredits(float64(p.Hauls)))
	if len(p.Agents) > 0 {
		b.WriteString("<p class=\"subtle\">")
		for i, a := range p.Agents {
			if i > 0 {
				b.WriteString(" · ")
			}
			fmt.Fprintf(b, "%s %dh %s", html.EscapeString(a.AgentID), a.Hauls, formatCredits(a.NetPerJump))
		}
		b.WriteString("</p>\n")
	}
	b.WriteString("</section>\n")
}

// renderLifetimeLine emits a sub-row spanning all six columns with a worker's
// lifetime throughput. Ship losses show as an em dash until a death counter
// exists (SP-loss).
func renderLifetimeLine(b *strings.Builder, lt AgentLifetime) {
	fmt.Fprintf(b, "<tr class=\"eff-line\"><td colspan=\"6\" class=\"subtle\">%s hauls · %s jumps · — losses · avg %s cr/jump</td></tr>\n",
		formatCredits(float64(lt.Hauls)), formatCredits(float64(lt.Jumps)), formatCredits(lt.AvgPerJump))
}

// nameText is the worker's agent id, suffixed with its faction tag as
// "agent (TAG)" once the tag is known so the table shows faction membership.
func nameText(w balances.LiveRecord) string {
	// Server pads faction tags to a fixed width (e.g. " DB "); trim so it renders
	// as "agent (DB)" rather than "agent ( DB )".
	if tag := strings.TrimSpace(w.FactionTag); tag != "" {
		return fmt.Sprintf("%s (%s)", w.AgentID, tag)
	}
	return w.AgentID
}

// roleText is the fleet role: the assigned role, noting the standing behavior
// when it differs (e.g. a hauler temporarily running a different behavior).
func roleText(w balances.LiveRecord) string {
	if w.StandingBehavior != "" && w.StandingBehavior != w.Role {
		return fmt.Sprintf("%s (%s)", w.Role, w.StandingBehavior)
	}
	if w.Role == "" {
		return w.StandingBehavior
	}
	return w.Role
}

// statusText synthesizes the worker's current task/status from the snapshot:
// an explicit task id when assigned, otherwise the standing behavior, annotated
// with health, docked/in-transit, and staleness flags.
func statusText(w balances.LiveRecord, stale bool) string {
	var parts []string
	if !w.Seen {
		parts = append(parts, "no heartbeat yet")
	}
	if w.ActiveTaskID != "" {
		parts = append(parts, "task "+w.ActiveTaskID)
	} else if w.StandingBehavior != "" {
		parts = append(parts, w.StandingBehavior)
	} else {
		parts = append(parts, "idle")
	}
	if w.Docked {
		parts = append(parts, "docked")
	} else {
		parts = append(parts, "in transit")
	}
	if !w.Healthy {
		parts = append(parts, "⚠ unhealthy")
	}
	if w.Restarts > 0 {
		parts = append(parts, fmt.Sprintf("%d restart%s", w.Restarts, plural(w.Restarts)))
	}
	if stale {
		parts = append(parts, "⚠ stale")
	}
	return strings.Join(parts, " · ")
}

func positionText(w balances.LiveRecord) string {
	switch {
	case w.System == "" && w.POI == "":
		return "—"
	case w.POI == "":
		return w.System
	case w.System == "":
		return w.POI
	default:
		return w.System + " / " + w.POI
	}
}

func lastSeenText(ts string, now time.Time) string {
	if ts == "" {
		return "never"
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}
	return relTime(t, now)
}

func isStale(ts string, now time.Time) bool {
	if ts == "" {
		return true
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return false
	}
	return now.Sub(t) > staleAfter
}

// relTime renders a compact "Ns/Nm/Nh/Nd ago" (or "in N…" for future stamps).
func relTime(t, now time.Time) string {
	d := now.Sub(t)
	suffix := "ago"
	if d < 0 {
		d = -d
		suffix = "from now"
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds %s", int(d.Seconds()), suffix)
	case d < time.Hour:
		return fmt.Sprintf("%dm %s", int(d.Minutes()), suffix)
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh %s", int(d.Hours()), suffix)
	default:
		return fmt.Sprintf("%dd %s", int(d.Hours()/24), suffix)
	}
}

// formatCredits renders an integer-credit value with thousands separators.
func formatCredits(c float64) string {
	n := int64(c)
	neg := n < 0
	if neg {
		n = -n
	}
	s := fmt.Sprintf("%d", n)
	var out []byte
	for i, ch := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, ch)
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func refreshLabel(refresh int) string {
	if refresh <= 0 {
		return "off"
	}
	if refresh%60 == 0 {
		return fmt.Sprintf("%dm", refresh/60)
	}
	return fmt.Sprintf("%ds", refresh)
}

// anchorID derives a stable, unique in-page anchor from the overmind name.
func anchorID(name string, idx int) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		slug = "overmind"
	}
	return fmt.Sprintf("%s-%d", slug, idx)
}

const styleBlock = `<style>
:root { color-scheme: light dark; }
body { font-family: system-ui, sans-serif; margin: 1.5rem; line-height: 1.4; }
header { display: flex; flex-wrap: wrap; align-items: baseline; gap: 0.75rem; }
h1 { margin: 0 0 0.25rem; font-size: 1.4rem; }
h2 { margin: 1.5rem 0 0.25rem; font-size: 1.15rem; }
.meta { font-size: 0.85rem; opacity: 0.8; }
.meta button { margin-left: 0.5rem; cursor: pointer; }
.toc { margin: 0.75rem 0 0.5rem; padding: 0.5rem 0.75rem; border: 1px solid rgba(128,128,128,0.3); border-radius: 6px; font-size: 0.95rem; }
.toc a { text-decoration: none; }
.toc .count { opacity: 0.6; font-size: 0.85rem; }
.subtle { opacity: 0.65; font-size: 0.85rem; margin: 0.15rem 0; }
.effpanel { margin: 0.5rem 0 1rem; padding: 0.5rem 0.75rem; border: 1px solid rgba(127,127,127,0.3); border-radius: 6px; }
.effhead { font-size: 1rem; margin: 0.2rem 0; }
tr.eff-line td { padding-top: 0; border-top: 0; }
.err { color: #c0392b; font-weight: 600; }
table { border-collapse: collapse; width: 100%; margin: 0.4rem 0 1rem; font-size: 0.9rem; }
th, td { text-align: left; padding: 0.3rem 0.6rem; border-bottom: 1px solid rgba(128,128,128,0.25); }
th { font-weight: 600; border-bottom: 2px solid rgba(128,128,128,0.5); }
td.num, th.num { text-align: right; font-variant-numeric: tabular-nums; }
tr.warn td { background: rgba(192,57,43,0.12); }
tbody tr:hover td { background: rgba(128,128,128,0.10); }
.subtle { font-weight: normal; }
</style>
`
