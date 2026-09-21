package ovdash

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func fixtureClaims(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "market.db")
	db, err := sql.Open(sqliteDriver, p)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close() //nolint:errcheck
	stmts := []string{
		`CREATE TABLE arbitrage_opportunities (
			id INTEGER PRIMARY KEY, item_id TEXT, quantity REAL,
			buy_price REAL, sell_price REAL, claimed_by TEXT, status TEXT,
			from_station_id TEXT, to_station_id TEXT)`,
		// trader-9: a full 1900-unit congregation load.
		`INSERT INTO arbitrage_opportunities VALUES
			(1229943,'liquid_hydrogen',933,66.3,81.4,'hauler-0','claimed','first_step_memorial_station','kael_arsenal'),
			(1229944,'plasma_gas',1900,52.0,85.0,'trader-9','claimed','a','b'),
			(1229945,'aluminum_sheet',700,18.0,22.6,'trader-1','completed','a','b'),
			(1229946,'steel_plate',50,10.0,12.0,NULL,'available','a','b')`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

// The fleet total on the dashboard counts wallet credits only, so capital a
// hauler has converted into cargo simply vanishes from the number -- which is
// exactly what made the total look like it was bleeding. Surfacing the cost
// basis per agent makes "where did the money go" answerable at a glance.
func TestLoadClaimedCargo_ValuesEachAgentsOpenClaim(t *testing.T) {
	got, err := LoadClaimedCargo(context.Background(), fixtureClaims(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d agents, want 2 (only open claims)", len(got))
	}
	h := got["hauler-0"]
	if h.OppID != 1229943 || h.ItemID != "liquid_hydrogen" || h.Quantity != 933 {
		t.Errorf("hauler-0 = %+v", h)
	}
	// 933 * 66.3 spent, 933 * 81.4 expected back.
	if int(h.CostBasis) != 61857 {
		t.Errorf("hauler-0 CostBasis = %v, want 61857", h.CostBasis)
	}
	if int(h.ExpectedProceeds) != 75946 {
		t.Errorf("hauler-0 ExpectedProceeds = %v, want 75946", h.ExpectedProceeds)
	}
	if int(got["trader-9"].CostBasis) != 98800 {
		t.Errorf("trader-9 CostBasis = %v, want 98800", got["trader-9"].CostBasis)
	}
}

// A completed or unclaimed row is not capital in anyone's hold. Counting them
// would inflate the very figure the panel exists to make trustworthy.
func TestLoadClaimedCargo_IgnoresCompletedAndUnclaimed(t *testing.T) {
	got, err := LoadClaimedCargo(context.Background(), fixtureClaims(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["trader-1"]; ok {
		t.Error("a completed opportunity must not count as cargo in hand")
	}
	for _, c := range got {
		if c.OppID == 1229946 {
			t.Error("an unclaimed opportunity must not be attributed to anyone")
		}
	}
}

// A missing market DB must degrade to "no cargo known" rather than taking the
// whole dashboard down: the panel is an aid, not a dependency.
func TestLoadClaimedCargo_MissingDBIsNotFatal(t *testing.T) {
	got, err := LoadClaimedCargo(context.Background(), filepath.Join(t.TempDir(), "absent.db"))
	if err != nil {
		t.Fatalf("missing db must not error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d entries from an absent db, want 0", len(got))
	}
}

// The panel is only useful if the value lands on the agent rows the UI already
// draws, and if the fleet-wide figure sits beside the credits total -- the two
// together are what make "we are down 17M" readable as cargo rather than loss.
func TestAttachClaimedCargo_PopulatesAgentsAndTheFleetTotal(t *testing.T) {
	s := &Snapshot{
		Agents: []AgentState{
			{AgentID: "hauler-0", Credits: 22_646_595},
			{AgentID: "trader-9", Credits: 30_111_756},
			{AgentID: "explorer-1", Credits: 25_747_228},
		},
		OffMap: []AgentState{{AgentID: "trader-3", Credits: 16_639_226}},
	}
	cargo := map[string]ClaimedCargo{
		"hauler-0": {OppID: 1, ItemID: "liquid_hydrogen", Quantity: 933, CostBasis: 61857, ExpectedProceeds: 75946},
		"trader-9": {OppID: 2, ItemID: "plasma_gas", Quantity: 1900, CostBasis: 98800, ExpectedProceeds: 161500},
		"trader-3": {OppID: 3, ItemID: "steel_plate", Quantity: 10, CostBasis: 100, ExpectedProceeds: 120},
		"ghost-1":  {OppID: 4, ItemID: "x", Quantity: 1, CostBasis: 5, ExpectedProceeds: 6},
	}
	s.AttachClaimedCargo(cargo)

	if got := s.Agents[0].CargoValue; int(got) != 61857 {
		t.Errorf("hauler-0 CargoValue = %v, want 61857", got)
	}
	if got := s.Agents[0].CargoItem; got != "liquid_hydrogen" {
		t.Errorf("hauler-0 CargoItem = %q", got)
	}
	if got := s.Agents[2].CargoValue; got != 0 {
		t.Errorf("explorer-1 holds no claim, CargoValue = %v, want 0", got)
	}
	// Off-map agents are still holding real cargo and must be counted.
	if got := s.OffMap[0].CargoValue; int(got) != 100 {
		t.Errorf("off-map trader-3 CargoValue = %v, want 100", got)
	}
	// A claim whose agent is in no status file cannot be shown on a row, but
	// the capital is still committed, so the total must include it -- that is
	// exactly the salvager-6 case, mid-roll and absent from the fleet.
	if int(s.CargoValueTotal) != 61857+98800+100+5 {
		t.Errorf("CargoValueTotal = %v, want every open claim counted", s.CargoValueTotal)
	}
}

// "Total credits" is the number that looked like it was bleeding. Pairing it
// with cargo value and their sum is what makes the strip readable: capital
// moving between the two columns is normal, and only the sum falling is loss.
func TestBuildAccounting_ReportsCargoValueAndTotalCapital(t *testing.T) {
	s := &Snapshot{
		Agents:          []AgentState{{Credits: 100, Healthy: true, Seen: true}, {Credits: 50, Healthy: true, Seen: true}},
		CargoValueTotal: 75,
	}
	a := BuildAccounting(s, SourceEarnings{}, SourceEarnings{}, SourceEarnings{}, 0)
	if a.TotalCredits != 150 {
		t.Errorf("TotalCredits = %v, want 150", a.TotalCredits)
	}
	if a.CargoValue != 75 {
		t.Errorf("CargoValue = %v, want 75", a.CargoValue)
	}
	if a.TotalCapital != 225 {
		t.Errorf("TotalCapital = %v, want 225 (credits + goods)", a.TotalCapital)
	}
}
