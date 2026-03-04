package knowledge

import (
	"context"
	"testing"
)

func TestSkill_GetXPForLevel(t *testing.T) {
	skill := Skill{
		ID:         "mining",
		Name:       "Mining",
		MaxLevel:   5,
		XPPerLevel: []int{100, 300, 600, 1000, 1500},
	}

	tests := []struct {
		level    int
		expected int
	}{
		{1, 100},
		{2, 300},
		{5, 1500},
		{0, 0},  // Below range
		{-1, 0}, // Negative
		{6, 0},  // Above max level
		{10, 0}, // Way above max level
	}

	for _, tt := range tests {
		got := skill.GetXPForLevel(tt.level)
		if got != tt.expected {
			t.Errorf("GetXPForLevel(%d) = %d, want %d", tt.level, got, tt.expected)
		}
	}
}

func TestSkill_GetXPForLevel_EmptyXPTable(t *testing.T) {
	skill := Skill{
		ID:         "test",
		MaxLevel:   5,
		XPPerLevel: nil,
	}

	if got := skill.GetXPForLevel(1); got != 0 {
		t.Errorf("GetXPForLevel(1) with nil table = %d, want 0", got)
	}
}

func TestSkill_GetNextLevelXP(t *testing.T) {
	skill := Skill{
		ID:         "mining",
		Name:       "Mining",
		MaxLevel:   5,
		XPPerLevel: []int{100, 300, 600, 1000, 1500},
	}

	tests := []struct {
		currentLevel int
		expected     int
	}{
		{0, 100},  // Level 0 -> next is level 1
		{1, 300},  // Level 1 -> next is level 2
		{4, 1500}, // Level 4 -> next is level 5
		{5, 0},    // At max level
		{10, 0},   // Beyond max level
	}

	for _, tt := range tests {
		got := skill.GetNextLevelXP(tt.currentLevel)
		if got != tt.expected {
			t.Errorf("GetNextLevelXP(%d) = %d, want %d", tt.currentLevel, got, tt.expected)
		}
	}
}

func TestGetStaticSkills(t *testing.T) {
	skills := getStaticSkills()

	if len(skills) == 0 {
		t.Fatal("Expected non-empty static skills list")
	}

	// Check for known skills
	knownSkills := map[string]bool{
		"mining":    false,
		"advanced_mining": false,
		"exploration":     false,
		"trading":         false,
	}

	for _, s := range skills {
		if _, ok := knownSkills[s.ID]; ok {
			knownSkills[s.ID] = true
		}
		if s.MaxLevel <= 0 {
			t.Errorf("Skill %s has invalid MaxLevel: %d", s.ID, s.MaxLevel)
		}
		if len(s.XPPerLevel) != s.MaxLevel {
			t.Errorf("Skill %s: XPPerLevel length %d != MaxLevel %d", s.ID, len(s.XPPerLevel), s.MaxLevel)
		}
	}

	for id, found := range knownSkills {
		if !found {
			t.Errorf("Expected skill %s not found in static skills", id)
		}
	}
}

func TestGetStaticSkill(t *testing.T) {
	// Existing skill
	s := getStaticSkill("mining")
	if s == nil {
		t.Fatal("Expected non-nil skill for mining_basic")
	}
	if s.ID != "mining" {
		t.Errorf("Expected ID mining_basic, got %s", s.ID)
	}

	// Non-existent skill
	s = getStaticSkill("nonexistent_skill")
	if s != nil {
		t.Error("Expected nil for nonexistent skill")
	}

	// Empty string
	s = getStaticSkill("")
	if s != nil {
		t.Error("Expected nil for empty skill ID")
	}
}

func TestSQLiteKB_StoreSkills(t *testing.T) {
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	skills := []Skill{
		{
			ID:             "test_skill",
			Name:           "Test Skill",
			Category:       "Testing",
			Description:    "A test skill",
			MaxLevel:       5,
			XPPerLevel:     []int{100, 300, 600, 1000, 1500},
			BonusPerLevel:  map[string]int{"speed": 5},
			RequiredSkills: map[string]int{"mining": 2},
		},
	}

	if err := kb.StoreSkills(ctx, skills); err != nil {
		t.Fatalf("StoreSkills failed: %v", err)
	}
}

