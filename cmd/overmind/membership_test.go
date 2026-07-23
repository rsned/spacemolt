package main

import (
	"context"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/rsned/spacemolt/pkg/overmind/control"
	"github.com/rsned/spacemolt/pkg/overmind/supervisor"
)

func TestDiffSpecs(t *testing.T) {
	cur := []supervisor.WorkerSpec{
		{AgentID: "keep", Role: "missionrunner"},
		{AgentID: "drop", Role: "missionrunner"},
		{AgentID: "change", Role: "missionrunner", FreightMaxPackages: 3},
	}
	des := []supervisor.WorkerSpec{
		{AgentID: "keep", Role: "missionrunner"},
		{AgentID: "change", Role: "missionrunner", FreightMaxPackages: 7},
		{AgentID: "new", Role: "missionrunner"},
	}
	got := diffSpecs(cur, des)
	want := map[string]supervisor.MembershipOp{
		"drop":   supervisor.MembershipRemove,
		"change": supervisor.MembershipUpdate,
		"new":    supervisor.MembershipAdd,
	}
	if len(got) != len(want) {
		t.Fatalf("got %d requests (%+v), want %d", len(got), got, len(want))
	}
	for _, r := range got {
		if want[r.Spec.AgentID] != r.Op {
			t.Fatalf("agent %q: op %q, want %q", r.Spec.AgentID, r.Op, want[r.Spec.AgentID])
		}
		if r.Spec.AgentID == "change" && r.Spec.FreightMaxPackages != 7 {
			t.Fatalf("update carried stale spec: %+v", r.Spec)
		}
	}
}

func TestReloadKeepsRosterOnParseError(t *testing.T) {
	dir := t.TempDir()
	fleetPath := filepath.Join(dir, "fleet.yaml")
	overridesPath := filepath.Join(dir, "fleet-overrides.json")
	good := "workers:\n  - { agent_id: a1, role: missionrunner, station: \"\" }\n"
	if err := os.WriteFile(fleetPath, []byte(good), 0o644); err != nil {
		t.Fatal(err)
	}
	rs := &rosterState{}
	logger := log.New(io.Discard, "", 0)
	eff, ok := rs.reload(fleetPath, overridesPath, logger)
	if !ok || len(eff) != 1 {
		t.Fatalf("good reload: eff=%+v ok=%v", eff, ok)
	}
	if err := os.WriteFile(fleetPath, []byte("workers: ["), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := rs.reload(fleetPath, overridesPath, logger); ok {
		t.Fatal("parse error must return ok=false (keep current roster)")
	}
}

func TestAdminHook(t *testing.T) {
	dir := t.TempDir()
	overridesPath := filepath.Join(dir, "o.json")
	rs := &rosterState{yamlSpecs: []supervisor.WorkerSpec{{AgentID: "a1", Role: "missionrunner"}}}
	fleet := supervisor.NewFleet()
	sup := supervisor.NewSupervisor(nil, fleet, nil, func(ctx context.Context, spec supervisor.WorkerSpec, socket string) (*exec.Cmd, error) {
		return nil, nil
	}, log.New(io.Discard, "", 0))
	hook := makeAdminHook(rs, sup, overridesPath, log.New(io.Discard, "", 0))

	if ack := hook(control.TypeAdminRemove, "ghost"); ack.Status != control.AckUnknownAgent {
		t.Fatalf("remove ghost: %+v, want unknown_agent", ack)
	}
	if ack := hook(control.TypeAdminRemove, "a1"); ack.Status != control.AckAccepted {
		t.Fatalf("remove a1: %+v, want accepted", ack)
	}
	if ack := hook(control.TypeAdminReadd, "a1"); ack.Status != control.AckAccepted {
		t.Fatalf("readd a1: %+v, want accepted", ack)
	}
	if ack := hook(control.TypeAdminReadd, "ghost"); ack.Status != control.AckUnknownAgent {
		t.Fatalf("readd ghost: %+v, want unknown_agent (not in yaml)", ack)
	}
}
