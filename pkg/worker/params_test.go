package worker

import (
	"slices"
	"testing"
)

func TestSubstituteParams(t *testing.T) {
	lines := []string{
		"autopilot $TARGET_SYSTEM$",
		"loop -f $COUNT$ mine",
		"travel $ASTEROID_BELT$", // live token — must be left untouched
	}
	got := SubstituteParams(lines, map[string]string{"TARGET_SYSTEM": "bunda", "COUNT": "20"})
	want := []string{
		"autopilot bunda",
		"loop -f 20 mine",
		"travel $ASTEROID_BELT$",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestSubstituteParamsTokenInQuotedArg(t *testing.T) {
	got := SubstituteParams([]string{`chat "heading to $TARGET_SYSTEM$ now"`},
		map[string]string{"TARGET_SYSTEM": "sol"})
	want := []string{`chat "heading to sol now"`}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestSubstituteParamsEmptyIsNoOp(t *testing.T) {
	in := []string{"autopilot $TARGET_SYSTEM$"}
	got := SubstituteParams(in, nil)
	if !slices.Equal(got, in) {
		t.Fatalf("got %v, want unchanged %v", got, in)
	}
}