func TestSQLiteKB_GetSkill(t *testing.T) {
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	skills := []Skill{
		{
			ID:             "test_skill",
			Name:           "Test Skill",
			Category:       "Testing",
			Description:    "A test skill",
			MaxLevel:       5,
			XPPerLevel:     []int{100, 300, 600, 1000, 1500},
			BonusPerLevel:  map[string]int{"speed": 5},
			RequiredSkills: map[string]int{"mining": 2},
		},
	}
	if err := kb.StoreSkills(ctx, skills); err != nil {
		t.Fatalf("StoreSkills failed: %v", err)
	}

	s, err := kb.GetSkill("test_skill")
	if err != nil {
		t.Fatalf("GetSkill failed: %v", err)
	}
	if s == nil {
		t.Fatal("Expected non-nil skill")
	}
	if s.Name != "Test Skill" {
		t.Errorf("Expected name 'Test Skill', got '%s'", s.Name)
	}
	if s.MaxLevel != 5 {
		t.Errorf("Expected MaxLevel 5, got %d", s.MaxLevel)
	}
	if len(s.XPPerLevel) != 5 {
		t.Errorf("Expected 5 XP levels, got %d", len(s.XPPerLevel))
	}
	if s.BonusPerLevel["speed"] != 5 {
		t.Errorf("Expected bonus speed=5, got %d", s.BonusPerLevel["speed"])
	}
	if s.RequiredSkills["mining"] != 2 {
		t.Errorf("Expected required mining_basic=2, got %d", s.RequiredSkills["mining"])
	}
}

func TestSQLiteKB_GetSkill_FallbackToStatic(t *testing.T) {
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	// Don't store any skills - should fall back to static data
	s, err := kb.GetSkill("mining")
	if err != nil {
		t.Fatalf("GetSkill fallback failed: %v", err)
	}
	if s == nil {
		t.Fatal("Expected non-nil skill from static fallback")
	}
	if s.ID != "mining" {
		t.Errorf("Expected ID mining_basic, got %s", s.ID)
	}
}

func TestSQLiteKB_GetSkill_NotFound(t *testing.T) {
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	// Not in DB and not in static data
	s, err := kb.GetSkill("completely_unknown_skill")
	if err != nil {
		t.Fatalf("GetSkill for unknown failed: %v", err)
	}
	if s != nil {
		t.Error("Expected nil for completely unknown skill")
	}
}

func TestSQLiteKB_GetSkills(t *testing.T) {
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	skills := []Skill{
		{ID: "skill_a", Name: "Skill A", Category: "Cat1", MaxLevel: 5, XPPerLevel: []int{100, 200, 300, 400, 500}},
		{ID: "skill_b", Name: "Skill B", Category: "Cat2", MaxLevel: 3, XPPerLevel: []int{100, 200, 300}},
	}
	if err := kb.StoreSkills(ctx, skills); err != nil {
		t.Fatalf("StoreSkills failed: %v", err)
	}

	retrieved := kb.GetSkills()
	if len(retrieved) != 2 {
		t.Errorf("Expected 2 skills, got %d", len(retrieved))
	}
}

func TestSQLiteKB_GetSkills_FallbackToStatic(t *testing.T) {
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	// No skills stored - should fall back to static
	skills := kb.GetSkills()
	if len(skills) == 0 {
		t.Error("Expected non-empty skills from static fallback")
	}
}

func TestMemoryKB_GetSkill(t *testing.T) {
	kb := NewMemoryKB()

	// No stored skills - should fall back to static
	s, err := kb.GetSkill("mining")
	if err != nil {
		t.Fatalf("GetSkill failed: %v", err)
	}
	if s == nil {
		t.Fatal("Expected non-nil skill from static fallback")
	}

	// Store custom skills
	ctx := context.Background()
	skills := []Skill{
		{ID: "custom_skill", Name: "Custom Skill", MaxLevel: 3},
	}
	if err := kb.StoreSkills(ctx, skills); err != nil {
		t.Fatalf("StoreSkills failed: %v", err)
	}

	s, err = kb.GetSkill("custom_skill")
	if err != nil {
		t.Fatalf("GetSkill custom failed: %v", err)
	}
	if s == nil {
		t.Fatal("Expected non-nil custom skill")
	}
	if s.Name != "Custom Skill" {
		t.Errorf("Expected name 'Custom Skill', got '%s'", s.Name)
	}

	// Original static skill should no longer be available (replaced)
	s, err = kb.GetSkill("mining")
	if err != nil {
		t.Fatalf("GetSkill mining_basic after store failed: %v", err)
	}
	// After StoreSkills, only stored skills are in the map, so mining_basic
	// is not there - but fallback to static still works
	// The memory KB falls back to static if the ID is not in the map
	if s == nil {
		t.Fatal("Expected non-nil skill (static fallback)")
	}
}

func TestMemoryKB_GetSkills(t *testing.T) {
	kb := NewMemoryKB()

	// No stored skills - should return static
	skills := kb.GetSkills()
	if len(skills) == 0 {
		t.Error("Expected non-empty skills from static fallback")
	}

	// Store custom skills
	ctx := context.Background()
	custom := []Skill{
		{ID: "custom_1", Name: "Custom 1", MaxLevel: 3},
		{ID: "custom_2", Name: "Custom 2", MaxLevel: 5},
	}
	if err := kb.StoreSkills(ctx, custom); err != nil {
		t.Fatalf("StoreSkills failed: %v", err)
	}

	skills = kb.GetSkills()
	if len(skills) != 2 {
		t.Errorf("Expected 2 custom skills, got %d", len(skills))
	}
}
