package main

import (
	"fmt"
	"strings"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// missionRef renders "Title (mission_id)".
func missionRef(p serverapi.MissionPointer) string {
	if p.Title == "" || p.Title == p.MissionID {
		return p.MissionID
	}
	return fmt.Sprintf("%s (%s)", p.Title, p.MissionID)
}

// missionWhere renders "Base, System", or "" when the mission is not tied to
// one station.
func missionWhere(p serverapi.MissionPointer) string {
	switch {
	case p.BaseName != "" && p.SystemName != "":
		return p.BaseName + ", " + p.SystemName
	case p.BaseName != "":
		return p.BaseName
	default:
		return p.SystemName
	}
}

// prerequisiteLine renders one still-required mission and, when it differs,
// the next mission to work on toward it.
func prerequisiteLine(r serverapi.MissionPrerequisite) string {
	line := "needs " + missionRef(r.Mission)
	if r.NextStep.MissionID != "" && r.NextStep.MissionID != r.Mission.MissionID {
		line += " → next: " + missionRef(r.NextStep)
		if w := missionWhere(r.NextStep); w != "" {
			line += " at " + w
		}
	} else if w := missionWhere(r.Mission); w != "" {
		line += " at " + w
	}
	return line
}

// formatUnlocks renders complete_mission / view_completed_mission "unlocks".
func formatUnlocks(unlocks []serverapi.MissionUnlock) string {
	if len(unlocks) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Unlocks:\n")
	for _, u := range unlocks {
		where := missionWhere(u.Mission)
		switch {
		case u.Available:
			line := "  ✓ " + missionRef(u.Mission) + " — available"
			if where != "" {
				line += " at " + where
			}
			b.WriteString(line + "\n")
		case len(u.StillRequires) == 0:
			fmt.Fprintf(&b, "  ✗ %s — non-mission requirement unmet (accept_mission names it)\n", missionRef(u.Mission))
		default:
			line := "  ✗ " + missionRef(u.Mission)
			if where != "" {
				line += " at " + where
			}
			b.WriteString(line + "\n")
			for _, r := range u.StillRequires {
				b.WriteString("      " + prerequisiteLine(r) + "\n")
			}
		}
	}
	return b.String()
}

// formatLockedMissions renders get_missions "locked_missions". The locked
// missions themselves are not named by the server.
func formatLockedMissions(hints []serverapi.LockedMissionHint) string {
	if len(hints) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Locked here (%d):\n", len(hints))
	for i, h := range hints {
		for j, r := range h.StillRequires {
			marker := "   "
			if j == 0 {
				marker = fmt.Sprintf("%d.", i+1)
			}
			fmt.Fprintf(&b, "  %s %s\n", marker, prerequisiteLine(r))
		}
	}
	return b.String()
}
