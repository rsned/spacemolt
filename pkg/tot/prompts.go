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

// BuildAssessPrompt constructs the Stage 1 prompt.
func BuildAssessPrompt(p agent.Personality, state *game.State, validActions []ActionOption) string {
	var sb strings.Builder
	// /no_think disables qwen3's extended thinking to get direct JSON output
	sb.WriteString("/no_think\n")
	fmt.Fprintf(&sb, "You are %s, a %s.\n\n", p.Name, p.Role)

	sb.WriteString("CURRENT SITUATION:\n")
	fmt.Fprintf(&sb, "- System: %s (%s)\n", state.System.Name, state.System.ID)
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
		sb.WriteString("- IN COMBAT\n")
	}

	if len(state.System.POIs) > 0 {
		sb.WriteString("\nNEARBY LOCATIONS:\n")
		for _, poi := range state.System.POIs {
			fmt.Fprintf(&sb, "  - %s: %s (%s)\n", poi.ID, poi.Name, poi.Type)
		}
	}

	if len(state.System.Connections) > 0 {
		sb.WriteString("\nCONNECTED SYSTEMS:\n")
		for _, conn := range state.System.Connections {
			fmt.Fprintf(&sb, "  - %s: %s\n", conn.SystemID, conn.Name)
		}
	}

	sb.WriteString("\nAVAILABLE ACTIONS:\n")
	for _, a := range validActions {
		fmt.Fprintf(&sb, "  - %s: %s", a.Action, a.Description)
		if len(a.Targets) > 0 {
			fmt.Fprintf(&sb, " [targets: %s]", strings.Join(a.Targets, ", "))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("\nAnalyze the situation and list 3-5 viable options you could take right now.\n")
	sb.WriteString("For each option, explain briefly why it's worth considering.\n\n")
	sb.WriteString("Respond in this exact JSON format:\n")
	sb.WriteString(`{"situation":"one sentence summary","options":[{"action":"action_name","target":"target_id_or_empty","rationale":"why this option"}]}`)
	sb.WriteString("\n")

	return sb.String()
}

// BuildEvaluatePrompt constructs a Stage 2 prompt for one option.
func BuildEvaluatePrompt(p agent.Personality, state *game.State, situation string, option AssessOption) string {
	var sb strings.Builder
	sb.WriteString("/no_think\n")
	fmt.Fprintf(&sb, "You are %s, a %s.\n\n", p.Name, p.Role)
	fmt.Fprintf(&sb, "SITUATION: %s\n\n", situation)
	fmt.Fprintf(&sb, "You are considering this action: %s", option.Action)
	if option.Target != "" {
		fmt.Fprintf(&sb, " targeting %s", option.Target)
	}
	fmt.Fprintf(&sb, "\nRationale: %s\n\n", option.Rationale)

	sb.WriteString("CURRENT STATE:\n")
	if state.MaxFuel > 0 {
		fmt.Fprintf(&sb, "- Fuel: %.0f%%", state.Fuel/state.MaxFuel*100)
	}
	if state.MaxHull > 0 {
		fmt.Fprintf(&sb, " | Hull: %.0f%%", state.Hull/state.MaxHull*100)
	}
	sb.WriteString("\n")
	fmt.Fprintf(&sb, "- Cargo: %d/%d | Credits: %.0f\n", len(state.Cargo), state.MaxCargo, state.Credits)
	if state.InCombat {
		sb.WriteString("- IN COMBAT\n")
	}

	sb.WriteString("\nEvaluate this action on these 5 criteria (score each 0-100):\n")
	sb.WriteString("1. survival: How well does this keep me alive? (hull, fuel, escape from threats)\n")
	sb.WriteString("2. profit: Does this earn credits or acquire valuable resources?\n")
	sb.WriteString("3. goal_progress: Does this advance my current goal?\n")
	sb.WriteString("4. risk: How safe is this choice? (100 = very safe, 0 = very dangerous)\n")
	sb.WriteString("5. efficiency: Am I spending my time wisely with this action?\n")
	sb.WriteString("\nAlso suggest what logical next step would follow this action.\n\n")
	sb.WriteString("Respond in this exact JSON format:\n")
	fmt.Fprintf(&sb, `{"action":"%s","target":"%s","analysis":"2-3 sentence evaluation","scores":{"survival":0,"profit":0,"goal_progress":0,"risk":0,"efficiency":0},"next_step":{"action":"next_action","target":"next_target"}}`, option.Action, option.Target)
	sb.WriteString("\n")

	return sb.String()
}
