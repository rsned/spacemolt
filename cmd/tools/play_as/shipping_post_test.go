package main

import (
	"context"
	"strings"
	"testing"
)

// shippingRecorder captures the action + payload a shipping subcommand sends,
// so the tests assert on the wire shape rather than on a stub returning nil.
type shippingRecorder struct {
	action  string
	payload map[string]any
	calls   int
}

func (s *shippingRecorder) Shipping(_ context.Context, action string, payload map[string]any) error {
	s.action = action
	s.payload = payload
	s.calls++

	return nil
}

// quote and post are the SHIPPER side of freight — posting cargo for someone
// else to haul. play_as shipped only the carrier/among-others actions
// (profile, list, get, track, pay_debt), so a package could be sealed and then
// not sent from a play_as session at all.
func TestShippingQuoteSendsPackageAndDestination(t *testing.T) {
	c := &shippingRecorder{}
	// --base_reward is passed deliberately: quote must STRIP it. An earlier
	// version of this test omitted the flag, so deleting the strip left the
	// test green — it was asserting that an absent key stayed absent.
	err := shipperCommand(context.Background(), c,
		[]string{"shipping", "quote", "pkg_123", "frontier_station", "--base_reward", "9999"})
	if err != nil {
		t.Fatalf("quote: %v", err)
	}
	if c.action != "quote" {
		t.Errorf("action = %q, want quote", c.action)
	}
	if c.payload["package_id"] != "pkg_123" {
		t.Errorf("package_id = %v, want pkg_123", c.payload["package_id"])
	}
	if c.payload["destination_base_id"] != "frontier_station" {
		t.Errorf("destination_base_id = %v, want frontier_station", c.payload["destination_base_id"])
	}
	// quote must not carry a reward: it is the call you make to FIND OUT what
	// to offer, and sending one would misreport the estimate as a decision.
	if _, ok := c.payload["base_reward"]; ok {
		t.Error("quote must not send base_reward")
	}
}

// base_reward is required to post — the server rejects a post without it
// (reward_required), and there is no automatic distance-based rate. Catching
// that locally beats a round trip to be told off.
func TestShippingPostRequiresReward(t *testing.T) {
	c := &shippingRecorder{}
	err := shipperCommand(context.Background(), c, []string{"shipping", "post", "pkg_123", "frontier_station"})
	if err == nil {
		t.Fatal("post without a reward must fail locally, not at the server")
	}
	if !strings.Contains(err.Error(), "reward") {
		t.Errorf("error = %q, must name the missing reward", err)
	}
	if c.calls != 0 {
		t.Error("a post missing its reward must not reach the wire")
	}
}

func TestShippingPostSendsReward(t *testing.T) {
	c := &shippingRecorder{}
	err := shipperCommand(context.Background(), c, []string{"shipping", "post", "pkg_123", "frontier_station", "--base_reward", "2500"})
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	if c.action != "post" {
		t.Errorf("action = %q, want post", c.action)
	}
	// The server types base_reward as an integer; sending "2500" as a string
	// is the kind of drift that decodes to zero and reads as "omitted".
	got, ok := c.payload["base_reward"].(int64)
	if !ok {
		t.Fatalf("base_reward = %#v (%T), want an int64", c.payload["base_reward"], c.payload["base_reward"])
	}
	if got != 2500 {
		t.Errorf("base_reward = %d, want 2500", got)
	}
}

// Optional flags must only appear when given. A payload that always carries
// every key sends zero values the server would read as real choices — an
// uninsured request, or a service level nobody picked.
func TestShippingPostOmitsUnsetOptionals(t *testing.T) {
	c := &shippingRecorder{}
	if err := shipperCommand(context.Background(), c, []string{"shipping", "post", "p", "d", "--base_reward", "10"}); err != nil {
		t.Fatalf("post: %v", err)
	}
	for _, k := range []string{"insured", "service_level", "speed_bonus", "shipper", "visibility", "max_total_cost"} {
		if _, ok := c.payload[k]; ok {
			t.Errorf("unset optional %q must be absent from the payload, got %v", k, c.payload[k])
		}
	}
}

