package worker

import (
	"context"
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
