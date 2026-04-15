package game

import (
	"testing"
)

// TestCheckXPChanges_NilBaselines tests that checkXPChanges handles nil baselines correctly.
// This verifies the fix for spurious XP detection when beforeXP or beforeSkills are nil.
func TestCheckXPChanges_NilBaselines(t *testing.T) {
	tests := []struct {
		name           string
		beforeSkills   map[string]Skill
		beforeXP       map[string]float64
		currentSkills  map[string]Skill
		currentXP      map[string]float64
		shouldFire     bool // whether XPCallback should be invoked
		description    string
	}{
		{
			name:         "both_baselines_nil_returns_early",
			beforeSkills: nil,
			beforeXP:     nil,
			currentSkills: map[string]Skill{
				"smuggling": {Level: 0, XP: 50},
			},
			currentXP: map[string]float64{
				"smuggling": 50,
			},
			shouldFire:  false,
			description: "Should return early when both baselines are nil (first call)",
		},
		{
			name:         "beforeXP_nil_with_non_nil_beforeSkills_returns_early",
			beforeSkills: map[string]Skill{
				"smuggling": {Level: 0, XP: 0},
			},
			beforeXP: nil,
			currentSkills: map[string]Skill{
				"smuggling": {Level: 0, XP: 50},
			},
			currentXP: map[string]float64{
				"smuggling": 50,
			},
			shouldFire:  false,
			description: "Should return early when beforeXP is nil even if beforeSkills is not nil (prevents spurious detection)",
		},
		{
			name:         "beforeSkills_nil_with_non_nil_beforeXP_returns_early",
			beforeSkills: nil,
			beforeXP: map[string]float64{
				"smuggling": 0,
			},
			currentSkills: map[string]Skill{
				"smuggling": {Level: 0, XP: 50},
			},
			currentXP: map[string]float64{
				"smuggling": 50,
			},
			shouldFire:  false,
			description: "Should return early when beforeSkills is nil even if beforeXP is not nil (prevents spurious detection)",
		},
		{
			name: "both_baselines_populated_with_change_fires_callback",
			beforeSkills: map[string]Skill{
				"smuggling": {Level: 0, XP: 0},
			},
			beforeXP: map[string]float64{
				"smuggling": 0,
			},
			currentSkills: map[string]Skill{
				"smuggling": {Level: 0, XP: 50},
			},
			currentXP: map[string]float64{
				"smuggling": 50,
			},
			shouldFire:  true,
			description: "Should fire callback when both baselines are populated and XP changed",
		},
		{
			name: "both_baselines_populated_no_change_does_not_fire",
			beforeSkills: map[string]Skill{
				"smuggling": {Level: 0, XP: 50},
			},
			beforeXP: map[string]float64{
				"smuggling": 50,
			},
			currentSkills: map[string]Skill{
				"smuggling": {Level: 0, XP: 50},
			},
			currentXP: map[string]float64{
				"smuggling": 50,
			},
			shouldFire:  false,
			description: "Should not fire callback when both baselines are populated and nothing changed",
		},
		{
			name: "skill_removed_from_currentXP_detects_change",
			beforeSkills: map[string]Skill{
				"smuggling": {Level: 0, XP: 50},
				"mining":    {Level: 1, XP: 100},
			},
			beforeXP: map[string]float64{
				"smuggling": 50,
				"mining":    100,
			},
			currentSkills: map[string]Skill{
				"smuggling": {Level: 0, XP: 50},
			},
			currentXP: map[string]float64{
				"smuggling": 50,
			},
			shouldFire:  true,
			description: "Should detect change when skill is removed from currentXP (mining removed)",
		},
		{
			name: "skill_removed_from_currentSkills_detects_change",
			beforeSkills: map[string]Skill{
				"smuggling": {Level: 0, XP: 50},
				"mining":    {Level: 1, XP: 100},
			},
			beforeXP: map[string]float64{
				"smuggling": 50,
				"mining":    100,
			},
			currentSkills: map[string]Skill{
				"smuggling": {Level: 0, XP: 50},
			},
			currentXP: map[string]float64{
				"smuggling": 50,
				"mining":    100,
			},
			shouldFire:  true,
			description: "Should detect change when skill is removed from currentSkills (mining removed)",
		},
		{
			name: "level_change_with_no_XP_change_fires_callback",
			beforeSkills: map[string]Skill{
				"smuggling": {Level: 0, XP: 55},
			},
			beforeXP: map[string]float64{
				"smuggling": 55,
			},
			currentSkills: map[string]Skill{
				"smuggling": {Level: 1, XP: 5}, // Level up, XP reset
			},
			currentXP: map[string]float64{
				"smuggling": 5,
			},
			shouldFire:  true,
			description: "Should fire callback when level changes even if XP decreased (level up reset)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a client with the test state
			c := &Client{
				state: &State{
					Player: Player{
						Skills: tt.currentSkills,
					},
					SkillXP: tt.currentXP,
				},
				xpLastSkills: tt.beforeSkills,
				xpLastXP:     tt.beforeXP,
				xpLastAction: "test_action",
				xpLastTarget: "test_target",
			}

			callbackFired := false
			c.XPCallback = func(action, target string, beforeSkills, afterSkills map[string]Skill, beforeXP, afterXP map[string]float64, gameTick int64) {
				callbackFired = true
			}

			// Call checkXPChanges with c.mu held (as required by the function)
			c.mu.Lock()
			c.checkXPChanges()
			c.mu.Unlock()

			if callbackFired != tt.shouldFire {
				t.Errorf("%s: callback fired = %v, want %v\nDescription: %s", tt.name, callbackFired, tt.shouldFire, tt.description)
			}
		})
	}
}

