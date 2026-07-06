package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/overmind/control"
	"github.com/rsned/spacemolt/pkg/overmind/supervisor"
	"github.com/rsned/spacemolt/pkg/rescue"
)

// rescueTestSpawn returns a SpawnFunc that launches a long-lived `sleep`,
// counting invocations into n — the same fake-spawn pattern as
// pkg/overmind/supervisor's aliveSpawn, reimplemented here since it is
// unexported in that package.
func rescueTestSpawn(n *atomic.Int32) supervisor.SpawnFunc {
	return func(ctx context.Context, spec supervisor.WorkerSpec, socket string) (*exec.Cmd, error) {
		n.Add(1)
		cmd := exec.CommandContext(ctx, "sleep", "60")
		return cmd, cmd.Start()
	}
}

// writeQueueRecords writes recs verbatim (bypassing the flock-guarded Queue
// API) so test fixtures can set up arbitrary lifecycle states directly.
func writeQueueRecords(t *testing.T, path string, recs []rescue.Record) {
	t.Helper()
	data, err := json.Marshal(recs)
	if err != nil {
		t.Fatalf("marshal fixture records: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write queue fixture: %v", err)
	}
}

// historyLines reads path and returns its non-empty lines (missing file -> nil).
func historyLines(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read history: %v", err)
	}
	var lines []string
	for _, l := range strings.Split(string(raw), "\n") {
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

func TestRestoreQuarantine(t *testing.T) {
	dir := t.TempDir()
	queuePath := filepath.Join(dir, "q.json")
	queue := rescue.NewQueue(queuePath)
	histPath := filepath.Join(dir, "history.jsonl")

	writeQueueRecords(t, queuePath, []rescue.Record{
		{AgentID: "dead-pending", Fleet: "haul", Status: rescue.StatusPending, Reason: "fuel-dead"},
		{AgentID: "dead-claimed", Fleet: "haul", Status: rescue.StatusClaimed, Reason: "fuel-dead", ClaimedBy: "assist-sol"},
		{AgentID: "dead-failed", Fleet: "haul", Status: rescue.StatusFailed, Reason: "fuel-dead", Error: "travel: no route"},
		{AgentID: "dead-done", Fleet: "haul", Status: rescue.StatusDone, Reason: "fuel-dead", ClaimedBy: "assist-sol"},
		{AgentID: "other-fleet", Fleet: "shuttle", Status: rescue.StatusPending, Reason: "fuel-dead"},
	})

	fleet := supervisor.NewFleet()
	logger := log.New(io.Discard, "", 0)
	restoreQuarantine(logger, fleet, queue, histPath, "haul")

	for _, id := range []string{"dead-pending", "dead-claimed", "dead-failed"} {
		if !fleet.IsQuarantined(id) {
			t.Errorf("%s: want quarantined (open record), got not quarantined", id)
		}
	}
	if fleet.IsQuarantined("dead-done") {
		t.Error("dead-done: done record must archive, not quarantine")
	}
	if fleet.IsQuarantined("other-fleet") {
		t.Error("other-fleet: record belongs to a different fleet, must be untouched")
	}

	recs, err := queue.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	byAgent := make(map[string]rescue.Record, len(recs))
	for _, r := range recs {
		byAgent[r.AgentID] = r
	}
	if _, ok := byAgent["dead-done"]; ok {
		t.Error("dead-done record should have been removed from the queue on archive")
	}
	for _, id := range []string{"dead-pending", "dead-claimed", "dead-failed", "other-fleet"} {
		if _, ok := byAgent[id]; !ok {
			t.Errorf("%s: record should remain in the queue (only done archives)", id)
		}
	}

	lines := historyLines(t, histPath)
	if len(lines) != 1 {
		t.Fatalf("want exactly 1 archived history line, got %d: %v", len(lines), lines)
	}
	var archived rescue.Record
	if err := json.Unmarshal([]byte(lines[0]), &archived); err != nil {
		t.Fatalf("unmarshal archived history line: %v", err)
	}
	if archived.AgentID != "dead-done" {
		t.Fatalf("archived record = %+v, want agent_id dead-done", archived)
	}
}

func TestPollRescuesArchivesDoneAndReleases(t *testing.T) {
	dir := t.TempDir()
	queuePath := filepath.Join(dir, "q.json")
	queue := rescue.NewQueue(queuePath)
	histPath := filepath.Join(dir, "history.jsonl")
	writeQueueRecords(t, queuePath, []rescue.Record{
		{AgentID: "resc1", Fleet: "haul", Status: rescue.StatusDone, RescueFuel: 15, ClaimedBy: "assist-sol"},
	})

	fleet := supervisor.NewFleet()
	now := time.Now()
	fleet.ApplyHello(control.Hello{AgentID: "resc1", Role: "hauler"}, 1, now)
	fleet.ApplyStatus("resc1", control.Status{}, now)
	fleet.Quarantine("resc1", "fuel-dead: stalled")

	var spawned atomic.Int32
	specs := []supervisor.WorkerSpec{{AgentID: "resc1"}}
	sup := supervisor.NewSupervisor(nil, fleet, specs, rescueTestSpawn(&spawned), log.New(io.Discard, "", 0))
	logger := log.New(io.Discard, "", 0)

	pollRescues(logger, sup, queue, histPath, "haul", t.TempDir(), 0, fleet.Snapshot())

	recs, err := queue.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(recs) != 0 {
		t.Fatalf("done record should be archived out of the queue, got %+v", recs)
	}
	if lines := historyLines(t, histPath); len(lines) != 1 {
		t.Fatalf("want 1 archived history line, got %d", len(lines))
	}

	// Release is deferred to the next reap tick (drainReleases), so the flag
	// is still set immediately after pollRescues returns.
	if !fleet.IsQuarantined("resc1") {
		t.Fatal("quarantine flag must not clear before the next reap tick")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sup.Tick(ctx)

	if fleet.IsQuarantined("resc1") {
		t.Fatal("resc1 should be released after the next reap tick")
	}
	if spawned.Load() != 1 {
		t.Fatalf("released worker should relaunch, got %d spawns", spawned.Load())
	}
}

func TestPollRescuesHoldsOnOpenRecord(t *testing.T) {
	dir := t.TempDir()
	queuePath := filepath.Join(dir, "q.json")
	queue := rescue.NewQueue(queuePath)
	histPath := filepath.Join(dir, "history.jsonl")
	writeQueueRecords(t, queuePath, []rescue.Record{
		{AgentID: "resc2", Fleet: "haul", Status: rescue.StatusClaimed, ClaimedBy: "assist-sol"},
	})

	fleet := supervisor.NewFleet()
	now := time.Now()
	fleet.ApplyHello(control.Hello{AgentID: "resc2", Role: "hauler"}, 1, now)
	fleet.ApplyStatus("resc2", control.Status{}, now)
	fleet.Quarantine("resc2", "fuel-dead: stalled")

	var spawned atomic.Int32
	specs := []supervisor.WorkerSpec{{AgentID: "resc2"}}
	sup := supervisor.NewSupervisor(nil, fleet, specs, rescueTestSpawn(&spawned), log.New(io.Discard, "", 0))
	logger := log.New(io.Discard, "", 0)

	pollRescues(logger, sup, queue, histPath, "haul", t.TempDir(), 0, fleet.Snapshot())

	recs, err := queue.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(recs) != 1 || recs[0].AgentID != "resc2" || recs[0].Status != rescue.StatusClaimed {
		t.Fatalf("open record must be left untouched, got %+v", recs)
	}
	if lines := historyLines(t, histPath); len(lines) != 0 {
		t.Fatalf("nothing should be archived, got %d lines", len(lines))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sup.Tick(ctx)

	if !fleet.IsQuarantined("resc2") {
		t.Fatal("resc2 must stay quarantined while its record is still open")
	}
	if spawned.Load() != 0 {
		t.Fatalf("still-quarantined worker must not relaunch, got %d spawns", spawned.Load())
	}
}

func TestPollRescuesReleasesWithNoRecord(t *testing.T) {
	dir := t.TempDir()
	// No record at all for resc3 (queue file doesn't even exist): the
	// manual-resolution path — operator deleted the record by hand.
	queue := rescue.NewQueue(filepath.Join(dir, "q.json"))
	histPath := filepath.Join(dir, "history.jsonl")

	fleet := supervisor.NewFleet()
	now := time.Now()
	fleet.ApplyHello(control.Hello{AgentID: "resc3", Role: "hauler"}, 1, now)
	fleet.ApplyStatus("resc3", control.Status{}, now)
	fleet.Quarantine("resc3", "fuel-dead: stalled")

	var spawned atomic.Int32
	specs := []supervisor.WorkerSpec{{AgentID: "resc3"}}
	sup := supervisor.NewSupervisor(nil, fleet, specs, rescueTestSpawn(&spawned), log.New(io.Discard, "", 0))
	logger := log.New(io.Discard, "", 0)

	pollRescues(logger, sup, queue, histPath, "haul", t.TempDir(), 0, fleet.Snapshot())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sup.Tick(ctx)

	if fleet.IsQuarantined("resc3") {
		t.Fatal("resc3 should be released when no record exists (manual resolution)")
	}
	if spawned.Load() != 1 {
		t.Fatalf("released worker should relaunch, got %d spawns", spawned.Load())
	}
}

func TestPollRescuesFastPathSkipsCorruptQueue(t *testing.T) {
	dir := t.TempDir()
	queuePath := filepath.Join(dir, "q.json")
	histPath := filepath.Join(dir, "history.jsonl")
	// Deliberately corrupt: if pollRescues took the slow path it would call
	// queue.List(), fail to parse this, and log a "queue read" error.
	corrupt := []byte("{not valid json")
	if err := os.WriteFile(queuePath, corrupt, 0o644); err != nil {
		t.Fatalf("write corrupt queue: %v", err)
	}
	queue := rescue.NewQueue(queuePath)

	fleet := supervisor.NewFleet()
	now := time.Now()
	fleet.ApplyHello(control.Hello{AgentID: "healthy1", Role: "hauler"}, 1, now)
	fleet.ApplyStatus("healthy1", control.Status{}, now) // not quarantined

	var spawned atomic.Int32
	specs := []supervisor.WorkerSpec{{AgentID: "healthy1"}}
	sup := supervisor.NewSupervisor(nil, fleet, specs, rescueTestSpawn(&spawned), log.New(io.Discard, "", 0))

	var logBuf bytes.Buffer
	logger := log.New(&logBuf, "", 0)

	pollRescues(logger, sup, queue, histPath, "haul", t.TempDir(), 0, fleet.Snapshot())

	if strings.Contains(logBuf.String(), "queue read") {
		t.Fatalf("fast path must not read the queue when nothing is quarantined; log = %q", logBuf.String())
	}
	// The corrupt file must be left exactly as written — proof the queue was
	// never touched.
	after, err := os.ReadFile(queuePath)
	if err != nil {
		t.Fatalf("read queue file after poll: %v", err)
	}
	if !bytes.Equal(after, corrupt) {
		t.Fatalf("corrupt queue file must be untouched, got %q", after)
	}
}

// writeTestCredentials writes the credentials.json ResolveUsername reads.
// game.LoadCredentials requires username, password, and empire to all be
// non-empty, so all three must be present even though only username matters
// here.
func writeTestCredentials(t *testing.T, agentsDir, agentID, username string) {
	t.Helper()
	d := filepath.Join(agentsDir, agentID)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "credentials.json"),
		[]byte(`{"username":"`+username+`","password":"x","empire":"nebula"}`), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestPollRescuesWritesDebtOnRealRescue: a done record that actually spent an
// assister's fuel records a fee debt naming the rescuer's in-game username.
func TestPollRescuesWritesDebtOnRealRescue(t *testing.T) {
	dir := t.TempDir()
	queuePath := filepath.Join(dir, "q.json")
	queue := rescue.NewQueue(queuePath)
	histPath := filepath.Join(dir, "history.jsonl")
	agentsDir := filepath.Join(dir, "agents")
	writeTestCredentials(t, agentsDir, "assist-haven", "shipside_assist_haven")
	writeQueueRecords(t, queuePath, []rescue.Record{
		{AgentID: "salvager-10", Fleet: "haul", Status: rescue.StatusDone, RescueFuel: 110, ClaimedBy: "assist-haven"},
	})

	fleet := supervisor.NewFleet()
	now := time.Now()
	fleet.ApplyHello(control.Hello{AgentID: "salvager-10", Role: "hauler"}, 1, now)
	fleet.ApplyStatus("salvager-10", control.Status{}, now)
	fleet.Quarantine("salvager-10", "fuel-dead: stalled")

	var spawned atomic.Int32
	specs := []supervisor.WorkerSpec{{AgentID: "salvager-10"}}
	sup := supervisor.NewSupervisor(nil, fleet, specs, rescueTestSpawn(&spawned), log.New(io.Discard, "", 0))
	logger := log.New(io.Discard, "", 0)

	pollRescues(logger, sup, queue, histPath, "haul", agentsDir, 1000, fleet.Snapshot())

	debts, err := rescue.LoadDebts(agentsDir, "salvager-10")
	if err != nil || len(debts) != 1 || debts[0].Recipient != "shipside_assist_haven" || debts[0].Credits != 1000 {
		t.Fatalf("debts = %+v (err %v), want one 1000cr debt to shipside_assist_haven", debts, err)
	}
}

// TestPollRescuesNoDebtForTowOrManual: a done record with no rescuer (server
// tow / operator flip) or zero fuel owes no fee.
func TestPollRescuesNoDebtForTowOrManual(t *testing.T) {
	dir := t.TempDir()
	queuePath := filepath.Join(dir, "q.json")
	queue := rescue.NewQueue(queuePath)
	histPath := filepath.Join(dir, "history.jsonl")
	agentsDir := filepath.Join(dir, "agents")
	writeQueueRecords(t, queuePath, []rescue.Record{
		{AgentID: "trader-1", Fleet: "haul", Status: rescue.StatusDone, ClaimedBy: "", RescueFuel: 0},
	})

	fleet := supervisor.NewFleet()
	now := time.Now()
	fleet.ApplyHello(control.Hello{AgentID: "trader-1", Role: "hauler"}, 1, now)
	fleet.ApplyStatus("trader-1", control.Status{}, now)
	fleet.Quarantine("trader-1", "fuel-dead: stalled")

	var spawned atomic.Int32
	specs := []supervisor.WorkerSpec{{AgentID: "trader-1"}}
	sup := supervisor.NewSupervisor(nil, fleet, specs, rescueTestSpawn(&spawned), log.New(io.Discard, "", 0))
	logger := log.New(io.Discard, "", 0)

	pollRescues(logger, sup, queue, histPath, "haul", agentsDir, 1000, fleet.Snapshot())

	if debts, _ := rescue.LoadDebts(agentsDir, "trader-1"); len(debts) != 0 {
		t.Fatalf("no-rescuer record must owe no fee, got %+v", debts)
	}
}
