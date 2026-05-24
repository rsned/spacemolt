package main

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

// stubClientWithErrorFrame returns a fixed error frame for the "_last_error"
// raw-JSON slot, mimicking a deferred dock that fails on a later tick with
// {"code":"no_base","message":"No base at this location"}.
type stubClientWithErrorFrame struct {
	game.GameClient
}

func (stubClientWithErrorFrame) GetRawJSON(key string) []byte {
	if key == "_last_error" {
		return []byte(`{"code":"no_base","message":"No base at this location"}`)
	}
	return nil
}

// captureStdout runs fn with os.Stdout redirected to a pipe and returns
// everything written to stdout.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fn()
	_ = w.Close()
	os.Stdout = orig
	return <-done
}

// TestSimpleCommand_StyledErrorSkipsSuccessFormatter is a regression test for
// a deferred dock that fails after its "pending" ack: in styled mode the error
// path must NOT route the error frame through the command-specific success
// formatter (formatDock), which would unmarshal {"code","message"} into an
// empty success struct and print blank fields like `Docked at ""`. The styled
// "❌ Error: ..." line is printed by the REPL dispatcher, not here.
func TestSimpleCommand_StyledErrorSkipsSuccessFormatter(t *testing.T) {
	client := stubClientWithErrorFrame{}
	fn := func(context.Context) error { return errors.New("No base at this location") }

	out := captureStdout(t, func() {
		_ = simpleCommand(client, fn, context.Background(), 0, "dock", formatStyled)
	})

	if strings.Contains(out, "Docked at") {
		t.Errorf("styled error path ran the dock success formatter; got output:\n%s", out)
	}
}

// TestSimpleCommand_RawErrorShowsErrorFrame confirms the error frame is still
// surfaced for debugging in non-styled (raw/JSON) modes.
func TestSimpleCommand_RawErrorShowsErrorFrame(t *testing.T) {
	client := stubClientWithErrorFrame{}
	fn := func(context.Context) error { return errors.New("No base at this location") }

	out := captureStdout(t, func() {
		_ = simpleCommand(client, fn, context.Background(), 0, "dock", formatRaw)
	})

	if !strings.Contains(out, "no_base") {
		t.Errorf("raw error path should surface the error frame; got output:\n%s", out)
	}
}
