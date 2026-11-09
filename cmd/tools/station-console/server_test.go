package main

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/assets"
	"github.com/rsned/spacemolt/pkg/stationstate"
)

const testBase = "c976fa7df53c87f9616165d7d411b41a"

var testNow = time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)

func newTestServer(t *testing.T) (*server, *assets.Store) {
	t.Helper()
	store, err := assets.Open(assets.Config{DBPath: filepath.Join(t.TempDir(), "assets.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return &server{store: store, baseID: testBase, modelsDir: t.TempDir(), now: func() time.Time { return testNow }}, store
}

func get(t *testing.T, s *server, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func seedCJ(t *testing.T, store *assets.Store) {
	t.Helper()
	ctx := context.Background()
	fixture := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join("..", "..", "..", "pkg", "assets", "testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	base := assets.BaseSnapshot{
		BaseID: testBase, Hull: 85000, MaxHull: 85000, Shield: 12000, MaxShield: 12000, Armor: 60, WeaponDPS: 30,
		FuelReserve: 21293, FuelCapacity: 55000, PowerSupply: 40, LifeSupportSupply: 12, LifeSupportDemand: 10,
		SatisfactionPct: 100,
	}
	if err := store.InsertBaseSnapshot(ctx, "a1", base, testNow.Add(-5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	_, rows, ok := assets.FacilityRowsFrom(fixture("cj_facility_list.json"))
	if !ok {
		t.Fatal("facility fixture unreadable")
	}
	if err := store.InsertFacilityRows(ctx, testBase, "a1", rows, testNow.Add(-5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	p, ok := assets.PassengerSnapshotFrom(fixture("cj_station_passengers.json"))
	if !ok {
		t.Fatal("passenger fixture unreadable")
	}
	if err := store.InsertPassengerSnapshot(ctx, testBase, "a1", p, testNow.Add(-5*time.Minute)); err != nil {
		t.Fatal(err)
	}
}

func TestAPIState_NoData(t *testing.T) {
	s, _ := newTestServer(t)
	rec := get(t, s, "/api/state")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"Word":"NO DATA"`) {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body)
	}
}

func TestAPIState_CJ(t *testing.T) {
	s, store := newTestServer(t)
	seedCJ(t, store)
	rec := get(t, s, "/api/state")
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body)
	}
	var got struct {
		Word           string
		Zones          []json.RawMessage
		Station        string
		TourismPerHour *float64 `json:"tourism_per_hour"`
		Passengers     *json.RawMessage
		Base           struct{ FuelReserve int64 }
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Word != "STRAINED" || len(got.Zones) != 8 || got.Station != "Conjunction Junction" {
		t.Errorf("word=%q zones=%d station=%q", got.Word, len(got.Zones), got.Station)
	}
	if got.TourismPerHour != nil || !strings.Contains(rec.Body.String(), `"tourism_per_hour":null`) {
		t.Errorf("tourism_per_hour should be null: %s", rec.Body)
	}
	if got.Passengers == nil || got.Base.FuelReserve != 21293 {
		t.Errorf("passengers=%v base=%+v", got.Passengers, got.Base)
	}
}

func TestAPIHistory_Clamp(t *testing.T) {
	s, store := newTestServer(t)
	base := assets.BaseSnapshot{BaseID: testBase, Hull: 1, MaxHull: 1}
	// 100h old is inside a clamped 168h window but outside the default 48h one.
	if err := store.InsertBaseSnapshot(context.Background(), "a1", base, testNow.Add(-100*time.Hour)); err != nil {
		t.Fatal(err)
	}
	count := func(q string) int {
		rec := get(t, s, "/api/history"+q)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: code=%d", q, rec.Code)
		}
		var pts []json.RawMessage
		if err := json.Unmarshal(rec.Body.Bytes(), &pts); err != nil {
			t.Fatal(err)
		}
		return len(pts)
	}
	if n := count("?hours=9999"); n != 1 {
		t.Errorf("hours=9999: %d points, want 1", n)
	}
	if n := count(""); n != 0 {
		t.Errorf("default window: %d points, want 0", n)
	}
	if rec := get(t, s, "/api/history?hours=abc"); rec.Code != http.StatusBadRequest {
		t.Errorf("hours=abc: code=%d", rec.Code)
	}
}

func TestModels_DefaultWheel(t *testing.T) {
	s, _ := newTestServer(t)
	var ms []stationModel
	if err := json.Unmarshal(get(t, s, "/api/models").Body.Bytes(), &ms); err != nil {
		t.Fatal(err)
	}
	if len(ms) != 9 {
		t.Fatalf("%d models", len(ms))
	}
	var defaults []string
	for _, m := range ms {
		if m.Default {
			defaults = append(defaults, m.ID)
		}
	}
	if len(defaults) != 1 || defaults[0] != "player__wheel__capital_s5521" {
		t.Errorf("defaults = %v", defaults)
	}
}

func TestModelFile(t *testing.T) {
	s, _ := newTestServer(t)
	dir := filepath.Join(s.modelsDir, "player__wheel__capital_s5521")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mesh.glb"), []byte("glb"), 0o644); err != nil {
		t.Fatal(err)
	}
	if rec := get(t, s, "/models/player__wheel__capital_s5521/mesh.glb"); rec.Code != http.StatusOK || rec.Body.String() != "glb" {
		t.Errorf("code=%d body=%q", rec.Code, rec.Body)
	}
	if rec := get(t, s, "/models/player__wheel__capital_s5521/keyed.png"); rec.Code != http.StatusNotFound {
		t.Errorf("missing file: code=%d", rec.Code)
	}
}

func TestModelFile_RejectsUnknown(t *testing.T) {
	s, _ := newTestServer(t)
	for _, p := range []string{"/models/..%2F..%2Fetc/mesh.glb", "/models/nope/mesh.glb"} {
		if rec := get(t, s, p); rec.Code != http.StatusNotFound {
			t.Errorf("%s: code=%d", p, rec.Code)
		}
	}
}

func TestStaticAndZones(t *testing.T) {
	s, _ := newTestServer(t)
	if rec := get(t, s, "/"); rec.Code != http.StatusOK {
		t.Errorf("/: code=%d", rec.Code)
	}
	if rec := get(t, s, "/zones/nope.json"); rec.Code != http.StatusNotFound {
		t.Errorf("zones: code=%d", rec.Code)
	}
	for _, p := range []string{"/static/console.js", "/static/console.css"} {
		if rec := get(t, s, p); rec.Code != http.StatusOK {
			t.Errorf("%s: code=%d", p, rec.Code)
		}
	}
}

// Every zone map is embedded, names a listed model, and places all eight zones.
func TestZoneMaps(t *testing.T) {
	s, _ := newTestServer(t)
	wheel := "/zones/" + stationModels[0].ID + ".json"
	if rec := get(t, s, wheel); rec.Code != http.StatusOK {
		t.Fatalf("%s: code=%d", wheel, rec.Code)
	}
	files, err := fs.Glob(webFS, "web/zones/*.json")
	if err != nil || len(files) == 0 {
		t.Fatalf("zone maps: %v %v", files, err)
	}
	for _, f := range files {
		raw, err := webFS.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var m struct {
			Model   string
			Regions []struct{ Zone string }
		}
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if _, ok := findModel(m.Model); !ok || filepath.Base(f) != m.Model+".json" {
			t.Errorf("%s: model %q", f, m.Model)
		}
		seen := map[string]bool{}
		for _, r := range m.Regions {
			seen[r.Zone] = true
		}
		for _, z := range stationstate.Zones {
			if !seen[string(z)] {
				t.Errorf("%s: no region for zone %s", f, z)
			}
		}
	}
}
