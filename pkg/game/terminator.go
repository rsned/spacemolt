package game

import (
	"github.com/rsned/spacemolt/internal/protocol"
)

// Terminator reports whether a response resolves a mutation. It runs only
// against responses that have already passed the mutation's Classifier.
// done=true means the mutation is finished; err non-nil means it failed.
//
// Implementations MUST be pure: execMutation evaluates the terminator twice
// against the same terminal response (once inside the router's dispatch and
// again on receive to surface the err the router discards). Side effects
// or non-deterministic logic will produce inconsistent outcomes.
type Terminator func(resp protocol.Response) (done bool, err error)

// terminateOnAction is the default terminator for mutations whose terminal
// server message is a TypeActionResult. TypeActionError / TypeError also
// terminate, with a server error extracted from the payload. An "ok" with
// pending:true is intermediate and does NOT terminate.
func terminateOnAction(resp protocol.Response) (bool, error) {
	switch resp.Type {
	case protocol.TypeActionResult:
		return true, nil
	case protocol.TypeActionError, protocol.TypeError:
		return true, serverErrorFromPayload(resp.Payload)
	}
	return false, nil
}

// terminateOnActionOrOK extends terminateOnAction to also treat a synchronous,
// non-pending TypeOK as terminal. Some mutations reply with a plain OK after
// the pending ack instead of a queued action_result — e.g. battle subactions
// (retreat/stance/target). A pending:true OK remains intermediate so a
// multi-tick action still waits for its real completion frame.
func terminateOnActionOrOK(resp protocol.Response) (bool, error) {
	switch resp.Type {
	case protocol.TypeActionResult:
		return true, nil
	case protocol.TypeActionError, protocol.TypeError:
		return true, serverErrorFromPayload(resp.Payload)
	case protocol.TypeOK:
		if pending, _ := resp.Payload["pending"].(bool); pending {
			return false, nil
		}
		return true, nil
	}
	return false, nil
}

// terminateOnTypes builds a Terminator that returns done=true on any of the
// named response types. ActionError/Error in the list terminate with an
// error; others (e.g. POIArrival, Docked) terminate successfully.
func terminateOnTypes(types ...string) Terminator {
	errTypes := map[string]struct{}{
		protocol.TypeActionError: {},
		protocol.TypeError:       {},
	}
	set := make(map[string]struct{}, len(types))
	for _, t := range types {
		set[t] = struct{}{}
	}
	return func(resp protocol.Response) (bool, error) {
		if _, ok := set[resp.Type]; !ok {
			return false, nil
		}
		if _, isErr := errTypes[resp.Type]; isErr {
			return true, serverErrorFromPayload(resp.Payload)
		}
		return true, nil
	}
}

// serverErrorFromPayload builds a *ServerError from a server error payload,
// preserving the structured Code field so callers can classify via
// errors.As — required for the GoalReached sentinel logic in
// server_errors.go (e.g. mine→cargo_full, refuel→tank_full).
func serverErrorFromPayload(p map[string]any) error {
	code, _ := p["code"].(string)
	msg, _ := p["message"].(string)
	return &ServerError{Code: code, Message: msg}
}
