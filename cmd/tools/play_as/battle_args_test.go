package main

import (
	"reflect"
	"testing"
)

// Live 2026-11-13: `battle stance fire` sent {"action":"stance"} and the server
// refused it as invalid_stance; the positional stance was dropped.
func TestBattlePayload(t *testing.T) {
	for _, tc := range []struct {
		in   []string
		want map[string]any
	}{
		{[]string{"battle", "stance", "fire"}, map[string]any{"action": "stance", "stance": "fire"}},
		{[]string{"battle", "stance", "--stance", "flee"}, map[string]any{"action": "stance", "stance": "flee"}},
		{[]string{"battle", "target", "c123"}, map[string]any{"action": "target", "target_id": "c123"}},
		{[]string{"battle", "engage", "--side_id", "2"}, map[string]any{"action": "engage", "side_id": 2}},
		{[]string{"battle", "retreat"}, map[string]any{"action": "retreat"}},
	} {
		got, err := battlePayload(tc.in)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("battlePayload(%v) = %v, %v; want %v", tc.in, got, err, tc.want)
		}
	}
	for _, bad := range [][]string{{"battle"}, {"battle", "retreat", "now"}, {"battle", "stance"}} {
		if _, err := battlePayload(bad); err == nil {
			t.Errorf("battlePayload(%v) accepted", bad)
		}
	}
}
