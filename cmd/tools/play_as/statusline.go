package main

import (
	"fmt"
	"math"
	"strings"

	"github.com/rsned/spacemolt/pkg/game"
)

// ANSI color codes.
const (
	ansiReset     = "\033[0m"
	ansiRed       = "\033[31m"
	ansiBrightRed = "\033[91m"
	ansiYellow    = "\033[33m"
	ansiGreen     = "\033[32m"
	ansiBrightGreen = "\033[92m"
	ansiMagenta   = "\033[35m"
	ansiCyan      = "\033[36m"
	ansiWhite     = "\033[37m"
	ansiGray      = "\033[90m"
)

// colorCode maps color names from config to ANSI escape sequences.
func colorCode(name string) string {
	switch strings.ToLower(name) {
	case "red":
		return ansiRed
	case "bright_red":
		return ansiBrightRed
	case "yellow":
		return ansiYellow
	case "green":
		return ansiGreen
	case "bright_green":
		return ansiBrightGreen
	case "magenta":
		return ansiMagenta
	case "cyan":
		return ansiCyan
	case "white":
		return ansiWhite
	case "gray":
		return ansiGray
	default:
		return ""
	}
}

// colorize wraps text with the given ANSI color code and reset.
func colorize(text, color string) string {
	code := colorCode(color)
	if code == "" {
		return text
	}
	return code + text + ansiReset
}

// thresholdColor returns the color name for a percentage based on thresholds.
// If inverted is true, high values are bad (e.g. cargo usage).
func thresholdColor(pct float64, t ThresholdConfig, inverted bool) string {
	if inverted {
		pct = 100 - pct
	}
	switch {
	case pct <= float64(t.Critical):
		return "red"
	case pct <= float64(t.Warning):
		return "yellow"
	case pct <= float64(t.Good):
		return "green"
	default:
		return "bright_green"
	}
}

// pctOf computes the percentage of cur/max, returning 0 if max is 0.
func pctOf(cur, max float64) float64 {
	if max <= 0 {
		return 0
	}
	return (cur / max) * 100
}

// renderBar renders a mini bar like [████░░] 73%.
func renderBar(pct float64, cfg StatuslineConfig, color string) string {
	filled := min(max(int(math.Round(pct/100*float64(cfg.BarWidth))), 0), cfg.BarWidth)
	empty := cfg.BarWidth - filled

	bar := "[" + strings.Repeat("█", filled) + strings.Repeat("░", empty) + "]"
	pctStr := fmt.Sprintf("%d%%", int(math.Round(pct)))

	var text string
	switch cfg.BarStyle {
	case "numeric":
		text = pctStr
	case "bar_only":
		text = bar
	default: // "bar"
		text = bar + " " + pctStr
	}

	return colorize(text, color)
}

// renderPctSegment renders a percentage-based segment with bar and color.
func renderPctSegment(cur, max float64, cfg StatuslineConfig, inverted bool) string {
	pct := pctOf(cur, max)
	color := thresholdColor(pct, cfg.Thresholds, inverted)
	return renderBar(pct, cfg, color)
}

// formatCredits formats a number with comma separators.
func formatCredits(n float64) string {
	neg := n < 0
	if neg {
		n = -n
	}

	whole := int64(n)
	s := fmt.Sprintf("%d", whole)

	// Insert commas from the right.
	if len(s) > 3 {
		var b strings.Builder
		offset := len(s) % 3
		if offset > 0 {
			b.WriteString(s[:offset])
		}
		for i := offset; i < len(s); i += 3 {
			if b.Len() > 0 {
				b.WriteByte(',')
			}
			b.WriteString(s[i : i+3])
		}
		s = b.String()
	}

	if neg {
		return "-" + s
	}
	return s
}

// renderStatusline renders the full statusline string from game state.
func renderStatusline(client game.GameClient, cfg PlayAsConfig, agentID string) string {
	if !cfg.Statusline.Enabled {
		return ""
	}

	state := client.GetState()
	sl := cfg.Statusline

	// Build segment lookup.
	segments := map[string]string{
		// Identity
		"agent":      agentID,
		"player":     state.Player.Username,
		"empire":     state.Player.Empire,
		"faction":    state.Player.FactionID,

		// Location
		"system":     colorize(state.System.Name, securityColor(state.System.SecurityStatus, sl)),
		"system_id":  colorize(state.System.ID, securityColor(state.System.SecurityStatus, sl)),
		"security":   colorize(state.System.SecurityStatus, securityColor(state.System.SecurityStatus, sl)),
		"poi":        state.CurrentPOI,

		// Dock status
		"dock": dockIndicator(state, sl),

		// Ship identity
		"ship_class": state.Ship.ClassID,
		"ship_name":  state.Ship.Name,

		// Percentage bars
		"hull":   renderPctSegment(state.Ship.Hull, state.Ship.MaxHull, sl, false),
		"shield": renderPctSegment(state.Ship.Shield, state.Ship.MaxShield, sl, false),
		"fuel":   renderPctSegment(state.Ship.Fuel, state.Ship.MaxFuel, sl, false),
		"cargo":  renderPctSegment(state.Ship.CargoUsed, state.Ship.CargoCapacity, sl, true),
		"cpu":    renderPctSegment(state.Ship.CPUUsed, state.Ship.CPUCapacity, sl, true),
		"power":  renderPctSegment(state.Ship.PowerUsed, state.Ship.PowerCapacity, sl, true),

		// Status
		"tick":    tickString(state),
		"credits": formatCredits(state.Credits),

		// Conditional indicators
		"combat": combatIndicator(state, sl),
		"cloak":  cloakIndicator(state, sl),
		"nearby": fmt.Sprintf("%d", len(state.Nearby)),
	}

	// Expand format string by replacing {name} tokens.
	result := sl.Format
	for name, value := range segments {
		result = strings.ReplaceAll(result, "{"+name+"}", value)
	}

	return result
}

// securityColor returns the config color name for a security status string.
func securityColor(status string, sl StatuslineConfig) string {
	if c, ok := sl.SecurityColors[status]; ok {
		return c
	}
	return ""
}

// dockIndicator returns the dock/space indicator string.
func dockIndicator(state *game.State, sl StatuslineConfig) string {
	if state.Doc {
		return sl.Indicators.Docked
	}
	return sl.Indicators.InSpace
}

// combatIndicator returns the combat indicator if in combat, empty otherwise.
func combatIndicator(state *game.State, sl StatuslineConfig) string {
	if state.InCombat || state.InBattle {
		return colorize(sl.Indicators.InCombat, "bright_red")
	}
	return ""
}

// cloakIndicator returns the cloak indicator if cloaked, empty otherwise.
func cloakIndicator(state *game.State, sl StatuslineConfig) string {
	if state.Player.IsCloaked {
		return colorize(sl.Indicators.Cloaked, "cyan")
	}
	return ""
}

// tickString returns the current tick from the game clock if available,
// falling back to the state's tick value.
func tickString(state *game.State) string {
	if globalClock != nil {
		return fmt.Sprintf("%d", globalClock.Tick())
	}
	return fmt.Sprintf("%d", state.CurrentTick)
}
