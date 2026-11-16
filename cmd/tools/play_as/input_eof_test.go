package main

import (
	"errors"
	"fmt"
	"io"
	"testing"
)

// Piped play_as sessions spun on "Error reading input: EOF" after the last
// command until their timeout (an 8.9 GB log, 11-16): end of input must end
// the session.
func TestInputEnded(t *testing.T) {
	if !inputEnded(io.EOF) || !inputEnded(fmt.Errorf("read: %w", io.EOF)) {
		t.Error("EOF must end the session")
	}
	if inputEnded(errors.New("some other read error")) || inputEnded(nil) {
		t.Error("only EOF ends the session")
	}
}
