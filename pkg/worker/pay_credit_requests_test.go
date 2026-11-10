package worker

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/creditreq"
)

type fakeGifts struct {
	payloads []map[string]any
	err      error
	after    func() // runs after a successful send
}

func (f *fakeGifts) SendGift(_ context.Context, p map[string]any) error {
	if f.after != nil && f.err == nil {
		defer f.after()
	}
	if f.err != nil {
		return f.err
	}
	f.payloads = append(f.payloads, p)
	return nil
}

var payNow = time.Date(2026, 11, 9, 15, 0, 0, 0, time.UTC)

type payEnv struct {
	d     PayDeps
	gifts *fakeGifts
	dir   string
}

func newPayEnv(t *testing.T) *payEnv {
	t.Helper()
	dir := t.TempDir()
	g := &fakeGifts{}
	return &payEnv{gifts: g, dir: dir, d: PayDeps{
		Queue:      creditreq.NewQueue(filepath.Join(dir, "q", "queue.json")),
		History:    creditreq.NewHistory(filepath.Join(dir, "h", "hist.jsonl")),
		Sender:     g,
		OwnCredits: func() float64 { return 2_000_000 },
		Usernames:  func(id string) string { return map[string]string{"a": "Josephus Miller", "b": "Bea", "c": "Cy"}[id] },
		Now:        func() time.Time { return payNow },
	}}
}

func (e *payEnv) file(t *testing.T, id string, at time.Time) {
	t.Helper()
	if _, err := e.d.Queue.File(creditreq.Record{AgentID: id, RequestedAt: at}); err != nil {
		t.Fatal(err)
	}
}

func (e *payEnv) record(t *testing.T, id string) (creditreq.Record, bool) {
	t.Helper()
	recs, err := e.d.Queue.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range recs {
		if r.AgentID == id {
			return r, true
		}
	}
	return creditreq.Record{}, false
}

