package worker

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/handoff"
)

// newHandoffTestQueue builds a handoff.Queue backed by a tempdir file.
func newHandoffTestQueue(t *testing.T) *handoff.Queue {
	t.Helper()
	return handoff.NewQueue(filepath.Join(t.TempDir(), "handoff.json"))
}

// newHandoffTestClient builds a deliverFakeClient (reused from deliver_test.go)
// docked at station with the given cargo capacity and storage stock.
func newHandoffTestClient(cargoCap float64, docked bool, station string, storage map[string]float64) *deliverFakeClient {
	return &deliverFakeClient{
		fakeClient: &fakeClient{state: &game.State{
			Doc: docked,
			Player: game.Player{
				DockedAtBase: station,
			},
			Ship: game.Ship{CargoCapacity: cargoCap},
		}},
		storageStock: storage,
	}
}

// findHandoffRecord locates rec by ID after a pass, failing the test if absent.
func findHandoffRecord(t *testing.T, q *handoff.Queue, id string) handoff.Record {
	t.Helper()
	recs, err := q.List()
	if err != nil {
		t.Fatalf("q.List: %v", err)
	}
	for _, r := range recs {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("record %s not found after pass", id)
	return handoff.Record{}
}

// TestHandoffPassFulfillsOwnPendingRecordAtCurrentStation is brief step-1
// case (a): a pending record held by this agent at the station it's
// currently docked at is withdrawn from storage and gifted in full, then
// marked done with MovedQty == Qty.
func TestHandoffPassFulfillsOwnPendingRecordAtCurrentStation(t *testing.T) {
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "hauler-9", "Hauler Nine")

	client := newHandoffTestClient(100, true, "station-a", map[string]float64{"copper_piping": 20})
	d := NewWorkerDispatch(client, nil, nil, nil)
	d.AgentID = "craftsman-2"
	d.AgentsDir = agentsDir

	q := newHandoffTestQueue(t)
	rec := handoff.Record{
		ID: "plan1/node1", Holder: "craftsman-2", Station: "station-a",
		ItemID: "copper_piping", Qty: 8, Recipient: "hauler-9",
	}
	if _, err := q.Enqueue(rec); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if err := d.HandoffPass(context.Background(), q); err != nil {
		t.Fatalf("HandoffPass: %v", err)
	}

	if len(client.giftCalls) != 1 {
		t.Fatalf("SendGift calls = %d, want 1 (%+v)", len(client.giftCalls), client.giftCalls)
	}
	got := client.giftCalls[0]
	if got["item_id"] != "copper_piping" || got["quantity"] != float64(8) || got["recipient"] != "Hauler Nine" || got["source"] != "storage" {
		t.Fatalf("SendGift payload = %+v, want item_id=copper_piping quantity=8 recipient=Hauler Nine source=storage", got)
	}
	for _, c := range client.calls {
		if strings.HasPrefix(c, "withdraw:") {
			t.Fatalf("a storage gift needs no withdraw, got calls %v", client.calls)
		}
	}

	got2 := findHandoffRecord(t, q, "plan1/node1")
	if got2.Status != handoff.StatusDone {
		t.Fatalf("record status = %q, want done", got2.Status)
	}
	if got2.MovedQty != 8 {
		t.Fatalf("record MovedQty = %d, want 8", got2.MovedQty)
	}
}

// TestHandoffPassLeavesOtherHoldersRecordUntouched is brief step-1 case (b):
// a record held by a different agent must not be touched even though it
// sits at the station this worker is docked at.
func TestHandoffPassLeavesOtherHoldersRecordUntouched(t *testing.T) {
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "hauler-9", "Hauler Nine")

	client := newHandoffTestClient(100, true, "station-a", map[string]float64{"copper_piping": 20})
	d := NewWorkerDispatch(client, nil, nil, nil)
	d.AgentID = "craftsman-2"
	d.AgentsDir = agentsDir

	q := newHandoffTestQueue(t)
	rec := handoff.Record{
		ID: "plan1/node1", Holder: "someone-else", Station: "station-a",
		ItemID: "copper_piping", Qty: 8, Recipient: "hauler-9",
	}
	if _, err := q.Enqueue(rec); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if err := d.HandoffPass(context.Background(), q); err != nil {
		t.Fatalf("HandoffPass: %v", err)
	}

	if len(client.giftCalls) != 0 || len(client.calls) != 0 {
		t.Fatalf("expected no calls for another holder's record, got calls=%+v gifts=%+v", client.calls, client.giftCalls)
	}
	got := findHandoffRecord(t, q, "plan1/node1")
	if got.Status != handoff.StatusPending {
		t.Fatalf("record status = %q, want still pending", got.Status)
	}
}

