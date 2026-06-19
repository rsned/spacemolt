package game

import (
	"io"
	"log"
	"testing"

	"github.com/rsned/spacemolt/internal/protocol"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
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

func TestActionResponseTypesRegistersV0389Actions(t *testing.T) {
	for _, action := range []string{"owned", "recycle", "job_list", "craft"} {
		if _, ok := actionResponseTypes[action]; !ok {
			t.Errorf("actionResponseTypes missing %q", action)
		}
	}
}

func TestOnCraftingUpdateCallbackFires(t *testing.T) {
	c := &Client{
		debugLogger:   log.New(io.Discard, "", 0),
		latestRawJSON: make(map[string][]byte),
		state:         &State{},
	}
	var got *serverapi.CraftingUpdateEvent
	c.SetOnCraftingUpdate(func(ev serverapi.CraftingUpdateEvent) {
		got = &ev
	})
	resp := protocol.Response{
		Type: protocol.TypeCraftingUpdate,
		Payload: map[string]any{
			"tick": float64(1043),
			"jobs": []any{
				map[string]any{
					"job_id": "j1", "recipe": "r", "mode": "craft",
					"venue": "Workshop", "storage": "station",
					"deposited": []any{
						map[string]any{"item_id": "steel_plate", "item_name": "Steel Plate", "quantity": float64(1)},
					},
					"runs_done": float64(1), "runs_remaining": float64(4), "completed": false,
				},
			},
		},
	}
	c.handleResponse(resp)
	if got == nil {
		t.Fatal("callback not fired")
	}
	if got.Tick != 1043 || len(got.Jobs) != 1 || got.Jobs[0].Deposited[0].ItemName != "Steel Plate" {
		t.Fatalf("bad event: %+v", got)
	}
}
