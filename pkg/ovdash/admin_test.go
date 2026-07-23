package ovdash

import (
	"net"
	"path/filepath"
	"testing"

	"github.com/rsned/spacemolt/pkg/overmind/control"
	"github.com/rsned/spacemolt/pkg/overmind/supervisor"
)

func TestAdminRemoveRoundTripAndOffline(t *testing.T) {
	dir := t.TempDir()
	def, ok := fleetByLabel("haul")
	if !ok {
		t.Fatal("haul fleet missing from registry")
	}
	sock := filepath.Join(dir, def.Socket)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close() //nolint:errcheck
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close() //nolint:errcheck
		dec := control.NewDecoder(conn)
		env, err := dec.Decode()
		if err != nil || env.Type != control.TypeAdminRemove {
			return
		}
		var req control.AdminRequest
		_ = env.Into(&req)
		reply, _ := control.NewEnvelope(control.TypeAdminAck, req.AgentID, control.AdminAck{AgentID: req.AgentID, Status: control.AckAccepted})
		_ = control.NewEncoder(conn).Encode(reply)
	}()

	res, err := AdminRemove(dir, "haul", "trader-9")
	if err != nil {
		t.Fatalf("AdminRemove: %v", err)
	}
	if res.Status != control.AckAccepted {
		t.Fatalf("status = %q, want accepted", res.Status)
	}
	ov, err := supervisor.LoadOverrides(filepath.Join(dir, "haul-overrides.json"))
	if err != nil || !ov.IsRemoved("trader-9") {
		t.Fatalf("overrides not recorded: %+v err=%v", ov, err)
	}

	// Socket down: override still recorded, degraded status.
	res2, err := AdminRemove(dir, "mb", "marketbot_001")
	if err != nil {
		t.Fatalf("offline AdminRemove: %v", err)
	}
	if res2.Status != "recorded_offline" {
		t.Fatalf("offline status = %q, want recorded_offline", res2.Status)
	}
	ov2, _ := supervisor.LoadOverrides(filepath.Join(dir, "mb-overrides.json"))
	if !ov2.IsRemoved("marketbot_001") {
		t.Fatal("offline path did not record override")
	}

	// Readd clears the override.
	if _, err := AdminReadd(dir, "mb", "marketbot_001"); err != nil {
		t.Fatalf("AdminReadd: %v", err)
	}
	ov3, _ := supervisor.LoadOverrides(filepath.Join(dir, "mb-overrides.json"))
	if ov3.IsRemoved("marketbot_001") {
		t.Fatal("readd did not clear override")
	}
}
