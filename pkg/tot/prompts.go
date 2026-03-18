package tot

import (
	"fmt"
	"strings"

	"github.com/rsned/spacemolt/pkg/agent"
	"github.com/rsned/spacemolt/pkg/game"
)

// AssessOption is one option returned by Stage 1.
type AssessOption struct {
	Action    string `json:"action"`
	Target    string `json:"target"`
	Rationale string `json:"rationale"`
}

// AssessResponse is the parsed Stage 1 LLM output.
type AssessResponse struct {
	Situation string         `json:"situation"`
	Options   []AssessOption `json:"options"`
}

// EvaluateResponse is the parsed Stage 2 LLM output.
type EvaluateResponse struct {
	Action   string     `json:"action"`
	Target   string     `json:"target"`
	Analysis string     `json:"analysis"`
	Scores   AxisScores `json:"scores"`
	NextStep struct {
		Action string `json:"action"`
		Target string `json:"target"`
	} `json:"next_step"`
}

// BuildAssessPrompt constructs the Stage 1 prompt with full context.
func BuildAssessPrompt(p agent.Personality, state *game.State, validActions []ActionOption, pctx *PromptContext) string {
	var sb strings.Builder

	// --- Identity & Personality ---
	fmt.Fprintf(&sb, "You are %s, a %s.\n", p.Name, p.Role)
	if pctx != nil && pctx.BiographyExcerpt != "" {
		fmt.Fprintf(&sb, "%s\n", pctx.BiographyExcerpt)
	}
	if p.Motivations.Primary != "" {
		fmt.Fprintf(&sb, "Your primary drive: %s.", p.Motivations.Primary)
		if p.Motivations.Secondary != "" {
			fmt.Fprintf(&sb, " You also value %s.", p.Motivations.Secondary)
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\n")

	// --- Strategic Goal ---
	if pctx != nil && pctx.Goal != nil {
		fmt.Fprintf(&sb, "CURRENT GOAL: %s — %s (%.0f%% complete, priority %d/10)\n",
			pctx.Goal.Type, pctx.Goal.Target, pctx.Goal.Progress*100, pctx.Goal.Priority)
		if pctx.Goal.Reasoning != "" {
			fmt.Fprintf(&sb, "  Why: %s\n", pctx.Goal.Reasoning)
		}
	}
	if pctx != nil && pctx.Priority != nil && pctx.Priority.Focus != "" {
		fmt.Fprintf(&sb, "FOCUS: %s", pctx.Priority.Focus)
		if len(pctx.Priority.Constraints) > 0 {
			fmt.Fprintf(&sb, " (constraints: %s)", strings.Join(pctx.Priority.Constraints, ", "))
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\n")

	// --- Current Situation ---
	sb.WriteString("CURRENT SITUATION:\n")
	fmt.Fprintf(&sb, "- System: %s", state.System.Name)
	if pctx != nil && pctx.SystemSecurity != "" {
		fmt.Fprintf(&sb, " [%s]", pctx.SystemSecurity)
	}
	sb.WriteString("\n")
	if state.Doc {
		fmt.Fprintf(&sb, "- Docked at: %s\n", state.CurrentPOI)
	}
	if state.MaxFuel > 0 {
		fmt.Fprintf(&sb, "- Fuel: %.0f%%\n", state.Fuel/state.MaxFuel*100)
	}
	if state.MaxHull > 0 {
		fmt.Fprintf(&sb, "- Hull: %.0f%%\n", state.Hull/state.MaxHull*100)
	}
	fmt.Fprintf(&sb, "- Cargo: %d/%d\n", len(state.Cargo), state.MaxCargo)
	fmt.Fprintf(&sb, "- Credits: %.0f\n", state.Credits)
	if state.InCombat {
		sb.WriteString("- !! IN COMBAT !!\n")
	}
	sb.WriteString("\n")

	// --- Recent Actions (short-term memory) ---
	if pctx != nil && len(pctx.RecentActions) > 0 {
		sb.WriteString("RECENT ACTIONS (most recent first):\n")
		for _, a := range pctx.RecentActions {
			result := "ok"
			if a.Result == "error" {
				result = "FAILED"
				if a.Error != "" {
					result = "FAILED: " + a.Error
				}
			}
			if a.Target != "" {
				fmt.Fprintf(&sb, "  %s %s → %s\n", a.Action, a.Target, result)
			} else {
				fmt.Fprintf(&sb, "  %s → %s\n", a.Action, result)
			}
		}
		sb.WriteString("Do NOT repeat failed actions. Avoid repeating query actions (get_*) unless you need fresh data.\n\n")
	}

	// --- Nearby Locations (role-filtered) ---
	writePOIs(&sb, p.Role, state.System.POIs)

	// --- Connected Systems (with security) ---
	if pctx != nil && len(pctx.ConnectedSystemInfo) > 0 {
		sb.WriteString("CONNECTED SYSTEMS:\n")
		for _, sys := range pctx.ConnectedSystemInfo {
			fmt.Fprintf(&sb, "  - %s: %s [%s]", sys.ID, sys.Name, sys.Security)
			if sys.Notes != "" {
				fmt.Fprintf(&sb, " — %s", sys.Notes)
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	} else if len(state.System.Connections) > 0 {
		sb.WriteString("CONNECTED SYSTEMS:\n")
		for _, conn := range state.System.Connections {
			fmt.Fprintf(&sb, "  - %s: %s\n", conn.SystemID, conn.Name)
		}
		sb.WriteString("\n")
	}

	// --- Available Actions ---
	sb.WriteString("AVAILABLE ACTIONS:\n")
	for _, a := range validActions {
		fmt.Fprintf(&sb, "  - %s: %s", a.Action, a.Description)
		if len(a.Targets) > 0 {
			fmt.Fprintf(&sb, " [targets: %s]", strings.Join(a.Targets, ", "))
		}
		sb.WriteString("\n")
	}

	// --- Instructions ---
	sb.WriteString("\nChoose 3 actions that best advance your goal given your situation and recent history.\n")
	sb.WriteString("Prioritize actions that MAKE PROGRESS (mine, travel, sell, undock) over information queries (get_*).\n\n")
	sb.WriteString("Respond with JSON: {\"situation\": \"one sentence\", \"options\": [{\"action\": \"name\", \"target\": \"id or empty\", \"rationale\": \"why\"}]}\n")

	return sb.String()
}

// BuildEvaluatePrompt constructs a Stage 2 prompt for one option.
func BuildEvaluatePrompt(p agent.Personality, state *game.State, situation string, option AssessOption) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "You are %s, a %s.\n\n", p.Name, p.Role)
	fmt.Fprintf(&sb, "SITUATION: %s\n\n", situation)
	fmt.Fprintf(&sb, "You are considering: %s", option.Action)
	if option.Target != "" {
		fmt.Fprintf(&sb, " targeting %s", option.Target)
	}
	fmt.Fprintf(&sb, "\nRationale: %s\n\n", option.Rationale)

	sb.WriteString("STATE: ")
	if state.MaxFuel > 0 {
		fmt.Fprintf(&sb, "Fuel %.0f%%", state.Fuel/state.MaxFuel*100)
	}
	if state.MaxHull > 0 {
		fmt.Fprintf(&sb, " | Hull %.0f%%", state.Hull/state.MaxHull*100)
	}
	fmt.Fprintf(&sb, " | Cargo %d/%d | Credits %.0f", len(state.Cargo), state.MaxCargo, state.Credits)
	if state.InCombat {
		sb.WriteString(" | IN COMBAT")
	}
	sb.WriteString("\n\n")

	sb.WriteString("Score this action 0-100 on each axis:\n")
	sb.WriteString("- survival: keeps me alive (hull, fuel, escape)\n")
	sb.WriteString("- profit: earns credits or resources\n")
	sb.WriteString("- goal_progress: advances my current goal\n")
	sb.WriteString("- risk: how safe (100=safe, 0=dangerous)\n")
	sb.WriteString("- efficiency: good use of my time\n\n")
	sb.WriteString("Respond with JSON: {\"action\": \"" + option.Action + "\", \"target\": \"" + option.Target + "\", ")
	sb.WriteString("\"analysis\": \"2-3 sentences\", \"scores\": {\"survival\": N, \"profit\": N, \"goal_progress\": N, \"risk\": N, \"efficiency\": N}, ")
	sb.WriteString("\"next_step\": {\"action\": \"next\", \"target\": \"id\"}}\n")

	return sb.String()
}

// writePOIs writes role-filtered nearby locations.
func writePOIs(sb *strings.Builder, role string, pois []game.POI) {
	if len(pois) == 0 {
		return
	}

	roleLower := strings.ToLower(role)

	switch {
	case strings.Contains(roleLower, "miner"):
		// Miners care about: resource locations + stations (to sell/refuel)
		sb.WriteString("NEARBY LOCATIONS (mining-relevant):\n")
		for _, poi := range pois {
			switch poi.Type {
			case "asteroid_belt", "asteroid", "asteroid_field":
				fmt.Fprintf(sb, "  - %s: %s (%s) ← MINE HERE\n", poi.ID, poi.Name, poi.Type)
			case "station", "outpost":
				fmt.Fprintf(sb, "  - %s: %s (station — sell/refuel/repair)\n", poi.ID, poi.Name)
			}
		}

	case strings.Contains(roleLower, "trader"), strings.Contains(roleLower, "merchant"):
		// Traders care about: stations (markets) + connections
		sb.WriteString("NEARBY LOCATIONS (trade-relevant):\n")
		for _, poi := range pois {
			if poi.Type == "station" || poi.Type == "outpost" {
				fmt.Fprintf(sb, "  - %s: %s (station — market)\n", poi.ID, poi.Name)
			}
		}

	case strings.Contains(roleLower, "explorer"):
		// Explorers see everything
		sb.WriteString("NEARBY LOCATIONS:\n")
		for _, poi := range pois {
			fmt.Fprintf(sb, "  - %s: %s (%s)\n", poi.ID, poi.Name, poi.Type)
		}

	case strings.Contains(roleLower, "pirate"), strings.Contains(roleLower, "fighter"),
		strings.Contains(roleLower, "mercenary"), strings.Contains(roleLower, "combat"):
		// Combat roles care about: targets + safe havens
		sb.WriteString("NEARBY LOCATIONS:\n")
		for _, poi := range pois {
			switch poi.Type {
			case "station", "outpost":
				fmt.Fprintf(sb, "  - %s: %s (station — safe haven)\n", poi.ID, poi.Name)
			case "asteroid_belt", "asteroid_field":
				fmt.Fprintf(sb, "  - %s: %s (%s — potential ambush site)\n", poi.ID, poi.Name, poi.Type)
			default:
				fmt.Fprintf(sb, "  - %s: %s (%s)\n", poi.ID, poi.Name, poi.Type)
			}
		}

	default:
		// Generic — show all
		sb.WriteString("NEARBY LOCATIONS:\n")
		for _, poi := range pois {
			fmt.Fprintf(sb, "  - %s: %s (%s)\n", poi.ID, poi.Name, poi.Type)
		}
	}
	sb.WriteString("\n")
}
