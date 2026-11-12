package worker

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

// An idle assist below assistCreditFloor asks for credits; one at or above
// the floor does not.
func TestAssistKeepFloor(t *testing.T) {
	for _, tc := range []struct {
		credits float64
		want    int
	}{{3, 1}, {assistCreditFloor - 1, 1}, {assistCreditFloor, 0}, {120000, 0}} {
		c := &requestingFake{fakeClient: &fakeClient{state: &game.State{Credits: tc.credits}}}
		assistKeepFloor(t.Context(), AssistDeps{Client: c})
		if len(c.asked) != tc.want {
			t.Errorf("credits %.0f: asked %d time(s), want %d", tc.credits, len(c.asked), tc.want)
		}
	}
}
