package mbox

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadBlocklistMissingFileIsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spam_list.json")

	bl, err := LoadBlocklist(path)
	if err != nil {
		t.Fatalf("LoadBlocklist(missing): %v", err)
	}
	if got := bl.List(); len(got) != 0 {
		t.Fatalf("List() = %v, want empty", got)
	}
}

func TestBlocklistAddPersistsJSONArray(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spam_list.json")
	bl, err := LoadBlocklist(path)
	if err != nil {
		t.Fatalf("LoadBlocklist: %v", err)
	}

	added, err := bl.Add("storgio17")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if !added {
		t.Fatal("Add(new): want added=true, got false")
	}

	// File on disk is a JSON array of strings.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var arr []string
	if err := json.Unmarshal(raw, &arr); err != nil {
		t.Fatalf("unmarshal %q: %v", raw, err)
	}
	if len(arr) != 1 || arr[0] != "storgio17" {
		t.Fatalf("persisted array = %v, want [storgio17]", arr)
	}

	// A fresh load sees the persisted entry.
	bl2, err := LoadBlocklist(path)
	if err != nil {
		t.Fatalf("LoadBlocklist(reopen): %v", err)
	}
	if !bl2.IsBlocked("", "storgio17") {
		t.Fatal("reopened blocklist did not contain storgio17")
	}
}

func TestBlocklistAddDuplicateIsNoOp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spam_list.json")
	bl, _ := LoadBlocklist(path)

	if _, err := bl.Add("storgio17"); err != nil {
		t.Fatalf("Add first: %v", err)
	}
	added, err := bl.Add("STORGIO17") // case-insensitive duplicate
	if err != nil {
		t.Fatalf("Add dup: %v", err)
	}
	if added {
		t.Fatal("Add(case-insensitive duplicate): want added=false, got true")
	}
	if got := bl.List(); len(got) != 1 {
		t.Fatalf("List() = %v, want a single entry", got)
	}
}

func TestBlocklistIsBlockedMatchesIDOrNameCaseInsensitive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spam_list.json")
	bl, _ := LoadBlocklist(path)
	if _, err := bl.Add("17a08149befb15b51a1fcf8bca325c36"); err != nil {
		t.Fatalf("Add id: %v", err)
	}
	if _, err := bl.Add("storgio17"); err != nil {
		t.Fatalf("Add name: %v", err)
	}

	cases := []struct {
		name           string
		senderID       string
		sender         string
		wantBlocked    bool
	}{
		{"match by id", "17a08149befb15b51a1fcf8bca325c36", "Federation Customs I", true},
		{"match by id uppercased", "17A08149BEFB15B51A1FCF8BCA325C36", "Federation Customs I", true},
		{"match by name", "some-other-id", "storgio17", true},
		{"match by name different case", "some-other-id", "Storgio17", true},
		{"no match", "unknown-id", "Buddy27", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := bl.IsBlocked(tc.senderID, tc.sender); got != tc.wantBlocked {
				t.Errorf("IsBlocked(%q, %q) = %v, want %v", tc.senderID, tc.sender, got, tc.wantBlocked)
			}
		})
	}
}

func TestBlocklistRemove(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spam_list.json")
	bl, _ := LoadBlocklist(path)
	if _, err := bl.Add("storgio17"); err != nil {
		t.Fatalf("Add: %v", err)
	}

	removed, err := bl.Remove("STORGIO17") // case-insensitive
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if !removed {
		t.Fatal("Remove(present): want removed=true, got false")
	}
	if bl.IsBlocked("", "storgio17") {
		t.Fatal("entry still blocked after Remove")
	}

	removed, err = bl.Remove("storgio17")
	if err != nil {
		t.Fatalf("Remove absent: %v", err)
	}
	if removed {
		t.Fatal("Remove(absent): want removed=false, got true")
	}
}
