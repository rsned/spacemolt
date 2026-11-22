package supervisor

import (
	"context"
	"testing"
)

func TestWorkerSpecGiftToForwardsFlag(t *testing.T) {
	spawn := DefaultSpawn("true", "", "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd, err := spawn(ctx, WorkerSpec{AgentID: "hunter-1", Role: "hunt", GiftTo: "hauler-3"}, "/tmp/does-not-matter.sock")
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	_ = cmd.Wait()
	if !hasArgPair(cmd.Args, "--gift-to", "hauler-3") {
		t.Errorf("args %v missing --gift-to hauler-3", cmd.Args)
	}
	cmd, err = spawn(ctx, WorkerSpec{AgentID: "plain"}, "/tmp/does-not-matter.sock")
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	_ = cmd.Wait()
	if hasArg(cmd.Args, "--gift-to") {
		t.Errorf("args %v must not carry --gift-to", cmd.Args)
	}
}

func TestSupervisorForwardsHuntFlags(t *testing.T) {
	spawn := DefaultSpawn("true", "", "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd, err := spawn(ctx, WorkerSpec{AgentID: "h", Role: "hunt", HuntAmmoCap: 2500, HuntExplore: "return=0.05"}, "/tmp/does-not-matter.sock")
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	_ = cmd.Wait()
	if !hasArgPair(cmd.Args, "--hunt-ammo-cap", "2500") || !hasArgPair(cmd.Args, "--hunt-explore", "return=0.05") {
		t.Errorf("args %v missing hunt flags", cmd.Args)
	}
	cmd, err = spawn(ctx, WorkerSpec{AgentID: "plain"}, "/tmp/does-not-matter.sock")
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	_ = cmd.Wait()
	if hasArg(cmd.Args, "--hunt-ammo-cap") || hasArg(cmd.Args, "--hunt-explore") {
		t.Errorf("args %v must carry neither hunt flag", cmd.Args)
	}
}
