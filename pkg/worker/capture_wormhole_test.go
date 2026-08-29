package worker

import (
	"testing"
	"time"
)

// TestWormholeExpiryAt covers the conversion that was missing entirely: the
// server states a wormhole's life as a relative duration, and only an absolute
// timestamp survives being stored.
func TestWormholeExpiryAt(t *testing.T) {
	now := time.Date(2026, 8, 28, 19, 11, 25, 0, time.UTC)

	tests := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{"hours, the observed shape", "12h", "2026-08-29T07:11:25Z", true},
		{"minutes", "45m", "2026-08-28T19:56:25Z", true},
		{"days, which ParseDuration cannot do alone", "2d", "2026-08-30T19:11:25Z", true},
		{"days plus hours", "1d6h", "2026-08-30T01:11:25Z", true},
		{"surrounding space", "  3h  ", "2026-08-28T22:11:25Z", true},
		{"empty means no wormhole detail", "", "", false},
		{"unparseable is dropped, not guessed", "soon", "", false},
		{"a bad day count is dropped", "xd4h", "", false},
		{"zero is not an expiry", "0h", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := wormholeExpiryAt(now, tc.in)
			if ok != tc.ok {
				t.Fatalf("wormholeExpiryAt(%q) ok = %v, want %v", tc.in, ok, tc.ok)
			}
			if got != tc.want {
				t.Errorf("wormholeExpiryAt(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
