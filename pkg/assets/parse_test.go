package assets

import "testing"

// TestParseCurrentMax pins the OwnedShip hull/fuel format. These arrive as
// STRINGS ("1020/1020", "150/200"), not numbers. The partial case is the one
// that matters: a hull that exists but is not ready to fly.
func TestParseCurrentMax(t *testing.T) {
	for _, tc := range []struct {
		in       string
		cur, max int
		ok       bool
	}{
		{"1020/1020", 1020, 1020, true},
		{"150/200", 150, 200, true},
		{"0/200", 0, 200, true},
		{"", 0, 0, false},
		{"340", 0, 0, false},
		{"a/b", 0, 0, false},
		{"1/2/3", 0, 0, false},
	} {
		cur, max, ok := ParseCurrentMax(tc.in)
		if cur != tc.cur || max != tc.max || ok != tc.ok {
			t.Errorf("ParseCurrentMax(%q) = (%d,%d,%v), want (%d,%d,%v)",
				tc.in, cur, max, ok, tc.cur, tc.max, tc.ok)
		}
	}
}

// TestHullsFromGoldenPayload decodes a real list_ships body. The raw hull and
// fuel strings are retained alongside the parsed ints so a server-side format
// change surfaces as a mismatch instead of silent zeros.
func TestHullsFromGoldenPayload(t *testing.T) {
	// Live wrapper shape, verified against databot 2026-08-01: active_ship_class
	// / active_ship_id / count / ships, and NO "action" field. An earlier
	// version of this fixture invented one, which is what let the unreachable
	// "owned_ships" cache key go unnoticed.
	raw := []byte(`{"active_ship_class":"survey_vessel","count":2,"active_ship_id":"aaa",
	 "ships":[
	  {"class_id":"survey_vessel","class_name":"Survey Vessel","fuel":"1020/1020",
	   "hull":"340/340","is_active":false,"location":"stored at Grand Exchange Station",
	   "location_base_id":"grand_exchange_station","modules":3,
	   "ship_id":"74aeb79e64d9a12f682a2ee6daad79e4"},
	  {"class_id":"reclaim","class_name":"Reclaim","fuel":"150/200","hull":"180/180",
	   "is_active":true,"location":"stored at Grand Exchange Station",
	   "location_base_id":"grand_exchange_station","modules":2,
	   "ship_id":"67ef4a3e25dc336829d7d3e25736fe61"}]}`)

	hulls, ok, err := HullsFrom(raw)
	if err != nil {
		t.Fatalf("HullsFrom: %v", err)
	}
	if !ok {
		t.Fatal("HullsFrom(payload) ok = false, want true")
	}
	if len(hulls) != 2 {
		t.Fatalf("got %d hulls, want 2", len(hulls))
	}

	h := hulls[0]
	if h.ShipID != "74aeb79e64d9a12f682a2ee6daad79e4" || h.ClassID != "survey_vessel" {
		t.Errorf("hull[0] identity = %q/%q", h.ShipID, h.ClassID)
	}
	if h.ClassName != "Survey Vessel" {
		t.Errorf("hull[0] class name = %q, want %q", h.ClassName, "Survey Vessel")
	}
	if h.HullCurrent != 340 || h.HullMax != 340 || h.HullRaw != "340/340" {
		t.Errorf("hull[0] hull = %d/%d raw=%q", h.HullCurrent, h.HullMax, h.HullRaw)
	}
	if h.FuelCurrent != 1020 || h.FuelMax != 1020 || h.FuelRaw != "1020/1020" {
		t.Errorf("hull[0] fuel = %d/%d raw=%q", h.FuelCurrent, h.FuelMax, h.FuelRaw)
	}
	if h.CargoUsed != 0 {
		t.Errorf("hull[0] cargo used = %d, want 0", h.CargoUsed)
	}
	if h.LocationBaseID != "grand_exchange_station" || h.Modules != 3 {
		t.Errorf("hull[0] base=%q modules=%d", h.LocationBaseID, h.Modules)
	}
	if h.IsActive {
		t.Error("hull[0] must not be active")
	}

	h = hulls[1]
	if h.HullCurrent != 180 || h.HullMax != 180 || h.HullRaw != "180/180" {
		t.Errorf("hull[1] hull = %d/%d raw=%q", h.HullCurrent, h.HullMax, h.HullRaw)
	}
	if h.FuelCurrent != 150 || h.FuelMax != 200 {
		t.Errorf("hull[1] fuel = %d/%d, want 150/200", h.FuelCurrent, h.FuelMax)
	}
	if !h.IsActive {
		t.Error("hull[1] must be active")
	}
}

