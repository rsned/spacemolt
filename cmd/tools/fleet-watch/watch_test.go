package main

import (
	"errors"
	"testing"
	"time"
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
