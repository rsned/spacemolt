package market

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreAndGetLatestAnalysis(t *testing.T) {
	c, err := Open(Config{DBPath: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	ctx := context.Background()

	if got, err := c.GetLatestAnalysis(ctx, "stn1"); err != nil || got != nil {
		t.Fatalf("empty GetLatestAnalysis = (%v, %v), want (nil, nil)", got, err)
	}

	older := MarketAnalysis{
		StationID: "stn1", SystemID: "sys1", GameTick: 100,
		CapturedAt: time.Date(2026, 6, 20, 9, 0, 0, 0, time.UTC),
		Mode:       "basic", SkillLevel: 2, ItemsScanned: 10, Hint: "old",
		TopInsights:  []map[string]any{{"item": "iron", "score": 1.0}},
		XPGained:     map[string]any{"trading": 5},
		AnalysisData: map[string]any{"k": "v"},
	}
	newer := older
	newer.CapturedAt = time.Date(2026, 6, 21, 9, 0, 0, 0, time.UTC)
	newer.Hint = "new"

	if err := c.StoreAnalysis(ctx, older); err != nil {
		t.Fatalf("StoreAnalysis older: %v", err)
	}
	if err := c.StoreAnalysis(ctx, newer); err != nil {
		t.Fatalf("StoreAnalysis newer: %v", err)
	}

	got, err := c.GetLatestAnalysis(ctx, "stn1")
	if err != nil {
		t.Fatalf("GetLatestAnalysis: %v", err)
	}
	if got == nil || got.Hint != "new" {
		t.Fatalf("expected newest analysis (hint=new), got %+v", got)
	}
	if got.SkillLevel != 2 || len(got.TopInsights) != 1 || got.AnalysisData["k"] != "v" {
		t.Errorf("fields not round-tripped: %+v", got)
	}
	// JSON round-trip turns numbers into float64; assert the nested slice/map
	// contents survive, not just their lengths.
	if got.TopInsights[0]["item"] != "iron" {
		t.Errorf("TopInsights[0][item] = %v, want iron", got.TopInsights[0]["item"])
	}
	if score, ok := got.TopInsights[0]["score"].(float64); !ok || score != 1.0 {
		t.Errorf("TopInsights[0][score] = %v, want 1.0", got.TopInsights[0]["score"])
	}
	if xp, ok := got.XPGained["trading"].(float64); !ok || xp != 5 {
		t.Errorf("XPGained[trading] = %v, want 5", got.XPGained["trading"])
	}
}