// TestHandoffPassLeavesRecordsAtOtherStationsUntouched is brief step-1 case
// (c): a record this agent holds, but staged at a station other than the
// one it's currently docked at, must be left pending untouched.
func TestHandoffPassLeavesRecordsAtOtherStationsUntouched(t *testing.T) {
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "hauler-9", "Hauler Nine")

	client := newHandoffTestClient(100, true, "station-a", map[string]float64{"copper_piping": 20})
	d := NewWorkerDispatch(client, nil, nil, nil)
	d.AgentID = "craftsman-2"
	d.AgentsDir = agentsDir

	q := newHandoffTestQueue(t)
	rec := handoff.Record{
		ID: "plan1/node1", Holder: "craftsman-2", Station: "station-b",
		ItemID: "copper_piping", Qty: 8, Recipient: "hauler-9",
	}
	if _, err := q.Enqueue(rec); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if err := d.HandoffPass(context.Background(), q); err != nil {
		t.Fatalf("HandoffPass: %v", err)
	}

	if len(client.giftCalls) != 0 || len(client.calls) != 0 {
		t.Fatalf("expected no calls for a record at another station, got calls=%+v gifts=%+v", client.calls, client.giftCalls)
	}
	got := findHandoffRecord(t, q, "plan1/node1")
	if got.Status != handoff.StatusPending {
		t.Fatalf("record status = %q, want still pending", got.Status)
	}
}

// TestHandoffPassShortStorageMarksDoneWithPartialMovedQty is brief step-1
// case (d): storage only has 3 of the requested 8 — the withdraw comes up
// short, and that's a definitive outcome (no more will show up later), so
// the record is marked done with MovedQty = 3, not left pending forever.
func TestHandoffPassShortStorageMarksDoneWithPartialMovedQty(t *testing.T) {
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "hauler-9", "Hauler Nine")

	client := newHandoffTestClient(100, true, "station-a", map[string]float64{"copper_piping": 3})
	d := NewWorkerDispatch(client, nil, nil, nil)
	d.AgentID = "craftsman-2"
	d.AgentsDir = agentsDir

	q := newHandoffTestQueue(t)
	rec := handoff.Record{
		ID: "plan1/node1", Holder: "craftsman-2", Station: "station-a",
		ItemID: "copper_piping", Qty: 8, Recipient: "hauler-9",
	}
	if _, err := q.Enqueue(rec); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if err := d.HandoffPass(context.Background(), q); err != nil {
		t.Fatalf("HandoffPass: %v", err)
	}

	if len(client.giftCalls) != 1 {
		t.Fatalf("SendGift calls = %d, want 1 (%+v)", len(client.giftCalls), client.giftCalls)
	}
	if got := client.giftCalls[0]["quantity"]; got != float64(3) {
		t.Fatalf("gift quantity = %v, want 3", got)
	}

	got := findHandoffRecord(t, q, "plan1/node1")
	if got.Status != handoff.StatusDone {
		t.Fatalf("record status = %q, want done", got.Status)
	}
	if got.MovedQty != 3 {
		t.Fatalf("record MovedQty = %d, want 3", got.MovedQty)
	}
}

// TestHandoffPassEmptyStorageFailsRecordForReplan (I2a): a record whose holder
// storage is empty moves NOTHING. That must not be reported as a done handoff
// with MovedQty=0 (which would let a downstream craft proceed blaming the wrong
// node) — the record transitions to failed with a replan-flavored error so the
// runner parks the node needs-operator.
func TestHandoffPassEmptyStorageFailsRecordForReplan(t *testing.T) {
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "hauler-9", "Hauler Nine")

	client := newHandoffTestClient(100, true, "station-a", map[string]float64{"copper_piping": 0})
	d := NewWorkerDispatch(client, nil, nil, nil)
	d.AgentID = "craftsman-2"
	d.AgentsDir = agentsDir

	q := newHandoffTestQueue(t)
	rec := handoff.Record{
		ID: "plan1/node1", Holder: "craftsman-2", Station: "station-a",
		ItemID: "copper_piping", Qty: 8, Recipient: "hauler-9",
	}
	if _, err := q.Enqueue(rec); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if err := d.HandoffPass(context.Background(), q); err != nil {
		t.Fatalf("HandoffPass: %v", err)
	}

	if len(client.giftCalls) != 0 {
		t.Fatalf("SendGift must not be called when storage is empty, got %+v", client.giftCalls)
	}
	got := findHandoffRecord(t, q, "plan1/node1")
	if got.Status != handoff.StatusFailed {
		t.Fatalf("record status = %q, want failed (nothing moved must not fake-done)", got.Status)
	}
	if !strings.Contains(strings.ToLower(got.Error), "replan") {
		t.Fatalf("record Error = %q, want a replan-flavored message", got.Error)
	}
}

