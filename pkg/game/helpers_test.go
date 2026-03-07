package game

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindStation(t *testing.T) {
	tests := []struct {
		name     string
		state    *State
		wantNil  bool
		wantID   string
	}{
		{
			name: "finds station",
			state: &State{
				System: SystemData{
					POIs: []POI{
						{ID: "belt1", Type: "asteroid_belt"},
						{ID: "station1", Type: "station"},
						{ID: "station2", Type: "station"},
					},
				},
			},
			wantID: "station1",
		},
		{
			name: "no station",
			state: &State{
				System: SystemData{
					POIs: []POI{
						{ID: "belt1", Type: "asteroid_belt"},
						{ID: "gate1", Type: "jump_gate"},
					},
				},
			},
			wantNil: true,
		},
		{
			name: "empty POIs",
			state: &State{
				System: SystemData{POIs: []POI{}},
			},
			wantNil: true,
		},
		{
			name:    "nil POIs",
			state:   &State{},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FindStation(tt.state)
			if tt.wantNil {
				if got != nil {
					t.Errorf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected non-nil station")
			}
			if got.ID != tt.wantID {
				t.Errorf("got ID %q, want %q", got.ID, tt.wantID)
			}
		})
	}
}

func TestFindStationID(t *testing.T) {
	state := &State{
		System: SystemData{
			POIs: []POI{
				{ID: "belt1", Type: "asteroid_belt"},
				{ID: "station1", Type: "station"},
			},
		},
	}
	if got := FindStationID(state); got != "station1" {
		t.Errorf("got %q, want %q", got, "station1")
	}

	empty := &State{}
	if got := FindStationID(empty); got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestHasStation(t *testing.T) {
	withStation := &State{
		System: SystemData{
			POIs: []POI{{ID: "s1", Type: "station"}},
		},
	}
	if !HasStation(withStation) {
		t.Error("expected HasStation to return true")
	}

	without := &State{}
	if HasStation(without) {
		t.Error("expected HasStation to return false")
	}
}

func TestFindJumpGate(t *testing.T) {
	state := &State{
		System: SystemData{
			POIs: []POI{
				{ID: "belt1", Type: "asteroid_belt"},
				{ID: "gate1", Type: "jump_gate"},
			},
		},
	}
	got := FindJumpGate(state)
	if got == nil {
		t.Fatal("expected non-nil jump gate")
	}
	if got.ID != "gate1" {
		t.Errorf("got ID %q, want %q", got.ID, "gate1")
	}

	empty := &State{}
	if FindJumpGate(empty) != nil {
		t.Error("expected nil for state without jump gate")
	}
}

func TestFindMiningLocation(t *testing.T) {
	tests := []struct {
		name   string
		pois   []POI
		wantID string
	}{
		{
			name:   "finds asteroid_belt",
			pois:   []POI{{ID: "belt1", Type: "asteroid_belt"}},
			wantID: "belt1",
		},
		{
			name:   "finds asteroid_field",
			pois:   []POI{{ID: "field1", Type: "asteroid_field"}},
			wantID: "field1",
		},
		{
			name:   "prefers first match",
			pois:   []POI{{ID: "belt1", Type: "asteroid_belt"}, {ID: "field1", Type: "asteroid_field"}},
			wantID: "belt1",
		},
		{
			name: "no mining location",
			pois: []POI{{ID: "station1", Type: "station"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &State{System: SystemData{POIs: tt.pois}}
			got := FindMiningLocation(state)
			if tt.wantID == "" {
				if got != nil {
					t.Errorf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected non-nil")
			}
			if got.ID != tt.wantID {
				t.Errorf("got ID %q, want %q", got.ID, tt.wantID)
			}
		})
	}
}

func TestFindPOIByType(t *testing.T) {
	state := &State{
		System: SystemData{
			POIs: []POI{
				{ID: "sun1", Type: "sun"},
				{ID: "planet1", Type: "planet"},
				{ID: "station1", Type: "station"},
			},
		},
	}

	got := FindPOIByType(state, "planet")
	if got == nil || got.ID != "planet1" {
		t.Errorf("expected planet1, got %v", got)
	}

	got = FindPOIByType(state, "wormhole")
	if got != nil {
		t.Errorf("expected nil for unknown type, got %+v", got)
	}

	got = FindPOIByType(state, "")
	if got != nil {
		t.Errorf("expected nil for empty type, got %+v", got)
	}
}

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Solar System", "Solar_System"},
		{"Alpha Station", "Alpha_Station"},
		{"hello-world_123", "hello-world_123"},
		{"test@#$%^&*file", "testfile"},
		{"", ""},
		{"no spaces", "no_spaces"},
		{"a.b.c", "abc"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sanitizeFilename(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeFilename(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestGenerateDataFilename(t *testing.T) {
	got := GenerateDataFilename("Solar System", "20250101", "system")
	want := "Solar_System.20250101.system.json"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	got = GenerateDataFilename("Alpha Station", "20250102150405", "market")
	want = "Alpha_Station.20250102150405.market.json"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	got = GenerateDataFilename("", "", "")
	want = "...json"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSaveJSONToFile(t *testing.T) {
	dir := t.TempDir()

	// Normal save
	data := []byte(`{"key":"value"}`)
	err := SaveJSONToFile(data, dir, "test.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify file contents
	got, err := os.ReadFile(filepath.Join(dir, "test.json"))
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("got %q, want %q", string(got), string(data))
	}

	// Creates nested directories
	nestedDir := filepath.Join(dir, "a", "b", "c")
	err = SaveJSONToFile(data, nestedDir, "nested.json")
	if err != nil {
		t.Fatalf("unexpected error creating nested dirs: %v", err)
	}

	// Empty data
	err = SaveJSONToFile([]byte{}, dir, "empty.json")
	if err != nil {
		t.Fatalf("unexpected error with empty data: %v", err)
	}
}

func TestSaveStructToFile(t *testing.T) {
	dir := t.TempDir()

	data := map[string]any{
		"system_id": "sol",
		"count":     42,
	}

	err := SaveStructToFile(data, dir, "struct.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "struct.json"))
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	// Verify it's valid JSON with indentation
	if len(got) == 0 {
		t.Fatal("file is empty")
	}
	if got[0] != '{' {
		t.Errorf("expected JSON object, got %q", string(got[:10]))
	}

	// Test with unmarshalable data
	err = SaveStructToFile(make(chan int), dir, "bad.json")
	if err == nil {
		t.Error("expected error for unmarshalable data")
	}
}

func TestTimestampConstants(t *testing.T) {
	if TimestampDaily != "20060102" {
		t.Errorf("TimestampDaily = %q, want %q", TimestampDaily, "20060102")
	}
	if TimestampPrecise != "20060102150405" {
		t.Errorf("TimestampPrecise = %q, want %q", TimestampPrecise, "20060102150405")
	}
}

func TestParseIPBlockSeconds(t *testing.T) {
	tests := []struct {
		msg  string
		want int
	}{
		{"Your IP has been temporarily blocked due to excessive rate limit violations. Try again in 943 seconds.", 943},
		{"Try again in 60 seconds.", 60},
		{"Try again in 1 seconds.", 1},
		{"no match here", 300},
		{"", 300},
	}
	for _, tt := range tests {
		if got := parseIPBlockSeconds(tt.msg); got != tt.want {
			t.Errorf("parseIPBlockSeconds(%q) = %d, want %d", tt.msg, got, tt.want)
		}
	}
}
