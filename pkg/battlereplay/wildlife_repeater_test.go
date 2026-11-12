package battlereplay

import "testing"

func TestIsWildlifeIncludesRepeaters(t *testing.T) {
	for _, tc := range []struct {
		p    Participant
		want bool
	}{
		{Participant{PlayerID: "crt_1", Kind: "creature"}, true},
		{Participant{PlayerID: "repeater_383b6aeca7f9224a7eb52dfca1da288b", Username: "Repeater Pane"}, true},
		{Participant{PlayerID: "x", Kind: "repeater"}, true},
		{Participant{PlayerID: "a50924913cef881c5e4d14257589d9ba", Kind: "player"}, false},
	} {
		if got := IsWildlife(tc.p); got != tc.want {
			t.Errorf("IsWildlife(%+v) = %v, want %v", tc.p, got, tc.want)
		}
	}
}