// TestHandoffPassRecipientIsHolderFailsRecord covers controller decision 2:
// Recipient == the holder's own agent id is an invalid plan state — the
// record must transition to failed with a clear error, never fake-done, and
// nothing should be withdrawn or gifted.
func TestHandoffPassRecipientIsHolderFailsRecord(t *testing.T) {
	agentsDir := t.TempDir()

	client := newHandoffTestClient(100, true, "station-a", map[string]float64{"copper_piping": 20})
	d := NewWorkerDispatch(client, nil, nil, nil)
	d.AgentID = "craftsman-2"
	d.AgentsDir = agentsDir

	q := newHandoffTestQueue(t)
	rec := handoff.Record{
		ID: "plan1/node1", Holder: "craftsman-2", Station: "station-a",
		ItemID: "copper_piping", Qty: 8, Recipient: "craftsman-2",
	}
	if _, err := q.Enqueue(rec); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if err := d.HandoffPass(context.Background(), q); err != nil {
		t.Fatalf("HandoffPass: %v", err)
	}

	if len(client.giftCalls) != 0 || len(client.calls) != 0 {
		t.Fatalf("expected no withdraw/gift calls for a self-recipient record, got calls=%+v gifts=%+v", client.calls, client.giftCalls)
	}
	got := findHandoffRecord(t, q, "plan1/node1")
	if got.Status != handoff.StatusFailed {
		t.Fatalf("record status = %q, want failed", got.Status)
	}
	if got.Error == "" {
		t.Fatal("expected a non-empty Error message on the failed record")
	}
}

// TestHandoffPassBadRecipientCredentialsFailsRecord covers controller
// decision 1: resolving Recipient (an agent id) to a username fails when
// credentials.json is missing — the record must transition pending->failed
// with the resolution error, not retry forever, and nothing should be
// withdrawn first.
func TestHandoffPassBadRecipientCredentialsFailsRecord(t *testing.T) {
	agentsDir := t.TempDir() // no credentials.json for "ghost-9"

	client := newHandoffTestClient(100, true, "station-a", map[string]float64{"copper_piping": 20})
	d := NewWorkerDispatch(client, nil, nil, nil)
	d.AgentID = "craftsman-2"
	d.AgentsDir = agentsDir

	q := newHandoffTestQueue(t)
	rec := handoff.Record{
		ID: "plan1/node1", Holder: "craftsman-2", Station: "station-a",
		ItemID: "copper_piping", Qty: 8, Recipient: "ghost-9",
	}
	if _, err := q.Enqueue(rec); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if err := d.HandoffPass(context.Background(), q); err != nil {
		t.Fatalf("HandoffPass: %v", err)
	}

	for _, c := range client.calls {
		if strings.HasPrefix(c, "withdraw:") {
			t.Fatalf("must not withdraw before the recipient resolves, got call %q", c)
		}
	}
	if len(client.giftCalls) != 0 {
		t.Fatalf("SendGift must not be called for an unresolvable recipient, got %+v", client.giftCalls)
	}
	got := findHandoffRecord(t, q, "plan1/node1")
	if got.Status != handoff.StatusFailed {
		t.Fatalf("record status = %q, want failed", got.Status)
	}
	if !strings.Contains(got.Error, "ghost-9") {
		t.Fatalf("record Error = %q, want it to mention the recipient %q", got.Error, "ghost-9")
	}
}

