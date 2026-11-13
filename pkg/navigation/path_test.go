package navigation

import (
	"slices"
	"testing"
)

func diamond() JumpGraph {
	// a - b - d   and   a - c - e - d : b is the short way, c/e the detour.
	return JumpGraph{
		"a": {"b", "c"}, "b": {"a", "d"}, "c": {"a", "e"},
		"e": {"c", "d"}, "d": {"b", "e"},
	}
}

func TestShortestPathPrefersFewestJumps(t *testing.T) {
	got := ShortestPath(diamond(), "a", "d", nil)
	if !slices.Equal(got, []string{"a", "b", "d"}) {
		t.Errorf("path = %v, want [a b d]", got)
	}
}

func TestShortestPathRoutesAroundExcluded(t *testing.T) {
	got := ShortestPath(diamond(), "a", "d", map[string]bool{"b": true})
	if !slices.Equal(got, []string{"a", "c", "e", "d"}) {
		t.Errorf("path = %v, want [a c e d]", got)
	}
}

func TestShortestPathNilWhenExclusionCutsTheGraph(t *testing.T) {
	if got := ShortestPath(diamond(), "a", "d", map[string]bool{"b": true, "e": true}); got != nil {
		t.Errorf("path = %v, want nil", got)
	}
}

// The source itself may be on the exclude list (a ship respawned inside an
// avoided system must still be able to leave it).
func TestShortestPathLeavesAnExcludedSource(t *testing.T) {
	got := ShortestPath(diamond(), "b", "d", map[string]bool{"b": true})
	if !slices.Equal(got, []string{"b", "d"}) {
		t.Errorf("path = %v, want [b d]", got)
	}
}
