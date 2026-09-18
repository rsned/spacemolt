package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// chain_next exists in exactly one place: the complete_mission reply. The
// action log carries no chain key, and mission_templates.chain_next covers
// 0.7% of rows. If the reply is only printed, the link is gone for good --
// frontier_extension's successor is currently unknown for exactly this reason.
func TestCaptureMissionCompletionAppendsChainLink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mission_completions.jsonl")

	if err := captureMissionCompletion(path, "craftsman-1", []byte(crossingBordersReply)); err != nil {
		t.Fatalf("capture: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var rec missionCompletionRecord
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &rec); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if rec.AgentID != "craftsman-1" {
		t.Errorf("agent: got %q", rec.AgentID)
	}
	if rec.Title != "Crossing Borders" {
		t.Errorf("title: got %q", rec.Title)
	}
	if rec.ChainNext != "frontier_extension" {
		t.Errorf("chain_next: got %q", rec.ChainNext)
	}
	// The instance hash is NOT the template slug and the reply carries no
	// template_id, so both the hash and the title must be kept: the title is
	// the only join back to mission_templates.
	if rec.MissionID != "0b13e1168bc24caaae7c3434db4bd262" {
		t.Errorf("mission_id: got %q", rec.MissionID)
	}
	if rec.CreditsEarned != 2557 || rec.CreditsPromised != 5500 || rec.CreditsShortfall != 2943 {
		t.Errorf("credits: got %+v", rec)
	}
	if rec.ObservedUTC == "" {
		t.Error("observed_utc must be stamped")
	}
	if rec.Tick != 1915553 {
		t.Errorf("tick: got %d", rec.Tick)
	}
}

// Completions accumulate; the file is an append-only ledger.
func TestCaptureMissionCompletionAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mission_completions.jsonl")
	for range 3 {
		if err := captureMissionCompletion(path, "craftsman-1", []byte(crossingBordersReply)); err != nil {
			t.Fatalf("capture: %v", err)
		}
	}
	data, _ := os.ReadFile(path)
	if n := len(strings.Split(strings.TrimSpace(string(data)), "\n")); n != 3 {
		t.Errorf("want 3 lines, got %d", n)
	}
}

// A reply with no chain link is still worth keeping for the payout history,
// but a reply we cannot parse must not write a junk row.
func TestCaptureMissionCompletionSkipsUnparseable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mission_completions.jsonl")
	if err := captureMissionCompletion(path, "a", []byte("not json")); err == nil {
		t.Error("want an error for unparseable input")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("nothing should have been written")
	}
}

// The accept reply carries BOTH ids: the procedural instance hash and the
// template_id the completion reply omits. Capturing acceptances is therefore
// what makes a completion joinable to its template by id instead of by title.
// Verbatim from craftsman-1, 2026-09-18.
const crimsonVintageAccept = `{
  "command":"accept_mission",
  "result":{
    "expires_at":"2026-09-23T16:58:34Z",
    "message":"Blood Forge is deep Crimson territory and they're not known for their hospitality.",
    "mission_id":"9198e04fc3c0862ee34d0a05ae393df9",
    "template_id":"crimson_vintage",
    "title":"Crimson Vintage",
    "type":"delivery"
  },
  "tick":1916322
}`

func TestCaptureMissionAcceptanceKeepsBothIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mission_completions.jsonl")
	if err := captureMissionEvent(path, "craftsman-1", missionEventAccepted, []byte(crimsonVintageAccept)); err != nil {
		t.Fatalf("capture: %v", err)
	}
	data, _ := os.ReadFile(path)
	var rec missionCompletionRecord
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &rec); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if rec.Event != missionEventAccepted {
		t.Errorf("event: got %q", rec.Event)
	}
	if rec.MissionID != "9198e04fc3c0862ee34d0a05ae393df9" {
		t.Errorf("instance id: got %q", rec.MissionID)
	}
	if rec.TemplateID != "crimson_vintage" {
		t.Errorf("template id: got %q", rec.TemplateID)
	}
	if rec.ExpiresAt != "2026-09-23T16:58:34Z" {
		t.Errorf("expires_at: got %q", rec.ExpiresAt)
	}
	if rec.Title != "Crimson Vintage" || rec.Type != "delivery" {
		t.Errorf("title/type: %+v", rec)
	}
}

// Completions keep being tagged as completions.
func TestCaptureMissionCompletionTagsEvent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mission_completions.jsonl")
	if err := captureMissionCompletion(path, "craftsman-1", []byte(crossingBordersReply)); err != nil {
		t.Fatalf("capture: %v", err)
	}
	data, _ := os.ReadFile(path)
	var rec missionCompletionRecord
	_ = json.Unmarshal([]byte(strings.TrimSpace(string(data))), &rec)
	if rec.Event != missionEventCompleted {
		t.Errorf("event: got %q", rec.Event)
	}
}
