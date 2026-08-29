package game

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// TestSlugSystemID pins the conversion that repairs the join between player
// sightings and the systems table. The names are the ones actually stored.
func TestSlugSystemID(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"a display name with a space", "Alpha Centauri", "alpha_centauri"},
		{"a display name with a dash", "GSC-0002", "gsc_0002"},
		{"the one name with an apostrophe", "Trader's Rest", "traders_rest"},
		{"simple capitalisation", "Bellatrix", "bellatrix"},
		{"several words", "Epsilon Eridani", "epsilon_eridani"},
		{"a real id passes through untouched", "alpha_centauri", "alpha_centauri"},
		{"a real id with digits", "gsc_0007", "gsc_0007"},
		{"empty stays empty", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := slugSystemID(tc.in); got != tc.want {
				t.Errorf("slugSystemID(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestSlugSystemIDIsIdempotent guards the property migration 55 relies on: it
// rewrites every row, so slugging an already-correct id must be a no-op.
func TestSlugSystemIDIsIdempotent(t *testing.T) {
	for _, in := range []string{"Alpha Centauri", "GSC-0002", "bellatrix", "gsc_0007", ""} {
		once := slugSystemID(in)
		if twice := slugSystemID(once); twice != once {
			t.Errorf("slugSystemID(%q) not idempotent: %q then %q", in, once, twice)
		}
	}
}

// TestNotifyPlayers_SlugsADisplayName pins the contract the sightings repair
// depends on: whatever spelling state carries, an observation is stamped with
// the id form the KB joins on.
//
// This is the regression. CurrentSystem holds the system NAME, and the observer
// stamped it verbatim, so "Alpha Centauri" was written where systems.id holds
// "alpha_centauri" -- 83% of player sightings unjoinable to the systems table.
func TestNotifyPlayers_SlugsADisplayName(t *testing.T) {
	tests := []struct {
		name  string
		state *State
		want  string
	}{
		{
			name:  "a display name in CurrentSystem is slugged",
			state: &State{CurrentSystem: "Alpha Centauri"},
			want:  "alpha_centauri",
		},
		{
			name:  "System.ID wins over CurrentSystem",
			state: &State{CurrentSystem: "Alpha Centauri", System: SystemData{ID: "bellatrix"}},
			want:  "bellatrix",
		},
		{
			name:  "a name that reached System.ID is slugged too",
			state: &State{System: SystemData{ID: "Trader's Rest"}},
			want:  "traders_rest",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := &Client{state: tc.state}
			var captured []ObservedPlayer
			c.SetPlayerObserver(func(obs []ObservedPlayer) { captured = append(captured, obs...) })
			c.notifyPlayers("get_nearby", []serverapi.NearbyPlayer{{PlayerID: "p1", Username: "u1"}}, "poi-x")

			if len(captured) != 1 {
				t.Fatalf("got %d observations, want 1", len(captured))
			}
			if captured[0].SystemID != tc.want {
				t.Errorf("SystemID = %q, want %q", captured[0].SystemID, tc.want)
			}
		})
	}
}
