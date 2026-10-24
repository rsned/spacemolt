package worker

import (
	"bytes"
	"context"
	"slices"
	"testing"
)

func TestParsePipeline(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    *Pipeline
		wantErr bool
	}{
		{"valid", "water_ice,glenhaven,glenhaven_ice_belt,treasure_cache_trading_post,trader-1",
			&Pipeline{Item: "water_ice", SystemID: "glenhaven", POI: "glenhaven_ice_belt", To: "treasure_cache_trading_post", Recipient: "trader-1"}, false},
		{"four fields", "water_ice,glenhaven,glenhaven_ice_belt,treasure_cache_trading_post", nil, true},
		{"six fields", "a,b,c,d,e,f", nil, true},
		{"empty item", ",glenhaven,glenhaven_ice_belt,tc,trader-1", nil, true},
		{"empty recipient", "ice,glenhaven,belt,tc,", nil, true},
		{"empty string", "", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParsePipeline(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			switch {
			case tt.want == nil && got != nil:
				t.Fatalf("got %+v, want nil", got)
			case tt.want != nil && (got == nil || *got != *tt.want):
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestPipelineMineUsesFreeHold(t *testing.T) {
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "trader-1", "Trader One")
	kb := newMineAtTestKB(t, "", false)
	// Two passes of 100 and 65: only a target of 165 (175 capacity - 10 used)
	// needs both; a smaller target would stop after the first.
	client := newMineAtClient(130, 100, 65)
	client.state.Ship.CargoCapacity = 175
	client.state.Ship.CargoUsed = 10
	d := noWaitMineDispatch(client, kb, agentsDir)
	d.Out = &bytes.Buffer{}
	d.Pipeline = &Pipeline{Item: "ice", SystemID: "glenhaven", POI: "glenhaven_ice_belt", To: "to_base", Recipient: "trader-1"}

	if err := d.PipelineMine(context.Background()); err != nil {
		t.Fatalf("PipelineMine: %v", err)
	}
	mines := 0
	for _, c := range client.calls {
		if c == "mine" {
			mines++
		}
	}
	if mines != 2 {
		t.Fatalf("mine calls = %d (%v), want 2 for a 165-unit target", mines, client.calls)
	}
	if !slices.Contains(client.calls, "travel:glenhaven_ice_belt") {
		t.Fatalf("calls %v lack travel to the pinned POI", client.calls)
	}
}

func TestPipelineMineWithoutConfigErrors(t *testing.T) {
	d := &WorkerDispatch{Out: &bytes.Buffer{}}
	err := d.PipelineMine(context.Background())
	if err == nil || err.Error() != "pipeline_mine: no --pipeline configured" {
		t.Fatalf("err = %v", err)
	}
}
