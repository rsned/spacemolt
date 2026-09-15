package main

import (
	"reflect"
	"testing"
)

// The fixtures below are the reply shapes documented in
// server_docs/openapi.json (ArenaStatusResponse, ArenaChallengeResponse,
// ArenaAcceptResponse) for v0.586.0. They are the contract these parsers
// exist to satisfy -- if the server shape changes, these fail first.

func TestParseArenaStatusFull(t *testing.T) {
	raw := []byte(`{
	  "action": "status",
	  "at_arena": true,
	  "battle_id": "b17",
	  "arena_wins": 3,
	  "arena_losses": 1,
	  "arena_knockouts": 4,
	  "xp_cap_per_skill": 500,
	  "xp_used_today": {"gunnery": 120, "shields": 40},
	  "incoming": {
	    "challenge_id": "c99",
	    "opponent_id": "p_bot1",
	    "opponent_name": "battle_bot1",
	    "poi_id": "blood_arena",
	    "max_side_size": 1,
	    "expires_tick": 4200
	  }
	}`)
	got, err := parseArenaStatus(raw)
	if err != nil {
		t.Fatalf("parseArenaStatus: %v", err)
	}
	if !got.AtArena {
		t.Errorf("AtArena = false, want true")
	}
	if got.BattleID != "b17" {
		t.Errorf("BattleID = %q", got.BattleID)
	}
	if got.Incoming == nil {
		t.Fatalf("Incoming = nil, want the pending challenge")
	}
	if got.Incoming.ChallengeID != "c99" || got.Incoming.OpponentName != "battle_bot1" {
		t.Errorf("Incoming = %+v", got.Incoming)
	}
	if got.Outgoing != nil {
		t.Errorf("Outgoing = %+v, want nil when the key is absent", got.Outgoing)
	}
	if got.XPCapPerSkill != 500 {
		t.Errorf("XPCapPerSkill = %d", got.XPCapPerSkill)
	}
	if !reflect.DeepEqual(got.XPUsedToday, map[string]int{"gunnery": 120, "shields": 40}) {
		t.Errorf("XPUsedToday = %v", got.XPUsedToday)
	}
}

// A pilot standing at the arena with nothing pending: every optional key is
// omitted. The parser must not invent a challenge out of the zero value --
// the accept path keys off Incoming != nil.
func TestParseArenaStatusIdle(t *testing.T) {
	got, err := parseArenaStatus([]byte(`{"action":"status","at_arena":true,"xp_cap_per_skill":500}`))
	if err != nil {
		t.Fatalf("parseArenaStatus: %v", err)
	}
	if got.Incoming != nil || got.Outgoing != nil {
		t.Errorf("in=%+v out=%+v, want both nil", got.Incoming, got.Outgoing)
	}
	if got.BattleID != "" {
		t.Errorf("BattleID = %q, want empty when not in a match", got.BattleID)
	}
	if !got.AtArena {
		t.Errorf("AtArena = false")
	}
}

// at_arena false is the preflight guard: challenging from the wrong POI is
// the failure this catches before a mutation is spent on it.
func TestParseArenaStatusNotAtArena(t *testing.T) {
	got, err := parseArenaStatus([]byte(`{"action":"status","at_arena":false}`))
	if err != nil {
		t.Fatalf("parseArenaStatus: %v", err)
	}
	if got.AtArena {
		t.Errorf("AtArena = true, want false")
	}
}

func TestParseArenaStatusMalformed(t *testing.T) {
	if _, err := parseArenaStatus([]byte(`not json`)); err == nil {
		t.Errorf("parseArenaStatus(garbage) = nil error, want a parse error")
	}
	if _, err := parseArenaStatus(nil); err == nil {
		t.Errorf("parseArenaStatus(nil) = nil error, want an error (empty raw cache slot)")
	}
}

func TestParseArenaChallenge(t *testing.T) {
	raw := []byte(`{
	  "action": "challenge",
	  "challenge_id": "c99",
	  "target_id": "p_bot2",
	  "target_name": "battle_bot2",
	  "poi_id": "blood_arena",
	  "max_side_size": 1,
	  "expires_tick": 4200,
	  "message": "Challenge issued."
	}`)
	got, err := parseArenaChallenge(raw)
	if err != nil {
		t.Fatalf("parseArenaChallenge: %v", err)
	}
	if got.ChallengeID != "c99" || got.TargetName != "battle_bot2" || got.MaxSideSize != 1 {
		t.Errorf("challenge = %+v", got)
	}
	if got.POIID != "blood_arena" {
		t.Errorf("POIID = %q", got.POIID)
	}
}

// The accept reply is where the battle id comes from in arena mode -- the
// runner no longer has to scrape it off the first battle view.
func TestParseArenaAccept(t *testing.T) {
	raw := []byte(`{
	  "action": "accept",
	  "battle_id": "b17",
	  "your_side": 2,
	  "opponent_side": 1,
	  "participants": [
	    {"player_id": "p_bot1", "username": "battle_bot1", "side_id": 1},
	    {"player_id": "p_bot2", "username": "battle_bot2", "side_id": 2}
	  ],
	  "message": "Match started."
	}`)
	got, err := parseArenaAccept(raw)
	if err != nil {
		t.Fatalf("parseArenaAccept: %v", err)
	}
	if got.BattleID != "b17" {
		t.Errorf("BattleID = %q", got.BattleID)
	}
	if got.YourSide != 2 || got.OpponentSide != 1 {
		t.Errorf("sides = %d/%d", got.YourSide, got.OpponentSide)
	}
	if len(got.Participants) != 2 || got.Participants[0].Username != "battle_bot1" {
		t.Errorf("participants = %+v", got.Participants)
	}
}

// An accept reply with no battle id means the match did not start; the
// caller must not proceed to drive a battle that isn't there.
func TestParseArenaAcceptMissingBattleID(t *testing.T) {
	if _, err := parseArenaAccept([]byte(`{"action":"accept","message":"nope"}`)); err == nil {
		t.Errorf("parseArenaAccept without battle_id = nil error, want an error")
	}
}

func TestArenaStatusCappedSkills(t *testing.T) {
	s := arenaStatus{
		XPCapPerSkill: 500,
		XPUsedToday:   map[string]int{"gunnery": 500, "shields": 499, "tactics": 501},
	}
	got := s.CappedSkills()
	want := []string{"gunnery", "tactics"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("CappedSkills() = %v, want %v", got, want)
	}
	// No cap reported (older server, or no arena XP yet): nothing is capped.
	none := arenaStatus{XPUsedToday: map[string]int{"gunnery": 900}}
	if got := none.CappedSkills(); len(got) != 0 {
		t.Errorf("CappedSkills() with no cap = %v, want empty", got)
	}
}