func (e *payEnv) history(t *testing.T) []creditreq.Entry {
	t.Helper()
	es, err := e.d.History.Since(time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	return es
}

func countEvents(es []creditreq.Entry, ev string) int {
	n := 0
	for _, e := range es {
		if e.Event == ev {
			n++
		}
	}
	return n
}

func TestPay_PaysPending(t *testing.T) {
	e := newPayEnv(t)
	e.file(t, "a", payNow.Add(-time.Minute))
	paid, held, err := PayCreditRequests(t.Context(), e.d)
	if err != nil || paid != 1 || held != 0 {
		t.Fatalf("paid=%d held=%d err=%v", paid, held, err)
	}
	if len(e.gifts.payloads) != 1 || e.gifts.payloads[0]["recipient"] != "Josephus Miller" || e.gifts.payloads[0]["credits"] != 20000 {
		t.Fatalf("payloads = %v", e.gifts.payloads)
	}
	if _, ok := e.record(t, "a"); ok {
		t.Fatal("record not removed")
	}
	es := e.history(t)
	if len(es) != 1 || es[0].Event != creditreq.EventPaid || es[0].Amount != 20000 {
		t.Fatalf("history = %+v", es)
	}
}

func TestPay_24hRuleHolds(t *testing.T) {
	e := newPayEnv(t)
	e.file(t, "a", payNow.Add(-time.Minute))
	_ = e.d.History.Append(creditreq.Entry{At: payNow.Add(-2 * time.Hour), AgentID: "a", Event: creditreq.EventPaid, Amount: 20000})
	paid, held, err := PayCreditRequests(t.Context(), e.d)
	if err != nil || paid != 0 || held != 1 || len(e.gifts.payloads) != 0 {
		t.Fatalf("paid=%d held=%d gifts=%d err=%v", paid, held, len(e.gifts.payloads), err)
	}
	r, _ := e.record(t, "a")
	if r.Status != creditreq.StatusHeld || r.HeldReason != "paid 2026-11-09T13:00:00Z already" {
		t.Fatalf("record = %+v", r)
	}
	if countEvents(e.history(t), creditreq.EventHeld) != 1 {
		t.Fatal("no held entry")
	}
}

func TestPay_DailyCapHolds(t *testing.T) {
	e := newPayEnv(t)
	e.file(t, "a", payNow.Add(-time.Minute))
	for range 50 {
		_ = e.d.History.Append(creditreq.Entry{At: payNow.Add(-10 * time.Hour), AgentID: "x", Event: creditreq.EventPaid, Amount: 20000})
	}
	_, held, err := PayCreditRequests(t.Context(), e.d)
	if err != nil || held != 1 || len(e.gifts.payloads) != 0 {
		t.Fatalf("held=%d gifts=%d err=%v", held, len(e.gifts.payloads), err)
	}
	if r, _ := e.record(t, "a"); r.HeldReason != "daily cap" {
		t.Fatalf("reason = %q", r.HeldReason)
	}
}

func TestPay_LowBankerPaysNothing(t *testing.T) {
	e := newPayEnv(t)
	e.d.OwnCredits = func() float64 { return 900_000 }
	e.file(t, "a", payNow.Add(-time.Minute))
	paid, held, err := PayCreditRequests(t.Context(), e.d)
	if err != nil || paid != 0 || held != 0 || len(e.gifts.payloads) != 0 {
		t.Fatalf("paid=%d held=%d err=%v", paid, held, err)
	}
	if r, ok := e.record(t, "a"); !ok || r.Status != creditreq.StatusPending {
		t.Fatalf("record = %+v ok=%v", r, ok)
	}
}

func TestPay_UnknownUsernameHolds(t *testing.T) {
	e := newPayEnv(t)
	e.file(t, "zzz", payNow.Add(-time.Minute))
	_, held, err := PayCreditRequests(t.Context(), e.d)
	if err != nil || held != 1 {
		t.Fatalf("held=%d err=%v", held, err)
	}
	if r, _ := e.record(t, "zzz"); r.HeldReason != "no username" {
		t.Fatalf("reason = %q", r.HeldReason)
	}
	e.d.Usernames = nil
	e.file(t, "a", payNow)
	if _, held, _ = PayCreditRequests(t.Context(), e.d); held != 1 {
		t.Fatalf("nil Usernames: held=%d", held)
	}
}

func TestPay_ThreeSendFailuresHold(t *testing.T) {
	e := newPayEnv(t)
	e.gifts.err = errors.New("boom")
	e.file(t, "a", payNow.Add(-time.Minute))
	for i := range 3 {
		_, held, err := PayCreditRequests(t.Context(), e.d)
		if err != nil {
			t.Fatal(err)
		}
		if i < 2 && held != 0 {
			t.Fatalf("run %d held=%d", i, held)
		}
	}
	r, _ := e.record(t, "a")
	if r.Status != creditreq.StatusHeld || r.Attempts != 3 || r.HeldReason != "send failed: boom" {
		t.Fatalf("record = %+v", r)
	}
	es := e.history(t)
	if countEvents(es, creditreq.EventSendFailed) != 3 || countEvents(es, creditreq.EventHeld) != 1 {
		t.Fatalf("history = %+v", es)
	}
}

func TestPay_HistoryBeforeTransition(t *testing.T) {
	e := newPayEnv(t)
	e.file(t, "a", payNow.Add(-time.Minute))
	qdir := filepath.Join(e.dir, "q")
	if err := os.Chmod(qdir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(qdir, 0o755) })
	if _, err := os.Create(filepath.Join(qdir, "probe")); err == nil {
		t.Skip("directory permissions not enforced (root?)")
	}
	if _, _, err := PayCreditRequests(t.Context(), e.d); err == nil {
		t.Fatal("expected Remove failure")
	}
	if len(e.gifts.payloads) != 1 {
		t.Fatalf("gifts after run 1 = %d", len(e.gifts.payloads))
	}
	_, _, _ = PayCreditRequests(t.Context(), e.d)
	if len(e.gifts.payloads) != 1 {
		t.Fatalf("second run gifted again: %d", len(e.gifts.payloads))
	}
}

func TestPay_OldestFirst(t *testing.T) {
	e := newPayEnv(t)
	e.file(t, "b", payNow.Add(-time.Minute))
	e.file(t, "a", payNow.Add(-time.Hour))
	e.file(t, "c", payNow.Add(-time.Second))
	if _, _, err := PayCreditRequests(t.Context(), e.d); err != nil {
		t.Fatal(err)
	}
	var got []any
	for _, p := range e.gifts.payloads {
		got = append(got, p["recipient"])
	}
	if len(got) != 3 || got[0] != "Josephus Miller" || got[1] != "Bea" || got[2] != "Cy" {
		t.Fatalf("order = %v", got)
	}
}

