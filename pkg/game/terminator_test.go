package game

import (
	"testing"

	"github.com/rsned/spacemolt/internal/protocol"
)

// TestTerminateOnTypesMatch verifies that terminateOnTypes returns true for
// any response whose Type field is one of the listed types.
func TestTerminateOnTypesMatch(t *testing.T) {
	term := terminateOnTypes(protocol.TypeOK, protocol.TypeActionError)

	if !term(protocol.Response{Type: protocol.TypeOK}) {
		t.Error("expected TypeOK to terminate")
	}
	if !term(protocol.Response{Type: protocol.TypeActionError}) {
		t.Error("expected TypeActionError to terminate")
	}
}

// TestTerminateOnTypesMiss verifies that terminateOnTypes returns false for
// a response whose Type is not in the list.
func TestTerminateOnTypesMiss(t *testing.T) {
	term := terminateOnTypes(protocol.TypeOK, protocol.TypeActionError)

	if term(protocol.Response{Type: protocol.TypeMiningYield}) {
		t.Error("expected TypeMiningYield not to terminate")
	}
	if term(protocol.Response{Type: protocol.TypePOIArrival}) {
		t.Error("expected TypePOIArrival not to terminate")
	}
	if term(protocol.Response{}) {
		t.Error("expected zero response not to terminate")
	}
}

// TestTerminateOnTypesSingle verifies that a single-type terminator works.
func TestTerminateOnTypesSingle(t *testing.T) {
	term := terminateOnTypes(protocol.TypePOIArrival)

	if !term(protocol.Response{Type: protocol.TypePOIArrival}) {
		t.Error("expected TypePOIArrival to terminate")
	}
	if term(protocol.Response{Type: protocol.TypeOK}) {
		t.Error("expected TypeOK not to terminate")
	}
}

// TestTerminateOnTypesEmpty verifies that an empty terminator never terminates.
func TestTerminateOnTypesEmpty(t *testing.T) {
	term := terminateOnTypes()

	if term(protocol.Response{Type: protocol.TypeOK}) {
		t.Error("expected empty terminateOnTypes never to match")
	}
	if term(protocol.Response{}) {
		t.Error("expected empty terminateOnTypes never to match zero response")
	}
}

// TestTerminateNever verifies that terminateNever always returns false.
func TestTerminateNever(t *testing.T) {
	if terminateNever(protocol.Response{Type: protocol.TypeOK}) {
		t.Error("expected terminateNever to return false for TypeOK")
	}
	if terminateNever(protocol.Response{Type: protocol.TypePOIArrival}) {
		t.Error("expected terminateNever to return false for TypePOIArrival")
	}
	if terminateNever(protocol.Response{}) {
		t.Error("expected terminateNever to return false for zero response")
	}
}

// TestTerminateAlways verifies that terminateAlways always returns true.
func TestTerminateAlways(t *testing.T) {
	if !terminateAlways(protocol.Response{Type: protocol.TypeOK}) {
		t.Error("expected terminateAlways to return true for TypeOK")
	}
	if !terminateAlways(protocol.Response{}) {
		t.Error("expected terminateAlways to return true for zero response")
	}
}
