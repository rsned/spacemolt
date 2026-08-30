package main

import (
	"slices"
	"testing"
)

// Private is the one channel where "missed while offline" is the normal
// case — a DM sent to a parked pilot is never pushed and never "new" to the
// poller — so a bare `mbox backfill` must pull it without a flag.
func TestParseBackfillOpts_DefaultIncludesPrivate(t *testing.T) {
	opts, err := parseBackfillOpts(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, ch := range []string{"system", "local", "faction", "private"} {
		if !slices.Contains(opts.Channels, ch) {
			t.Errorf("default channels %v lack %q", opts.Channels, ch)
		}
	}
}

func TestParseBackfillOpts_ChannelFlagNarrows(t *testing.T) {
	opts, err := parseBackfillOpts([]string{"--channel", "Private", "--limit", "50", "-f"})
	if err != nil {
		t.Fatal(err)
	}
	if len(opts.Channels) != 1 || opts.Channels[0] != "private" || opts.MaxPerChannel != 50 || !opts.ResetCursor {
		t.Errorf("opts = %+v", opts)
	}
}
