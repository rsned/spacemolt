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
