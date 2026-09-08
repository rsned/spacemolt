package main

import "testing"

// get_ship gained a ship_id parameter server-side (it reads any ship you own
// from anywhere, without docking or travelling, and reports its installed
// modules). The client wrapper predates that and discarded parts[1:] entirely,
// so --ship_id, --ship_id=, and a bare positional were all ignored
// identically and the command always reported the ACTIVE ship. Silent, because
// the output looked like a valid ship.
func TestShipIDFromArgs(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"no args -> active ship", nil, ""},
		{"space form", []string{"--ship_id", "c77bafafd2328de51e88aa63516ed476"}, "c77bafafd2328de51e88aa63516ed476"},
		{"equals form", []string{"--ship_id=c77bafafd2328de51e88aa63516ed476"}, "c77bafafd2328de51e88aa63516ed476"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := shipIDFromArgs(tc.args)
			if err != nil {
				t.Fatalf("shipIDFromArgs(%v) error: %v", tc.args, err)
			}
			if got != tc.want {
				t.Errorf("shipIDFromArgs(%v) = %q, want %q", tc.args, got, tc.want)
			}
		})
	}
}

// A bare positional id is a plausible operator typo. It must fail loudly
// rather than silently reporting the active ship -- the failure mode this
// whole fix is about.
func TestShipIDFromArgsRejectsBarePositional(t *testing.T) {
	if _, err := shipIDFromArgs([]string{"c77bafafd2328de51e88aa63516ed476"}); err == nil {
		t.Error("bare positional ship id: want error, got nil")
	}
}
