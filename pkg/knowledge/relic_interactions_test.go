package knowledge

import (
	"context"
	"testing"
	"time"
)

// Every interact_poi attempt at a relic is kept, per agent, so the operator
// can list the relics that gave something back.
func TestRelicInteractionsRoundTrip(t *testing.T) {
	stores := map[string]Base{"sqlite": newTestKB(t), "memory": NewMemoryKB()}
	for name, kb := range stores {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			at := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
			rows := []RelicInteraction{
				{POIID: "old_gate", SystemID: "far_wick", AgentID: "explorer-1", Tick: 100, Outcome: RelicItems,
					Prompt: "A dormant gate.", Message: "You recover parts.", Items: map[string]int{"relic_shard": 2, "scrap": 5},
					ObservedUTC: at},
				{POIID: "old_gate", SystemID: "far_wick", AgentID: "explorer-2", Tick: 140, Outcome: RelicMissionSkipped,
					Prompt: "A dormant gate.", MissionJSON: `{"id":"m1"}`, ObservedUTC: at.Add(time.Hour)},
				{POIID: "vault", SystemID: "far_wick", AgentID: "explorer-1", Tick: 150, Outcome: RelicDataCopied,
					DataCopied: true, ObservedUTC: at.Add(2 * time.Hour)},
				{POIID: "husk", SystemID: "far_wick", AgentID: "explorer-1", Tick: 160, Outcome: RelicRefused,
					Error: "nothing here", ObservedUTC: at.Add(3 * time.Hour)},
			}
			for _, r := range rows {
				if err := kb.RecordRelicInteraction(ctx, r); err != nil {
					t.Fatalf("record %s/%s: %v", r.POIID, r.AgentID, err)
				}
			}
			got, err := kb.RelicInteractions(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(rows) {
				t.Fatalf("read %d rows, want %d", len(got), len(rows))
			}
			byKey := map[string]RelicInteraction{}
			for _, r := range got {
				byKey[r.POIID+"/"+r.AgentID] = r
			}
			gate := byKey["old_gate/explorer-1"]
			if gate.Items["relic_shard"] != 2 || gate.Items["scrap"] != 5 || gate.Tick != 100 ||
				gate.Prompt != "A dormant gate." || gate.Message != "You recover parts." || !gate.ObservedUTC.Equal(at) {
				t.Errorf("items row came back as %+v", gate)
			}
			if m := byKey["old_gate/explorer-2"]; m.MissionJSON != `{"id":"m1"}` || m.Outcome != RelicMissionSkipped {
				t.Errorf("mission row came back as %+v", m)
			}
			if e := byKey["husk/explorer-1"]; e.Error != "nothing here" {
				t.Errorf("refused row lost its error: %+v", e)
			}
			if !byKey["vault/explorer-1"].DataCopied {
				t.Error("data_copied flag lost")
			}
			for key, want := range map[string]bool{
				"old_gate/explorer-1": true, "vault/explorer-1": true,
				"old_gate/explorer-2": false, "husk/explorer-1": false,
			} {
				if byKey[key].Responded() != want {
					t.Errorf("%s Responded()=%v, want %v", key, !want, want)
				}
			}
		})
	}
}