// TestCheckXPChanges_MultipleSkills tests that checkXPChanges correctly handles
// multiple skills with various combinations of changes.
func TestCheckXPChanges_MultipleSkills(t *testing.T) {
	tests := []struct {
		name           string
		beforeSkills   map[string]Skill
		beforeXP       map[string]float64
		currentSkills  map[string]Skill
		currentXP      map[string]float64
		shouldFire     bool
		expectedDeltas map[string]float64 // expected XP deltas for skills that changed
	}{
		{
			name: "multiple_skills_one_changed",
			beforeSkills: map[string]Skill{
				"smuggling": {Level: 0, XP: 0},
				"mining":    {Level: 1, XP: 50},
				"trading":   {Level: 0, XP: 0},
			},
			beforeXP: map[string]float64{
				"smuggling": 0,
				"mining":    50,
				"trading":   0,
			},
			currentSkills: map[string]Skill{
				"smuggling": {Level: 0, XP: 50}, // +50 XP
				"mining":    {Level: 1, XP: 50},  // No change
				"trading":   {Level: 0, XP: 0},   // No change
			},
			currentXP: map[string]float64{
				"smuggling": 50,
				"mining":    50,
				"trading":   0,
			},
			shouldFire: true,
			expectedDeltas: map[string]float64{
				"smuggling": 50,
			},
		},
		{
			name: "multiple_skills_all_changed",
			beforeSkills: map[string]Skill{
				"smuggling": {Level: 0, XP: 0},
				"mining":    {Level: 1, XP: 50},
			},
			beforeXP: map[string]float64{
				"smuggling": 0,
				"mining":    50,
			},
			currentSkills: map[string]Skill{
				"smuggling": {Level: 0, XP: 50}, // +50 XP
				"mining":    {Level: 1, XP: 100}, // +50 XP
			},
			currentXP: map[string]float64{
				"smuggling": 50,
				"mining":    100,
			},
			shouldFire: true,
			expectedDeltas: map[string]float64{
				"smuggling": 50,
				"mining":    50,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Client{
				state: &State{
					Player: Player{
						Skills: tt.currentSkills,
					},
					SkillXP: tt.currentXP,
				},
				xpLastSkills: tt.beforeSkills,
				xpLastXP:     tt.beforeXP,
				xpLastAction: "test_action",
				xpLastTarget: "test_target",
			}

			callbackFired := false
			var capturedBeforeXP, capturedAfterXP map[string]float64

			c.XPCallback = func(action, target string, beforeSkills, afterSkills map[string]Skill, beforeXP, afterXP map[string]float64, gameTick int64) {
				callbackFired = true
				capturedBeforeXP = beforeXP
				capturedAfterXP = afterXP

				// Verify expected deltas
				for skillID, expectedDelta := range tt.expectedDeltas {
					actualDelta := afterXP[skillID] - beforeXP[skillID]
					if actualDelta != expectedDelta {
						t.Errorf("%s: XP delta for %s = %.1f, want %.1f", tt.name, skillID, actualDelta, expectedDelta)
					}
				}
			}

			c.mu.Lock()
			c.checkXPChanges()
			c.mu.Unlock()

			if callbackFired != tt.shouldFire {
				t.Errorf("%s: callback fired = %v, want %v", tt.name, callbackFired, tt.shouldFire)
			}

			if callbackFired && len(tt.expectedDeltas) > 0 {
				// Verify the maps passed to the callback
				for skillID := range tt.expectedDeltas {
					beforeVal, beforeOk := capturedBeforeXP[skillID]
					afterVal, afterOk := capturedAfterXP[skillID]

					if !beforeOk || !afterOk {
						t.Errorf("%s: skill %s missing from callback XP maps", tt.name, skillID)
						continue
					}

					delta := afterVal - beforeVal
					expectedDelta := tt.expectedDeltas[skillID]
					if delta != expectedDelta {
						t.Errorf("%s: callback XP delta for %s = %.1f, want %.1f", tt.name, skillID, delta, expectedDelta)
					}
				}
			}
		})
	}
}

