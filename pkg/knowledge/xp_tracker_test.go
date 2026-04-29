package knowledge

import (
	"context"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

// captureKB records every XPObservation passed to RecordXPObservation
// so tests can inspect the source-mapping behaviour of XPTracker.
type captureKB struct {
	MemoryKB
	captured []XPObservation
}

func (c *captureKB) RecordXPObservation(_ context.Context, obs XPObservation) error {
	c.captured = append(c.captured, obs)
	return nil
}

// fakeXPClient is a no-op stub for game.XPCallbackSetter; XPTracker only
// uses it to register a callback, which the test invokes directly.
type fakeXPClient struct{ cb game.XPCallbackFunc }

func (f *fakeXPClient) SetXPCallback(fn game.XPCallbackFunc) { f.cb = fn }

func TestXPTracker_SourceMapping(t *testing.T) {
	cases := []struct {
		name       string
		action     string
		wantSource string
	}{
		{"mining action", "mine", "action"},
		{"travel action", "travel", "action"},
		{"unknown action defaults to action", "craft", "action"},
		{"complete_mission", "complete_mission", "mission_reward"},
		{"get_skills is passive", "get_skills", "passive_skill"},
		{"login is passive", "login", "passive_skill"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kb := &captureKB{MemoryKB: *NewMemoryKB()}
			client := &fakeXPClient{}
			tracker := NewXPTracker(client, kb, "agent-1", nil)
			if tracker == nil {
				t.Fatalf("NewXPTracker returned nil")
			}
			if client.cb == nil {
				t.Fatalf("XPCallback was not installed")
			}

			before := map[string]game.Skill{"engineering": {Level: 5, XP: 100}}
			after := map[string]game.Skill{"engineering": {Level: 5, XP: 200}}
			beforeXP := map[string]float64{"engineering": 100}
			afterXP := map[string]float64{"engineering": 200}

			client.cb(tc.action, "", 1, before, after, beforeXP, afterXP, 12345)

			if len(kb.captured) != 1 {
				t.Fatalf("expected 1 observation, got %d", len(kb.captured))
			}
			got := kb.captured[0].Source
			if got != tc.wantSource {
				t.Errorf("source = %q, want %q", got, tc.wantSource)
			}
		})
	}
}
