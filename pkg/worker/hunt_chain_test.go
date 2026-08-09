package worker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// chainRecordFor reads the chain record a pass wrote for the fixture agent.
// A missing file is reported as an empty set, which is what the gate sees.
func chainRecordFor(t *testing.T, c *huntFake) map[string]huntChainRecord {
	t.Helper()
	recs, err := loadHuntChain(huntChainPath(c.agentsDir, "pirate-6"))
	if err != nil {
		t.Fatalf("load chain record: %v", err)
	}
	return recs
}

// The continuation is recorded from the completion reply itself. This is the
// primary evidence path: the server names chain_next in the reply to the
// complete_mission that earned it, so one frame proves both halves of the rule
// — that the predecessor was COMPLETED, and which mission it unlocks.
func TestHuntRecordsTheContinuationTheCompletionReplyNames(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{
		board:     []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 1}},
		creatures: huntGrazers("c1"),
	})
	var log strings.Builder
	if err := Hunt(context.Background(), huntDeps(c, &log)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if !called(c, "complete:hex-first_hunt_belt_grazers") {
		t.Fatalf("the pass must have completed the mission first, calls: %v", c.calls)
	}

	recs := chainRecordFor(t, c)
	rec, ok := recs["cracking_the_shell"]
	if !ok {
		t.Fatalf("want cracking_the_shell recorded as earned, got %+v\nlog:\n%s", recs, log.String())
	}
	if rec.Predecessor != "first_hunt_belt_grazers" {
		t.Errorf("predecessor = %q, want the mission that was completed", rec.Predecessor)
	}
	// The completion marker is what separates "completed" from "accepted", so
	// a record without it is not evidence at all.
	if rec.CompletedAt == "" {
		t.Errorf("record carries no completed_at: %+v", rec)
	}
	if rec.ActiveID != "hex-first_hunt_belt_grazers" {
		t.Errorf("active_id = %q, want the instance the reply reported", rec.ActiveID)
	}
	if !strings.Contains(log.String(), "recorded chain continuation cracking_the_shell") {
		t.Errorf("an earned continuation must be visible in the log, got:\n%s", log.String())
	}
}

// The reply is action_result-WRAPPED: chain_next sits under "result". A
// decoder that reads the top-level keys succeeds with every field zero, which
// is why this defect class fails as a missing continuation rather than as an
// error — the same trap craft and shipping both hit.
func TestHuntUnwrapsTheCompletionReplyBeforeReadingTheChain(t *testing.T) {
	var frame map[string]any
	if err := json.Unmarshal([]byte(huntRealCompleteMissionReply), &frame); err != nil {
		t.Fatalf("the captured reply must decode: %v", err)
	}
	if _, wrapped := frame["result"]; !wrapped {
		t.Fatal("the capture is supposed to be action_result-wrapped; fixture no longer proves the case")
	}
	if _, leaked := frame["chain_next"]; leaked {
		t.Fatal("chain_next must sit UNDER result in the capture, or the unwrap is untested")
	}

	c := newHuntFake(t, huntFakeOpts{
		board:     []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 1}},
		creatures: huntGrazers("c1"),
	})
	if err := Hunt(context.Background(), huntDeps(c, io.Discard)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if _, ok := chainRecordFor(t, c)["cracking_the_shell"]; !ok {
		t.Error("the wrapped reply named a continuation and it was not recorded")
	}
}

// Recording it is only half the point: a LATER pass has to admit the
// continuation over the difficulty cap. The two halves happen in different
// passes, which is the whole reason the record is on disk.
func TestHuntChainRecordUnlocksTheNextPass(t *testing.T) {
	first := newHuntFake(t, huntFakeOpts{
		board:     []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 1}},
		creatures: huntGrazers("c1"),
	})
	if err := Hunt(context.Background(), huntDeps(first, io.Discard)); err != nil {
		t.Fatalf("Hunt (first pass): %v", err)
	}

	// A second worker process for the same agent: a fresh client, a fresh
	// board, and nothing carried over but the directory on disk.
	second := newHuntFake(t, huntFakeOpts{
		board:     []boardMission{{id: "cracking_the_shell", difficulty: 2, quantity: 1}},
		creatures: []huntCreature{{id: "t1", species: "slag_tortoise", role: "grazer", hull: 90}},
	})
	second.agentsDir = first.agentsDir
	var log strings.Builder
	if err := Hunt(context.Background(), huntDeps(second, &log)); err != nil {
		t.Fatalf("Hunt (second pass): %v", err)
	}
	if !strings.Contains(log.String(), "CHAIN CONTINUATION: admitting cracking_the_shell") {
		t.Errorf("the earned continuation must be admitted over the cap, got:\n%s", log.String())
	}
	if !strings.Contains(log.String(), "earned by completing first_hunt_belt_grazers") {
		t.Errorf("the admission must name the predecessor that paid for it, got:\n%s", log.String())
	}
	if !called(second, "accept:cracking_the_shell") {
		t.Errorf("want the continuation accepted, calls: %v", second.calls)
	}
}