// TestHandoffPassNotDockedIsNoop covers controller decision 3: a worker that
// isn't currently docked anywhere must not touch any record — even one it
// otherwise holds — and the pass returns nil rather than erroring.
func TestHandoffPassNotDockedIsNoop(t *testing.T) {
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "hauler-9", "Hauler Nine")

	client := newHandoffTestClient(100, false, "", map[string]float64{"copper_piping": 20})
	d := NewWorkerDispatch(client, nil, nil, nil)
	d.AgentID = "craftsman-2"
	d.AgentsDir = agentsDir

	q := newHandoffTestQueue(t)
	rec := handoff.Record{
		ID: "plan1/node1", Holder: "craftsman-2", Station: "station-a",
		ItemID: "copper_piping", Qty: 8, Recipient: "hauler-9",
	}
	if _, err := q.Enqueue(rec); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if err := d.HandoffPass(context.Background(), q); err != nil {
		t.Fatalf("HandoffPass: %v", err)
	}

	if len(client.giftCalls) != 0 || len(client.calls) != 0 {
		t.Fatalf("expected no calls when not docked, got calls=%+v gifts=%+v", client.calls, client.giftCalls)
	}
	got := findHandoffRecord(t, q, "plan1/node1")
	if got.Status != handoff.StatusPending {
		t.Fatalf("record status = %q, want still pending", got.Status)
	}
}

// TestHandoffPassProcessesAllMatchingRecordsInOnePass covers controller
// decision 5: multiple pending records for this agent at this station are
// all processed in a single call — one failing (self-recipient) must not
// stop the others from being fulfilled.
func TestHandoffPassProcessesAllMatchingRecordsInOnePass(t *testing.T) {
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "hauler-9", "Hauler Nine")

	client := newHandoffTestClient(100, true, "station-a", map[string]float64{
		"copper_piping": 20,
		"pressure_seal": 20,
	})
	d := NewWorkerDispatch(client, nil, nil, nil)
	d.AgentID = "craftsman-2"
	d.AgentsDir = agentsDir

	q := newHandoffTestQueue(t)
	recs := []handoff.Record{
		{ID: "plan1/node1", Holder: "craftsman-2", Station: "station-a", ItemID: "copper_piping", Qty: 5, Recipient: "hauler-9"},
		{ID: "plan1/node2", Holder: "craftsman-2", Station: "station-a", ItemID: "pressure_seal", Qty: 4, Recipient: "craftsman-2"}, // invalid: self
	}
	for _, r := range recs {
		if _, err := q.Enqueue(r); err != nil {
			t.Fatalf("Enqueue: %v", err)
		}
	}

	if err := d.HandoffPass(context.Background(), q); err != nil {
		t.Fatalf("HandoffPass: %v", err)
	}

	if len(client.giftCalls) != 1 {
		t.Fatalf("SendGift calls = %d, want 1 (%+v)", len(client.giftCalls), client.giftCalls)
	}

	done := findHandoffRecord(t, q, "plan1/node1")
	if done.Status != handoff.StatusDone || done.MovedQty != 5 {
		t.Fatalf("plan1/node1 = %+v, want done MovedQty=5", done)
	}
	failed := findHandoffRecord(t, q, "plan1/node2")
	if failed.Status != handoff.StatusFailed {
		t.Fatalf("plan1/node2 status = %q, want failed", failed.Status)
	}
}

// TestHandoffPassTransientGiftFailureLeavesPendingThenResumes: a transient
// SendGift error moves nothing, so the record stays pending with MovedQty 0,
// and the next pass gifts the full quantity exactly once.
func TestHandoffPassTransientGiftFailureLeavesPendingThenResumes(t *testing.T) {
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "hauler-9", "Hauler Nine")

	client := newHandoffTestClient(5, true, "station-a", map[string]float64{"copper_piping": 20})
	client.giftErrOnAttempt = 1
	client.giftErr = errors.New("transient: connection reset")
	d := NewWorkerDispatch(client, nil, nil, nil)
	d.AgentID = "craftsman-2"
	d.AgentsDir = agentsDir

	q := newHandoffTestQueue(t)
	rec := handoff.Record{
		ID: "plan1/node1", Holder: "craftsman-2", Station: "station-a",
		ItemID: "copper_piping", Qty: 8, Recipient: "hauler-9",
	}
	if _, err := q.Enqueue(rec); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if err := d.HandoffPass(context.Background(), q); err != nil {
		t.Fatalf("HandoffPass (pass 1): %v", err)
	}
	mid := findHandoffRecord(t, q, "plan1/node1")
	if mid.Status != handoff.StatusPending || mid.MovedQty != 0 {
		t.Fatalf("record after failed gift = %+v, want pending MovedQty=0", mid)
	}

	if err := d.HandoffPass(context.Background(), q); err != nil {
		t.Fatalf("HandoffPass (pass 2): %v", err)
	}
	if len(client.giftCalls) != 1 || client.giftCalls[0]["quantity"] != float64(8) {
		t.Fatalf("landed gifts = %+v, want exactly one of 8", client.giftCalls)
	}
	final := findHandoffRecord(t, q, "plan1/node1")
	if final.Status != handoff.StatusDone || final.MovedQty != 8 {
		t.Fatalf("final record = %+v, want done MovedQty=8", final)
	}
}

