package main

import (
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/market"
)

func TestBuildHaulStats(t *testing.T) {
	windowed := []market.HaulEfficiencyRow{
		{AgentID: "a", Hauls: 2, SumProfit: 1000, SumJumps: 10}, // net 100-45 = 55
		{AgentID: "b", Hauls: 1, SumProfit: 2000, SumJumps: 10}, // net 200-45 = 155
	}
	fleet := market.HaulEfficiencyRow{Hauls: 3, SumProfit: 3000, SumJumps: 20} // gross 150, fuel 45, net 105
	lifetime := []market.HaulEfficiencyRow{
		{AgentID: "a", Hauls: 9, SumProfit: 9000, SumJumps: 90}, // avg 100
	}
	hs := buildHaulStats(windowed, fleet, lifetime, "48h", 9, 5)

	if hs.Panel.GrossPerJump != 150 || hs.Panel.FuelPerJump != 45 || hs.Panel.NetPerJump != 105 || hs.Panel.Hauls != 3 {
		t.Fatalf("panel = %+v, want gross150 fuel45 net105 hauls3", hs.Panel)
	}
	if hs.Panel.WindowLabel != "48h" {
		t.Errorf("window label = %q, want 48h", hs.Panel.WindowLabel)
	}
	if len(hs.Panel.Agents) != 2 || hs.Panel.Agents[0].AgentID != "b" || hs.Panel.Agents[1].AgentID != "a" {
		t.Fatalf("ranking = %+v, want b(155) then a(55)", hs.Panel.Agents)
	}
	if lt := hs.Lifetime["a"]; lt.Hauls != 9 || lt.Jumps != 90 || lt.AvgPerJump != 100 {
		t.Fatalf("lifetime a = %+v, want 9/90/100", lt)
	}
}

func TestWindowLabel(t *testing.T) {
	if got := windowLabel(48 * time.Hour); got != "48h" {
		t.Errorf("48h -> %q", got)
	}
	if got := windowLabel(90 * time.Minute); got != "1h30m0s" {
		t.Errorf("90m -> %q (want the Duration.String fallback)", got)
	}
}
