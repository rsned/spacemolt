package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// formatBase puts a station's tourism figures above its full get_base reply. It
// returns "" when the reply has none, which leaves the plain JSON view. The
// server omits these fields at a station with nothing to report.
func formatBase(raw []byte) string {
	var resp serverapi.GetBaseResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}

	var parts []string
	if resp.TourismRevenue > 0 {
		parts = append(parts, commaInt(resp.TourismRevenue)+" cr lifetime")
	}
	if resp.DiningPoints > 0 {
		parts = append(parts, fmt.Sprintf("dining %d", resp.DiningPoints))
	}
	if resp.LeisurePoints > 0 {
		parts = append(parts, fmt.Sprintf("leisure %d", resp.LeisurePoints))
	}
	if len(parts) == 0 {
		return ""
	}

	var pretty any
	if err := json.Unmarshal(raw, &pretty); err != nil {
		return ""
	}
	formatted, err := json.MarshalIndent(pretty, "", "  ")
	if err != nil {
		return ""
	}

	return fmt.Sprintf("Tourism:  %s\n\n%s", strings.Join(parts, ", "), formatted)
}
