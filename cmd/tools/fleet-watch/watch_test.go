package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/creditreq"
)

// realTail is shaped exactly like the live logs, including the mid-line start a
// byte-offset tail read produces and the ✓ prefix on the reconnect line.
const realTail = `ent salvager-1] 2026/06/28 10:08:48 Disconnected: failed to get reader: EOF
[worker:trader-9] 2026/08/18 15:18:20 Disconnected: failed to get reader: failed to read frame header: EOF
[worker:trader-7] 2026/08/18 15:18:29 ✓ Reconnected successfully
[worker:marketbot_market_prime] 2026/08/18 16:09:01 OK: Dock action pending. Will execute on next tick.
`

func TestParseTail_NewestStampAndCounts(t *testing.T) {
	got := ParseTail([]byte(realTail))
	if !got.HasStamp {
		t.Fatal("no stamp parsed from a tail that plainly has several")
	}
	// The newest line is the LAST one, not the largest-looking one earlier.
	if h, m := got.Newest.Hour(), got.Newest.Minute(); h != 16 || m != 9 {
		t.Errorf("newest = %s, want 16:09", got.Newest.Format("15:04:05"))
	}
	if got.Disconnects != 2 || got.Reconnects != 1 {
		t.Errorf("disc/recon = %d/%d, want 2/1", got.Disconnects, got.Reconnects)
	}
}

func TestParseTail_EmptyAndStampless(t *testing.T) {
	if s := ParseTail(nil); s.HasStamp {
		t.Error("empty tail reported a stamp")
	}
	if s := ParseTail([]byte("no timestamps here at all\n")); s.HasStamp {
		t.Error("stampless tail reported a stamp")
	}
}

// TestEvaluate_TransientDisconnectIsNotAnAlert is the property the whole design
// turns on: the fleet disconnects and reconnects constantly (haul did 22 in half
// an hour, all recovered). Alerting on the first unmatched disconnect would fire
// all day and be ignored, which is worse than no watcher.
func TestEvaluate_TransientDisconnectIsNotAnAlert(t *testing.T) {
	now := time.Now()
	samples := []Sample{{Fleet: "haul", Newest: now, HasStamp: true, Disconnects: 3, Reconnects: 2}}

	alerts, state := Evaluate(samples, map[string]int{}, 3*time.Minute, now)
	if len(alerts) != 0 {
		t.Fatalf("alerted on a first-sighting imbalance: %+v", alerts)
	}
	if state["haul"] != 1 {
		t.Errorf("state = %v, want the imbalance carried to the next pass", state)
	}

	// Still unmatched on the next pass: now it is real.
	alerts, _ = Evaluate(samples, state, 3*time.Minute, now)
	if len(alerts) != 1 || alerts[0].Kind != KindUnrecovered {
		t.Fatalf("second consecutive imbalance did not alert: %+v", alerts)
	}
}

// TestEvaluate_RecoveryClearsTheState: once reconnects catch up, the fleet must
// stop alerting without anything resetting it by hand.
func TestEvaluate_RecoveryClearsTheState(t *testing.T) {
	now := time.Now()
	healthy := []Sample{{Fleet: "haul", Newest: now, HasStamp: true, Disconnects: 5, Reconnects: 5}}

	alerts, state := Evaluate(healthy, map[string]int{"haul": 2}, 3*time.Minute, now)
	if len(alerts) != 0 {
		t.Errorf("alerted on a fully recovered fleet: %+v", alerts)
	}
	if state["haul"] != 0 {
		t.Errorf("state = %v, want cleared", state)
	}
}

