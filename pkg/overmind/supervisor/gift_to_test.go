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
