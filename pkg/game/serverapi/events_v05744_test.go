package serverapi

import (
	"encoding/json"
	"testing"
)

// v0.574.x additions: death/kill pushes now carry the wreck's LOCATION
// (feeding per-death loss capture), player_died gains wreck_suppressed,
// battle-log snapshots flag NPCs and bosses, and a battle-log entry can
// carry a recovered summary for battles reconstructed after the fact.
func TestDecodeV0574WreckLocationFields(t *testing.T) {
	var pk PlayerKill
	if err := json.Unmarshal([]byte(`{"victim":"v","wreck_id":"w1","wreck_system_id":"nashira","wreck_system_name":"Nashira","wreck_poi_id":"nashira_belt","wreck_poi_name":"Nashira Belt","wreck_has_cargo":true}`), &pk); err != nil {
		t.Fatal(err)
	}
	if pk.WreckSystemID != "nashira" || pk.WreckPOIID != "nashira_belt" {
		t.Errorf("player_kill = %+v", pk)
	}

	var pd PlayerDied
	if err := json.Unmarshal([]byte(`{"killer_name":"MoltenOne","wreck_id":"w2","wreck_system_id":"rigel","wreck_poi_id":"rigel_belt","wreck_suppressed":true}`), &pd); err != nil {
		t.Fatal(err)
	}
	if pd.WreckSystemID != "rigel" || pd.WreckPOIID != "rigel_belt" || !pd.WreckSuppressed {
		t.Errorf("player_died = %+v", pd)
	}

	var pir PirateDestroyed
	if err := json.Unmarshal([]byte(`{"pirate_id":"p","wreck_id":"w3","wreck_system_id":"crix","wreck_poi_id":"crix_belt","wreck_poi_name":"Crix Belt","wreck_system_name":"Crix"}`), &pir); err != nil {
		t.Fatal(err)
	}
	if pir.WreckSystemID != "crix" || pir.WreckPOIName != "Crix Belt" {
		t.Errorf("pirate_destroyed = %+v", pir)
	}

	var snap ParticipantSnapshot
	if err := json.Unmarshal([]byte(`{"player_id":"x","kind":"pirate","is_npc":true,"is_boss":true}`), &snap); err != nil {
		t.Fatal(err)
	}
	if !snap.IsNPC || !snap.IsBoss {
		t.Errorf("snapshot = %+v", snap)
	}

	var entry BattleLogEntry
	if err := json.Unmarshal([]byte(`{"battle_id":"b","tick":1,"recovered_summary":{"category":"wildlife","ships_destroyed":2,"start_tick":9,"duration":30,"total_damage":400,"participant_names":["A"],"ships_captured":1}}`), &entry); err != nil {
		t.Fatal(err)
	}
	if entry.RecoveredSummary == nil || entry.RecoveredSummary.ShipsDestroyed != 2 || entry.RecoveredSummary.Category != "wildlife" {
		t.Errorf("recovered_summary = %+v", entry.RecoveredSummary)
	}
}
