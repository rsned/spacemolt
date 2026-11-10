package worker

import (
	"io"
	"path/filepath"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

type completionKBFake struct {
	game.GameClient
	raw map[string][]byte
}

func (f *completionKBFake) GetRawJSON(key string) []byte { return f.raw[key] }

// A worker-run completion stores its per-empire reputation and its unlock
// edges, keyed to the template the worker accepted.
func TestMissionRecordCompletionKB(t *testing.T) {
	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: filepath.Join(t.TempDir(), "kb.db")})
	if err != nil {
		t.Fatal(err)
	}
	fake := &completionKBFake{raw: map[string][]byte{"complete_mission": []byte(`{"command":"complete_mission","tick":5,"result":{
"mission_id":"inst9","title":"Survey Equipment","credits_earned":5000,"skill_xp_gained":{"piloting":1040},
"reputation_changes":{"outerrim":2},"unlocks":[{"available":true,"mission":{"mission_id":"environmental_sampling","title":"Environmental Sampling"}}]}}`)}}
	deps := MissionDeps{Client: fake, KB: kb, AgentID: "craftsman-1"}
	c := missionCandidate{ActiveID: "inst9", Entry: serverapi.MissionBoardEntry{TemplateID: "survey_equipment"}}
	missionRecordCompletionKB(t.Context(), deps, io.Discard, c)

	var tmpl, rep, xp string
	if err = kb.DB().QueryRowContext(t.Context(), `SELECT template_id, reputation_changes, skill_xp_gained FROM mission_completions WHERE mission_id='inst9'`).Scan(&tmpl, &rep, &xp); err != nil {
		t.Fatal(err)
	}
	if tmpl != "survey_equipment" || rep != `{"outerrim":2}` || xp != `{"piloting":1040}` {
		t.Fatalf("tmpl=%q rep=%q xp=%q", tmpl, rep, xp)
	}
	var req string
	if err = kb.DB().QueryRowContext(t.Context(), `SELECT requires_mission_id FROM mission_prerequisites WHERE mission_id='environmental_sampling'`).Scan(&req); err != nil {
		t.Fatal(err)
	}
	if req != "survey_equipment" {
		t.Fatalf("req=%q", req)
	}

	// A stale frame (different instance) records nothing.
	missionRecordCompletionKB(t.Context(), deps, io.Discard, missionCandidate{ActiveID: "other"})
	var n int
	if err = kb.DB().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM mission_completions`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}