// TestEvaluate_MoreReconnectsThanDisconnects: the tail window can cut a
// disconnect off the front, leaving an orphan reconnect. That is a windowing
// artefact, not a fault.
func TestEvaluate_MoreReconnectsThanDisconnects(t *testing.T) {
	now := time.Now()
	s := []Sample{{Fleet: "mb", Newest: now, HasStamp: true, Disconnects: 0, Reconnects: 4}}
	alerts, state := Evaluate(s, map[string]int{"mb": 3}, 3*time.Minute, now)
	if len(alerts) != 0 {
		t.Errorf("orphan reconnects alerted: %+v", alerts)
	}
	if state["mb"] != 0 {
		t.Errorf("negative gap leaked into state: %v", state)
	}
}

func TestEvaluate_StaleFleet(t *testing.T) {
	now := time.Now()
	s := []Sample{{Fleet: "craft", Newest: now.Add(-9 * time.Minute), HasStamp: true}}
	alerts, _ := Evaluate(s, map[string]int{}, 3*time.Minute, now)
	if len(alerts) != 1 || alerts[0].Kind != KindStale {
		t.Fatalf("a fleet silent for 9 minutes did not alert: %+v", alerts)
	}
	// A fleet that logged a second ago is fine.
	fresh := []Sample{{Fleet: "craft", Newest: now.Add(-time.Second), HasStamp: true}}
	if a, _ := Evaluate(fresh, map[string]int{}, 3*time.Minute, now); len(a) != 0 {
		t.Errorf("alerted on a live fleet: %+v", a)
	}
}

func TestEvaluate_UnreadableLog(t *testing.T) {
	now := time.Now()
	s := []Sample{{Fleet: "hunt", ReadErr: errors.New("permission denied")}}
	alerts, _ := Evaluate(s, map[string]int{}, 3*time.Minute, now)
	if len(alerts) != 1 || alerts[0].Kind != KindUnreadable {
		t.Fatalf("unreadable log did not alert: %+v", alerts)
	}
}

// TestProcessAlerts covers the unsupervised daemons — the scanner and the pruner
// have each died silently before and were noticed only by downstream damage.
func TestProcessAlerts(t *testing.T) {
	got := ProcessAlerts(
		map[string]int{"overmind": 7, "worker": 158, "arbitrage-scanner": 0, "market-prune": 1},
		map[string]int{"overmind": 7, "worker": 150, "arbitrage-scanner": 1, "market-prune": 1},
	)
	if len(got) != 1 {
		t.Fatalf("alerts = %+v, want only the dead scanner", got)
	}
	if got[0].Fleet != "arbitrage-scanner" || got[0].Kind != KindProcess {
		t.Errorf("wrong alert: %+v", got[0])
	}
}

// TestNotifiable_SuppressesOnlySelfHealingChurn: the disconnect-imbalance alert
// is the one that recovers on its own, and it is the one that buried the alerts
// that don't. Everything else must still reach the screen.
func TestNotifiable_SuppressesOnlySelfHealingChurn(t *testing.T) {
	if Notifiable(KindUnrecovered) {
		t.Error("disconnect churn must not raise a desktop notification")
	}
	for _, kind := range []string{KindStale, KindUnreadable, KindProcess} {
		if !Notifiable(kind) {
			t.Errorf("%s does not self-heal and must still notify", kind)
		}
	}
}

// TestEvaluate_StillRecordsUnrecovered guards against fixing the noise by
// deleting the signal: suppression is a notification-layer decision, and the
// alert must still be produced so it lands in the log and the status file.
func TestEvaluate_StillRecordsUnrecovered(t *testing.T) {
	now := time.Now()
	samples := []Sample{{
		Fleet: "haul", HasStamp: true, Newest: now,
		Disconnects: 9, Reconnects: 2,
	}}
	prev := map[string]int{"haul": 4}

	alerts, _ := Evaluate(samples, prev, time.Minute, now)

	var found bool
	for _, a := range alerts {
		if a.Kind == KindUnrecovered {
			found = true
		}
	}
	if !found {
		t.Fatalf("unrecovered alert no longer produced: %+v", alerts)
	}
}

