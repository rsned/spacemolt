package dataservice

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// The server delivers a private message's target_id as a CONVERSATION key —
// "<recipient>:<sender>" — not the bare recipient id this filter was written
// against. Observed live 2026-08-10:
//
//	skip 4600c288…: target_id="0e72a09d…:a5092491…" != agent_id="0e72a09d…"
//
// Exact equality therefore rejected every inbound DM, and because the skip
// path marks the message read, the evidence was consumed as it went. databot
// answered nothing from 2026-06-12 onward. Every pre-existing fixture in this
// package still uses the old bare shape, which is why the suite stayed green
// straight through the outage.
func TestService_DispatchesWhenTargetIsAConversationKey(t *testing.T) {
	svc, client, _, cleanup := newTestService(t)
	defer cleanup()

	client.setHistory([]serverapi.ChatMessage{
		{
			ID: "m1", Channel: "private", SenderID: "miner-1", Sender: "Preston",
			Content: "echo hello", TargetID: "databot-test:miner-1",
			TimestampUTC: time.Now().UTC().Format(time.RFC3339Nano),
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	go func() { _ = svc.Run(ctx) }()

	if !waitUntil(t, 400*time.Millisecond, func() bool { return client.countSent() == 1 }) {
		t.Fatalf("no reply to a conversation-key DM; sent=%d", client.countSent())
	}
	sent := client.sentSnapshot()[0]
	if sent.TargetID != "miner-1" {
		t.Errorf("reply target: got %q, want the SENDER id", sent.TargetID)
	}
	if !strings.Contains(sent.Content, "echo: hello") {
		t.Errorf("content: got %q", sent.Content)
	}
}

// The pairing order is not guaranteed, so both sides are matched. This must
// not turn into "reply to everything": the two cases below are the guardrails.
func TestService_DispatchesWhenConversationKeyIsReversed(t *testing.T) {
	svc, client, _, cleanup := newTestService(t)
	defer cleanup()

	client.setHistory([]serverapi.ChatMessage{
		{
			ID: "m1", Channel: "private", SenderID: "miner-1", Sender: "Preston",
			Content: "echo hello", TargetID: "miner-1:databot-test",
			TimestampUTC: time.Now().UTC().Format(time.RFC3339Nano),
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	go func() { _ = svc.Run(ctx) }()

	if !waitUntil(t, 400*time.Millisecond, func() bool { return client.countSent() == 1 }) {
		t.Fatalf("no reply to a reversed conversation-key DM; sent=%d", client.countSent())
	}
}

// A conversation between two OTHER parties must still be ignored — the
// widened match must not degrade into a substring or always-true test.
func TestService_IgnoresConversationKeyBetweenOthers(t *testing.T) {
	svc, client, _, cleanup := newTestService(t)
	defer cleanup()

	client.setHistory([]serverapi.ChatMessage{
		{
			ID: "m1", Channel: "private", SenderID: "miner-1", Sender: "M",
			Content: "echo hi", TargetID: "some-other-bot:miner-1",
			TimestampUTC: time.Now().UTC().Format(time.RFC3339Nano),
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_ = svc.Run(ctx)

	if n := client.countSent(); n != 0 {
		t.Errorf("replied to a conversation between others; sent=%d", n)
	}
}

// Our own id appears in the key of a message WE sent, so matching either side
// makes the from-self check load-bearing rather than redundant.
func TestService_IgnoresOwnMessageInConversationKey(t *testing.T) {
	svc, client, _, cleanup := newTestService(t)
	defer cleanup()

	client.setHistory([]serverapi.ChatMessage{
		{
			ID: "m1", Channel: "private", SenderID: "databot-test", Sender: "D",
			Content: "echo self", TargetID: "miner-1:databot-test",
			TimestampUTC: time.Now().UTC().Format(time.RFC3339Nano),
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_ = svc.Run(ctx)

	if n := client.countSent(); n != 0 {
		t.Errorf("self-replied via a conversation key; sent=%d", n)
	}
}

// A near-miss must not match: ids are compared whole, not by prefix.
func TestAddressedToComparesWholeSegments(t *testing.T) {
	for _, tc := range []struct {
		name     string
		targetID string
		agentID  string
		want     bool
	}{
		{"bare id, the historical shape", "bot", "bot", true},
		{"conversation key, recipient first", "bot:sender", "bot", true},
		{"conversation key, sender first", "sender:bot", "bot", true},
		{"between others", "other:sender", "bot", false},
		{"prefix of a longer id", "bottom:sender", "bot", false},
		{"substring inside a segment", "xxbotxx:sender", "bot", false},
		{"empty target", "", "bot", false},
		{"empty agent id matches nothing", "bot:sender", "", false},
		// Both empty is the case the guard exists for: splitting "" yields one
		// empty segment, which would otherwise equal an empty agent id and make
		// a misconfigured bot answer every message in the channel.
		{"both empty", "", "", false},
		{"empty segment in the key", "bot:", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := addressedTo(tc.targetID, tc.agentID); got != tc.want {
				t.Errorf("addressedTo(%q, %q) = %v, want %v", tc.targetID, tc.agentID, got, tc.want)
			}
		})
	}
}
