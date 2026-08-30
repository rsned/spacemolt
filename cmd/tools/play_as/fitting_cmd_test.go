package main

import (
	"encoding/json"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// The operator's Survey Vessel get_ship reply (2026-08-30), trimmed to the
// fields the link needs. Slot numbering follows the modules array order.
const surveyVesselShip = `{"ship":{"class_id":"survey_vessel"},"modules":[
 {"id":"a","type_id":"anomaly_detector","slot":"utility","type":"utility"},
 {"id":"b","type_id":"survey_scanner_ii","slot":"utility","type":"utility"},
 {"id":"c","type_id":"cloaking_device_i","slot":"utility","type":"utility"},
 {"id":"d","type_id":"shield_booster_iv","slot":"defense","type":"defense"},
 {"id":"e","type_id":"shield_recharger_ii","slot":"defense","type":"defense"},
 {"id":"f","type_id":"pulse_laser_iii","slot":"weapon","type":"weapon"},
 {"id":"g","type_id":"pulse_laser_iii","slot":"weapon","type":"weapon"},
 {"id":"h","type_id":"ship_scanner_iii","slot":"utility","type":"utility"}]}`

func decodeShip(t *testing.T, raw string) serverapi.GetShipResponse {
	t.Helper()
	var resp serverapi.GetShipResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestFittingURL_SurveyVessel(t *testing.T) {
	got := fittingURL(decodeShip(t, surveyVesselShip), 22)
	want := fittingBaseURL + "?ship=survey_vessel&eng=22&stock=0" +
		"&w1=pulse_laser_iii&w2=pulse_laser_iii" +
		"&d1=shield_booster_iv&d2=shield_recharger_ii" +
		"&u1=anomaly_detector&u2=survey_scanner_ii&u3=cloaking_device_i&u4=ship_scanner_iii"
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

// A loaded round rides along as am_<ammo_type>=<round>; a weapon with nothing
// loaded contributes no ammo param so the page fills its default.
func TestFittingURL_LoadedAmmo(t *testing.T) {
	raw := `{"ship":{"class_id":"eviction_notice"},"modules":[
 {"id":"a","type_id":"autocannon_ii","slot":"weapon","ammo_type":"autocannon","loaded_ammo_id":"antimatter_core_rounds_box"},
 {"id":"b","type_id":"railgun_i","slot":"weapon","ammo_type":"railgun"}]}`
	got := fittingURL(decodeShip(t, raw), 0)
	want := fittingBaseURL + "?ship=eviction_notice&stock=0&w1=autocannon_ii&w2=railgun_i&am_autocannon=antimatter_core_rounds_box"
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

// An empty hull is just the ship; eng=0 is omitted like the page does.
func TestFittingURL_EmptyHull(t *testing.T) {
	got := fittingURL(decodeShip(t, `{"ship":{"class_id":"absence"},"modules":[]}`), 0)
	if want := fittingBaseURL + "?ship=absence"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestFittingURL_NoShip(t *testing.T) {
	if got := fittingURL(serverapi.GetShipResponse{}, 5); got != "" {
		t.Errorf("got %q for an empty reply, want \"\"", got)
	}
}

// The operator's Eviction Notice, live 2026-08-30: get_ship mounts FOUR Pulse
// Laser IIIs (the hand-built link they compared against named three) and no
// ammo-fed weapon, so no am_ params — the link says what is fitted, not what
// the page last remembered.
func TestFittingURL_EvictionNotice(t *testing.T) {
	raw := `{"ship":{"class_id":"eviction_notice"},"modules":[
 {"id":"1","type_id":"adaptive_shield_iii","slot":"defense"},
 {"id":"2","type_id":"adaptive_shield_iii","slot":"defense"},
 {"id":"3","type_id":"pulse_laser_iii","slot":"weapon"},
 {"id":"4","type_id":"pulse_laser_iii","slot":"weapon"},
 {"id":"5","type_id":"pulse_laser_iii","slot":"weapon"},
 {"id":"6","type_id":"pulse_laser_iii","slot":"weapon"},
 {"id":"7","type_id":"basic_tow_rig","slot":"utility"}]}`
	got := fittingURL(decodeShip(t, raw), 22)
	want := fittingBaseURL + "?ship=eviction_notice&eng=22&stock=0" +
		"&w1=pulse_laser_iii&w2=pulse_laser_iii&w3=pulse_laser_iii&w4=pulse_laser_iii" +
		"&d1=adaptive_shield_iii&d2=adaptive_shield_iii&u1=basic_tow_rig"
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}
