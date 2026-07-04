package balances

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse time %q: %v", s, err)
	}
	return tm
}

func TestWriteAndReadStatusRoundTrip(t *testing.T) {
	dir := t.TempDir()
	sp := filepath.Join(dir, "fleet-status.json")
	r, err := NewRecorder(sp, filepath.Join(dir, "fleet-history.jsonl"))
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}
	live := []LiveRecord{
		{AgentID: "trader-1", Role: "hauler", System: "Sol", Credits: 1000, Seen: true},
		{AgentID: "trader-2", Role: "hauler", System: "Krynn", Credits: 2000, Seen: true},
	}
	if err := r.WriteStatus(live, mustTime(t, "2026-06-25T12:00:00Z")); err != nil {
		t.Fatalf("WriteStatus: %v", err)
	}
	sf, err := ReadStatus(sp)
	if err != nil {
		t.Fatalf("ReadStatus: %v", err)
	}
	if len(sf.Workers) != 2 || sf.Workers[0].AgentID != "trader-1" || sf.Workers[1].Credits != 2000 {
		t.Fatalf("status roundtrip mismatch: %+v", sf)
	}
	if sf.CapturedAt != "2026-06-25T12:00:00Z" {
		t.Fatalf("captured_at = %q", sf.CapturedAt)
	}
}

func TestReadMissingFilesAreEmpty(t *testing.T) {
	dir := t.TempDir()
	if recs, err := ReadHistory(filepath.Join(dir, "nope.jsonl")); err != nil || recs != nil {
		t.Fatalf("ReadHistory missing: recs=%v err=%v", recs, err)
	}
	if sf, err := ReadStatus(filepath.Join(dir, "nope.json")); err != nil || len(sf.Workers) != 0 {
		t.Fatalf("ReadStatus missing: %+v err=%v", sf, err)
	}
}

func TestDailySnapshotOncePerDayAndStartingBalance(t *testing.T) {
	dir := t.TempDir()
	hp := filepath.Join(dir, "fleet-history.jsonl")
	r, err := NewRecorder(filepath.Join(dir, "s.json"), hp)
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}
	live := []LiveRecord{{AgentID: "trader-1", Credits: 500, Seen: true}}

	// First call ever -> writes the starting balance.
	n, err := r.MaybeSnapshotDaily(live, mustTime(t, "2026-06-25T00:00:10Z"))
	if err != nil || n != 1 {
		t.Fatalf("first snapshot: n=%d err=%v", n, err)
	}
	// Same day, later tick -> already recorded, no-op (credit change ignored).
	live[0].Credits = 700
	n, err = r.MaybeSnapshotDaily(live, mustTime(t, "2026-06-25T23:59:00Z"))
	if err != nil || n != 0 {
		t.Fatalf("same-day snapshot should no-op: n=%d err=%v", n, err)
	}
	// Next UTC day -> writes again.
	n, err = r.MaybeSnapshotDaily(live, mustTime(t, "2026-06-26T00:00:20Z"))
	if err != nil || n != 1 {
		t.Fatalf("next-day snapshot: n=%d err=%v", n, err)
	}

	recs, err := ReadHistory(hp)
	if err != nil {
		t.Fatalf("ReadHistory: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("want 2 history rows, got %d: %+v", len(recs), recs)
	}
	if recs[0].Date != "2026-06-25" || recs[0].Credits != 500 {
		t.Fatalf("starting balance row wrong: %+v", recs[0])
	}
	if recs[1].Date != "2026-06-26" || recs[1].Credits != 700 {
		t.Fatalf("day-2 row wrong: %+v", recs[1])
	}
}

func TestRecorderRestartDoesNotDuplicateSameDay(t *testing.T) {
	dir := t.TempDir()
	hp := filepath.Join(dir, "fleet-history.jsonl")
	r1, _ := NewRecorder(filepath.Join(dir, "s.json"), hp)
	live := []LiveRecord{{AgentID: "trader-1", Credits: 500, Seen: true}}
	if n, _ := r1.MaybeSnapshotDaily(live, mustTime(t, "2026-06-25T00:01:00Z")); n != 1 {
		t.Fatalf("first snapshot should write 1, got %d", n)
	}
	// Simulate a restart: a fresh Recorder must read the existing day and skip.
	r2, err := NewRecorder(filepath.Join(dir, "s.json"), hp)
	if err != nil {
		t.Fatalf("NewRecorder restart: %v", err)
	}
	if n, _ := r2.MaybeSnapshotDaily(live, mustTime(t, "2026-06-25T06:00:00Z")); n != 0 {
		t.Fatalf("restart same-day must not duplicate the snapshot, wrote %d", n)
	}
	recs, _ := ReadHistory(hp)
	if len(recs) != 1 {
		t.Fatalf("want 1 row after restart, got %d", len(recs))
	}
}

