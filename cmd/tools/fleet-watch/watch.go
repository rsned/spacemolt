package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/creditreq"
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
	KindCredit      = "credit"
)

// Credit-request alert thresholds.
const (
	creditPendingStale = 30 * time.Minute
	creditRepeatWindow = 7 * 24 * time.Hour
	creditRepeatPaid   = 2
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

// CreditAlerts reports credit requests that need a human: any held request,
// any pending one the banker has not paid within creditPendingStale, and any
// agent paid creditRepeatPaid or more times inside creditRepeatWindow (hist
// is expected to already be limited to that window). Each alert has its own
// Fleet key (<agent>:held, :pending, :repeat) so conditions never mask each other.
func CreditAlerts(open []creditreq.Record, hist []creditreq.Entry, now time.Time) []Alert {
	var out []Alert
	for _, r := range open {
		switch r.Status {
		case creditreq.StatusHeld:
			out = append(out, Alert{Fleet: r.AgentID + ":held", Kind: KindCredit,
				Message: fmt.Sprintf("%s credit request held: %s", r.AgentID, r.HeldReason)})
		case creditreq.StatusPending:
			if age := now.Sub(r.RequestedAt); age > creditPendingStale {
				out = append(out, Alert{Fleet: r.AgentID + ":pending", Kind: KindCredit,
					Message: fmt.Sprintf("%s credit request pending %dm — is the banker paying?", r.AgentID, int(age.Minutes()))})
			}
		}
	}

	paid := map[string]int{}
	for _, e := range hist {
		if e.Event == creditreq.EventPaid && !e.At.Before(now.Add(-creditRepeatWindow)) {
			paid[e.AgentID]++
		}
	}
	agents := make([]string, 0, len(paid))
	for a, n := range paid {
		if n >= creditRepeatPaid {
			agents = append(agents, a)
		}
	}
	sort.Strings(agents)
	for _, a := range agents {
		out = append(out, Alert{Fleet: a + ":repeat", Kind: KindCredit,
			Message: fmt.Sprintf("%s paid %d times in 7 days", a, paid[a])})
	}

	return out
}

// CreditHaltAlerts raises an alert when the banker's halt file exists. The
// banker writes <queue>.halt when a payment went out unrecorded and pays
// nothing until a human reconciles and deletes it. A stat error other than
// not-exist is returned so the caller can log it.
func CreditHaltAlerts(haltPath string) ([]Alert, error) {
	_, err := os.Stat(haltPath)
	switch {
	case err == nil:
		return []Alert{{Fleet: "banker", Kind: KindCredit,
			Message: fmt.Sprintf("credit payments HALTED — %s exists; reconcile and delete it", haltPath)}}, nil
	case os.IsNotExist(err):
		return nil, nil
	default:
		return nil, err
	}
}

// ReadCreditAlerts reads the queue and history at queuePath and the sibling
// history file, and returns every credit alert. Missing files yield no
// alerts; each other failure is returned in errs and the remaining checks
// still run, so one bad file cannot hide the others.
func ReadCreditAlerts(queuePath string, now time.Time) (alerts []Alert, errs []error) {
	// Stat first: List takes a lock and creates a .lock file, which a
	// watcher must not do to a queue that does not exist yet.
	var open []creditreq.Record
	if _, err := os.Stat(queuePath); err == nil {
		open, err = creditreq.NewQueue(queuePath).List()
		if err != nil {
			errs = append(errs, fmt.Errorf("credit queue: %w", err))
		}
	} else if !os.IsNotExist(err) {
		errs = append(errs, fmt.Errorf("credit queue: %w", err))
	}

	histPath := filepath.Join(filepath.Dir(queuePath), "credit-requests-history.jsonl")
	hist, err := creditreq.NewHistory(histPath).Since(now.Add(-creditRepeatWindow))
	if err != nil {
		errs = append(errs, fmt.Errorf("credit history: %w", err))
	}

	alerts = CreditAlerts(open, hist, now)

	halt, err := CreditHaltAlerts(queuePath + ".halt")
	if err != nil {
		errs = append(errs, fmt.Errorf("credit halt file: %w", err))
	}

	return append(alerts, halt...), errs
}
