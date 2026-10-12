package serverapi

import (
	"encoding/json"
	"testing"
)

// v0.618 returns the shared rent aggregate on faction_list as faction_rent.
// Without the field the api monitor logs "New fields in faction_list response
// not in FactionListResponse: [faction_rent]" on every call, and the arrears
// figure that predicts a repossession is silently dropped.
func TestFactionListResponseCarriesFactionRent(t *testing.T) {
	raw := []byte(`{
      "factions": [], "total_count": 0,
      "base_id": "grand_exchange_station",
      "faction_id": "e727c0e918d994c72db2978fe5b18edc",
      "faction_rent": {
        "facilities": 10,
        "total_rent_per_cycle": 1351,
        "est_rent_per_day": 116186,
        "arrears_owed": 43886,
        "grace_cycles": 260
      }
    }`)
	var resp FactionListResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.FactionRent == nil {
		t.Fatal("FactionRent is nil, want the rent aggregate decoded")
	}
	if got, want := resp.FactionRent.TotalRentPerCycle, 1351; got != want {
		t.Errorf("TotalRentPerCycle = %d, want %d", got, want)
	}
	if got, want := resp.FactionRent.ArrearsOwed, 43886; got != want {
		t.Errorf("ArrearsOwed = %d, want %d", got, want)
	}
	if got, want := resp.FactionRent.GraceCycles, 260; got != want {
		t.Errorf("GraceCycles = %d, want %d", got, want)
	}
}
