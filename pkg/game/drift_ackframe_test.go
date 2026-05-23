package game

import "testing"

func TestDriftAckFrameFields(t *testing.T) {
	assertActionFields(t, map[string][]string{
		"dock":          {"command", "pending"},
		"jump":          {"command", "pending"},
		"mine":          {"command", "pending"},
		"self_destruct": {"command", "pending", "message"},
	})
}
