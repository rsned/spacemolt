package worker

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

type consignFake struct {
	game.GameClient
	state *game.State
	gifts []map[string]any
	raw   map[string][]byte
	err   error
	// errFor fails gifts of one item only, so a test can see that one failed
	// rule does not stop the rest.
	errFor map[string]error
}

func (f *consignFake) GetState() *game.State { return f.state }

func (f *consignFake) ViewStorage(context.Context) error { return nil }

func (f *consignFake) GetRawJSON(key string) []byte { return f.raw[key] }

func (f *consignFake) SendGift(_ context.Context, payload map[string]any) error {
	if f.err != nil {
		return f.err
	}
	if e := f.errFor[payload["item_id"].(string)]; e != nil {
		return e
	}
	f.gifts = append(f.gifts, payload)

	return nil
}

func consignDockedAt(base string) *game.State {
	st := &game.State{}
	st.Player.DockedAtBase = base

	return st
}

func storageFrame(base string, items map[string]int) []byte {
	out := `{"base_id":"` + base + `","items":[`
	first := true
	for id, q := range items {
		if !first {
			out += ","
		}
		first = false
		out += `{"item_id":"` + id + `","quantity":` + itoa(q) + `,"size":1}`
	}

	return []byte(out + `]}`)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}

	return string(b)
}

// A collection point should hand accumulated goods to a hauler as part of its
// ordinary idle pass -- no manual control, no interrupted routine.
// marketbot_alhena is pinned at voss_redoubt with drones mining adamantite_ore
// into its storage; the hauler collects whenever it arrives, because send_gift
// lands in the recipient's storage at the SENDING station and waits.
func TestConsign_GiftsAboveTheMinimum(t *testing.T) {
	f := &consignFake{
		state: consignDockedAt("voss_redoubt_station"),
		raw:   map[string][]byte{"storage": storageFrame("voss_redoubt_station", map[string]int{"adamantite_ore": 260})},
	}
	rules := []ConsignRule{{Item: "adamantite_ore", To: "salvager-2", Min: 50}}

	n, err := Consign(context.Background(), f, rules, func(string) (string, error) { return "Sal 'Salvage' Two", nil })
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("sent %d gifts, want 1", n)
	}
	g := f.gifts[0]
	if g["recipient"] != "Sal 'Salvage' Two" {
		t.Errorf("recipient = %v, want the resolved username", g["recipient"])
	}
	if g["item_id"] != "adamantite_ore" {
		t.Errorf("item_id = %v", g["item_id"])
	}
	if g["quantity"] != 260 {
		t.Errorf("quantity = %v, want the whole 260", g["quantity"])
	}
	if g["source"] != "storage" {
		t.Errorf("source = %v, want storage (never cargo: a ship's hold is its own work)", g["source"])
	}
}

// keep leaves a reserve behind, for an item the agent also consumes.
func TestConsign_KeepLeavesAReserve(t *testing.T) {
	f := &consignFake{
		state: consignDockedAt("voss_redoubt_station"),
		raw:   map[string][]byte{"storage": storageFrame("voss_redoubt_station", map[string]int{"water_ice": 500})},
	}
	rules := []ConsignRule{{Item: "water_ice", To: "salvager-2", Min: 50, Keep: 200}}

	if _, err := Consign(context.Background(), f, rules, func(string) (string, error) { return "Sal", nil }); err != nil {
		t.Fatal(err)
	}
	if len(f.gifts) != 1 {
		t.Fatalf("want one gift, got %d", len(f.gifts))
	}
	if got := f.gifts[0]["quantity"]; got != 300 {
		t.Errorf("quantity = %v, want 300 (500 held minus keep 200)", got)
	}
}

// Below the minimum SENDABLE amount it batches instead of dribbling. keep is
// subtracted first, so min applies to what would actually be sent.
func TestConsign_BatchesBelowMinimum(t *testing.T) {
	for _, tc := range []struct {
		name string
		held int
		rule ConsignRule
	}{
		{"under min", 30, ConsignRule{Item: "adamantite_ore", To: "x", Min: 50}},
		{"keep eats it", 220, ConsignRule{Item: "adamantite_ore", To: "x", Min: 50, Keep: 200}},
		{"exactly keep", 200, ConsignRule{Item: "adamantite_ore", To: "x", Min: 1, Keep: 200}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &consignFake{
				state: consignDockedAt("voss_redoubt_station"),
				raw:   map[string][]byte{"storage": storageFrame("voss_redoubt_station", map[string]int{"adamantite_ore": tc.held})},
			}
			n, err := Consign(context.Background(), f, []ConsignRule{tc.rule},
				func(string) (string, error) { return "X", nil })
			if err != nil {
				t.Fatal(err)
			}
			if n != 0 || len(f.gifts) != 0 {
				t.Errorf("sent %d gifts from %d held, want none", len(f.gifts), tc.held)
			}
		})
	}
}

// Undocked is a no-op: storage reads and gifts are docked-station-local, so
// there is nothing to read and nothing to send.
func TestConsign_UndockedIsANoOp(t *testing.T) {
	f := &consignFake{state: &game.State{}, raw: map[string][]byte{}}
	n, err := Consign(context.Background(), f, []ConsignRule{{Item: "x", To: "y", Min: 1}},
		func(string) (string, error) { return "Y", nil })
	if err != nil || n != 0 {
		t.Errorf("want a quiet no-op, got n=%d err=%v", n, err)
	}
}

