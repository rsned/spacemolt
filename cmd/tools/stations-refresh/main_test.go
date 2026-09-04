package main

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

const payload = `{"empires":[],"stations":[
  {"base_id":"b1","poi_id":"p1","name":"The Veil Anchor","description":"d1","type":"station",
   "system_id":"bd20_2457","system_name":"BD+20 2457","services":["market","refuel"],
   "condition":"thriving","condition_text":"ok","satisfaction_pct":100},
  {"base_id":"b2","poi_id":"p2","name":"ENDL:saltwind","description":"d2","type":"outpost",
   "system_id":"saltwind","system_name":"Saltwind","services":[],
   "condition":"","condition_text":"","satisfaction_pct":0}]}`

func serve(t *testing.T, status int, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	return srv.URL
}

func TestFetch(t *testing.T) {
	got, err := fetch(serve(t, http.StatusOK, payload))
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d stations, want 2", len(got))
	}
	if got[0].BaseID != "b1" || got[0].POIID != "p1" {
		t.Errorf("alias pair not decoded: %+v", got[0])
	}
	if len(got[0].Services) != 2 || got[0].Services[0] != "market" {
		t.Errorf("services not decoded: %+v", got[0].Services)
	}
	// The service-less outpost must decode as an EMPTY list, not as a missing
	// field that a caller could mistake for "unknown, assume it refuels".
	if got[1].Services == nil || len(got[1].Services) != 0 {
		t.Errorf("outpost services = %v, want empty non-nil", got[1].Services)
	}
}

func TestFetchErrors(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"http error", "nope", http.StatusInternalServerError},
		{"malformed", "{not json", http.StatusOK},
		{"empty station list", `{"stations":[]}`, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := fetch(serve(t, tc.status, tc.body)); err == nil {
				t.Error("want an error, got nil")
			}
		})
	}
}

// newDB builds the two tables the tool touches, with the same defaults the
// knowledge schema uses.
func newDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "kb.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`
		CREATE TABLE bases (id TEXT PRIMARY KEY, poi_id TEXT NOT NULL, name TEXT NOT NULL,
			description TEXT, empire TEXT, defense_level INTEGER DEFAULT 0,
			has_drones BOOLEAN DEFAULT 0, public_access BOOLEAN DEFAULT 1,
			last_updated_tick INTEGER DEFAULT 0, story TEXT DEFAULT '',
			pirate_rep_required INTEGER DEFAULT 0, condition TEXT DEFAULT '',
			condition_text TEXT DEFAULT '', satisfaction_pct INTEGER DEFAULT 0,
			satisfied_count INTEGER DEFAULT 0, total_service_infra INTEGER DEFAULT 0);
		CREATE TABLE base_services (base_id TEXT NOT NULL, service_name TEXT NOT NULL,
			available BOOLEAN DEFAULT 1, PRIMARY KEY (base_id, service_name));`); err != nil {
		t.Fatalf("schema: %v", err)
	}

	return db
}

// TestWritePreservesLocalOnlyFields is the point of the whole tool. The
// canonical payload has no empire (it carries faction_* instead) and no story,
// so a refresh must update the fields it knows and leave the rest alone.
// Routing this through knowledge.RememberBase would blank empire on every row,
// because that upsert assigns empire = excluded.empire unconditionally.
func TestWritePreservesLocalOnlyFields(t *testing.T) {
	db := newDB(t)
	if _, err := db.Exec(`
		INSERT INTO bases (id, poi_id, name, description, empire, story, public_access,
		                   total_service_infra, last_updated_tick)
		VALUES ('b1','STALE_POI','Old Name','old','crimson','a story',1,17,1795812)`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO base_services VALUES ('b1','shipyard',1)`); err != nil {
		t.Fatalf("seed services: %v", err)
	}

	stations, err := fetch(serve(t, http.StatusOK, payload))
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	// b1 exists locally (update), b2 does not (insert).
	if err := write(db, stations[1:], stations[:1]); err != nil {
		t.Fatalf("write: %v", err)
	}

	var poi, name, empire, story string
	var infra int
	var tick int64
	if err := db.QueryRow(`SELECT poi_id, name, empire, story, total_service_infra, last_updated_tick
	                       FROM bases WHERE id='b1'`).
		Scan(&poi, &name, &empire, &story, &infra, &tick); err != nil {
		t.Fatalf("read back: %v", err)
	}
	// Canonical fields refreshed...
	if poi != "p1" {
		t.Errorf("poi_id = %q, want p1 (the alias must be corrected)", poi)
	}
	if name != "The Veil Anchor" {
		t.Errorf("name = %q, want The Veil Anchor", name)
	}
	// ...local-only fields untouched.
	if empire != "crimson" {
		t.Errorf("empire = %q, want crimson preserved -- this is the RememberBase trap", empire)
	}
	if story != "a story" || infra != 17 || tick != 1795812 {
		t.Errorf("local fields clobbered: story=%q infra=%d tick=%d", story, infra, tick)
	}

	// Services are replaced wholesale, so the stale shipyard must be gone.
	var svc int
	if err := db.QueryRow(`SELECT count(*) FROM base_services WHERE base_id='b1' AND service_name='shipyard'`).
		Scan(&svc); err != nil {
		t.Fatalf("count services: %v", err)
	}
	if svc != 0 {
		t.Error("stale service survived; the endpoint states the full set and must replace it")
	}

	// The inserted outpost exists and correctly offers nothing.
	if err := db.QueryRow(`SELECT count(*) FROM base_services WHERE base_id='b2'`).Scan(&svc); err != nil {
		t.Fatalf("count b2 services: %v", err)
	}
	if svc != 0 {
		t.Errorf("b2 services = %d, want 0 (a service-less outpost)", svc)
	}
	var got string
	if err := db.QueryRow(`SELECT name FROM bases WHERE id='b2'`).Scan(&got); err != nil {
		t.Fatalf("b2 not inserted: %v", err)
	}
}
