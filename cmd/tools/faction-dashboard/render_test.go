package main

import (
	"strings"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

func sampleView() *knowledge.FactionView {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	return &knowledge.FactionView{
		Faction: knowledge.FactionRecord{
			FactionID: "f1", Name: "Crafters Union", Tag: "CRFT",
			LeaderUsername: "boss", Treasury: 1240500, MemberCount: 2, OwnedBases: 1,
			Description: "We build things.", Charter: "Be excellent.",
			PrimaryColor: "#34d399", FoundedUTC: "2026-01-01T00:00:00Z", CapturedAt: now,
		},
		Members: []knowledge.FactionMember{
			{PlayerID: "p1", Username: "boss", Role: "Leader", IsOnline: true, CapturedAt: now},
			{PlayerID: "p2", Username: "grunt", Role: "Member", CapturedAt: now},
		},
		Storage: []knowledge.FactionStorageRow{
			{BaseID: "b1", Credits: 500, ItemCount: 1, CapturedAt: now,
				Items: []knowledge.FactionStorageItem{{ItemID: "iron_ore", Name: "Iron Ore", Quantity: 42, Size: 1}}},
		},
	}
}

func TestRenderFactionHTML(t *testing.T) {
	html, err := renderFactionHTML(sampleView())
	if err != nil {
		t.Fatalf("renderFactionHTML: %v", err)
	}
	for _, want := range []string{"CRFT", "Crafters Union", "Be excellent.", "Iron Ore", "data-tab=\"overview\"", "data-tab=\"storage\""} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered HTML missing %q", want)
		}
	}
	// User content must be escaped (no raw script injection path).
	if strings.Contains(html, "<script>alert") {
		t.Errorf("unexpected unescaped content")
	}
}

func TestRenderFactionHTMLEscaping(t *testing.T) {
	v := &knowledge.FactionView{
		Faction: knowledge.FactionRecord{
			Tag: "X", Name: "<script>alert(1)</script>", Charter: "<b>x</b>",
		},
	}
	html, err := renderFactionHTML(v)
	if err != nil {
		t.Fatalf("renderFactionHTML: %v", err)
	}
	if strings.Contains(html, "<script>alert(1)</script>") {
		t.Errorf("user content was not escaped")
	}
	if !strings.Contains(html, "&lt;script&gt;") {
		t.Errorf("expected escaped form of user content")
	}
}
