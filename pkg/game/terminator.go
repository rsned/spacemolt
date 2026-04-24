package game

import (
	"fmt"

	"github.com/rsned/spacemolt/internal/protocol"
)

// Terminator reports whether a response resolves a mutation. It runs only
// against responses that have already passed the mutation's Classifier.
// done=true means the mutation is finished; err non-nil means it failed.
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

// serverErrorFromPayload builds a descriptive error from a server error
// payload. Prefers the "message" field, falls back to a generic message.
func serverErrorFromPayload(p map[string]any) error {
	if msg, ok := p["message"].(string); ok && msg != "" {
		return fmt.Errorf("server error: %s", msg)
	}
	return fmt.Errorf("server error (no message)")
}
