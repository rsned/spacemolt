package game

import (
	"log"
	"strings"
	"testing"
)

func TestFormatPlayerDied(t *testing.T) {
	p := map[string]any{"cause": "pirate", "killer_name": "Rustjaw", "ship_lost": "Wayfarer",
		"insurance_payout": float64(12000), "clone_cost": float64(500)}
	w := WreckSite{SystemID: "oxbow", SystemName: "Oxbow", POIID: "oxbow_belt_b", POIName: "Oxbow Belt B", RespawnBase: "krynn_station"}

	got := formatPlayerDied(p, w)
	want := "💀 SHIP DESTROYED cause=pirate killer=Rustjaw ship=Wayfarer wreck=Oxbow Belt B (Oxbow) respawn=krynn_station insurance=12000 clone_cost=500"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	if got := formatPlayerDied(map[string]any{}, WreckSite{}); got != "💀 SHIP DESTROYED" {
		t.Errorf("empty payload: %q", got)
	}
}

// Event lines carry the agent tag of the client's logger, or a death in a
// shared fleet log names no worker (salvager-5, 2026-11-11).
func TestEventLoggerKeepsAgentPrefix(t *testing.T) {
	var b strings.Builder
	c := NewClient("ws://unused", "u", "p", log.New(&b, "[salvager-5-GAME] ", 0))
	c.logEvent("💀 SHIP DESTROYED cause=wildlife")
	if !strings.HasPrefix(b.String(), "[salvager-5-GAME] ") {
		t.Errorf("event line %q lost the agent prefix", b.String())
	}
}
