package supervisor

import (
	"context"
	"testing"
)

func TestWorkerSpecFactionPullForwardsFlag(t *testing.T) {
	spawn := DefaultSpawn("true", "", "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd, err := spawn(ctx, WorkerSpec{AgentID: "cr-1", Role: "crafter",
		FactionPull: []string{"pirate_moonshine", "stolen_art"}}, "/tmp/does-not-matter.sock")
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	_ = cmd.Wait()
	if !hasArgPair(cmd.Args, "--faction-pull", "pirate_moonshine,stolen_art") {
		t.Errorf("args %v missing --faction-pull pirate_moonshine,stolen_art", cmd.Args)
	}
	cmd, err = spawn(ctx, WorkerSpec{AgentID: "plain"}, "/tmp/does-not-matter.sock")
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	_ = cmd.Wait()
	if hasArg(cmd.Args, "--faction-pull") {
		t.Errorf("args %v must not carry --faction-pull", cmd.Args)
	}
}