func TestDailySnapshotSkipsUnseenAndDoesNotBurnDate(t *testing.T) {
	dir := t.TempDir()
	hp := filepath.Join(dir, "fleet-history.jsonl")
	r, _ := NewRecorder(filepath.Join(dir, "s.json"), hp)
	// All unseen on the first tick -> nothing recorded.
	unseen := []LiveRecord{{AgentID: "trader-1", Seen: false}}
	if n, _ := r.MaybeSnapshotDaily(unseen, mustTime(t, "2026-06-25T00:00:30Z")); n != 0 {
		t.Fatalf("unseen-only snapshot should not write, got %d", n)
	}
	// Heartbeat arrives later the same day -> now it records.
	seen := []LiveRecord{{AgentID: "trader-1", Credits: 300, Seen: true}}
	if n, _ := r.MaybeSnapshotDaily(seen, mustTime(t, "2026-06-25T00:05:00Z")); n != 1 {
		t.Fatalf("snapshot should write once a worker is seen, got %d", n)
	}
	recs, _ := ReadHistory(hp)
	if len(recs) != 1 || recs[0].Credits != 300 {
		t.Fatalf("want one seen row at 300, got %+v", recs)
	}
}

func TestStaggeredWorkersEachGetStartingRowSameDay(t *testing.T) {
	dir := t.TempDir()
	hp := filepath.Join(dir, "fleet-history.jsonl")
	r, _ := NewRecorder(filepath.Join(dir, "s.json"), hp)
	// Tick 1: only trader-1 has connected.
	t1 := []LiveRecord{{AgentID: "trader-1", Credits: 100, Seen: true}, {AgentID: "trader-2", Seen: false}}
	if n, _ := r.MaybeSnapshotDaily(t1, mustTime(t, "2026-06-25T00:00:10Z")); n != 1 {
		t.Fatalf("tick1 should record only trader-1, got %d", n)
	}
	// Tick 2 (same day): trader-2 now connected; trader-1 already has today's row.
	t2 := []LiveRecord{{AgentID: "trader-1", Credits: 150, Seen: true}, {AgentID: "trader-2", Credits: 200, Seen: true}}
	if n, _ := r.MaybeSnapshotDaily(t2, mustTime(t, "2026-06-25T00:00:40Z")); n != 1 {
		t.Fatalf("tick2 should record only the newly-seen trader-2, got %d", n)
	}
	recs, _ := ReadHistory(hp)
	if len(recs) != 2 {
		t.Fatalf("want 2 starting rows, got %d: %+v", len(recs), recs)
	}
	// trader-1's starting balance is its FIRST value (100), not the later 150.
	var t1Start float64 = -1
	for _, rec := range recs {
		if rec.AgentID == "trader-1" {
			t1Start = rec.Credits
		}
	}
	if t1Start != 100 {
		t.Fatalf("trader-1 starting balance should be 100 (first seen), got %v", t1Start)
	}
}

func TestBuildReportDeltasAndProfitJoin(t *testing.T) {
	live := []LiveRecord{
		{AgentID: "trader-2", Role: "hauler", System: "Krynn", Credits: 130000, Seen: true, Healthy: true},
		{AgentID: "trader-1", Role: "hauler", System: "Sol", Credits: 90000, Seen: true, Healthy: true},
	}
	history := []DailyRecord{
		{Date: "2026-06-24", AgentID: "trader-1", Credits: 100000},
		{Date: "2026-06-25", AgentID: "trader-1", Credits: 95000},
		{Date: "2026-06-24", AgentID: "trader-2", Credits: 100000},
	}
	profit := map[string]ProfitAgg{
		"trader-2": {Hauls: 5, Gross: 74286},
	}
	got := BuildReport(live, history, profit)
	if len(got) != 2 || got[0].AgentID != "trader-1" {
		t.Fatalf("expected sorted report, got %+v", got)
	}
	t1 := got[0]
	if t1.StartingBalance != 100000 || t1.StartDate != "2026-06-24" {
		t.Fatalf("t1 starting balance wrong: %+v", t1)
	}
	if t1.TotalDelta != -10000 { // 90000 - 100000
		t.Fatalf("t1 total delta = %v, want -10000", t1.TotalDelta)
	}
	if t1.SinceSnapshot != -5000 { // 90000 - 95000 (latest snapshot 2026-06-25)
		t.Fatalf("t1 since-snapshot = %v, want -5000", t1.SinceSnapshot)
	}
	if t1.DaysTracked != 2 {
		t.Fatalf("t1 days tracked = %d, want 2", t1.DaysTracked)
	}
	t2 := got[1]
	if t2.Hauls != 5 || t2.RealizedProfit != 74286 {
		t.Fatalf("t2 profit join wrong: %+v", t2)
	}
	if t2.TotalDelta != 30000 { // 130000 - 100000
		t.Fatalf("t2 total delta = %v, want 30000", t2.TotalDelta)
	}
}

func TestLiveRecordQuarantineFields(t *testing.T) {
	rec := LiveRecord{AgentID: "trader-8", Quarantined: true, QuarantineReason: "fuel-dead"}
	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"quarantined":true`, `"quarantine_reason":"fuel-dead"`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("marshal missing %s: %s", want, data)
		}
	}
	// omitted when healthy — keeps the existing status files byte-compatible
	data, _ = json.Marshal(LiveRecord{AgentID: "ok"})
	if strings.Contains(string(data), "quarantine") {
		t.Errorf("quarantine fields must be omitempty: %s", data)
	}
}
