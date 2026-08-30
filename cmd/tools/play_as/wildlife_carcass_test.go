package main

import (
	"context"
	"io"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// stubCarcassClient serves the raw caches captureCarcasses reads (the last
// get_wrecks and get_nearby replies) and the state it stamps kills with.
type stubCarcassClient struct {
	game.GameClient
	raw   map[string][]byte
	state *game.State
}

func (s stubCarcassClient) GetRawJSON(key string) []byte { return s.raw[key] }
func (s stubCarcassClient) GetState() *game.State        { return s.state }

// Live get_wrecks reply, 2026-08-30 17:10Z, after the operator killed an
// Aeonbear by hand: type creature, victim crt_…, EMPTY ship_class.
const aeonbearWrecks = `{"count":1,"wrecks":[{"cargo":[{"item_id":"exotic_cyst","quantity":1,"size":1}],
"created_at":"2026-08-30T17:10:08Z","expire_tick":1752868,"id":"28c23c6022f9e046ed34150eda43807b",
"killer_id":"a509","killer_name":"Arthur","modules":[],"poi_id":"alula_belt","salvage_value":90,
"ship_class":"","system_id":"alula","type":"creature",
"victim_id":"crt_dbb225b6785f6dce769f26dd2257696a","victim_name":"Aeonbear"}]}`

func newCarcassKB(t *testing.T) *knowledge.SQLiteKB {
	t.Helper()
	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: ":memory:"})
	if err != nil {
		t.Fatalf("NewSQLiteKB: %v", err)
	}
	t.Cleanup(func() { _ = kb.Close() })
	return kb
}

func carcassState() *game.State {
	st := &game.State{CurrentTick: 1752000, LastBattleID: "b-aeon"}
	st.System.ID = "alula"
	return st
}

// A creature wreck in the last get_wrecks reply is a kill with a readable
// carcass: the species comes from the get_nearby entry that named it, and
// the drop roll is recorded from the wreck before anything is looted.
func TestCaptureCarcasses_RecordsKillAndDropsFromNearbySpecies(t *testing.T) {
	kb := newCarcassKB(t)
	ctx := context.Background()
	client := stubCarcassClient{state: carcassState(), raw: map[string][]byte{
		"wrecks": []byte(aeonbearWrecks),
		"nearby": []byte(`{"creatures":[{"creature_id":"crt_dbb225b6785f6dce769f26dd2257696a","species":"aeonbear","name":"Aeonbear","role":"apex","max_hull":900}]}`),
	}}
	seen := map[string]bool{}
	if n := captureCarcasses(ctx, client, kb, "arthur", seen, io.Discard); n != 1 {
		t.Fatalf("captured %d carcasses, want 1", n)
	}
	if got, _ := kb.CountWildlifeCarcassesRead(ctx, "aeonbear"); got != 1 {
		t.Errorf("carcasses read for aeonbear = %d, want 1", got)
	}
	var item string
	var qty float64
	var battle, agent string
	if err := kb.DB().QueryRow(`SELECT d.item_id, d.quantity, k.battle_id, k.agent_id FROM wildlife_kill_drops d
		JOIN wildlife_kills k ON k.creature_id = d.creature_id AND k.game_tick = d.game_tick`).
		Scan(&item, &qty, &battle, &agent); err != nil {
		t.Fatalf("drop row: %v", err)
	}
	if item != "exotic_cyst" || qty != 1 || battle != "b-aeon" || agent != "arthur" {
		t.Errorf("drop = %s x%v battle %q agent %q", item, qty, battle, agent)
	}
	// Re-running `wrecks` must not count the same carcass twice.
	if n := captureCarcasses(ctx, client, kb, "arthur", seen, io.Discard); n != 0 {
		t.Errorf("second pass captured %d, want 0", n)
	}
}

// When the creature is no longer in the nearby cache (it is dead, and nearby
// was re-run), the species is inferred from the wreck's victim_name.
func TestCaptureCarcasses_InfersSpeciesFromVictimName(t *testing.T) {
	kb := newCarcassKB(t)
	ctx := context.Background()
	wrecks := `{"wrecks":[{"id":"w1","type":"creature","victim_id":"crt_1","victim_name":"Belt-Grazer","cargo":[]}]}`
	client := stubCarcassClient{state: carcassState(), raw: map[string][]byte{"wrecks": []byte(wrecks)}}
	if n := captureCarcasses(ctx, client, kb, "arthur", map[string]bool{}, io.Discard); n != 1 {
		t.Fatalf("captured %d, want 1", n)
	}
	if got, _ := kb.CountWildlifeCarcassesRead(ctx, "belt_grazer"); got != 1 {
		t.Errorf("belt_grazer carcasses read = %d, want 1", got)
	}
}

// Player and station wrecks are not kills of wildlife and are left alone.
func TestCaptureCarcasses_IgnoresNonCreatureWrecks(t *testing.T) {
	kb := newCarcassKB(t)
	wrecks := `{"wrecks":[{"id":"w2","type":"ship","ship_class":"underwriter","victim_id":"molten","victim_name":"MoltenOne","cargo":[{"item_id":"iron_ore","quantity":5}]}]}`
	client := stubCarcassClient{state: carcassState(), raw: map[string][]byte{"wrecks": []byte(wrecks)}}
	if n := captureCarcasses(context.Background(), client, kb, "arthur", map[string]bool{}, io.Discard); n != 0 {
		t.Errorf("captured %d from a player wreck, want 0", n)
	}
}

func TestSpeciesFromVictimName(t *testing.T) {
	for in, want := range map[string]string{"Aeonbear": "aeonbear", "Belt-Grazer": "belt_grazer", "Rainbow Leviathan": "rainbow_leviathan", "": ""} {
		if got := speciesFromVictimName(in); got != want {
			t.Errorf("speciesFromVictimName(%q) = %q, want %q", in, got, want)
		}
	}
}
