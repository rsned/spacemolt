package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

func TestRecordCompletionInKB(t *testing.T) {
	dir := t.TempDir()
	ledger := filepath.Join(dir, "mission_completions.jsonl")
	accept := `{"agent_id":"craftsman-1","event":"accepted","mission_id":"inst1","template_id":"processing_the_data","title":"Processing the Data"}` + "\n"
	if err := os.WriteFile(ledger, []byte(accept), 0o644); err != nil {
		t.Fatal(err)
	}
	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: filepath.Join(dir, "kb.db")})
	if err != nil {
		t.Fatal(err)
	}
	raw := `{"command":"complete_mission","tick":77,"result":{"mission_id":"inst1","title":"Processing the Data","credits_earned":1000,
"reputation_changes":{"outerrim":2},"unlocks":[{"available":true,"mission":{"mission_id":"joining_the_investigators","title":"Joining the Investigators"}}]}}`
	if err := recordCompletionInKB(t.Context(), kb, "craftsman-1", ledger, []byte(raw)); err != nil {
		t.Fatal(err)
	}
	var tmpl, rep string
	if err := kb.DB().QueryRowContext(t.Context(), `SELECT template_id, reputation_changes FROM mission_completions WHERE mission_id='inst1'`).Scan(&tmpl, &rep); err != nil {
		t.Fatal(err)
	}
	if tmpl != "processing_the_data" || rep != `{"outerrim":2}` {
		t.Fatalf("tmpl=%q rep=%q", tmpl, rep)
	}
	var req string
	if err := kb.DB().QueryRowContext(t.Context(), `SELECT requires_mission_id FROM mission_prerequisites WHERE mission_id='joining_the_investigators'`).Scan(&req); err != nil {
		t.Fatal(err)
	}
	if req != "processing_the_data" {
		t.Fatalf("req=%q", req)
	}
}
