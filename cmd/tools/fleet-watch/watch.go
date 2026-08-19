package main

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Fleet log lines are prefixed "[worker:<agent>] 2026/08/18 15:18:29 ..." or
// "[overmind] 2026/08/18 ...". The stamp is LOCAL time, not UTC — the build
// banners in the same file are UTC, and mixing the two silently reports a
// healthy fleet as hours stale.
const logStampLayout = "2006/01/02 15:04:05"

const (
	markerDisconnect = "Disconnected:"
	markerReconnect  = "Reconnected successfully"
)

// Sample is one observation of one fleet's log tail.
type Sample struct {
	Fleet string
	// Newest is the most recent log stamp found, in local time.
	Newest time.Time
	// HasStamp is false when the tail held no parseable stamp at all, which is
	// different from an old one: it means the file is empty, or the tail window
	// landed inside a single enormous line.
	HasStamp    bool
	Disconnects int
	Reconnects  int
	ReadErr     error
}

// Alert is one thing worth waking a human for.
type Alert struct {
	Fleet   string
	Kind    string
	Message string
}

// alert kinds
const (
	KindStale       = "stale"
	KindUnrecovered = "unrecovered"
	KindUnreadable  = "unreadable"
	KindProcess     = "process"
)

// ParseTail extracts the newest timestamp and the disconnect/reconnect counts
// from a log tail.
//
// It scans backwards for the stamp because the newest line is the last one, and
// a 2 MiB tail of a 7 GB log holds tens of thousands of lines — walking forward
// to find the maximum would read them all for a value that is always at the end.
// The first line is skipped when the window starts mid-line.
func ParseTail(tail []byte) Sample {
	var s Sample
	s.Disconnects = bytes.Count(tail, []byte(markerDisconnect))
	s.Reconnects = bytes.Count(tail, []byte(markerReconnect))

	lines := bytes.Split(tail, []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		if ts, ok := stampFromLine(string(lines[i])); ok {
			s.Newest = ts
			s.HasStamp = true

			break
		}
	}

	return s
}

// stampFromLine pulls the "2026/08/18 15:18:29" stamp out of a log line. The
// prefix before it varies ("[worker:trader-9] ", "[overmind] "), so the stamp is
// located by shape rather than by field position.
func stampFromLine(line string) (time.Time, bool) {
	fields := strings.Fields(line)
	for i := 0; i+1 < len(fields); i++ {
		if len(fields[i]) != 10 || len(fields[i+1]) < 8 {
			continue
		}
		ts, err := time.ParseInLocation(logStampLayout, fields[i]+" "+fields[i+1][:8], time.Local)
		if err == nil {
			return ts, true
		}
	}

	return time.Time{}, false
}

// Evaluate turns this pass's samples into alerts.
//
// prevUnrecovered carries the per-fleet imbalance seen last pass. An imbalance
// is NOT alerted on the first sighting: a disconnect that happened moments ago
// has not had time to reconnect yet, and the fleet reconnects within seconds, so
// alerting immediately would fire constantly during normal operation. Two
// consecutive passes with an unmatched disconnect is the signal that the client
// is failing to get back in — which is exactly what "other players report
// connection issues" would look like if it reached us.
func Evaluate(samples []Sample, prevUnrecovered map[string]int, staleAfter time.Duration, now time.Time) ([]Alert, map[string]int) {
	unrecovered := make(map[string]int, len(samples))
	var alerts []Alert

	for _, s := range samples {
		if s.ReadErr != nil {
			alerts = append(alerts, Alert{Fleet: s.Fleet, Kind: KindUnreadable,
				Message: fmt.Sprintf("log unreadable: %v", s.ReadErr)})

			continue
		}
		if !s.HasStamp {
			alerts = append(alerts, Alert{Fleet: s.Fleet, Kind: KindUnreadable,
				Message: "no timestamp in log tail"})

			continue
		}

		if age := now.Sub(s.Newest); age > staleAfter {
			alerts = append(alerts, Alert{Fleet: s.Fleet, Kind: KindStale,
				Message: fmt.Sprintf("silent for %s (last line %s)",
					age.Round(time.Second), s.Newest.Format("15:04:05"))})
		}

		gap := s.Disconnects - s.Reconnects
		if gap < 0 {
			// More reconnects than disconnects only means the tail window cut
			// the matching disconnect off the front. Not a fault.
			gap = 0
		}
		unrecovered[s.Fleet] = gap
		if gap > 0 && prevUnrecovered[s.Fleet] > 0 {
			alerts = append(alerts, Alert{Fleet: s.Fleet, Kind: KindUnrecovered,
				Message: fmt.Sprintf("%d disconnect(s) without a reconnect, two passes running", gap)})
		}
	}

	sort.Slice(alerts, func(i, j int) bool {
		if alerts[i].Fleet != alerts[j].Fleet {
			return alerts[i].Fleet < alerts[j].Fleet
		}

		return alerts[i].Kind < alerts[j].Kind
	})

	return alerts, unrecovered
}

// ProcessAlerts reports expected processes that are missing or short.
//
// The daemons are here because nothing supervises them: the arbitrage scanner
// and the market pruner have each died silently before and were noticed only by
// their downstream damage (a frozen opportunity pool; a 62 GB database).
func ProcessAlerts(counts map[string]int, want map[string]int) []Alert {
	var out []Alert
	names := make([]string, 0, len(want))
	for n := range want {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, n := range names {
		got, min := counts[n], want[n]
		if got < min {
			out = append(out, Alert{Fleet: n, Kind: KindProcess,
				Message: fmt.Sprintf("%d running, expected at least %d", got, min)})
		}
	}

	return out
}

// Notifiable reports whether an alert of this kind should interrupt the
// operator with a desktop notification, as opposed to being recorded only.
//
// KindUnrecovered is deliberately excluded. A disconnect/reconnect wave is
// self-healing: across 42 such alerts during the 2026-08-18 server restarts,
// every single one recovered without intervention, and the churn produced
// bursts of 37 notifications an hour. An alert nobody can act on trains the
// operator to dismiss the ones that matter, so this kind stays in the log and
// the status file where it is useful for forensics, and off the screen.
//
// The remaining kinds do not self-heal: a silent log, an unreadable log, and a
// missing daemon all stay broken until someone intervenes.
func Notifiable(kind string) bool {
	return kind != KindUnrecovered
}
