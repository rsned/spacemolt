// Package game — structured server-error types and goal-reached
// classification. See docs/superpowers/specs/2026-04-22-loop-goal-reached-design.md.
package game

import (
	"errors"
	"fmt"
)

// ServerError is a structured error returned by waitForActionResponse
// when the server replies with an `error` or `action_error` message.
// Holding Code separately (rather than only formatting into a string)
// lets callers classify without regexing the message text.
type ServerError struct {
	Code    string
	Message string
}

// Error returns the server's human-readable message, falling back to
// the code when no message was supplied.
func (e *ServerError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return "error: " + e.Code
}

// GoalReachedError signals that a command's natural goal has been
// achieved (e.g. cargo hold is full after mine, fuel tank is full
// after refuel). It is distinct from:
//   - nil (success, may be repeated)
//   - a plain error (failure)
//
// The play_as loop executor treats *GoalReachedError as "exit the
// innermost enclosing loop cleanly with overall success". Standalone
// callers treat it as success with a friendly message.
type GoalReachedError struct {
	Command string // the command that reached its goal, e.g. "mine"
	Code    string // server error code, e.g. "no_cargo_space"
	Message string // server's human-readable message
}

func (e *GoalReachedError) Error() string {
	return fmt.Sprintf("goal reached: %s (%s)", e.Code, e.Message)
}

// goalReachedCodes maps command name -> set of server error codes that
// mean "this command's goal is already achieved". Initial set covers
// the most-looped workflows (mining fills cargo, refuel tops the tank,
// repair brings hull to max, deposit_all empties cargo). Extend as
// more commands gain loop-friendly goal semantics.
var goalReachedCodes = map[string]map[string]struct{}{
	"mine":        {"no_cargo_space": {}},
	"refuel":      {"tank_full": {}},
	"repair":      {"no_damage": {}},
	"deposit_all": {"empty_cargo": {}},
}

// maybeGoalReached inspects err and, when command+code matches
// goalReachedCodes, wraps it as *GoalReachedError. Non-matching errors
// and non-*ServerError values pass through unchanged. A nil input
// returns nil.
func maybeGoalReached(command string, err error) error {
	if err == nil {
		return nil
	}
	var se *ServerError
	if !errors.As(err, &se) {
		return err
	}
	codes, ok := goalReachedCodes[command]
	if !ok {
		return err
	}
	if _, match := codes[se.Code]; !match {
		return err
	}
	return &GoalReachedError{
		Command: command,
		Code:    se.Code,
		Message: se.Message,
	}
}
