package main

import (
	"strings"
	"testing"
)

// TestFormatDeposit_StorageToFaction guards the regression where deposit_items
// to faction storage printed "from cargo into storage. 0 ... now in storage."
// regardless of the actual source/destination. The real transfer response
// carries source/destination plus dest_total and source_remaining.
func TestFormatDeposit_StorageToFaction(t *testing.T) {
	raw := []byte(`{"action":"transfer","dest_total":225,"destination":"faction","item_id":"circuit_board","quantity":200,"source":"storage","source_remaining":371}`)
	got := formatDeposit(raw)
	for _, want := range []string{
		"Transferred 200 circuit_board from storage to faction.",
		"225 now in faction.",
		"371 left in storage.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "cargo") {
		t.Errorf("must not mention cargo for a storage->faction transfer:\n%s", got)
	}
}

// TestFormatDeposit_WrappedActionResult confirms the formatter unwraps the
// {"result":{...}} envelope that arrives on the action_result frame.
func TestFormatDeposit_WrappedActionResult(t *testing.T) {
	raw := []byte(`{"command":"deposit_items","result":{"action":"transfer","dest_total":225,"destination":"faction","item_id":"circuit_board","quantity":200,"source":"storage","source_remaining":371},"tick":963585}`)
	got := formatDeposit(raw)
	if !strings.Contains(got, "from storage to faction") {
		t.Errorf("wrapped result not unwrapped:\n%s", got)
	}
}

// TestFormatDeposit_LegacyCargoToStorage covers the older response shape (no
// source/destination; storage_total + cargo_remaining), which must still render
// with the default cargo->storage labels.
func TestFormatDeposit_LegacyCargoToStorage(t *testing.T) {
	raw := []byte(`{"action":"deposit","item_id":"iron_ore","quantity":50,"storage_total":120,"cargo_remaining":10}`)
	got := formatDeposit(raw)
	for _, want := range []string{
		"Transferred 50 iron_ore from cargo to storage.",
		"120 now in storage.",
		"10 left in cargo.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

// TestFormatWithdraw_FactionToCargo confirms withdraw uses the actual source and
// the storage->cargo defaults, and reports both totals.
func TestFormatWithdraw_FactionToCargo(t *testing.T) {
	raw := []byte(`{"action":"transfer","item_id":"circuit_board","quantity":40,"source":"faction","destination":"cargo","dest_total":40,"source_remaining":185}`)
	got := formatWithdraw(raw)
	for _, want := range []string{
		"Transferred 40 circuit_board from faction to cargo.",
		"40 now in cargo.",
		"185 left in faction.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

// TestFormatWithdraw_LegacyStorageToCargo covers the older withdraw shape
// (storage_remaining + cargo_total, no source/destination).
func TestFormatWithdraw_LegacyStorageToCargo(t *testing.T) {
	raw := []byte(`{"action":"withdraw","item_id":"iron_ore","quantity":25,"storage_remaining":75,"cargo_total":25}`)
	got := formatWithdraw(raw)
	for _, want := range []string{
		"Transferred 25 iron_ore from storage to cargo.",
		"25 now in cargo.",
		"75 left in storage.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

// TestFormatItemTransfer_BadJSON returns empty so the caller falls back to the
// generic OK message rather than printing a broken line.
func TestFormatItemTransfer_BadJSON(t *testing.T) {
	if got := formatDeposit([]byte(`not json`)); got != "" {
		t.Errorf("bad JSON should render empty, got %q", got)
	}
}