func TestShippingPostPassesOptionals(t *testing.T) {
	c := &shippingRecorder{}
	err := shipperCommand(context.Background(), c, []string{
		"shipping", "post", "p", "d",
		"--base_reward", "1000", "--service_level", "priority",
		"--insured", "true", "--speed_bonus", "250", "--shipper", "faction",
	})
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	if c.payload["service_level"] != "priority" {
		t.Errorf("service_level = %v, want priority", c.payload["service_level"])
	}
	if c.payload["insured"] != true {
		t.Errorf("insured = %v (%T), want bool true", c.payload["insured"], c.payload["insured"])
	}
	if c.payload["speed_bonus"] != int64(250) {
		t.Errorf("speed_bonus = %#v, want int64 250", c.payload["speed_bonus"])
	}
	if c.payload["shipper"] != "faction" {
		t.Errorf("shipper = %v, want faction", c.payload["shipper"])
	}
}

func TestShippingQuoteAndPostUsage(t *testing.T) {
	for _, args := range [][]string{
		{"shipping", "quote", "pkg_only"},
		{"shipping", "post", "pkg_only"},
	} {
		c := &shippingRecorder{}
		if err := shipperCommand(context.Background(), c, args); err == nil {
			t.Errorf("%v: missing destination must be a usage error", args)
		} else if !strings.Contains(err.Error(), "usage") {
			t.Errorf("%v: error %q should be a usage message", args, err)
		}
		if c.calls != 0 {
			t.Errorf("%v: nothing should reach the wire", args)
		}
	}
}

// `shipping active` lists the contracts you currently have outstanding. The
// freight fleet reconstructs this from disk today, and a listing that comes
// from the server is a better answer than one reconstructed from local state
// that a crash can desynchronise.
func TestShippingActiveSendsNoParamsByDefault(t *testing.T) {
	c := &shippingRecorder{}
	if err := shipperCommand(context.Background(), c, []string{"shipping", "active"}); err != nil {
		t.Fatalf("active: %v", err)
	}
	if c.action != "active" {
		t.Errorf("action = %q, want active", c.action)
	}
	// An unasked-for filter is a filter the operator did not choose. Sending
	// eligible_as or a page size by default would quietly narrow the answer.
	if len(c.payload) != 0 {
		t.Errorf("payload = %v, want empty when no flags are given", c.payload)
	}
}

func TestShippingActivePassesFilters(t *testing.T) {
	c := &shippingRecorder{}
	err := shipperCommand(context.Background(), c,
		[]string{"shipping", "active", "--eligible_as", "faction", "--per_page", "50"})
	if err != nil {
		t.Fatalf("active: %v", err)
	}
	if c.payload["eligible_as"] != "faction" {
		t.Errorf("eligible_as = %v, want faction", c.payload["eligible_as"])
	}
	if c.payload["per_page"] != int64(50) {
		t.Errorf("per_page = %#v, want int64 50", c.payload["per_page"])
	}
}

// cancel applies only while a contract is still posted, so getting the id
// wrong is a wasted round trip at best.
func TestShippingCancelRequiresAnID(t *testing.T) {
	c := &shippingRecorder{}
	if err := shipperCommand(context.Background(), c, []string{"shipping", "cancel"}); err == nil {
		t.Fatal("cancel without a shipment id must be a usage error")
	}
	if c.calls != 0 {
		t.Error("nothing should reach the wire")
	}
}

func TestShippingCancelSendsShipmentID(t *testing.T) {
	c := &shippingRecorder{}
	if err := shipperCommand(context.Background(), c, []string{"shipping", "cancel", "shp_77"}); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if c.action != "cancel" {
		t.Errorf("action = %q, want cancel", c.action)
	}
	if c.payload["shipment_id"] != "shp_77" {
		t.Errorf("shipment_id = %v, want shp_77", c.payload["shipment_id"])
	}
}