// TestHandoffPassIgnoresCargoSpace is the 2026-10-22 regression: a 2,837-unit
// record of a size-2 ore against a 100-capacity hold failed as "holder storage
// empty" because the pass withdrew a cargo-sized batch first. A storage gift
// never enters the hold, so a full hold must not limit or fail it.
func TestHandoffPassIgnoresCargoSpace(t *testing.T) {
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "craftsman-1", "Arthur 'Artificer' Artis")

	client := newHandoffTestClient(100, true, "voss_redoubt_station", map[string]float64{"adamantite_ore": 5675})
	client.state.Ship.CargoUsed = 100
	d := NewWorkerDispatch(client, nil, nil, nil)
	d.AgentID = "marketbot_alhena"
	d.AgentsDir = agentsDir

	q := newHandoffTestQueue(t)
	rec := handoff.Record{
		ID: "manual/half", Holder: "marketbot_alhena", Station: "voss_redoubt_station",
		ItemID: "adamantite_ore", Qty: 2837, Recipient: "craftsman-1",
	}
	if _, err := q.Enqueue(rec); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if err := d.HandoffPass(context.Background(), q); err != nil {
		t.Fatalf("HandoffPass: %v", err)
	}
	if len(client.giftCalls) != 1 || client.giftCalls[0]["quantity"] != float64(2837) {
		t.Fatalf("gifts = %+v, want one storage gift of 2837", client.giftCalls)
	}
	got := findHandoffRecord(t, q, "manual/half")
	if got.Status != handoff.StatusDone || got.MovedQty != 2837 {
		t.Fatalf("record = %+v, want done MovedQty=2837", got)
	}
}

// TestHandoffPassUnreadableStorageLeavesPending: a failed or undecodable
// view_storage is no information, not an empty storage. The record must stay
// pending, never fail as "storage empty", and nothing may be gifted.
func TestHandoffPassUnreadableStorageLeavesPending(t *testing.T) {
	for name, setup := range map[string]func(*deliverFakeClient){
		"view_storage error": func(c *deliverFakeClient) { c.viewStorageErr = errors.New("timeout") },
		"no storage frame":   func(c *deliverFakeClient) { c.storageStock = nil },
	} {
		t.Run(name, func(t *testing.T) {
			agentsDir := t.TempDir()
			writeDeliverCreds(t, agentsDir, "hauler-9", "Hauler Nine")

			client := newHandoffTestClient(100, true, "station-a", map[string]float64{"copper_piping": 20})
			setup(client)
			d := NewWorkerDispatch(client, nil, nil, nil)
			d.AgentID = "craftsman-2"
			d.AgentsDir = agentsDir

			q := newHandoffTestQueue(t)
			rec := handoff.Record{
				ID: "plan1/node1", Holder: "craftsman-2", Station: "station-a",
				ItemID: "copper_piping", Qty: 8, Recipient: "hauler-9",
			}
			if _, err := q.Enqueue(rec); err != nil {
				t.Fatalf("Enqueue: %v", err)
			}
			if err := d.HandoffPass(context.Background(), q); err != nil {
				t.Fatalf("HandoffPass: %v", err)
			}
			if len(client.giftCalls) != 0 {
				t.Fatalf("gifted %+v with no storage reading", client.giftCalls)
			}
			if got := findHandoffRecord(t, q, "plan1/node1"); got.Status != handoff.StatusPending {
				t.Fatalf("record = %+v, want still pending", got)
			}
		})
	}
}

