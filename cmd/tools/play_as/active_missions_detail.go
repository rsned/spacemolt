package main

import (
	"fmt"
	"strings"
)

// objectiveDetail is the second line under an active-mission objective: where
// it is done and, for item objectives, how much of the item the player already
// holds. Empty when the server sent none of that. The item_id itself goes on
// the objective row.
func objectiveDetail(targetBase, system string, inCargo, inStorage *int) string {
	var parts []string
	if targetBase != "" {
		where := targetBase
		if system != "" {
			where += ", " + system
		}
		parts = append(parts, where)
	}
	var have []string
	if inCargo != nil {
		have = append(have, fmt.Sprintf("%d in cargo", *inCargo))
	}
	if inStorage != nil {
		have = append(have, fmt.Sprintf("%d in storage", *inStorage))
	}
	if len(have) > 0 {
		parts = append(parts, "have "+strings.Join(have, ", "))
	}
	return strings.Join(parts, " · ")
}
