package serverapi

import (
	"encoding/json"
	"testing"
)

// Both frames are verbatim from the wire — craftsman-1 attacking a Drift-Ray at
// market_prime on 2026-08-08 — not composed by hand. The struct previously
// covered only the ack, so the terminal frame's target fields decoded to
// nothing and the client logged "[SERVER API CHANGE] New fields in \"attack\"
// response not in AttackResponse: [target target_name target_type]".
const (
	liveAttackAck      = `{"command":"attack","message":"Attack action pending. Will execute on next tick.","pending":true}`
	liveAttackTerminal = `{"action":"attack","kind":"npc","target":"crt_d439a40cf658db0487e1be6bbe26a215","target_name":"Drift-Ray","target_type":"creature"}`
)

func TestAttackResponseDecodesAck(t *testing.T) {
	var got AttackResponse
	if err := json.Unmarshal([]byte(liveAttackAck), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.Pending {
		t.Error("Pending must decode true — it is what distinguishes the ack from the result")
	}
	if got.Command != "attack" {
		t.Errorf("Command = %q, want attack", got.Command)
	}
	if got.Message == "" {
		t.Error("Message must survive decoding")
	}
	// The ack names no target; a caller keying off Target must not see a stale
	// or invented one here.
	if got.Target != "" || got.Action != "" {
		t.Errorf("ack carried Action=%q Target=%q, want both empty", got.Action, got.Target)
	}
}

func TestAttackResponseDecodesTerminalTarget(t *testing.T) {
	var got AttackResponse
	if err := json.Unmarshal([]byte(liveAttackTerminal), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Action != "attack" {
		t.Errorf("Action = %q, want attack", got.Action)
	}
	if got.Target != "crt_d439a40cf658db0487e1be6bbe26a215" {
		t.Errorf("Target = %q — the struck creature's id must decode", got.Target)
	}
	if got.TargetName != "Drift-Ray" {
		t.Errorf("TargetName = %q, want Drift-Ray", got.TargetName)
	}
	if got.TargetType != "creature" {
		t.Errorf("TargetType = %q, want creature — this is how wildlife is told from players", got.TargetType)
	}
	if got.Kind != "npc" {
		t.Errorf("Kind = %q, want npc", got.Kind)
	}
	// Pending is absent on the terminal frame, which is how a reader tells the
	// two apart.
	if got.Pending {
		t.Error("terminal frame must decode Pending false")
	}
}

// advance is the move that makes short-range weapons usable, so its frame has
// to be registered and decodable. Verbatim from the wire, 2026-08-08:
// the client logged 'Unhandled action "advance" in OK response' before this.
func TestAdvanceResponseDecodes(t *testing.T) {
	var got AdvanceResponse
	body := `{"action":"advance","message":"Advancing toward the enemy."}`
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Action != "advance" {
		t.Errorf("Action = %q, want advance", got.Action)
	}
	if got.Message != "Advancing toward the enemy." {
		t.Errorf("Message = %q, want the server's advance confirmation", got.Message)
	}
}