// TestCheckXPChanges_CallbackStateUpdate verifies that checkXPChanges updates
// the baseline state after firing the callback.
func TestCheckXPChanges_CallbackStateUpdate(t *testing.T) {
	beforeSkills := map[string]Skill{
		"smuggling": {Level: 0, XP: 0},
	}
	beforeXP := map[string]float64{
		"smuggling": 0,
	}

	c := &Client{
		state: &State{
			Player: Player{
				Skills: map[string]Skill{
					"smuggling": {Level: 0, XP: 50},
				},
			},
			SkillXP: map[string]float64{
				"smuggling": 50,
			},
		},
		xpLastSkills: beforeSkills,
		xpLastXP:     beforeXP,
		xpLastAction: "test_action",
		xpLastTarget: "test_target",
	}

	callbackFired := false
	c.XPCallback = func(action, target string, beforeSkills, afterSkills map[string]Skill, beforeXP, afterXP map[string]float64, gameTick int64) {
		callbackFired = true
	}

	// First call should fire and update baseline
	c.mu.Lock()
	c.checkXPChanges()
	c.mu.Unlock()

	if !callbackFired {
		t.Error("First call should fire callback")
	}

	// Verify baseline was updated
	c.xpMu.Lock()
	if c.xpLastXP["smuggling"] != 50 {
		t.Errorf("xpLastXP not updated, got %.1f, want 50", c.xpLastXP["smuggling"])
	}
	c.xpMu.Unlock()

	// Second call with no changes should not fire
	callbackFired = false
	c.mu.Lock()
	c.checkXPChanges()
	c.mu.Unlock()

	if callbackFired {
		t.Error("Second call with no changes should not fire callback")
	}
}

// TestCheckXPChanges_Integration is an integration test that simulates
// the actual flow of sending a command and receiving a response with XP changes.
func TestCheckXPChanges_Integration(t *testing.T) {
	// This test simulates the scenario from the bug report:
	// 1. Mission completion grants +50 XP to smuggling
	// 2. Subsequent actions should not detect spurious XP changes

	// Initial state: no XP
	c := &Client{
		state: &State{
			Player: Player{
				Skills: map[string]Skill{},
			},
			SkillXP: map[string]float64{},
		},
		xpLastSkills: map[string]Skill{},
		xpLastXP:     map[string]float64{},
	}

	// Simulate mission completion response
	callbackCount := 0
	c.XPCallback = func(action, target string, beforeSkills, afterSkills map[string]Skill, beforeXP, afterXP map[string]float64, gameTick int64) {
		callbackCount++
		if action != "complete_mission" {
			t.Errorf("Expected action 'complete_mission', got '%s'", action)
		}
	}

	// Send mission complete command (sets baseline)
	c.xpMu.Lock()
	c.xpLastAction = "complete_mission"
	c.xpLastTarget = "mission_id_123"
	c.xpMu.Unlock()

	// Simulate response with +50 XP to smuggling
	c.mu.Lock()
	c.state.Player.Skills = map[string]Skill{
		"smuggling": {Level: 0, XP: 50},
	}
	c.state.SkillXP = map[string]float64{
		"smuggling": 50,
	}
	c.checkXPChanges()
	c.mu.Unlock()

	if callbackCount != 1 {
		t.Errorf("Mission completion: expected 1 callback, got %d", callbackCount)
	}

	// Simulate subsequent actions (survey_system, travel, etc.)
	// These should NOT trigger XP callbacks because no XP changed
	actions := []string{"survey_system", "travel", "find_route", "refuel"}

	for _, action := range actions {
		callbackCount = 0

		// Set new action
		c.xpMu.Lock()
		c.xpLastAction = action
		c.xpLastTarget = ""
		c.xpMu.Unlock()

		// Simulate response with no XP changes ( smuggling still at 50)
		c.mu.Lock()
		c.checkXPChanges()
		c.mu.Unlock()

		if callbackCount > 0 {
			t.Errorf("Action %s: unexpected callback (spurious XP detection), expected 0 callbacks, got %d", action, callbackCount)
		}
	}

	// Verify that smuggling is still at 50 XP
	c.mu.Lock()
	smugglingXP := c.state.SkillXP["smuggling"]
	c.mu.Unlock()

	if smugglingXP != 50 {
		t.Errorf("Smuggling XP = %.1f, want 50", smugglingXP)
	}
}
