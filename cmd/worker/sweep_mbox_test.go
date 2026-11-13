package main

import "testing"

// A landing must read history from NOW backwards: Backfill otherwise resumes
// below the oldest stored message (explorer-1's was 10:44Z, before the
// sweep), so every landing walked old history and missed the fragments of
// the systems the sweep passed (live 2026-11-13: 0-1 messages per landing).
func TestSweepBackfillStartsFromNow(t *testing.T) {
	o := sweepBackfillOptions()
	if !o.ResetCursor {
		t.Error("ResetCursor = false; the crawl resumes below the oldest stored message")
	}
	if o.MaxPerChannel < 200 {
		t.Errorf("MaxPerChannel = %d; too small to reach back through a day of sweep landings", o.MaxPerChannel)
	}
	if len(o.Channels) != 2 || o.Channels[0] != "system" || o.Channels[1] != "local" {
		t.Errorf("channels %v, want [system local]", o.Channels)
	}
}
