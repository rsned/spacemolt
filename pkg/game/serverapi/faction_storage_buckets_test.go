package serverapi

import (
	"encoding/json"
	"testing"
)

// liveFactionStorageBuckets is trimmed from craftsman-1's view_faction_storage
// at Conjunction Junction on 2026-11-02; the client logged "[SERVER API
// CHANGE] New fields in \"view_faction_storage\" response not in
// ViewFactionStorageResponse: [buckets]".
const liveFactionStorageBuckets = `{"action":"view_faction_storage","base_id":"c976fa7df53c87f9616165d7d411b41a",` +
	`"buckets":[{"cap_per_item":100000,"id":"a6bc64fb3ce3ba7973fa08fe3fbaf3a3","items":[],"name":"Storage Extension","package_cap":1000}],` +
	`"credits":1352052,"faction_id":"e727c0e918d994c72db2978fe5b18edc","faction_name":"Crafting Collective","faction_tag":"CRFT",` +
	`"items":[{"item_id":"hull_plating","name":"Hull Plating","quantity":123,"size":2}]}`

func TestViewFactionStorageDecodesBuckets(t *testing.T) {
	var got ViewFactionStorageResponse
	if err := json.Unmarshal([]byte(liveFactionStorageBuckets), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Buckets) != 1 {
		t.Fatalf("buckets = %d, want 1", len(got.Buckets))
	}
	b := got.Buckets[0]
	if b.ID != "a6bc64fb3ce3ba7973fa08fe3fbaf3a3" || b.Name != "Storage Extension" || b.CapPerItem != 100000 || b.PackageCap != 1000 || len(b.Items) != 0 {
		t.Errorf("bucket = %+v", b)
	}
	if got.Credits != 1352052 || len(got.Items) != 1 {
		t.Errorf("credits=%d items=%d", got.Credits, len(got.Items))
	}
}
