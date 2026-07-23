package ovdash

import "testing"

func TestClassifyTiers(t *testing.T) {
	const cur = "v0.3.0"
	cases := []struct {
		name      string
		ver       string
		codeDirty bool
		want      Tier
	}{
		{"exact match clean is green", "v0.3.0", false, TierGreen},
		{"exact match code-dirty is yellow", "v0.3.0", true, TierYellow},
		{"same minor patch-behind is yellow", "v0.3.0-2-g8016cd8", false, TierYellow},
		{"minor behind is red", "v0.2.9", false, TierRed},
		{"minor ahead is red", "v0.4.0", false, TierRed},
		{"legacy empty is red", "", false, TierRed},
		{"dev unparseable is red", "dev", false, TierRed},
	}
	for _, c := range cases {
		if got := Classify(c.ver, c.codeDirty, cur); got != c.want {
			t.Errorf("%s: Classify(%q,%v,%q) = %q, want %q", c.name, c.ver, c.codeDirty, cur, got, c.want)
		}
	}
}

func TestCurrentVersionPicksNewestBuiltAt(t *testing.T) {
	got := currentVersion([]buildSample{
		{Version: "v0.2.9", BuiltAt: "2026-07-22T09:00:00Z"},
		{Version: "v0.3.0", BuiltAt: "2026-07-23T10:00:00Z"},
		{Version: "v0.1.0", BuiltAt: ""},        // no build time — ignored
		{Version: "bad", BuiltAt: "not-a-time"}, // unparseable — ignored
	})
	if got != "v0.3.0" {
		t.Fatalf("current = %q, want v0.3.0 (newest built_at)", got)
	}
	if currentVersion(nil) != "" {
		t.Fatalf("empty samples must yield empty current")
	}
}

func TestWorstTier(t *testing.T) {
	if worstTier(TierGreen, TierYellow, TierGreen) != TierYellow {
		t.Fatal("green+yellow → yellow")
	}
	if worstTier(TierGreen, TierRed, TierYellow) != TierRed {
		t.Fatal("any red → red")
	}
	if worstTier() != TierGreen {
		t.Fatal("no tiers → green")
	}
}
