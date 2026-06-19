package game

import (
	"testing"

	"github.com/rsned/spacemolt/internal/protocol"
)

func TestCraftingUpdateTypeConstant(t *testing.T) {
	if protocol.TypeCraftingUpdate != "crafting_update" {
		t.Fatalf("TypeCraftingUpdate = %q, want %q", protocol.TypeCraftingUpdate, "crafting_update")
	}
}

func TestCraftingUpdateEventInExpectedFields(t *testing.T) {
	fields, ok := eventExpectedFields[protocol.TypeCraftingUpdate]
	if !ok {
		t.Fatal("eventExpectedFields missing crafting_update")
	}
	for _, want := range []string{"tick", "jobs"} {
		if !fields[want] {
			t.Errorf("eventExpectedFields[crafting_update] missing %q", want)
		}
	}
}
