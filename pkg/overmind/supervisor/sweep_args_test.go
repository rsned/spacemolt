package supervisor

import (
	"context"
	"testing"
)

// The pathfinder_sweep role reads its ring, start angle and launch-cost
// estimate from the fleet yaml; a worker without them gets no sweep flags.
func TestSweepFlagsForwarded(t *testing.T) {
	spawn := DefaultSpawn("true", "", "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd, err := spawn(ctx, WorkerSpec{AgentID: "explorer-3", SweepRing: 7550, SweepStartDeg: 148, SweepLaunchCost: 23, SweepSpanDeg: 100, SweepStage: "node_beta,gsc_0049,ash_pearl"}, "/tmp/does-not-matter.sock")
	if err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	for _, p := range [][2]string{{"--sweep-ring", "7550"}, {"--sweep-start-deg", "148"}, {"--sweep-launch-cost", "23"}, {"--sweep-span-deg", "100"}, {"--sweep-stage", "node_beta,gsc_0049,ash_pearl"}} {
		if !hasArgPair(cmd.Args, p[0], p[1]) {
			t.Errorf("args %v missing %s %s", cmd.Args, p[0], p[1])
		}
	}

	cmd, err = spawn(ctx, WorkerSpec{AgentID: "plain"}, "/tmp/does-not-matter.sock")
	if err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	for _, f := range []string{"--sweep-ring", "--sweep-start-deg", "--sweep-launch-cost", "--sweep-span-deg", "--sweep-stage"} {
		if hasArg(cmd.Args, f) {
			t.Errorf("args %v must not contain %s", cmd.Args, f)
		}
	}
}
