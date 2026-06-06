package game

import (
	"strings"
	"testing"
)

func TestFormatScanAlert(t *testing.T) {
	tests := []struct {
		name    string
		payload map[string]any
		lawless bool
		want    []string // substrings that must be present
		wantNot []string // substrings that must be absent
	}{
		{
			name: "lawless no info",
			payload: map[string]any{
				"scanner_username":   "Specter",
				"scanner_ship_class": "lawn_dart",
				"revealed_info":      nil,
			},
			lawless: true,
			want:    []string{"LAWLESS SPACE", "Specter (lawn_dart)", "No information revealed"},
			wantNot: []string{"<nil>", "Revealed:"},
		},
		{
			name: "policed no info",
			payload: map[string]any{
				"scanner_username":   "Cipher",
				"scanner_ship_class": "lemming",
				"revealed_info":      nil,
			},
			lawless: false,
			want:    []string{"[SCANNED]", "Cipher (lemming)", "No information revealed"},
			wantNot: []string{"LAWLESS", "<nil>"},
		},
		{
			name: "revealed info present",
			payload: map[string]any{
				"scanner_username":   "Nemesis",
				"scanner_ship_class": "reaper",
				"revealed_info":      []any{"cargo", "fit"},
			},
			lawless: true,
			want:    []string{"Nemesis (reaper)", "Revealed: cargo, fit"},
			wantNot: []string{"No information revealed"},
		},
		{
			name:    "missing scanner falls back",
			payload: map[string]any{"revealed_info": nil},
			lawless: false,
			want:    []string{"an unknown ship", "No information revealed"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatScanAlert(tt.payload, tt.lawless)
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("formatScanAlert() = %q, missing %q", got, w)
				}
			}
			for _, w := range tt.wantNot {
				if strings.Contains(got, w) {
					t.Errorf("formatScanAlert() = %q, should not contain %q", got, w)
				}
			}
		})
	}
}

func TestSystemIsLawless(t *testing.T) {
	c := &Client{state: &State{}}
	c.state.System.PoliceLevel = 0
	if !c.systemIsLawless() {
		t.Error("PoliceLevel 0 should be lawless")
	}

	c.state.System.PoliceLevel = 3
	c.state.System.SecurityStatus = "High Security"
	if c.systemIsLawless() {
		t.Error("PoliceLevel 3 High Security should not be lawless")
	}

	c.state.System.PoliceLevel = 2
	c.state.System.SecurityStatus = "Lawless"
	if !c.systemIsLawless() {
		t.Error("SecurityStatus Lawless should be lawless even with nonzero police level")
	}
}
