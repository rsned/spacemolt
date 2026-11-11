package main

import (
	"strings"
	"testing"
)

func TestFormatInteractPOI(t *testing.T) {
	raw := `{"poi_id":"receiver_housing","message":"You copy what remains of the array's storage.","data_copied":true,"items_recovered":{"signal_fragment":2}}`
	got := formatInteractPOI([]byte(raw))
	for _, want := range []string{"Interaction at receiver_housing", "copy what remains", "Data copied", "signal_fragment", "x2"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if formatInteractPOI([]byte(`{}`)) != "" {
		t.Error("empty reply should fall back to raw JSON")
	}
}

// Live reply, 2026-11-11, Receiver Housing in The Sanctum (action_result
// wrapper with command/result/tick).
func TestFormatInteractPOILiveReceiverHousing(t *testing.T) {
	raw := `{"command":"interact_poi","result":{"data_copied":true,"message":"Something is receiving this. / What it is, it has not yet decided.\n\nBefore you heard this / what were you listening with","poi_id":"receiver_housing"},"tick":2380223}`
	got := formatInteractPOI([]byte(raw))
	for _, want := range []string{"Interaction at receiver_housing", "Something is receiving this.", "Data copied"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}