// TestHullsFromEmptyIsNotCaptured pins that an absent cache entry yields no
// hulls and no error, and reports ok=false: capture must degrade to "nothing
// this pass", never fail. The ok flag is load-bearing — an agent can never own
// zero ships (a destroyed last hull respawns a Tier 0 starter), so an empty
// decode means the cache was empty, not that the fleet is gone. Without the
// flag the caller cannot tell those apart and would wipe agent_hulls.
func TestHullsFromEmptyIsNotCaptured(t *testing.T) {
	hulls, ok, err := HullsFrom(nil)
	if err != nil || len(hulls) != 0 {
		t.Fatalf("HullsFrom(nil) = %v, %v; want empty, nil", hulls, err)
	}
	if ok {
		t.Error("HullsFrom(nil) ok = true, want false (nothing captured)")
	}
}

// TestHullsFromEmptyShipListIsCaptured pins the other side of the flag: a
// well-formed body that reports zero ships IS a capture (ok=true). The game
// makes this unreachable today, but conflating it with "no cache entry" is
// what made the wipe path possible in the first place.
func TestHullsFromEmptyShipListIsCaptured(t *testing.T) {
	hulls, ok, err := HullsFrom([]byte(`{"active_ship_id":"aaa","count":0,"ships":[]}`))
	if err != nil {
		t.Fatalf("HullsFrom: %v", err)
	}
	if !ok {
		t.Error("ok = false, want true: a clean decode is a capture")
	}
	if len(hulls) != 0 {
		t.Errorf("got %d hulls, want 0", len(hulls))
	}
}

// TestParseStorageHintLive drives the parser from payloads captured off live
// agents on 2026-08-06. Composed fixtures are what let owned_ships stay dead
// under a green suite; every string here came off the wire.
func TestParseStorageHintLive(t *testing.T) {
	tests := []struct {
		name      string
		hint      string
		wantOK    bool
		wantBases []string
		wantTotal float64
	}{
		{
			name:      "docked with holdings (databot)",
			hint:      "920 items in storage at confederacy_central_command",
			wantOK:    true,
			wantBases: []string{"confederacy_central_command"},
			wantTotal: 920,
		},
		{
			name:      "comma-grouped total (prophet-1)",
			hint:      "2,268 items in storage at central_nexus",
			wantOK:    true,
			wantBases: []string{"central_nexus"},
			wantTotal: 2268,
		},
		{
			// craftsman-1, the fleet's heaviest holder: 20 bases, no truncation.
			name: "multi-base (craftsman-1)",
			hint: "2,764,074 items in storage at cargo_lanes_freight_depot, central_nexus, " +
				"confederacy_central_command, crix_stronghold_station, dross_citadel_station, " +
				"frontier_station, gold_run_extraction_hub, grand_exchange_station, " +
				"kael_arsenal_station, market_prime_exchange, mera_sanctum_station, " +
				"nyx_nexus_station, sable_port_station, thane_keep_station, " +
				"the_experiment_research_station, the_rampart_checkpoint, " +
				"traders_rest_resort_station, treasure_cache_trading_post, " +
				"unknown_edge_waystation, voss_redoubt_station",
			wantOK: true,
			wantBases: []string{
				"cargo_lanes_freight_depot", "central_nexus", "confederacy_central_command",
				"crix_stronghold_station", "dross_citadel_station", "frontier_station",
				"gold_run_extraction_hub", "grand_exchange_station", "kael_arsenal_station",
				"market_prime_exchange", "mera_sanctum_station", "nyx_nexus_station",
				"sable_port_station", "thane_keep_station", "the_experiment_research_station",
				"the_rampart_checkpoint", "traders_rest_resort_station",
				"treasure_cache_trading_post", "unknown_edge_waystation", "voss_redoubt_station",
			},
			wantTotal: 2764074,
		},
		{
			name:      "nothing anywhere (random-clark)",
			hint:      "No items in storage at any station.",
			wantOK:    true,
			wantBases: nil,
			wantTotal: 0,
		},
		{
			// Captured off craftsman-1 on 2026-08-07, verbatim. The faction hint
			// differs from the agent hint in two ways that both broke the parser:
			// the separator reads "in faction storage at", and an instruction is
			// appended straight after the last base id with no delimiter.
			name: "faction storage with trailing fuel-bunker prose (craftsman-1)",
			hint: "178,357 items in faction storage at grand_exchange_station, " +
				"voss_redoubt_station Fuel bunker here: deposit fuel from your ship's " +
				"tank with storage deposit target=faction item_id=fuel.",
			wantOK:    true,
			wantBases: []string{"grand_exchange_station", "voss_redoubt_station"},
			wantTotal: 178357,
		},
		{
			name:      "faction storage, nothing anywhere",
			hint:      "No items in faction storage at any station.",
			wantOK:    true,
			wantBases: nil,
			wantTotal: 0,
		},
		{
			// Prose alone, with no base ids at all, must fail rather than record
			// words as bases -- ok=false is what suppresses deletion upstream.
			name:   "separator present but the tail is pure prose",
			hint:   "0 items in faction storage at no station you can reach right now.",
			wantOK: false,
		},
		{
			name:   "empty hint",
			hint:   "",
			wantOK: false,
		},
		{
			name:   "unrecognised prose",
			hint:   "storage is temporarily unavailable",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseStorageHint(tt.hint)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if len(got.Bases) != len(tt.wantBases) {
				t.Fatalf("bases = %v, want %v", got.Bases, tt.wantBases)
			}
			for i := range tt.wantBases {
				if got.Bases[i] != tt.wantBases[i] {
					t.Errorf("bases[%d] = %q, want %q", i, got.Bases[i], tt.wantBases[i])
				}
			}
			if got.Total != tt.wantTotal {
				t.Errorf("total = %v, want %v", got.Total, tt.wantTotal)
			}
		})
	}
}

