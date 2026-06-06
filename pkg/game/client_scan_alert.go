package game

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
)

// scanAlertLogger writes to stderr regardless of debug settings so that a
// player being scanned by another ship is always visible, the same way
// apiChangeLogger surfaces server API changes. Coloring is applied per-line
// (rather than via a fixed prefix) because the severity — and therefore the
// color — depends on whether the current system is lawless.
var scanAlertLogger = log.New(log.Writer(), "", log.LstdFlags)

// scanAlertIsTTY reports whether scan alerts should be colorized. Cached at
// package init so the check isn't repeated on every scan.
var scanAlertIsTTY = isatty.IsTerminal(os.Stderr.Fd())

// formatScanAlert renders a high-contrast, always-visible alert for a
// scan_detected event. In lawless/unpoliced space a scan is often the prelude
// to an attack, so it gets the loudest treatment (bold white on red); in
// policed space it is still surfaced but with a milder bold-yellow banner.
//
// revealed_info renders as "No information revealed" when the server reports
// none (null/empty), instead of the raw "<nil>".
func formatScanAlert(payload map[string]any, lawless bool) string {
	scanner, _ := payload["scanner_username"].(string)
	shipClass, _ := payload["scanner_ship_class"].(string)
	if scanner == "" {
		scanner = "an unknown ship"
	}

	who := scanner
	if shipClass != "" {
		who = fmt.Sprintf("%s (%s)", scanner, shipClass)
	}

	revealed := "No information revealed"
	if list, ok := payload["revealed_info"].([]any); ok && len(list) > 0 {
		parts := make([]string, 0, len(list))
		for _, v := range list {
			if s, ok := v.(string); ok && s != "" {
				parts = append(parts, s)
			}
		}
		if len(parts) > 0 {
			revealed = "Revealed: " + strings.Join(parts, ", ")
		}
	}

	var prefix string
	if lawless {
		prefix = "[⚠ SCANNED — LAWLESS SPACE]"
	} else {
		prefix = "[SCANNED]"
	}

	line := fmt.Sprintf("%s Scanned by %s — %s", prefix, who, revealed)
	if !scanAlertIsTTY {
		return line
	}

	if lawless {
		// Bold (1) + bright white fg (97) + red bg (41); reset (0).
		return "\x1b[1;97;41m" + line + "\x1b[0m"
	}
	// Bold (1) + bright yellow fg (93); reset (0). Visible but not alarming.
	return "\x1b[1;93m" + line + "\x1b[0m"
}

// systemIsLawless reports whether the current system is unpoliced — police
// level 0 or an explicit "Lawless" security status. Mirrors the security
// portion of spar.IsNonEmpireArena. Caller need not hold c.mu.
func (c *Client) systemIsLawless() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state.System.PoliceLevel == 0 || c.state.System.SecurityStatus == "Lawless"
}