// TestHandoffPassPreservesConcurrentMovedQtyUpdateAsDelta is the regression
// test for the stale-snapshot bug: moveHandoffStock must persist progress as
// a DELTA against the live record under lock (r.MovedQty += this batch's
// qty), never a precomputed cumulative (baseline-at-pickup + moved). A
// second writer bumps MovedQty on the same on-disk record — via a second
// *handoff.Queue handle on the same file, triggered from inside the fake
// client's SendGift hook, i.e. strictly between this pass's pickup (List in
// HandoffPass) and its own progress persist — while this pass is gifting a
// single batch. If the fix uses a delta, the concurrent writer's +2 and this
// pass's own +8 both land: final MovedQty == 10. The old cumulative-assign
// code would clobber the concurrent +2, leaving MovedQty == 8.
func TestHandoffPassPreservesConcurrentMovedQtyUpdateAsDelta(t *testing.T) {
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "hauler-9", "Hauler Nine")

	queuePath := filepath.Join(t.TempDir(), "handoff.json")
	q := handoff.NewQueue(queuePath)

	client := newHandoffTestClient(100, true, "station-a", map[string]float64{"copper_piping": 20})
	client.hookOnAttempt = 1
	client.hook = func() {
		q2 := handoff.NewQueue(queuePath)
		ok, err := q2.Transition("plan1/node1", handoff.StatusPending, handoff.StatusPending, func(r *handoff.Record) {
			r.MovedQty += 2
		})
		if err != nil || !ok {
			t.Fatalf("concurrent writer transition: ok=%v err=%v", ok, err)
		}
	}

	d := NewWorkerDispatch(client, nil, nil, nil)
	d.AgentID = "craftsman-2"
	d.AgentsDir = agentsDir

	rec := handoff.Record{
		ID: "plan1/node1", Holder: "craftsman-2", Station: "station-a",
		ItemID: "copper_piping", Qty: 8, Recipient: "hauler-9",
	}
	if _, err := q.Enqueue(rec); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if err := d.HandoffPass(context.Background(), q); err != nil {
		t.Fatalf("HandoffPass: %v", err)
	}

	if len(client.giftCalls) != 1 {
		t.Fatalf("SendGift calls = %d, want 1 (%+v)", len(client.giftCalls), client.giftCalls)
	}
	if got := client.giftCalls[0]["quantity"]; got != float64(8) {
		t.Fatalf("gift quantity = %v, want 8", got)
	}

	got := findHandoffRecord(t, q, "plan1/node1")
	if got.Status != handoff.StatusDone {
		t.Fatalf("record status = %q, want done", got.Status)
	}
	if got.MovedQty != 10 {
		t.Fatalf("record MovedQty = %d, want 10 (concurrent +2 plus this pass's +8, delta semantics)", got.MovedQty)
	}
}

// TestHandoffPassLeavesForeignStatusWhenRecordLeavesPendingMidGift: if
// another actor moves the record out of pending while the gift is in flight,
// the gift still lands but the pass must not overwrite that actor's status.
func TestHandoffPassLeavesForeignStatusWhenRecordLeavesPendingMidGift(t *testing.T) {
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "hauler-9", "Hauler Nine")

	queuePath := filepath.Join(t.TempDir(), "handoff.json")
	q := handoff.NewQueue(queuePath)

	client := newHandoffTestClient(3, true, "station-a", map[string]float64{"copper_piping": 20})
	client.hookOnAttempt = 1
	client.hook = func() {
		q2 := handoff.NewQueue(queuePath)
		ok, err := q2.Transition("plan1/node1", handoff.StatusPending, handoff.StatusFailed, func(r *handoff.Record) {
			r.Error = "concurrently failed by another actor"
		})
		if err != nil || !ok {
			t.Fatalf("concurrent writer transition: ok=%v err=%v", ok, err)
		}
	}

	d := NewWorkerDispatch(client, nil, nil, nil)
	d.AgentID = "craftsman-2"
	d.AgentsDir = agentsDir

	rec := handoff.Record{
		ID: "plan1/node1", Holder: "craftsman-2", Station: "station-a",
		ItemID: "copper_piping", Qty: 8, Recipient: "hauler-9",
	}
	if _, err := q.Enqueue(rec); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if err := d.HandoffPass(context.Background(), q); err != nil {
		t.Fatalf("HandoffPass: %v", err)
	}

	if len(client.giftCalls) != 1 {
		t.Fatalf("SendGift calls = %d, want 1 (%+v)", len(client.giftCalls), client.giftCalls)
	}
	got := findHandoffRecord(t, q, "plan1/node1")
	if got.Status != handoff.StatusFailed || got.Error != "concurrently failed by another actor" {
		t.Fatalf("record = %+v, want the concurrent actor's failed status untouched", got)
	}
}

