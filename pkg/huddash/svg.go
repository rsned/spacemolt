package huddash

import (
	"fmt"
	"html"
	"math"
	"strings"
)

// Chart geometry (px). Charts are small, fixed-size, and self-contained.
const (
	chartW = 360
	chartH = 96
	padL   = 30 // left gutter for y labels
	padR   = 6
	padT   = 8
	padB   = 16 // bottom gutter for x labels
)

// segColors are the four per-phase response segments (travel-src, buy, travel-dst, sell).
var segColors = [4]string{"#4263eb", "#ffd43b", "#9775fa", "#ff8787"}

// xyPoint is one (x, y) sample; X is unix seconds, Y the value.
type xyPoint struct{ X, Y float64 }

// bucketBar is one labeled bar (single value).
type bucketBar struct {
	Label string
	Value float64
	Title string // hover text
}

// stackBar is one labeled bar split into stacked segments.
type stackBar struct {
	Label string
	Segs  [4]float64
	Title string
}

func chartOpen(b *strings.Builder, caption string) {
	fmt.Fprintf(b, `<figure class="chart"><figcaption>%s</figcaption>`, html.EscapeString(caption))
	fmt.Fprintf(b, `<svg viewBox="0 0 %d %d" preserveAspectRatio="none" role="img">`, chartW, chartH)
	// baseline
	fmt.Fprintf(b, `<line x1="%d" y1="%d" x2="%d" y2="%d" class="axis"/>`,
		padL, chartH-padB, chartW-padR, chartH-padB)
}

func chartClose(b *strings.Builder) {
	b.WriteString(`</svg></figure>`)
}

func emptyChart(caption string) string {
	var b strings.Builder
	chartOpen(&b, caption)
	fmt.Fprintf(&b, `<text x="%d" y="%d" class="empty">no data yet</text>`, chartW/2, chartH/2)
	chartClose(&b)
	return b.String()
}

// lineChartSVG plots points (any order) as a single polyline scaled to its own min/max.
func lineChartSVG(caption string, pts []xyPoint) string {
	if len(pts) == 0 {
		return emptyChart(caption)
	}
	minX, maxX := pts[0].X, pts[0].X
	minY, maxY := pts[0].Y, pts[0].Y
	for _, p := range pts {
		minX, maxX = math.Min(minX, p.X), math.Max(maxX, p.X)
		minY, maxY = math.Min(minY, p.Y), math.Max(maxY, p.Y)
	}
	sx := func(x float64) float64 {
		if maxX == minX {
			return padL
		}
		return padL + (x-minX)/(maxX-minX)*(chartW-padL-padR)
	}
	sy := func(y float64) float64 {
		if maxY == minY {
			return chartH - padB - (chartH-padT-padB)/2
		}
		return chartH - padB - (y-minY)/(maxY-minY)*(chartH-padT-padB)
	}
	var poly strings.Builder
	for i, p := range pts {
		if i > 0 {
			poly.WriteByte(' ')
		}
		fmt.Fprintf(&poly, "%.1f,%.1f", sx(p.X), sy(p.Y))
	}
	var b strings.Builder
	chartOpen(&b, caption)
	fmt.Fprintf(&b, `<polyline fill="none" stroke="#2b8a3e" stroke-width="1.5" points="%s"/>`, poly.String())
	fmt.Fprintf(&b, `<text x="%d" y="%d" class="ax">%s</text>`, padL-2, padT+4, fmtNum(maxY))
	fmt.Fprintf(&b, `<text x="%d" y="%d" class="ax">%s</text>`, padL-2, chartH-padB, fmtNum(minY))
	chartClose(&b)
	return b.String()
}

// barChartSVG draws one bar per bucket, scaled to the max value, with an optional dashed
// reference line (refLabel non-empty draws it).
func barChartSVG(caption string, bars []bucketBar, ref float64, refLabel string) string {
	if len(bars) == 0 {
		return emptyChart(caption)
	}
	maxV := 0.0
	for _, bar := range bars {
		maxV = math.Max(maxV, bar.Value)
	}
	maxV = math.Max(maxV, ref)
	if maxV <= 0 {
		maxV = 1
	}
	plotW := float64(chartW - padL - padR)
	plotH := float64(chartH - padT - padB)
	slot := plotW / float64(len(bars))
	bw := math.Max(1, slot*0.7)
	var b strings.Builder
	chartOpen(&b, caption)
	fmt.Fprintf(&b, `<text x="%d" y="%d" class="ax">%s</text>`, padL-2, padT+4, fmtNum(maxV))
	for i, bar := range bars {
		x := float64(padL) + float64(i)*slot + (slot-bw)/2
		h := bar.Value / maxV * plotH
		y := float64(chartH-padB) - h
		fmt.Fprintf(&b, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" class="bar"><title>%s</title></rect>`,
			x, y, bw, h, html.EscapeString(bar.Title))
		if i == 0 || i == len(bars)-1 {
			fmt.Fprintf(&b, `<text x="%.1f" y="%d" class="xlab">%s</text>`,
				x+bw/2, chartH-4, html.EscapeString(bar.Label))
		}
	}
	if refLabel != "" {
		y := float64(chartH-padB) - ref/maxV*plotH
		fmt.Fprintf(&b, `<line x1="%d" y1="%.1f" x2="%d" y2="%.1f" class="ref"/>`, padL, y, chartW-padR, y)
		fmt.Fprintf(&b, `<text x="%d" y="%.1f" class="ax">%s</text>`, chartW-padR-2, y-2, html.EscapeString(refLabel))
	}
	chartClose(&b)
	return b.String()
}

// stackedBarChartSVG draws one stacked bar per bucket (four phase segments).
func stackedBarChartSVG(caption string, bars []stackBar) string {
	if len(bars) == 0 {
		return emptyChart(caption)
	}
	maxV := 0.0
	for _, bar := range bars {
		sum := bar.Segs[0] + bar.Segs[1] + bar.Segs[2] + bar.Segs[3]
		maxV = math.Max(maxV, sum)
	}
	if maxV <= 0 {
		maxV = 1
	}
	plotW := float64(chartW - padL - padR)
	plotH := float64(chartH - padT - padB)
	slot := plotW / float64(len(bars))
	bw := math.Max(1, slot*0.7)
	var b strings.Builder
	chartOpen(&b, caption)
	fmt.Fprintf(&b, `<text x="%d" y="%d" class="ax">%s</text>`, padL-2, padT+4, fmtNum(maxV))
	for i, bar := range bars {
		x := float64(padL) + float64(i)*slot + (slot-bw)/2
		yBase := float64(chartH - padB)
		for s, v := range bar.Segs {
			if v <= 0 {
				continue
			}
			h := v / maxV * plotH
			yBase -= h
			fmt.Fprintf(&b, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s"><title>%s</title></rect>`,
				x, yBase, bw, h, segColors[s], html.EscapeString(bar.Title))
		}
		if i == 0 || i == len(bars)-1 {
			fmt.Fprintf(&b, `<text x="%.1f" y="%d" class="xlab">%s</text>`,
				x+bw/2, chartH-4, html.EscapeString(bar.Label))
		}
	}
	chartClose(&b)
	return b.String()
}

// fmtNum renders a value compactly (k/M suffix) for axis labels.
func fmtNum(v float64) string {
	a := math.Abs(v)
	switch {
	case a >= 1e6:
		return fmt.Sprintf("%.1fM", v/1e6)
	case a >= 1e3:
		return fmt.Sprintf("%.0fk", v/1e3)
	default:
		return fmt.Sprintf("%.0f", v)
	}
}
