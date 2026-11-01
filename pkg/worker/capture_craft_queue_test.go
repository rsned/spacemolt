package worker

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// queueFake answers `craft action=queue` with a fixed reply.
type queueFake struct {
	game.GameClient
	reply string
	calls []map[string]any
}

func (f *queueFake) RawCommand(_ context.Context, command string, args map[string]any) error {
	if command == "craft" {
		f.calls = append(f.calls, args)
	}
	return nil
}

func (f *queueFake) GetRawJSON(key string) []byte {
	if key != "_last" {
		return nil
	}
	return []byte(f.reply)
}

func readQueueFile(t *testing.T, dir string) craftQueueFile {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, CraftQueueFile))
	if err != nil {
		t.Fatal(err)
	}
	var q craftQueueFile
	if err := json.Unmarshal(raw, &q); err != nil {
		t.Fatal(err)
	}
	return q
}

var queueNow = time.Date(2026, 11, 1, 18, 0, 0, 0, time.UTC)

func TestCaptureCraftQueueWritesJobs(t *testing.T) {
	dir := t.TempDir()
	c := &queueFake{reply: `{"action":"queue","jobs":[{"job_id":"j1","recipe":"grow_sunspindle","runs_remaining":1,"eta_ticks":72,"position":2,"facility_id":"","base_id":"c976fa7df53c87f9616165d7d411b41a","status":"queued"}]}`}

	n, err := CaptureCraftQueue(context.Background(), c, dir, queueNow)
	if err != nil || n != 1 {
		t.Fatalf("CaptureCraftQueue = %d, %v; want 1, nil", n, err)
	}
	if len(c.calls) != 1 || c.calls[0]["action"] != "queue" {
		t.Errorf("craft calls = %v, want one action=queue", c.calls)
	}
	q := readQueueFile(t, dir)
	if !q.CapturedAt.Equal(queueNow) || len(q.Jobs) != 1 {
		t.Fatalf("file = %+v", q)
	}
	if j := q.Jobs[0]; j.Recipe != "grow_sunspindle" || j.ETATicks != 72 || j.Position != 2 || j.RunsRemaining != 1 || j.BaseID != "c976fa7df53c87f9616165d7d411b41a" {
		t.Errorf("job = %+v", j)
	}
}

// An empty queue is a real observation: the file says so, with a fresh time.
func TestCaptureCraftQueueEmptyListingWritesEmpty(t *testing.T) {
	dir := t.TempDir()
	n, err := CaptureCraftQueue(context.Background(), &queueFake{reply: `{"action":"queue","jobs":[]}`}, dir, queueNow)
	if err != nil || n != 0 {
		t.Fatalf("CaptureCraftQueue = %d, %v; want 0, nil", n, err)
	}
	q := readQueueFile(t, dir)
	if !q.CapturedAt.Equal(queueNow) || q.Jobs == nil || len(q.Jobs) != 0 {
		t.Errorf("file = %+v, want an empty jobs list at %v", q, queueNow)
	}
}

// Another command's reply must not overwrite the last good capture.
func TestCaptureCraftQueueForeignReplyLeavesFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := CaptureCraftQueue(context.Background(), &queueFake{reply: `{"action":"queue","jobs":[{"job_id":"j1","recipe":"r","runs_remaining":3}]}`}, dir, queueNow); err != nil {
		t.Fatal(err)
	}
	if _, err := CaptureCraftQueue(context.Background(), &queueFake{reply: `{"credits":5}`}, dir, queueNow.Add(time.Hour)); err == nil {
		t.Error("a reply without an action was accepted")
	}
	if q := readQueueFile(t, dir); len(q.Jobs) != 1 || !q.CapturedAt.Equal(queueNow) {
		t.Errorf("previous capture was replaced: %+v", q)
	}
}

func TestCaptureCraftQueueIsSupported(t *testing.T) {
	if !supported["capture_craft_queue"] {
		t.Error(`"capture_craft_queue" missing from the supported command set`)
	}
}