// TestParseStorageHintSentinelIsNotABase pins the single nastiest case. The
// server says "No items in storage at any station." when an agent holds
// nothing. Cutting on " in storage at " leaves the tail "any station.", which a
// naive parser turns into a base id and then QUERIES -- and worse, a non-empty
// base list suppresses the "everything went to zero" deletion, so the ledger
// would keep reporting stock the agent has already sold.
func TestParseStorageHintSentinelIsNotABase(t *testing.T) {
	for _, sentinel := range []string{
		"No items in storage at any station.",
		"No items in faction storage at any station.",
	} {
		got, ok := ParseStorageHint(sentinel)
		if !ok {
			t.Fatalf("%q: the empty sentinel must parse successfully, not fail open", sentinel)
		}
		if len(got.Bases) != 0 {
			t.Errorf("%q: bases = %v, want empty (an 'any station.' entry is a parser bug)", sentinel, got.Bases)
		}
	}
}

// TestLooksLikeBaseIDRejectsProse is the second line of defence behind the
// exact-match sentinels above. If the server ever reworks that wording, the
// sentinel stops matching and the generic split runs on prose instead -- and
// every word in that prose is lowercase, so a charset-only check would record
// "any" or "no" as a base. A phantom base is worse than a missed one: it gets
// queried, and it makes the base list non-empty, which suppresses the deletion
// that should have cleared the agent's stale holdings.
func TestLooksLikeBaseIDRejectsProse(t *testing.T) {
	for _, want := range []string{
		"grand_exchange_station", "central_nexus", "ramens_rest", // real, named
		"59b102279f508c17831e557d3df6ad88", // real, opaque (one of three)
	} {
		if !looksLikeBaseID(want) {
			t.Errorf("looksLikeBaseID(%q) = false, want true (this is a real base id)", want)
		}
	}
	for _, reject := range []string{
		"any", "no", "station", "Fuel", "here:", "deposit", "", "target=faction",
	} {
		if looksLikeBaseID(reject) {
			t.Errorf("looksLikeBaseID(%q) = true, want false (prose must never become a base)", reject)
		}
	}
}