// The control for the test above: with no record, difficulty 2 is refused.
// Without this, an exemption that fired unconditionally would pass.
func TestHuntRefusesTheContinuationWithNoRecord(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{
		board:     []boardMission{{id: "cracking_the_shell", difficulty: 2, quantity: 1}},
		creatures: []huntCreature{{id: "t1", species: "slag_tortoise", role: "grazer", hull: 90}},
	})
	var log strings.Builder
	if err := Hunt(context.Background(), huntDeps(c, &log)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if !strings.Contains(log.String(), "difficulty 2 over cap 1") {
		t.Errorf("an unearned continuation must be refused by the cap, got:\n%s", log.String())
	}
	if called(c, "accept:") {
		t.Errorf("nothing may be accepted, calls: %v", c.calls)
	}
}

// A terminal chain step names no continuation. Absent chain_next is "nothing
// follows", never an error — one capture cannot say whether the server omits
// the field or sends it empty, so neither may be treated as a fault.
func TestHuntRecordsNothingWhenTheReplyNamesNoContinuation(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{
		board:           []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 1}},
		creatures:       huntGrazers("c1"),
		chainSuppressed: true,
	})
	var log strings.Builder
	if err := Hunt(context.Background(), huntDeps(c, &log)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if recs := chainRecordFor(t, c); len(recs) != 0 {
		t.Errorf("nothing was unlocked, but %+v was recorded", recs)
	}
	if !strings.Contains(log.String(), "names no continuation") {
		t.Errorf("the absence should be stated, not silent, got:\n%s", log.String())
	}
}

// The raw store keeps the LAST frame under complete_mission, so a completion
// whose reply never landed leaves an earlier pass's frame sitting there.
// Crediting it would record a continuation for a mission this pass did not
// finish.
func TestHuntIgnoresACompletionReplyForAnotherMission(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{
		board:         []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 1}},
		creatures:     huntGrazers("c1"),
		completeStale: true,
	})
	var log strings.Builder
	if err := Hunt(context.Background(), huntDeps(c, &log)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if recs := chainRecordFor(t, c); len(recs) != 0 {
		t.Errorf("a reply for another mission is not evidence, but %+v was recorded", recs)
	}
	if !strings.Contains(log.String(), "not counting it as chain evidence") {
		t.Errorf("the mismatch must be logged, got:\n%s", log.String())
	}
}

// No reply at all is the same answer: nothing recorded, and said out loud,
// because a lost frame is lost evidence rather than a mission with no sequel.
func TestHuntSaysSoWhenTheCompletionReplyNeverLands(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{
		board:           []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 1}},
		creatures:       huntGrazers("c1"),
		noCompleteReply: true,
	})
	var log strings.Builder
	if err := Hunt(context.Background(), huntDeps(c, &log)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if recs := chainRecordFor(t, c); len(recs) != 0 {
		t.Errorf("no reply means no evidence, but %+v was recorded", recs)
	}
	if !strings.Contains(log.String(), "goes unrecorded") {
		t.Errorf("a lost completion frame must be visible, got:\n%s", log.String())
	}
}

// A record with no completed_at is not evidence. The rule is the same one the
// listing path applies to completion_time: the waiver rests on a COMPLETED
// predecessor, so the completion marker is what is actually being checked, and
// a truncated or hand-edited file must not be able to forge one.
func TestHuntChainRecordNeedsACompletionMarker(t *testing.T) {
	dir := t.TempDir()
	path := huntChainPath(dir, "pirate-6")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"cracking_the_shell":{"predecessor":"first_hunt_belt_grazers"}}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	var log strings.Builder
	earned := huntRecordedContinuations(HuntDeps{AgentID: "pirate-6", AgentsDir: dir}, &log)
	if len(earned) != 0 {
		t.Errorf("a record with no completed_at must not be credited, got %v", earned)
	}
	if !strings.Contains(log.String(), "no completed_at") {
		t.Errorf("the refusal must say why, got:\n%s", log.String())
	}
}