func TestPay_HistoryAppendFailureHoldsAndStops(t *testing.T) {
	e := newPayEnv(t)
	var out bytes.Buffer
	e.d.Out = &out
	hdir := filepath.Join(e.dir, "h")
	if err := os.MkdirAll(hdir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Once the first gift is out, make the history unwritable.
	e.gifts.after = func() { _ = os.Chmod(hdir, 0o555) }
	t.Cleanup(func() { _ = os.Chmod(hdir, 0o755) })
	skipIfPermsNotEnforced(t, hdir)
	e.file(t, "a", payNow.Add(-time.Hour))
	e.file(t, "b", payNow.Add(-time.Minute))

	paid, held, err := PayCreditRequests(t.Context(), e.d)
	if err == nil {
		t.Fatal("expected the history error")
	}
	if paid != 1 || held != 1 || len(e.gifts.payloads) != 1 {
		t.Fatalf("paid=%d held=%d gifts=%d", paid, held, len(e.gifts.payloads))
	}
	a, _ := e.record(t, "a")
	if a.Status != creditreq.StatusHeld || a.Amount != 20000 || a.PaidAt.IsZero() || !strings.Contains(a.HeldReason, "history write failed") {
		t.Fatalf("record a = %+v", a)
	}
	if b, _ := e.record(t, "b"); b.Status != creditreq.StatusPending {
		t.Fatalf("record b = %+v", b)
	}
	if !strings.Contains(out.String(), "Josephus Miller") {
		t.Fatalf("log lacks username: %q", out.String())
	}
	// Re-run (history still broken): the unrecorded payment halts the run, so
	// b is not gifted either, and the halt is logged once.
	out.Reset()
	rp, rh, rerr := PayCreditRequests(t.Context(), e.d)
	if rerr != nil || rp != 0 || rh != 0 || len(e.gifts.payloads) != 1 {
		t.Fatalf("re-run paid=%d held=%d err=%v gifts=%d", rp, rh, rerr, len(e.gifts.payloads))
	}
	if n := strings.Count(out.String(), "halted"); n != 1 {
		t.Fatalf("halt lines = %d: %q", n, out.String())
	}
}

// skipIfPermsNotEnforced skips when a 0555 dir is still writable (root).
func skipIfPermsNotEnforced(t *testing.T, dir string) {
	t.Helper()
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o755) //nolint:errcheck
	if f, err := os.Create(filepath.Join(dir, "probe")); err == nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		t.Skip("directory permissions not enforced (root?)")
	}
}

func TestPay_FloorCountsThisRunsGifts(t *testing.T) {
	e := newPayEnv(t)
	var out bytes.Buffer
	e.d.Out = &out
	e.d.OwnCredits = func() float64 { return 1_010_000 }
	e.file(t, "a", payNow.Add(-time.Hour))
	e.file(t, "b", payNow.Add(-time.Minute))
	paid, _, err := PayCreditRequests(t.Context(), e.d)
	if err != nil || paid != 1 || len(e.gifts.payloads) != 1 {
		t.Fatalf("paid=%d gifts=%d err=%v", paid, len(e.gifts.payloads), err)
	}
	if r, _ := e.record(t, "b"); r.Status != creditreq.StatusPending {
		t.Fatalf("b = %+v", r)
	}
	if n := strings.Count(out.String(), "floor"); n != 1 {
		t.Fatalf("floor lines = %d: %q", n, out.String())
	}
}

func TestPay_DailyCapAcrossOneRun(t *testing.T) {
	e := newPayEnv(t)
	for range 49 {
		_ = e.d.History.Append(creditreq.Entry{At: payNow.Add(-5 * time.Hour), AgentID: "x", Event: creditreq.EventPaid, Amount: 20000})
	}
	e.file(t, "a", payNow.Add(-time.Hour))
	e.file(t, "b", payNow.Add(-time.Minute))
	paid, held, err := PayCreditRequests(t.Context(), e.d)
	if err != nil || paid != 1 || held != 1 {
		t.Fatalf("paid=%d held=%d err=%v", paid, held, err)
	}
	if r, _ := e.record(t, "b"); r.HeldReason != "daily cap" {
		t.Fatalf("b = %+v", r)
	}
}

func TestPay_CapResetsAtUTCMidnight(t *testing.T) {
	e := newPayEnv(t)
	e.d.Now = func() time.Time { return time.Date(2026, 11, 10, 0, 10, 0, 0, time.UTC) }
	for range 50 {
		_ = e.d.History.Append(creditreq.Entry{At: time.Date(2026, 11, 9, 23, 50, 0, 0, time.UTC), AgentID: "x", Event: creditreq.EventPaid, Amount: 20000})
	}
	e.file(t, "a", payNow)
	paid, held, err := PayCreditRequests(t.Context(), e.d)
	if err != nil || paid != 1 || held != 0 {
		t.Fatalf("paid=%d held=%d err=%v", paid, held, err)
	}
}

func TestPay_LowBalanceLogsOnce(t *testing.T) {
	e := newPayEnv(t)
	var out bytes.Buffer
	e.d.Out = &out
	e.d.OwnCredits = func() float64 { return 900_000 }
	for _, id := range []string{"a", "b", "c"} {
		e.file(t, id, payNow.Add(-time.Minute))
	}
	if _, _, err := PayCreditRequests(t.Context(), e.d); err != nil {
		t.Fatal(err)
	}
	recs, _ := e.d.Queue.List()
	if len(e.gifts.payloads) != 0 || len(recs) != 3 {
		t.Fatalf("gifts=%d recs=%d", len(e.gifts.payloads), len(recs))
	}
	for _, r := range recs {
		if r.Status != creditreq.StatusPending {
			t.Fatalf("record %+v", r)
		}
	}
	if n := strings.Count(out.String(), "floor"); n != 1 {
		t.Fatalf("floor lines = %d", n)
	}
}
