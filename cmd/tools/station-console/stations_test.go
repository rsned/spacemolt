package main

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/assets"
)

const testBeid = "b7ecd9b1ce7c2689d73d4f15f30dd134"

// stateNames is the slice of /api/state a station switch changes.
type stateNames struct {
	Word      string `json:"Word"`
	Station   string `json:"station"`
	StationID string `json:"station_id"`
	System    string `json:"system"`
}

func TestStations_ListsCJThenBeid(t *testing.T) {
	s, _ := newTestServer(t)
	var got []consoleStation
	rec := get(t, s, "/api/stations")
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 stations, got %+v", got)
	}
	if got[0].ID != testBase || got[0].Name != "Conjunction Junction" || got[0].System != "Cloverfield" || !got[0].Default {
		t.Errorf("first station = %+v, want default Conjunction Junction in Cloverfield", got[0])
	}
	if got[1].ID != testBeid || got[1].Name != "Jim Bob's Backwater Shack" || got[1].System != "Beid" || got[1].Default {
		t.Errorf("second station = %+v, want non-default Jim Bob's Backwater Shack in Beid", got[1])
	}
	for _, st := range got {
		if _, ok := findModel(st.Model); !ok {
			t.Errorf("station %s default model %q is not a listed model", st.ID, st.Model)
		}
	}
}

func TestAPIState_StationParamSelectsBeid(t *testing.T) {
	s, store := newTestServer(t)
	snap := assets.BaseSnapshot{BaseID: testBeid, Hull: 1000, MaxHull: 1000, PowerSupply: 20, LifeSupportSupply: 5, LifeSupportDemand: 5, SatisfactionPct: 100}
	if err := store.InsertBaseSnapshot(context.Background(), "craftsman-2", snap, testNow.Add(-5*time.Minute)); err != nil {
		t.Fatal(err)
	}

	var beid stateNames
	rec := get(t, s, "/api/state?station="+testBeid)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &beid); err != nil {
		t.Fatal(err)
	}
	if beid.Station != "Jim Bob's Backwater Shack" || beid.System != "Beid" || beid.StationID != testBeid {
		t.Errorf("beid state names %q/%q/%q", beid.Station, beid.System, beid.StationID)
	}
	if beid.Word == "NO DATA" {
		t.Errorf("beid has a reading but state says NO DATA")
	}

	// The default station is still CJ, which has no reading in this store.
	var cj stateNames
	if err := json.Unmarshal(get(t, s, "/api/state").Body.Bytes(), &cj); err != nil {
		t.Fatal(err)
	}
	if cj.StationID != testBase || cj.Station != "Conjunction Junction" || cj.Word != "NO DATA" {
		t.Errorf("default state = %q/%q word %q, want CJ with NO DATA", cj.StationID, cj.Station, cj.Word)
	}
}

func TestAPI_UnknownStationIsNotFound(t *testing.T) {
	s, _ := newTestServer(t)
	for _, p := range []string{"/api/state?station=nope", "/api/history?station=nope", "/api/state?station=../etc"} {
		if rec := get(t, s, p); rec.Code != http.StatusNotFound {
			t.Errorf("%s: code=%d, want 404", p, rec.Code)
		}
	}
}

func TestAPIHistory_StationParam(t *testing.T) {
	s, store := newTestServer(t)
	snap := assets.BaseSnapshot{BaseID: testBeid, Hull: 1000, MaxHull: 1000}
	if err := store.InsertBaseSnapshot(context.Background(), "craftsman-2", snap, testNow.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	var beid, cj []assets.HistoryPoint
	if err := json.Unmarshal(get(t, s, "/api/history?station="+testBeid).Body.Bytes(), &beid); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(get(t, s, "/api/history").Body.Bytes(), &cj); err != nil {
		t.Fatal(err)
	}
	if len(beid) != 1 || len(cj) != 0 {
		t.Errorf("history points beid=%d cj=%d, want 1 and 0", len(beid), len(cj))
	}
}
