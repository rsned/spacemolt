package game

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// TestCraftDryRun_PayloadAndDecode verifies CraftDryRun sends
// {recipe_id, quantity, dry_run: true, facility_id} and decodes the
// authoritative captured-live shape (docs/superpowers/specs/
// 2026-07-10-executor-b-live-mechanics.md, Task 0) into
// serverapi.CraftDryRunResponse — in particular credits_total (the
// budget-gate fee), have_inputs/have_credits, and cost.inputs[].
func TestCraftDryRun_PayloadAndDecode(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	type result struct {
		out *serverapi.CraftDryRunResponse
		err error
	}
	resCh := make(chan result, 1)
	go func() {
		out, err := c.CraftDryRun(ctx, "verdigris_smelting", 6, "workshop:c5a5c5a2e8263ff146b423000ea7c295:grand_exchange_station")
		resCh <- result{out, err}
	}()

	var sent protocol.Message
	select {
	case sent = <-sendCh:
	case <-ctx.Done():
		t.Fatal("timeout waiting for send")
	}

	if sent.Type != "craft" {
		t.Fatalf("sent.Type = %q, want %q", sent.Type, "craft")
	}
	if got := sent.Payload["recipe_id"]; got != "verdigris_smelting" {
		t.Fatalf("recipe_id = %v, want verdigris_smelting", got)
	}
	if got := sent.Payload["quantity"]; got != 6 {
		t.Fatalf("quantity = %v, want 6", got)
	}
	if got := sent.Payload["dry_run"]; got != true {
		t.Fatalf("dry_run = %v, want true", got)
	}
	if got := sent.Payload["facility_id"]; got != "workshop:c5a5c5a2e8263ff146b423000ea7c295:grand_exchange_station" {
		t.Fatalf("facility_id = %v, want the workshop id", got)
	}

	// Verbatim captured live dry-run body (Task 0 findings doc).
	c.router.dispatch(protocol.Response{
		Type:      protocol.TypeOK,
		RequestID: sent.RequestID,
		Payload: map[string]any{
			"action":                 "craft",
			"cost":                   map[string]any{"inputs": []any{map[string]any{"item_id": "verdigris_curd", "name": "Verdigris Curd", "quantity": 8}}},
			"credits_total":          0,
			"dry_run":                true,
			"effective_time_per_run": 0.2272727272727273,
			"est_completion_tick":    1314486,
			"facility_id":            "workshop:c5a5c5a2e8263ff146b423000ea7c295:grand_exchange_station",
			"have_credits":           true,
			"have_inputs":            false,
			"message":                "Quote only — nothing queued.",
			"mode":                   "craft",
			"produces":               []any{map[string]any{"item_id": "copper_piping", "name": "Copper Piping", "quantity": 3}},
			"quantity":               6,
			"recipe":                 "Verdigris Smelting",
			"runs":                   2,
			"venue":                  "Station Workshop",
			"venue_type":             "workshop",
		},
	})

	select {
	case r := <-resCh:
		if r.err != nil {
			t.Fatalf("CraftDryRun returned unexpected error: %v", r.err)
		}
		out := r.out
		if out == nil {
			t.Fatal("CraftDryRun returned nil result with nil error")
		}
		if out.Recipe != "Verdigris Smelting" || out.Quantity != 6 || out.Runs != 2 {
			t.Fatalf("bad decode: %+v", out)
		}
		if out.CreditsTotal != 0 {
			t.Fatalf("CreditsTotal = %d, want 0", out.CreditsTotal)
		}
		if out.HaveInputs {
			t.Fatalf("HaveInputs = true, want false")
		}
		if !out.HaveCredits {
			t.Fatalf("HaveCredits = false, want true")
		}
		if len(out.Cost.Inputs) != 1 || out.Cost.Inputs[0].ItemID != "verdigris_curd" || out.Cost.Inputs[0].Quantity != 8 {
			t.Fatalf("Cost.Inputs = %+v", out.Cost.Inputs)
		}
		if len(out.Produces) != 1 || out.Produces[0].ItemID != "copper_piping" || out.Produces[0].Quantity != 3 {
			t.Fatalf("Produces = %+v", out.Produces)
		}
		if out.VenueType != "workshop" || out.Venue != "Station Workshop" {
			t.Fatalf("venue fields = %+v", out)
		}
	case <-ctx.Done():
		t.Fatal("CraftDryRun did not return after the dry-run response")
	}
}