func TestCreditAlerts(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	paid := func(agent string, ago time.Duration) creditreq.Entry {
		return creditreq.Entry{At: now.Add(-ago), AgentID: agent, Event: creditreq.EventPaid}
	}
	tests := []struct {
		name string
		open []creditreq.Record
		hist []creditreq.Entry
		want []string
	}{
		{"held", []creditreq.Record{{AgentID: "a1", Status: creditreq.StatusHeld, HeldReason: "gift failed 3 times"}}, nil,
			[]string{"a1 credit request held: gift failed 3 times"}},
		{"pending 10m", []creditreq.Record{{AgentID: "a1", Status: creditreq.StatusPending, RequestedAt: now.Add(-10 * time.Minute)}}, nil, nil},
		{"pending 45m", []creditreq.Record{{AgentID: "a1", Status: creditreq.StatusPending, RequestedAt: now.Add(-45 * time.Minute)}}, nil,
			[]string{"a1 credit request pending 45m — is the banker paying?"}},
		{"two paid in 3 days", nil, []creditreq.Entry{paid("a1", 24*time.Hour), paid("a1", 72*time.Hour)},
			[]string{"a1 paid 2 times in 7 days"}},
		{"one paid", nil, []creditreq.Entry{paid("a1", time.Hour)}, nil},
		{"two paid by different agents", nil, []creditreq.Entry{paid("a1", time.Hour), paid("a2", time.Hour)}, nil},
		{"second payment outside window", nil, []creditreq.Entry{paid("a1", time.Hour), paid("a1", 8*24*time.Hour)}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CreditAlerts(tt.open, tt.hist, now)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d alerts %v, want %d", len(got), got, len(tt.want))
			}
			for i, a := range got {
				if a.Kind != KindCredit || a.Message != tt.want[i] {
					t.Errorf("alert %d = %q/%q, want %q", i, a.Kind, a.Message, tt.want[i])
				}
			}
		})
	}
}

func TestCreditHaltAlerts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credit-requests.json.halt")
	got, err := CreditHaltAlerts(path)
	if err != nil || len(got) != 0 {
		t.Fatalf("missing halt file: got %v, %v; want none", got, err)
	}
	if err = os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = CreditHaltAlerts(path)
	want := "credit payments HALTED — " + path + " exists; reconcile and delete it"
	if err != nil || len(got) != 1 || got[0].Kind != KindCredit || got[0].Message != want {
		t.Fatalf("got %v, %v; want one alert %q", got, err, want)
	}
}

func TestReadCreditAlerts_MissingFilesAreQuiet(t *testing.T) {
	got, errs := ReadCreditAlerts(filepath.Join(t.TempDir(), "credit-requests.json"), time.Now())
	if len(got) != 0 || len(errs) != 0 {
		t.Fatalf("got %v, %v; want nothing", got, errs)
	}
}

func TestReadCreditAlerts_ReadsQueueHistoryAndHalt(t *testing.T) {
	dir := t.TempDir()
	qp := filepath.Join(dir, "credit-requests.json")
	now := time.Now()
	q := creditreq.NewQueue(qp)
	if _, err := q.File(creditreq.Record{AgentID: "a1", RequestedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Update("a1", func(r *creditreq.Record) { r.Status, r.HeldReason = creditreq.StatusHeld, "x" }); err != nil {
		t.Fatal(err)
	}
	h := creditreq.NewHistory(filepath.Join(dir, "credit-requests-history.jsonl"))
	for range 2 {
		if err := h.Append(creditreq.Entry{At: now.Add(-time.Hour), AgentID: "a2", Event: creditreq.EventPaid}); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(qp+".halt", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	got, errs := ReadCreditAlerts(qp, now)
	if len(errs) != 0 || len(got) != 3 {
		t.Fatalf("got %v, %v; want 3 alerts", got, errs)
	}
}

func TestNotifiable_Credit(t *testing.T) {
	if !Notifiable(KindCredit) {
		t.Error("credit alerts must notify")
	}
}