// Every unreadable-record path falls CLOSED. Empty evidence is never
// permission: the waiver is the exception, so anything short of a readable,
// complete record means the plain difficulty cap applies.
func TestHuntRecordedContinuationsFailClosed(t *testing.T) {
	t.Run("no file", func(t *testing.T) {
		earned := huntRecordedContinuations(HuntDeps{AgentID: "pirate-6", AgentsDir: t.TempDir()}, io.Discard)
		if len(earned) != 0 {
			t.Errorf("a fresh agent has earned nothing, got %v", earned)
		}
	})
	t.Run("corrupt file", func(t *testing.T) {
		dir := t.TempDir()
		path := huntChainPath(dir, "pirate-6")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		var log strings.Builder
		earned := huntRecordedContinuations(HuntDeps{AgentID: "pirate-6", AgentsDir: dir}, &log)
		if len(earned) != 0 {
			t.Errorf("an unreadable record earns nothing, got %v", earned)
		}
		if !strings.Contains(log.String(), "stay gated") {
			t.Errorf("a corrupt record must be reported, got:\n%s", log.String())
		}
	})
	t.Run("no agent id", func(t *testing.T) {
		// The file is placed exactly where an empty agent id would resolve to,
		// so the test proves the guard rather than the absence of a file. A
		// shared <agentsDir>/hunt-chain.json is not this agent's history and
		// must not be read as one.
		dir := t.TempDir()
		if err := saveHuntChain(filepath.Join(dir, huntChainFile), map[string]huntChainRecord{
			"cracking_the_shell": {Predecessor: "first_hunt_belt_grazers", CompletedAt: "2026-08-08T19:52:23Z"},
		}); err != nil {
			t.Fatalf("save: %v", err)
		}
		earned := huntRecordedContinuations(HuntDeps{AgentsDir: dir}, io.Discard)
		if len(earned) != 0 {
			t.Errorf("with no agent there is no per-agent history, got %v", earned)
		}
	})
}

// The write side of the same rule: a pass with no agent id records nothing
// rather than writing a file every agent would share.
func TestHuntRecordChainNextNeedsAnAgentID(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{
		board:     []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 1}},
		creatures: huntGrazers("c1"),
	})
	if err := c.CompleteMission(context.Background(), "hex-first_hunt_belt_grazers"); err != nil {
		t.Fatalf("CompleteMission: %v", err)
	}
	dir := t.TempDir()
	var log strings.Builder
	huntRecordChainNext(HuntDeps{Client: c, AgentsDir: dir}, &log,
		huntJob{boardID: "first_hunt_belt_grazers", activeID: "hex-first_hunt_belt_grazers"})

	if _, err := os.Stat(filepath.Join(dir, huntChainFile)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a shared chain file was written: stat err = %v", err)
	}
	if !strings.Contains(log.String(), "no agent id") {
		t.Errorf("the dropped continuation must say why, got:\n%s", log.String())
	}
}

// The record survives a second completion: earning one continuation must not
// erase another. A map keyed by the unlocked mission is the shape that makes
// this true, and a reader has to be able to rely on it once the chain is more
// than two steps long.
func TestHuntChainRecordAccumulates(t *testing.T) {
	dir := t.TempDir()
	path := huntChainPath(dir, "pirate-6")
	// An unrelated continuation earned earlier — a DIFFERENT key, so a writer
	// that rebuilt the file from scratch each time would be caught.
	if err := saveHuntChain(path, map[string]huntChainRecord{
		"ghosts_in_the_cloud": {Predecessor: "cracking_the_shell", CompletedAt: "2026-08-08T19:52:23Z"},
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	c := newHuntFake(t, huntFakeOpts{
		board:     []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 1}},
		creatures: huntGrazers("c1"),
	})
	c.agentsDir = dir
	if err := Hunt(context.Background(), huntDeps(c, io.Discard)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}

	recs, err := loadHuntChain(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, ok := recs["ghosts_in_the_cloud"]; !ok {
		t.Errorf("the earlier record was lost: %+v", recs)
	}
	if _, ok := recs["cracking_the_shell"]; !ok {
		t.Errorf("the new record was not added: %+v", recs)
	}
}
