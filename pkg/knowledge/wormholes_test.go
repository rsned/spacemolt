package knowledge

import (
	"testing"
	"time"
)

// TestIsTransientWormhole uses ids taken verbatim from the KB on 2026-08-17.
func TestIsTransientWormhole(t *testing.T) {
	for _, tc := range []struct {
		id   string
		want bool
	}{
		{"wh_entrance_40a24840", true},  // the Goldcrest one, gone by 08-17
		{"wh_exit_826d1efd", true},      // hidden pair, 122 days stale
		{"wh_entrance_b1632c60", true},  // already typed collapsed
		{"frying_pan", false},           // named, permanent
		{"leap_of_faith", false},        // named, permanent
		{"whitecliff_star", false},      // the SQL LIKE '_' wildcard trap: 'wh_%' matches this
		{"wh_entrance_", false},         // no suffix at all
		{"wh_entrance_zzzzzzzz", false}, // not hex
		{"", false},
	} {
		if got := IsTransientWormhole(tc.id); got != tc.want {
			t.Errorf("IsTransientWormhole(%q) = %v, want %v", tc.id, got, tc.want)
		}
	}
}

// TestWormholeIntroMissionKey covers the join between a transient wormhole POI
// and the "Anomalous Readings" mission that wants it traversed: both carry the
// same hex suffix (wh_entrance_74993066 <-> wh_intro_nebula_74993066).
func TestWormholeIntroMissionKey(t *testing.T) {
	key, ok := WormholeIntroMissionKey("wh_entrance_74993066")
	if !ok || key != "74993066" {
		t.Errorf("key = %q ok=%v, want 74993066/true", key, ok)
	}
	if _, ok := WormholeIntroMissionKey("frying_pan"); ok {
		t.Error("a named wormhole has no intro-mission key")
	}
}

// TestWormholeLive is the liveness rule. The Goldcrest row is the case that
// motivated it: typed as a live entrance, 69 days unobserved, and gone.
func TestWormholeLive(t *testing.T) {
	day := 24 * time.Hour
	for _, tc := range []struct {
		name string
		w    WormholeStatus
		want bool
	}{
		{
			name: "collapsed is dead regardless of age",
			w:    WormholeStatus{POIID: "wh_entrance_b1632c60", Type: POITypeWormholeCollapsed, Transient: true, StaleFor: 2 * day},
			want: false,
		},
		{
			name: "the Goldcrest phantom: live-typed, transient, 69 days stale",
			w:    WormholeStatus{POIID: "wh_entrance_40a24840", Type: POITypeWormholeEntrance, Transient: true, StaleFor: 69 * day},
			want: false,
		},
		{
			name: "a transient one seen yesterday is worth acting on",
			w:    WormholeStatus{POIID: "wh_entrance_14910063", Type: POITypeWormholeEntrance, Transient: true, StaleFor: day},
			want: true,
		},
		{
			name: "a named wormhole never ages out",
			w:    WormholeStatus{POIID: "frying_pan", Type: POITypeWormholeEntrance, Transient: false, StaleFor: 300 * day},
			want: true,
		},
		{
			name: "a server expiry in the future wins over staleness",
			w: WormholeStatus{POIID: "wh_entrance_abc123", Type: POITypeWormholeEntrance, Transient: true,
				StaleFor: 60 * day, ExpiresAt: time.Now().Add(6 * time.Hour).UTC().Format(time.RFC3339)},
			want: true,
		},
		{
			name: "a server expiry in the past wins over freshness",
			w: WormholeStatus{POIID: "wh_entrance_abc123", Type: POITypeWormholeEntrance, Transient: true,
				StaleFor: time.Hour, ExpiresAt: time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)},
			want: false,
		},
	} {
		got, reason := wormholeLive(tc.w)
		if got != tc.want {
			t.Errorf("%s: live = %v (%s), want %v", tc.name, got, reason, tc.want)
		}
		if reason == "" {
			t.Errorf("%s: no reason given", tc.name)
		}
	}
}

// TestWormholeLiveness_AgainstAStoredRow walks the DB path and checks the tick
// arithmetic, which is what turns last_updated_tick into an age.
func TestWormholeLiveness_AgainstAStoredRow(t *testing.T) {
	kb := newTestKB(t)
	ctx := t.Context()

	if _, err := kb.db.ExecContext(ctx,
		`INSERT INTO systems (id, name, position_x, position_y) VALUES ('goldcrest','Goldcrest',0,0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := kb.db.ExecContext(ctx, `
		INSERT INTO pois (id, system_id, name, type, position_x, position_y, hidden, last_updated_tick)
		VALUES ('wh_entrance_40a24840','goldcrest','Wormhole Entrance','wormhole_entrance',0,0,1,1041109)`); err != nil {
		t.Fatal(err)
	}

	got, err := kb.WormholeLiveness(ctx, 1640000)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("rows = %d, want 1", len(got))
	}
	w := got[0]
	if !w.Transient || !w.Hidden {
		t.Errorf("classification = %+v, want transient and hidden", w)
	}
	// (1640000-1041109) ticks * 10s = ~69.3 days.
	if d := w.StaleFor.Hours() / 24; d < 69 || d > 70 {
		t.Errorf("StaleFor = %.1f days, want ~69.3", d)
	}
	if w.Live {
		t.Errorf("row reported live: %s", w.Reason)
	}
}

// TestKnownPermanentWormholes pins what the smuggling chain told us, since the
// KB holds no POI row for either and this list is the only record.
func TestKnownPermanentWormholes(t *testing.T) {
	w, ok := PermanentWormholeIn("hadar")
	if !ok || w.Name != "Frying Pan" || w.MissionID != "through_the_fire" {
		t.Errorf("hadar = %+v ok=%v", w, ok)
	}
	w, ok = PermanentWormholeIn("alzirr")
	if !ok || w.Name != "Leap of Faith" || w.MissionID != "leap_of_faith" {
		t.Errorf("alzirr = %+v ok=%v", w, ok)
	}
	if _, ok := PermanentWormholeIn("goldcrest"); ok {
		t.Error("goldcrest has no permanent wormhole; its row was a 69-day-old transient")
	}
	// POIID is empty on purpose: it needs a survey, and inventing one would put
	// a fabricated coordinate into the routing data.
	for _, k := range KnownPermanentWormholes {
		if k.POIID != "" {
			t.Errorf("%s carries a POI id we never observed: %q", k.Name, k.POIID)
		}
		if k.SystemID == "" || k.AnchorHint == "" {
			t.Errorf("%s is missing the location the mission text gave: %+v", k.Name, k)
		}
	}
}
