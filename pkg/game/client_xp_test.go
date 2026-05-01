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
				xpLastQuantity: 1,
			}

			callbackFired := false
			c.XPCallback = func(action, target string, quantity int, beforeSkills, afterSkills map[string]Skill, beforeXP, afterXP map[string]float64, gameTick int64) {
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
				xpLastQuantity: 1,
			}

			callbackFired := false
			var capturedBeforeXP, capturedAfterXP map[string]float64

			c.XPCallback = func(action, target string, quantity int, beforeSkills, afterSkills map[string]Skill, beforeXP, afterXP map[string]float64, gameTick int64) {
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
		xpLastQuantity: 1,
	}

	callbackFired := false
	c.XPCallback = func(action, target string, quantity int, beforeSkills, afterSkills map[string]Skill, beforeXP, afterXP map[string]float64, gameTick int64) {
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

// TestParseSkillsData_FiresXPCallbackForPassiveDelta verifies that parseSkillsData
// extracts per-player xp and level from the live get_skills response and calls
// checkXPChanges, enabling passive XP detection via the runner's periodic poll.
func TestParseSkillsData_FiresXPCallbackForPassiveDelta(t *testing.T) {
	c := &Client{
		state: &State{
			SkillXP: map[string]float64{"engineering": 300},
			Player:  Player{Skills: map[string]Skill{"engineering": {Level: 17, XP: 300}}},
		},
	}

	var fired struct {
		action      string
		skillID     string
		beforeXP    float64
		afterXP     float64
		levelBefore int
		levelAfter  int
	}
	c.XPCallback = func(action, target string, quantity int, before, after map[string]Skill, beforeXP, afterXP map[string]float64, gameTick int64) {
		fired.action = action
		// capture engineering specifically since the test only seeds engineering
		if s, ok := after["engineering"]; ok {
			fired.skillID = "engineering"
			fired.levelAfter = s.Level
		}
		if s, ok := before["engineering"]; ok {
			fired.levelBefore = s.Level
		}
		fired.beforeXP = beforeXP["engineering"]
		fired.afterXP = afterXP["engineering"]
	}

	// Seed baselines so checkXPChanges does not treat this as the first call.
	c.xpMu.Lock()
	c.xpLastSkills = map[string]Skill{"engineering": {Level: 17, XP: 300}}
	c.xpLastXP = map[string]float64{"engineering": 300}
	c.xpLastAction = "get_skills"
	c.xpMu.Unlock()

	payload := map[string]any{
		"skills": map[string]any{
			"engineering": map[string]any{
				"name":          "Engineering",
				"category":      "ship",
				"level":         float64(18),
				"max_level":     float64(99),
				"next_level_xp": float64(1000),
				"xp":            float64(405),
			},
		},
	}

	c.parseSkillsData(payload)

	if fired.action != "get_skills" {
		t.Errorf("XPCallback action = %q, want %q", fired.action, "get_skills")
	}
	if fired.skillID != "engineering" {
		t.Errorf("XPCallback did not fire for engineering")
	}
	if fired.beforeXP != 300 || fired.afterXP != 405 {
		t.Errorf("XP delta wrong: before=%v after=%v want before=300 after=405", fired.beforeXP, fired.afterXP)
	}
	if fired.levelBefore != 17 || fired.levelAfter != 18 {
		t.Errorf("level delta wrong: before=%v after=%v want 17→18", fired.levelBefore, fired.levelAfter)
	}
}

// TestParseSkillsData_BaselinesNewSkillsSilently verifies that parseSkillsData
// treats skills not yet in the XP baseline as a baseline-establishment event
// (no callback delta) rather than reporting their full cumulative XP as a
// spurious delta. Skills already in the baseline still produce real deltas.
//
// This is the post-reconnect scenario: login may carry partial skill data
// (or none), leaving xpLastXP non-nil but missing many keys. The first
// comprehensive get_skills snapshot would otherwise report each missing
// skill's cumulative XP as a delta of (cumulative - 0).
func TestParseSkillsData_BaselinesNewSkillsSilently(t *testing.T) {
	c := &Client{
		state: &State{
			SkillXP: map[string]float64{"engineering": 300},
			Player:  Player{Skills: map[string]Skill{"engineering": {Level: 17, XP: 300}}},
		},
	}

	var captured struct {
		fired       bool
		beforeXP    map[string]float64
		afterXP     map[string]float64
		beforeSkill map[string]Skill
		afterSkill  map[string]Skill
	}
	c.XPCallback = func(action, target string, quantity int, before, after map[string]Skill, beforeXP, afterXP map[string]float64, gameTick int64) {
		captured.fired = true
		captured.beforeXP = beforeXP
		captured.afterXP = afterXP
		captured.beforeSkill = before
		captured.afterSkill = after
	}

	// Partial baseline: engineering only. Mimics post-reconnect state where
	// login provided only one skill's data.
	c.xpMu.Lock()
	c.xpLastSkills = map[string]Skill{"engineering": {Level: 17, XP: 300}}
	c.xpLastXP = map[string]float64{"engineering": 300}
	c.xpLastAction = "get_skills"
	c.xpMu.Unlock()

	// Server returns full skill set: engineering changed (300→405, 17→18) and
	// piloting is brand-new to our tracking (cumulative 19105 at level 27).
	payload := map[string]any{
		"skills": map[string]any{
			"engineering": map[string]any{
				"level": float64(18),
				"xp":    float64(405),
			},
			"piloting": map[string]any{
				"level": float64(27),
				"xp":    float64(19105),
			},
		},
	}

	c.parseSkillsData(payload)

	if !captured.fired {
		t.Fatalf("XPCallback did not fire")
	}

	// Engineering: real delta 300→405, level 17→18 — must be visible.
	if captured.beforeXP["engineering"] != 300 || captured.afterXP["engineering"] != 405 {
		t.Errorf("engineering XP: before=%v after=%v, want 300/405",
			captured.beforeXP["engineering"], captured.afterXP["engineering"])
	}
	if captured.beforeSkill["engineering"].Level != 17 || captured.afterSkill["engineering"].Level != 18 {
		t.Errorf("engineering level: before=%d after=%d, want 17/18",
			captured.beforeSkill["engineering"].Level, captured.afterSkill["engineering"].Level)
	}

	// Piloting: NOT in prior baseline. After the fix, the baseline must be
	// silently seeded so before == after, producing a zero delta the tracker
	// will skip. Without the fix, before=0, after=19105 (the bug).
	if captured.beforeXP["piloting"] != 19105 {
		t.Errorf("piloting before XP = %v, want %v (silently baselined)",
			captured.beforeXP["piloting"], 19105.0)
	}
	if captured.afterXP["piloting"] != 19105 {
		t.Errorf("piloting after XP = %v, want %v",
			captured.afterXP["piloting"], 19105.0)
	}
	if captured.beforeSkill["piloting"].Level != 27 {
		t.Errorf("piloting before level = %d, want 27 (silently baselined)",
			captured.beforeSkill["piloting"].Level)
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
	c.XPCallback = func(action, target string, quantity int, beforeSkills, afterSkills map[string]Skill, beforeXP, afterXP map[string]float64, gameTick int64) {
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
