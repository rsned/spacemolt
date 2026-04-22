package game

import (
	"errors"
	"testing"
)

func TestMaybeGoalReached_MatchesTable(t *testing.T) {
	se := &ServerError{Code: "no_cargo_space", Message: "Cargo hold is full"}
	got := maybeGoalReached("mine", se)
	var goal *GoalReachedError
	if !errors.As(got, &goal) {
		t.Fatalf("expected *GoalReachedError, got %T (%v)", got, got)
	}
	if goal.Command != "mine" || goal.Code != "no_cargo_space" || goal.Message != "Cargo hold is full" {
		t.Errorf("unexpected goal: %+v", goal)
	}
}

func TestMaybeGoalReached_PassesThroughUnmatched(t *testing.T) {
	// Code not in table for this command.
	se := &ServerError{Code: "no_fuel", Message: "Insufficient fuel"}
	got := maybeGoalReached("mine", se)
	var goal *GoalReachedError
	if errors.As(got, &goal) {
		t.Fatalf("did not expect *GoalReachedError, got %+v", goal)
	}
	if got != se {
		t.Errorf("expected original error to be returned, got %v", got)
	}
}

func TestMaybeGoalReached_CommandNotInTable(t *testing.T) {
	se := &ServerError{Code: "no_cargo_space", Message: "Cargo hold is full"}
	got := maybeGoalReached("buy", se) // buy has no goal codes
	var goal *GoalReachedError
	if errors.As(got, &goal) {
		t.Fatalf("buy should not goal-reach on no_cargo_space, got %+v", goal)
	}
}

func TestMaybeGoalReached_NilError(t *testing.T) {
	if got := maybeGoalReached("mine", nil); got != nil {
		t.Errorf("nil input should return nil, got %v", got)
	}
}

func TestMaybeGoalReached_NonServerError(t *testing.T) {
	plain := errors.New("some transport failure")
	if got := maybeGoalReached("mine", plain); got != plain {
		t.Errorf("non-*ServerError should pass through, got %v", got)
	}
}

func TestServerError_Error(t *testing.T) {
	se := &ServerError{Code: "no_fuel", Message: "Insufficient fuel"}
	if se.Error() != "Insufficient fuel" {
		t.Errorf("Error() = %q, want %q", se.Error(), "Insufficient fuel")
	}
	// Fallback when message is empty: show the code.
	se2 := &ServerError{Code: "x"}
	if se2.Error() != "error: x" {
		t.Errorf("Error() = %q, want %q", se2.Error(), "error: x")
	}
}

func TestGoalReachedError_Error(t *testing.T) {
	g := &GoalReachedError{Command: "mine", Code: "no_cargo_space", Message: "Cargo hold is full"}
	want := "goal reached: no_cargo_space (Cargo hold is full)"
	if g.Error() != want {
		t.Errorf("Error() = %q, want %q", g.Error(), want)
	}
}

func TestServerError_RoundTripsThroughErrorsAs(t *testing.T) {
	// Producers: callers return *ServerError from waitForActionResponse.
	// Consumers: unwrap via errors.As to read code/message.
	var produced error = &ServerError{Code: "no_fuel", Message: "Insufficient fuel"}

	var se *ServerError
	if !errors.As(produced, &se) {
		t.Fatalf("expected errors.As to unwrap *ServerError from %T", produced)
	}
	if se.Code != "no_fuel" || se.Message != "Insufficient fuel" {
		t.Errorf("unexpected fields: %+v", se)
	}
}

func TestMaybeGoalReached_WrapsMineCargoFull(t *testing.T) {
	// Mirrors the exact error shape waitForActionResponse produces
	// after Task 2: *ServerError for any non-benign code.
	raw := error(&ServerError{Code: "no_cargo_space", Message: "Cargo hold is full"})

	out := maybeGoalReached("mine", raw)

	var goal *GoalReachedError
	if !errors.As(out, &goal) {
		t.Fatalf("mine + no_cargo_space should become *GoalReachedError, got %T (%v)", out, out)
	}
	if goal.Command != "mine" {
		t.Errorf("goal.Command = %q, want %q", goal.Command, "mine")
	}
	if goal.Code != "no_cargo_space" {
		t.Errorf("goal.Code = %q, want %q", goal.Code, "no_cargo_space")
	}
}