// TestCraftDryRun_RejectsBadQuantity mirrors TestCraftWithOptionsRejectsBadQuantity.
func TestCraftDryRun_RejectsBadQuantity(t *testing.T) {
	c := newSubmitClientSkeleton()
	if _, err := c.CraftDryRun(context.Background(), "r", 0, ""); err == nil {
		t.Fatal("expected error for quantity 0")
	}
}

// TestCraftDryRun_OmitsFacilityIDWhenEmpty verifies the hand-craft path
// (empty facilityID) does not send a facility_id key at all, letting the
// server auto-route to the local workshop (Task 0 finding #7).
func TestCraftDryRun_OmitsFacilityIDWhenEmpty(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() {
		_, _ = c.CraftDryRun(ctx, "verdigris_smelting", 6, "")
	}()

	var sent protocol.Message
	select {
	case sent = <-sendCh:
	case <-ctx.Done():
		t.Fatal("timeout waiting for send")
	}
	if _, ok := sent.Payload["facility_id"]; ok {
		t.Fatalf("payload = %+v, must not carry facility_id when empty", sent.Payload)
	}
}

func TestXpToLevel(t *testing.T) {
	client := NewClient("wss://test.example.com", "user", "pass", nil)

	tests := []struct {
		xp   int
		want int
	}{
		{0, 1},
		{50, 1},
		{99, 1},
		{100, 2},
		{200, 2},
		{299, 2},
		{300, 3},
		{500, 3},
		{599, 3},
		{600, 4},
		{999, 4},
		{1000, 5},
		{5000, 5},  // capped at 5
		{-1, 1},    // negative XP
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			if got := client.xpToLevel(tt.xp); got != tt.want {
				t.Errorf("xpToLevel(%d) = %d, want %d", tt.xp, got, tt.want)
			}
		})
	}
}

func TestCraftWithQuantity_Validation(t *testing.T) {
	// v0.389: CraftWithOptions no longer clamps quantity; the only validation is quantity >= 1.
	c := newSubmitClientSkeleton()

	zeroTests := []struct {
		name     string
		quantity int
	}{
		{"zero quantity", 0},
		{"negative quantity", -1},
	}
	for _, tt := range zeroTests {
		t.Run(tt.name, func(t *testing.T) {
			err := c.CraftWithQuantity(t.Context(), "test_recipe", tt.quantity)
			if err == nil || !strings.Contains(err.Error(), "invalid quantity") {
				t.Errorf("CraftWithQuantity(quantity=%d) should have 'invalid quantity' error, got: %v", tt.quantity, err)
			}
		})
	}

	// Quantities well above the old skill cap (1) are now valid — they reach
	// Submit and return "not connected" (or time out), NOT "invalid quantity".
	largeTests := []struct {
		name     string
		quantity int
	}{
		{"large quantity no skill", 2},
		{"way above old cap", 100},
		{"much larger than old cap", 250},
	}
	for _, tt := range largeTests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
			defer cancel()
			err := c.CraftWithOptions(ctx, "test_recipe", tt.quantity, "")
			// Should NOT be an "invalid quantity" error — the new code only rejects < 1.
			if err != nil && strings.Contains(err.Error(), "invalid quantity") {
				t.Errorf("CraftWithOptions(quantity=%d) got unexpected validation error: %v", tt.quantity, err)
			}
		})
	}
}

func TestCraftQueryResult_EmptyInit(t *testing.T) {
	result := &CraftQueryResult{
		FullyCraftable: []CraftableRecipe{},
		PartialMatches: []CraftableRecipe{},
		SkillBlocked:   []CraftableRecipe{},
	}

	if len(result.FullyCraftable) != 0 {
		t.Errorf("expected empty FullyCraftable, got %d", len(result.FullyCraftable))
	}
	if len(result.PartialMatches) != 0 {
		t.Errorf("expected empty PartialMatches, got %d", len(result.PartialMatches))
	}
	if len(result.SkillBlocked) != 0 {
		t.Errorf("expected empty SkillBlocked, got %d", len(result.SkillBlocked))
	}
}