// TestHandoffPassEscalatesToFailedWhenPersistErrorsAfterSuccessfulGift: a
// real error persisting progress AFTER the gift landed must not leave the
// record pending with a stale MovedQty (the next pass would re-gift). It
// escalates to failed, naming the gifted-but-unpersisted quantity.
func TestHandoffPassEscalatesToFailedWhenPersistErrorsAfterSuccessfulGift(t *testing.T) {
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "hauler-9", "Hauler Nine")

	client := newHandoffTestClient(3, true, "station-a", map[string]float64{"copper_piping": 20})
	d := NewWorkerDispatch(client, nil, nil, nil)
	d.AgentID = "craftsman-2"
	d.AgentsDir = agentsDir

	persistCalls := 0
	injectedErr := errors.New("disk write failed")
	d.handoffPersist = func(q *handoff.Queue, id string, from, to handoff.Status, mutate func(*handoff.Record)) (bool, error) {
		persistCalls++
		if persistCalls == 1 {
			// The progress-persist call fails with a real error after the
			// gift already landed.
			return false, injectedErr
		}
		return q.Transition(id, from, to, mutate)
	}

	q := newHandoffTestQueue(t)
	rec := handoff.Record{
		ID: "plan1/node1", Holder: "craftsman-2", Station: "station-a",
		ItemID: "copper_piping", Qty: 8, Recipient: "hauler-9",
	}
	if _, err := q.Enqueue(rec); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if err := d.HandoffPass(context.Background(), q); err != nil {
		t.Fatalf("HandoffPass: %v", err)
	}

	if len(client.giftCalls) != 1 {
		t.Fatalf("SendGift calls = %d, want 1 (%+v)", len(client.giftCalls), client.giftCalls)
	}
	if got := client.giftCalls[0]["quantity"]; got != float64(8) {
		t.Fatalf("gift quantity = %v, want 8", got)
	}

	got := findHandoffRecord(t, q, "plan1/node1")
	if got.Status != handoff.StatusFailed {
		t.Fatalf("record status = %q, want failed", got.Status)
	}
	if !strings.Contains(got.Error, "8") {
		t.Fatalf("record Error = %q, want it to name the gifted-but-unpersisted quantity (8)", got.Error)
	}
	if got.MovedQty != 8 {
		t.Fatalf("record MovedQty = %d, want 8 (recorded via the failed-transition's own mutate)", got.MovedQty)
	}
}

// TestHandoffPassLogsCriticalWhenEscalationAlsoFails covers the last-resort
// branch: if even the escalate-to-failed transition errors after a
// persist-after-gift failure, HandoffPass must not panic or silently drop
// the problem — it logs loudly via d.Out so an operator can find it.
func TestHandoffPassLogsCriticalWhenEscalationAlsoFails(t *testing.T) {
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "hauler-9", "Hauler Nine")

	client := newHandoffTestClient(100, true, "station-a", map[string]float64{"copper_piping": 20})
	var out strings.Builder
	d := NewWorkerDispatch(client, nil, nil, &out)
	d.AgentID = "craftsman-2"
	d.AgentsDir = agentsDir

	injectedErr := errors.New("disk write failed")
	d.handoffPersist = func(q *handoff.Queue, id string, from, to handoff.Status, mutate func(*handoff.Record)) (bool, error) {
		return false, injectedErr
	}

	q := newHandoffTestQueue(t)
	rec := handoff.Record{
		ID: "plan1/node1", Holder: "craftsman-2", Station: "station-a",
		ItemID: "copper_piping", Qty: 8, Recipient: "hauler-9",
	}
	if _, err := q.Enqueue(rec); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if err := d.HandoffPass(context.Background(), q); err != nil {
		t.Fatalf("HandoffPass: %v", err)
	}

	if len(client.giftCalls) != 1 {
		t.Fatalf("SendGift calls = %d, want 1 (%+v)", len(client.giftCalls), client.giftCalls)
	}
	if !strings.Contains(out.String(), "CRITICAL") {
		t.Fatalf("d.Out = %q, want it to contain a CRITICAL log line when the escalation itself also fails", out.String())
	}
}
