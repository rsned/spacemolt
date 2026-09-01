package serverapi

import (
	"encoding/json"
	"testing"
)

// v0.573.2 published payload shapes for eight push frames that were
// previously undocumented. These decode tests pin our structs to the
// documented fields.
func TestDecodeV05732PushPayloads(t *testing.T) {
	var restart ServerRestartWarning
	if err := json.Unmarshal([]byte(`{"message":"Server restarting in 60s","seconds_until_restart":60,"target_version":"0.573.2"}`), &restart); err != nil {
		t.Fatal(err)
	}
	if restart.SecondsUntilRestart != 60 || restart.TargetVersion != "0.573.2" {
		t.Errorf("restart = %+v", restart)
	}

	var adrift DroneAdrift
	if err := json.Unmarshal([]byte(`{"drone_id":"d1","drone_type":"mining","owner_id":"p1","system_id":"krynn","poi_id":"war_materials"}`), &adrift); err != nil {
		t.Fatal(err)
	}
	if adrift.DroneID != "d1" || adrift.SystemID != "krynn" || adrift.POIID != "war_materials" {
		t.Errorf("adrift = %+v", adrift)
	}

	var broken FactionAllianceBroken
	if err := json.Unmarshal([]byte(`{"by_faction_id":"f1","by_faction_name":"Crafting Collective","by_faction_tag":"CRFT","message":"m"}`), &broken); err != nil {
		t.Fatal(err)
	}
	if broken.ByFactionTag != "CRFT" {
		t.Errorf("broken = %+v", broken)
	}

	var war FactionWarDeclared
	if err := json.Unmarshal([]byte(`{"aggressor_faction_id":"a","aggressor_faction_name":"A","defender_faction_id":"d","defender_faction_name":"D","reason":"r","message":"m"}`), &war); err != nil {
		t.Fatal(err)
	}
	if war.AggressorFactionName != "A" || war.DefenderFactionID != "d" || war.Reason != "r" {
		t.Errorf("war = %+v", war)
	}

	var pp FactionPeaceProposal
	if err := json.Unmarshal([]byte(`{"from_faction_id":"f","from_faction_name":"F","terms":"t","message":"m"}`), &pp); err != nil {
		t.Fatal(err)
	}
	if pp.FromFactionName != "F" || pp.Terms != "t" {
		t.Errorf("peace proposal = %+v", pp)
	}

	var pa FactionPeaceAccepted
	if err := json.Unmarshal([]byte(`{"faction_id":"f","faction_name":"F","message":"m"}`), &pa); err != nil {
		t.Fatal(err)
	}
	if pa.FactionName != "F" {
		t.Errorf("peace accepted = %+v", pa)
	}

	var prop FactionAllianceProposal
	if err := json.Unmarshal([]byte(`{"from_faction_id":"f","from_faction_name":"F","from_faction_tag":"TAG ","message":"m"}`), &prop); err != nil {
		t.Fatal(err)
	}
	if prop.FromFactionID != "f" {
		t.Errorf("alliance proposal = %+v", prop)
	}

	var formed FactionAllianceFormed
	if err := json.Unmarshal([]byte(`{"with_faction_id":"f","with_faction_name":"F","with_faction_tag":"TAG","message":"m"}`), &formed); err != nil {
		t.Fatal(err)
	}
	if formed.WithFactionTag != "TAG" {
		t.Errorf("alliance formed = %+v", formed)
	}
}
