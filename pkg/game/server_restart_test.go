package game

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
)

// The live 2026-11-22 payload (v0.733.0 deploy).
func restartWarning(payload map[string]any) protocol.Response {
	return protocol.Response{Type: protocol.TypeServerRestartWarning, Payload: payload}
}

func TestServerRestartWarningRecordsPending(t *testing.T) {
	var buf bytes.Buffer
	c := facilityEventClient(&buf)
	before := time.Now()
	c.handleResponse(restartWarning(map[string]any{
		"message":               "Server restarting in 60s for an update to v0.733.0. Expect a brief disconnect.",
		"seconds_until_restart": float64(60),
		"target_version":        "0.733.0",
	}))

	st := c.GetState()
	if st.ServerRestart == nil {
		t.Fatal("ServerRestart not recorded")
	}
	if st.ServerRestart.Target != "0.733.0" {
		t.Errorf("Target = %q, want 0.733.0", st.ServerRestart.Target)
	}
	if d := st.ServerRestart.At.Sub(before); d < 59*time.Second || d > 61*time.Second {
		t.Errorf("At is %v after the warning, want about 60s", d)
	}
	if !st.RestartPending(time.Now()) {
		t.Error("RestartPending = false right after the warning")
	}
	if got := buf.String(); !strings.Contains(got, "SERVER RESTART") || !strings.Contains(got, "v0.733.0") {
		t.Errorf("event log %q missing the restart line", got)
	}
	// Clone must not share the restart.
	st.ServerRestart.Target = "mutated"
	if c.GetState().ServerRestart.Target != "0.733.0" {
		t.Error("State.Clone shares ServerRestart")
	}
}

func TestServerRestartWarningWithoutSecondsDefaults60s(t *testing.T) {
	c := facilityEventClient(&bytes.Buffer{})
	before := time.Now()
	c.handleResponse(restartWarning(map[string]any{"message": "Server restarting soon"}))
	st := c.GetState()
	if st.ServerRestart == nil {
		t.Fatal("ServerRestart not recorded")
	}
	if d := st.ServerRestart.At.Sub(before); d < 59*time.Second || d > 61*time.Second {
		t.Errorf("At is %v after the warning, want the 60s default", d)
	}
}

func TestRestartPendingExpiresAfterCap(t *testing.T) {
	at := time.Now()
	s := &State{ServerRestart: &ServerRestart{At: at, Target: "x"}, LastRestartAt: at}
	if !s.RestartPending(at.Add(SleepServerRestartMaxWait - time.Second)) {
		t.Error("RestartPending = false inside the cap")
	}
	if s.RestartPending(at.Add(SleepServerRestartMaxWait + time.Second)) {
		t.Error("RestartPending = true past the cap: a restart that never happened would wedge workers")
	}
	if (&State{}).RestartPending(at) {
		t.Error("RestartPending = true with no warning")
	}
}

func TestClearServerRestartKeepsLastRestartAt(t *testing.T) {
	c := facilityEventClient(&bytes.Buffer{})
	c.handleResponse(restartWarning(map[string]any{"seconds_until_restart": float64(60), "target_version": "0.733.0"}))
	at := c.GetState().ServerRestart.At
	c.clearServerRestart()

	st := c.GetState()
	if st.ServerRestart != nil {
		t.Fatal("ServerRestart still set after the reconnect cleared it")
	}
	if !st.LastRestartAt.Equal(at) {
		t.Errorf("LastRestartAt = %v, want %v", st.LastRestartAt, at)
	}
	now := time.Now()
	if !st.RestartRecent(now) {
		t.Error("RestartRecent = false just after the restart")
	}
	if st.RestartRecent(at.Add(SleepServerRestartMaxWait + time.Second)) {
		t.Error("RestartRecent = true long after the restart")
	}
}