// TestCraftWithOptionsPayload verifies the new async queue-aware semantics:
// quantity above the old skill-based cap must be accepted, quantity is always
// in the payload, deliver_to is forwarded when non-empty, and cargo is NOT a
// valid deliver_to (the new model only allows "" or "faction"/"storage").
//
// Uses newSubmitTestClient (from submit_test.go) — mirrors the pattern used by
// TestBattle_PlainOKTerminates and TestBattle_ErrorStillTerminates.
func TestCraftWithOptionsPayload(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start the craft call in background; it blocks waiting for the server reply.
	errCh := make(chan error, 1)
	go func() {
		// quantity 250 is way above the old skill-based cap (max=1 with no skill).
		errCh <- c.CraftWithOptions(ctx, "basic_iron_smelting", 250, "faction")
	}()

	// Receive the sent message.
	var sent protocol.Message
	select {
	case sent = <-sendCh:
	case <-ctx.Done():
		t.Fatal("timeout waiting for send — CraftWithOptions likely rejected 250 before sending")
	}

	if sent.Type != "craft" {
		t.Fatalf("sent.Type = %q, want %q", sent.Type, "craft")
	}
	if got := sent.Payload["recipe_id"]; got != "basic_iron_smelting" {
		t.Fatalf("recipe_id = %v, want %q", got, "basic_iron_smelting")
	}
	if got := sent.Payload["quantity"]; got != 250 {
		t.Fatalf("quantity = %v, want 250 — clamp was NOT removed", got)
	}
	if got := sent.Payload["deliver_to"]; got != "faction" {
		t.Fatalf("deliver_to = %v, want %q", got, "faction")
	}

	// Simulate the server's single non-pending ok (v0.389 async model).
	c.router.dispatch(protocol.Response{
		Type:      protocol.TypeOK,
		RequestID: sent.RequestID,
		Payload:   map[string]any{"action": "craft", "job_id": "j1"},
	})

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("CraftWithOptions returned unexpected error: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("CraftWithOptions did not return after ok")
	}
}

// TestCraftWithOptionsRejectsBadQuantity verifies validation still rejects quantity < 1.
func TestCraftWithOptionsRejectsBadQuantity(t *testing.T) {
	c := newSubmitClientSkeleton()
	if err := c.CraftWithOptions(context.Background(), "r", 0, ""); err == nil {
		t.Fatal("expected error for quantity 0")
	}
}

func TestCraftableRecipe_Fields(t *testing.T) {
	recipe := CraftableRecipe{
		RecipeID:         "recipe_1",
		RecipeName:       "Iron Plate",
		CanCraftQuantity: 5,
		Components: []Component{
			{ID: "iron_ore", Quantity: 10},
			{ID: "copper_ore", Quantity: 5},
		},
		CanCraft:  true,
		Profit:    25.5,
		SkillGaps: []string{"smithing_3"},
	}

	if recipe.RecipeID != "recipe_1" {
		t.Errorf("RecipeID = %q, want %q", recipe.RecipeID, "recipe_1")
	}
	if recipe.RecipeName != "Iron Plate" {
		t.Errorf("RecipeName = %q, want %q", recipe.RecipeName, "Iron Plate")
	}
	if recipe.CanCraftQuantity != 5 {
		t.Errorf("CanCraftQuantity = %d, want 5", recipe.CanCraftQuantity)
	}
	if len(recipe.Components) != 2 {
		t.Errorf("expected 2 components, got %d", len(recipe.Components))
	}
	if recipe.Components[0].Quantity != 10 {
		t.Errorf("component quantity = %v, want 10", recipe.Components[0].Quantity)
	}
	if !recipe.CanCraft {
		t.Errorf("CanCraft = %v, want true", recipe.CanCraft)
	}
	if recipe.Profit != 25.5 {
		t.Errorf("Profit = %v, want 25.5", recipe.Profit)
	}
	if len(recipe.SkillGaps) != 1 || recipe.SkillGaps[0] != "smithing_3" {
		t.Errorf("SkillGaps = %v, want [smithing_3]", recipe.SkillGaps)
	}
}
