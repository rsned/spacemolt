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
	if got["item_id"] != "copper_piping" || got["quantity"] != float64(8) || got["recipient"] != "Hauler Nine" {
		t.Fatalf("SendGift payload = %+v, want item_id=copper_piping quantity=8 recipient=Hauler Nine", got)
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

// TestHandoffPassTransientMidPassFailurePersistsProgressThenResumes is the
// regression test for the over-gift/lost-progress gap: a multi-batch record
// (cargo capacity 5, Qty 8) whose first batch gifts fine but whose second
// batch hits a transient SendGift error must leave the record pending with
// MovedQty == the first batch's size — not 0 (lost progress) — and a second
// pass must move only the remainder, never re-attempting the full Qty. Across
// both passes the total gifted must equal Qty exactly, and the record must
// end up done with the cumulative MovedQty.
func TestHandoffPassTransientMidPassFailurePersistsProgressThenResumes(t *testing.T) {
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "hauler-9", "Hauler Nine")

	client := newHandoffTestClient(5, true, "station-a", map[string]float64{"copper_piping": 20})
	client.giftErrOnAttempt = 2 // the second SendGift call across the test fails once
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

	// Pass 1: first batch (cargo-capacity-sized, 5 units) gifts fine; the
	// second batch's gift hits the injected transient error.
	if err := d.HandoffPass(context.Background(), q); err != nil {
		t.Fatalf("HandoffPass (pass 1): %v", err)
	}
	if len(client.giftCalls) != 1 {
		t.Fatalf("pass 1 SendGift calls = %d, want 1 (%+v)", len(client.giftCalls), client.giftCalls)
	}
	if got := client.giftCalls[0]["quantity"]; got != float64(5) {
		t.Fatalf("pass 1 gift quantity = %v, want 5", got)
	}
	mid := findHandoffRecord(t, q, "plan1/node1")
	if mid.Status != handoff.StatusPending {
		t.Fatalf("record status after pass 1 = %q, want still pending", mid.Status)
	}
	if mid.MovedQty != 5 {
		t.Fatalf("record MovedQty after pass 1 = %d, want 5 (first batch's progress must be persisted, not lost)", mid.MovedQty)
	}

	// Pass 2: the transient error is gone — only the remaining 3 units
	// (Qty 8 - MovedQty 5), not the full 8, must be moved.
	client.giftErrOnAttempt = 0
	if err := d.HandoffPass(context.Background(), q); err != nil {
		t.Fatalf("HandoffPass (pass 2): %v", err)
	}
	if len(client.giftCalls) != 2 {
		t.Fatalf("total SendGift calls across both passes = %d, want 2 (%+v)", len(client.giftCalls), client.giftCalls)
	}
	if got := client.giftCalls[1]["quantity"]; got != float64(3) {
		t.Fatalf("pass 2 gift quantity = %v, want 3 (remaining only — must not re-gift the full Qty)", got)
	}

	var totalGifted float64
	for _, gc := range client.giftCalls {
		qty, _ := gc["quantity"].(float64)
		totalGifted += qty
	}
	if totalGifted != 8 {
		t.Fatalf("total gifted across both passes = %v, want 8 (must not over-gift)", totalGifted)
	}

	final := findHandoffRecord(t, q, "plan1/node1")
	if final.Status != handoff.StatusDone {
		t.Fatalf("final record status = %q, want done", final.Status)
	}
	if final.MovedQty != 8 {
		t.Fatalf("final record MovedQty = %d, want 8 (cumulative across both passes)", final.MovedQty)
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

// TestHandoffPassStopsGiftingWhenRecordLeavesPendingMidPass is the
// regression test for the ignored-CAS-result bug: if the record leaves
// pending (e.g. another actor marks it failed) between two batches of the
// same pass, the pass must stop — it must not withdraw or gift a further
// batch for a record that may already be done or failed elsewhere. Cargo
// capacity 3 against Qty 8 forces three batches (3, 3, 2); a hook fires at
// the start of the SECOND SendGift call and marks the record failed via a
// second *handoff.Queue handle. That batch's own gift still lands (it was
// already in flight), but its progress-persist CAS then fails (ok=false)
// because the record is no longer pending — so the third batch must never
// be attempted.
func TestHandoffPassStopsGiftingWhenRecordLeavesPendingMidPass(t *testing.T) {
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "hauler-9", "Hauler Nine")

	queuePath := filepath.Join(t.TempDir(), "handoff.json")
	q := handoff.NewQueue(queuePath)

	client := newHandoffTestClient(3, true, "station-a", map[string]float64{"copper_piping": 20})
	client.hookOnAttempt = 2
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

	if len(client.giftCalls) != 2 {
		t.Fatalf("SendGift calls = %d, want 2 (batch 3 must never be attempted once the record left pending) (%+v)", len(client.giftCalls), client.giftCalls)
	}
	var totalGifted float64
	for _, gc := range client.giftCalls {
		qty, _ := gc["quantity"].(float64)
		totalGifted += qty
	}
	if totalGifted != 6 {
		t.Fatalf("total gifted = %v, want 6 (two 3-unit batches, no third)", totalGifted)
	}

	got := findHandoffRecord(t, q, "plan1/node1")
	if got.Status != handoff.StatusFailed {
		t.Fatalf("record status = %q, want failed (set by the concurrent actor, untouched by the stopped pass)", got.Status)
	}
}

// TestHandoffPassEscalatesToFailedWhenPersistErrorsAfterSuccessfulGift is
// the regression test for the persist-failure-after-gift bug: a real (not
// CAS-mismatch) error from the progress-persist call, occurring AFTER the
// gift already landed, must not be treated like a plain transient gift
// failure (which would leave the record pending with a stale, too-low
// MovedQty and cause a re-gift next pass). It must escalate to failed,
// naming the gifted-but-unpersisted quantity, and must not attempt any
// further batches. Cargo capacity 3 against Qty 8 would normally need three
// batches (3, 3, 2); the injected persist error on the very first
// progress-persist call must prevent batches 2 and 3 from ever being
// attempted.
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
			// The very first progress-persist call (the batch-1 pending->pending
			// CAS) fails with a real error, after batch 1's gift already landed.
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
		t.Fatalf("SendGift calls = %d, want 1 (no further batches after the persist error) (%+v)", len(client.giftCalls), client.giftCalls)
	}
	if got := client.giftCalls[0]["quantity"]; got != float64(3) {
		t.Fatalf("gift quantity = %v, want 3", got)
	}

	got := findHandoffRecord(t, q, "plan1/node1")
	if got.Status != handoff.StatusFailed {
		t.Fatalf("record status = %q, want failed", got.Status)
	}
	if !strings.Contains(got.Error, "3") {
		t.Fatalf("record Error = %q, want it to name the gifted-but-unpersisted quantity (3)", got.Error)
	}
	if got.MovedQty != 3 {
		t.Fatalf("record MovedQty = %d, want 3 (the gifted batch, recorded via the failed-transition's own mutate)", got.MovedQty)
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
