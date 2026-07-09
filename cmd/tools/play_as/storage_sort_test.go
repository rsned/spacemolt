package main

import (
	"strings"
	"testing"
)

// TestSortStorageItems_CountDescendingTieByID verifies count sort orders by
// quantity descending with item_id as the tie-break.
func TestSortStorageItems_CountDescendingTieByID(t *testing.T) {
	items := []storageItem{
		{ItemID: "aaa", Quantity: 5},
		{ItemID: "zzz", Quantity: 100},
		{ItemID: "bbb", Quantity: 50},
		{ItemID: "abb", Quantity: 50}, // ties with bbb on qty; id decides
	}
	sortStorageItems(items, "count")
	got := []string{items[0].ItemID, items[1].ItemID, items[2].ItemID, items[3].ItemID}
	want := []string{"zzz", "abb", "bbb", "aaa"} // 100, then 50(abb<bbb), then 5
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("count sort = %v, want %v", got, want)
		}
	}
}

// TestSortStorageItems_DefaultIsIDAscending verifies the empty/id key keeps the
// historical alphabetical-by-id order.
func TestSortStorageItems_DefaultIsIDAscending(t *testing.T) {
	for _, key := range []string{"", "id", "unknown_key"} {
		items := []storageItem{
			{ItemID: "zzz", Quantity: 100},
			{ItemID: "aaa", Quantity: 5},
		}
		sortStorageItems(items, key)
		if items[0].ItemID != "aaa" || items[1].ItemID != "zzz" {
			t.Fatalf("key %q: default should be id-ascending, got %s,%s", key, items[0].ItemID, items[1].ItemID)
		}
	}
}

// TestWriteStorageFlat_SortByCount verifies the flat table renders in
// quantity-descending order under --sort=count.
func TestWriteStorageFlat_SortByCount(t *testing.T) {
	items := []storageItem{
		{ItemID: "aaa", Name: "A", Quantity: 5, Size: 1},
		{ItemID: "zzz", Name: "Z", Quantity: 100, Size: 1},
		{ItemID: "mmm", Name: "M", Quantity: 50, Size: 1},
	}
	var b strings.Builder
	writeStorageFlat(&b, items, "qty")
	out := b.String()
	iZ, iM, iA := strings.Index(out, "zzz"), strings.Index(out, "mmm"), strings.Index(out, "aaa")
	if iZ < 0 || iZ >= iM || iM >= iA {
		t.Fatalf("count sort not descending (zzz=%d mmm=%d aaa=%d):\n%s", iZ, iM, iA, out)
	}
}

// TestWriteStorageGrouped_SortByCountWithinCategory verifies the sort applies
// within a category section. Both ids are absent from the catalog so they share
// the "unknown" category.
func TestWriteStorageGrouped_SortByCountWithinCategory(t *testing.T) {
	items := []storageItem{
		{ItemID: "zzz_unlisted_small", Quantity: 3, Size: 1},
		{ItemID: "zzz_unlisted_big", Quantity: 99, Size: 1},
	}
	var b strings.Builder
	writeStorageGrouped(&b, items, "count")
	out := b.String()
	if strings.Index(out, "zzz_unlisted_big") > strings.Index(out, "zzz_unlisted_small") {
		t.Fatalf("within-category count sort not descending:\n%s", out)
	}
}