// Only the allow-listed item is ever touched. A wildcard would be
// unrecoverable: gifting is irreversible, and the drone-reserve energy
// crystals and a ship's fuel cells sit in the same storage.
func TestConsign_NeverTouchesUnlistedItems(t *testing.T) {
	f := &consignFake{
		state: consignDockedAt("voss_redoubt_station"),
		raw: map[string][]byte{"storage": storageFrame("voss_redoubt_station", map[string]int{
			"adamantite_ore": 100, "energy_crystal": 9000, "fuel_cell": 40,
		})},
	}
	if _, err := Consign(context.Background(), f,
		[]ConsignRule{{Item: "adamantite_ore", To: "x", Min: 1}},
		func(string) (string, error) { return "X", nil }); err != nil {
		t.Fatal(err)
	}
	if len(f.gifts) != 1 || f.gifts[0]["item_id"] != "adamantite_ore" {
		t.Fatalf("gifts = %+v, want only adamantite_ore", f.gifts)
	}
}

// A failed gift must not fail the idle pass -- consignment is strictly less
// important than any paying activity, matching the capture family's policy.
// And the post-gift state read is never trusted: heartbeat credits read a
// stale 0 and make a landed gift look failed.
func TestConsign_SendFailureIsReportedAndOthersContinue(t *testing.T) {
	// A refused gift used to be dropped without a word. marketbot_015 sent a
	// platinum gift every pass for a day and every one failed with
	// gift_not_unlocked (an account must earn 1,000 credits before it can
	// gift), while the worker log showed only "send_gift pending" (2026-10-30).
	// A failure is now reported, and the remaining rules are still sent.
	f := &consignFake{
		state: consignDockedAt("ironhearth_station"),
		raw: map[string][]byte{"storage": storageFrame("ironhearth_station",
			map[string]int{"platinum_ore": 925, "carbon_ore": 925})},
		errFor: map[string]error{"platinum_ore": errors.New("gift_not_unlocked: You must earn at least 1000 credits before sending gifts.")},
	}
	n, err := Consign(context.Background(), f, []ConsignRule{
		{Item: "platinum_ore", To: "explorer-3", Min: 1},
		{Item: "carbon_ore", To: "explorer-3", Min: 1},
	}, func(string) (string, error) { return "Atlas", nil })
	if err == nil || !strings.Contains(err.Error(), "platinum_ore") || !strings.Contains(err.Error(), "gift_not_unlocked") {
		t.Errorf("err = %v, want it to name platinum_ore and gift_not_unlocked", err)
	}
	if n != 1 || len(f.gifts) != 1 || f.gifts[0]["item_id"] != "carbon_ore" {
		t.Errorf("n=%d gifts=%+v, want the carbon_ore gift still sent", n, f.gifts)
	}
}

// An unresolvable recipient is a configuration error worth surfacing once, not
// a silent skip -- a typo'd agent id would otherwise accumulate goods forever.
func TestConsign_UnresolvableRecipientIsReported(t *testing.T) {
	f := &consignFake{
		state: consignDockedAt("voss_redoubt_station"),
		raw:   map[string][]byte{"storage": storageFrame("voss_redoubt_station", map[string]int{"adamantite_ore": 100})},
	}
	_, err := Consign(context.Background(), f, []ConsignRule{{Item: "adamantite_ore", To: "nope", Min: 1}},
		func(string) (string, error) { return "", errors.New("no credentials") })
	if err == nil {
		t.Error("want an error naming the unresolvable recipient")
	}
	if len(f.gifts) != 0 {
		t.Error("nothing should be sent when the recipient cannot be resolved")
	}
}

// The fleet yaml carries rules as structs, but the supervisor forwards them to
// the worker as one CLI flag, so there is a compact wire form:
// item:recipient:min[:keep], comma-separated.
func TestParseConsignRules(t *testing.T) {
	got, err := ParseConsignRules("adamantite_ore:salvager-2:50,water_ice:salvager-4:100:200")
	if err != nil {
		t.Fatal(err)
	}
	want := []ConsignRule{
		{Item: "adamantite_ore", To: "salvager-2", Min: 50},
		{Item: "water_ice", To: "salvager-4", Min: 100, Keep: 200},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d rules, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("rule %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParseConsignRules_EmptyIsNoRules(t *testing.T) {
	got, err := ParseConsignRules("")
	if err != nil || len(got) != 0 {
		t.Errorf("got %+v err=%v, want no rules and no error", got, err)
	}
}

// A malformed rule must be rejected loudly rather than silently dropped: a
// typo'd flag that parsed to zero rules would leave a collection point
// accumulating goods forever with nothing to show it.
func TestParseConsignRules_RejectsMalformed(t *testing.T) {
	for _, bad := range []string{
		"adamantite_ore",                    // no recipient or min
		"adamantite_ore:salvager-2",         // no min
		"adamantite_ore:salvager-2:notanum", // min not a number
		":salvager-2:50",                    // no item
		"adamantite_ore::50",                // no recipient
		"adamantite_ore:salvager-2:50:-5",   // negative keep
		"adamantite_ore:salvager-2:0",       // min 0 would dribble every pass
	} {
		if _, err := ParseConsignRules(bad); err == nil {
			t.Errorf("ParseConsignRules(%q) = nil error, want a rejection", bad)
		}
	}
}
