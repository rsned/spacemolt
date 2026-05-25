package spar

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

func bs(parts ...game.BattleParticipant) *game.BattleState {
	return &game.BattleState{BattleID: "b1", IsParticipant: true, Participants: parts}
}

func TestBuildView_SplitsAlliesAndEnemies(t *testing.T) {
	state := bs(
		game.BattleParticipant{PlayerID: "me", SideID: "1", Zone: "mid", HullPct: 90},
		game.BattleParticipant{PlayerID: "ally", SideID: "1"},
		game.BattleParticipant{PlayerID: "foe", SideID: "2"},
	)
	v, ok := BuildView(state, "me")
	if !ok {
		t.Fatal("BuildView returned ok=false; want self found")
	}
	if v.Self.PlayerID != "me" || len(v.Allies) != 1 || len(v.Enemies) != 1 {
		t.Fatalf("unexpected view: self=%s allies=%d enemies=%d", v.Self.PlayerID, len(v.Allies), len(v.Enemies))
	}
}

func TestPolicies_Decide(t *testing.T) {
	enemy := game.BattleParticipant{PlayerID: "foe", SideID: "2", HullPct: 100}

	tests := []struct {
		name       string
		policy     Policy
		self       game.BattleParticipant
		wantKind   string
		wantAction string
		wantStance string // "" if not checked
	}{
		{"aggressor advances from outer", NewAggressor(), game.BattleParticipant{PlayerID: "me", SideID: "1", Zone: "outer", HullPct: 100}, "battle", "advance", ""},
		{"aggressor targets when engaged untargeted", NewAggressor(), game.BattleParticipant{PlayerID: "me", SideID: "1", Zone: "engaged", HullPct: 100}, "battle", "target", ""},
		{"aggressor fires when engaged+targeted, not firing", NewAggressor(), game.BattleParticipant{PlayerID: "me", SideID: "1", Zone: "engaged", TargetID: "foe", Stance: "brace", HullPct: 100}, "battle", "stance", "fire"},
		{"aggressor noop when engaged+targeted+firing", NewAggressor(), game.BattleParticipant{PlayerID: "me", SideID: "1", Zone: "engaged", TargetID: "foe", Stance: "fire", HullPct: 100}, "noop", "", ""},
		{"skirmisher advances toward mid from outer", NewSkirmisher(40), game.BattleParticipant{PlayerID: "me", SideID: "1", Zone: "outer", HullPct: 100}, "battle", "advance", ""},
		{"skirmisher retreats toward mid from inner", NewSkirmisher(40), game.BattleParticipant{PlayerID: "me", SideID: "1", Zone: "inner", HullPct: 100}, "battle", "retreat", ""},
		{"skirmisher retreats when hull low", NewSkirmisher(40), game.BattleParticipant{PlayerID: "me", SideID: "1", Zone: "mid", HullPct: 30}, "battle", "retreat", ""},
		{"skirmisher hull-low at outer advances (no retreat past outer)", NewSkirmisher(40), game.BattleParticipant{PlayerID: "me", SideID: "1", Zone: "outer", HullPct: 30}, "battle", "advance", ""},
		{"retreater flees", NewRetreater(), game.BattleParticipant{PlayerID: "me", SideID: "1", Zone: "mid", Stance: "fire", HullPct: 100}, "battle", "stance", "flee"},
		{"dummy braces", NewDummy(), game.BattleParticipant{PlayerID: "me", SideID: "1", Zone: "mid", Stance: "fire", HullPct: 100}, "battle", "stance", "brace"},
		{"dummy noop when already bracing", NewDummy(), game.BattleParticipant{PlayerID: "me", SideID: "1", Zone: "mid", Stance: "brace", HullPct: 100}, "noop", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := View{Self: tt.self, Enemies: []game.BattleParticipant{enemy}}
			got := tt.policy.Decide(v)
			if got.Kind != tt.wantKind {
				t.Fatalf("Kind = %q, want %q", got.Kind, tt.wantKind)
			}
			if tt.wantKind == "battle" && got.BattleAction != tt.wantAction {
				t.Fatalf("BattleAction = %q, want %q", got.BattleAction, tt.wantAction)
			}
			if tt.wantStance != "" {
				if got.Payload["stance"] != tt.wantStance {
					t.Fatalf("stance = %v, want %q", got.Payload["stance"], tt.wantStance)
				}
			}
		})
	}
}

func TestPolicyByName(t *testing.T) {
	for _, n := range []string{"aggressor", "skirmisher", "retreater", "dummy"} {
		if p, err := PolicyByName(n); err != nil || p == nil {
			t.Fatalf("PolicyByName(%q) = %v, %v", n, p, err)
		}
	}
	if _, err := PolicyByName("bogus"); err == nil {
		t.Fatal("PolicyByName(bogus) should error")
	}
}
