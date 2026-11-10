package worker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/creditreq"
)

type fakeGifts struct {
	payloads []map[string]any
	err      error
}

func (f *fakeGifts) SendGift(_ context.Context, p map[string]any) error {
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
		if want := 0; i < 2 && held != want {
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
	qdir := filepath.Dir(e.dir + "/q/queue.json")
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
	_, held, _ := PayCreditRequests(t.Context(), e.d)
	if len(e.gifts.payloads) != 1 {
		t.Fatalf("second run gifted again: %d", len(e.gifts.payloads))
	}
	_ = held
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
