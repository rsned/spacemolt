package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// completeMissionShortfallJSON builds a complete_mission action_result that
// pays less than the board advertised AND carries the server's own
// explanation fields — `message` and `skill_xp_gained` — which the worker
// currently parses into CompleteMissionResponse and then discards. Those are
// the only fields that can distinguish the server's reasons for a reduced
// payout, so they are what a server-bug report has to quote.
func completeMissionShortfallJSON(t *testing.T, missionID string, creditsEarned int64, message string) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"command": "complete_mission",
		"tick":    1000,
		"result": map[string]any{
			"mission_id":      missionID,
			"credits_earned":  creditsEarned,
			"message":         message,
			"skill_xp_gained": map[string]any{"smuggling": 50},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// missionDepsTo is missionDeps with the log captured instead of discarded.
func missionDepsTo(fc *fakeClient, store *fakeMissionStore, kb *fakeKB, out io.Writer) MissionDeps {
	d := missionDeps(fc, store, kb)
	d.Out = out
	return d
}

// TestMissionShortfallLogsServerPayload pins the diagnostic that makes
// reduced smuggling payouts filable as server bugs.
//
// Observed 2026-07-31: smuggling couriers paid 0-85% of the advertised
// reward, varying per agent for the SAME mission id at the SAME elapsed
// time. Time decay, partial delivery, confiscation and pool-depletion were
// all refuted from mission_results, because the discriminating field is not
// one we record — the server states it in the complete_mission `message`,
// which we parse and drop on the floor.
//
// When realized < advertised, the worker must log the server's own result
// payload verbatim plus the tick context needed to reconstruct the run.
func TestMissionShortfallLogsServerPayload(t *testing.T) {
	entry := boardEntry("m1", "steel", 20, "sol_station", "sol", 3000, 0)
	active := serverapi.ActiveMission{MissionID: "hex-m1", TemplateID: "m1", Type: "delivery", Title: "Deliver steel"}
	fc := &fakeClient{
		state:             missionState(true, 5000, 0),
		completeReward:    3000,
		activeMissionsSeq: [][]byte{activeJSON(t), activeJSON(t, active)},
		raw: map[string][]byte{
			"missions":         boardJSON(t, entry),
			"complete_mission": completeMissionShortfallJSON(t, "hex-m1", 412, "Payment reduced: cargo scanned at the border"),
		},
	}
	store := &fakeMissionStore{asks: map[string]float64{"steel": 20}}

	var log bytes.Buffer
	if err := Missions(context.Background(), missionDepsTo(fc, store, missionKB(), &log)); err != nil {
		t.Fatalf("Missions: %v", err)
	}

	got := log.String()
	if !strings.Contains(got, "shortfall") {
		t.Fatalf("a reduced payout must emit a shortfall diagnostic; log was:\n%s", got)
	}
	// The server's own explanation is the whole point — without it the line
	// restates numbers we already have.
	if !strings.Contains(got, "cargo scanned at the border") {
		t.Fatalf("shortfall line must quote the server's message verbatim; log was:\n%s", got)
	}
	// Tick context: what the budget was at accept and when delivery landed,
	// so a report can show the run was (or was not) late.
	if !strings.Contains(got, "tick=") || !strings.Contains(got, "budget=") {
		t.Fatalf("shortfall line must carry accept/deliver tick context; log was:\n%s", got)
	}
	// Full payment must stay quiet — this fires only on a real shortfall.
	if strings.Count(got, "shortfall") != 1 {
		t.Fatalf("want exactly one shortfall line, got %d; log was:\n%s", strings.Count(got, "shortfall"), got)
	}
}

// TestMissionFullPaymentLogsNoShortfall guards the negative case: a mission
// that pays what it advertised must not emit the diagnostic, or the signal
// drowns in the fleet's normal traffic.
func TestMissionFullPaymentLogsNoShortfall(t *testing.T) {
	entry := boardEntry("m1", "steel", 20, "sol_station", "sol", 3000, 0)
	active := serverapi.ActiveMission{MissionID: "hex-m1", TemplateID: "m1", Type: "delivery", Title: "Deliver steel"}
	fc := &fakeClient{
		state:             missionState(true, 5000, 0),
		completeReward:    3000,
		activeMissionsSeq: [][]byte{activeJSON(t), activeJSON(t, active)},
		raw: map[string][]byte{
			"missions":         boardJSON(t, entry),
			"complete_mission": completeMissionResultJSON(t, "hex-m1", 3000),
		},
	}
	store := &fakeMissionStore{asks: map[string]float64{"steel": 20}}

	var log bytes.Buffer
	if err := Missions(context.Background(), missionDepsTo(fc, store, missionKB(), &log)); err != nil {
		t.Fatalf("Missions: %v", err)
	}
	if strings.Contains(log.String(), "shortfall") {
		t.Fatalf("full payment must not log a shortfall; log was:\n%s", log.String())
	}
	_ = time.Now
}
