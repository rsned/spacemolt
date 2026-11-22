package worker

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/huntdecide"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// huntProvisionFake serves get_ship, view_storage and view_market from
// in-memory tables and records the withdraws and buys it is sent. A withdraw
// or buy moves items into cargo; a reload fills a magazine from cargo.
type huntProvisionFake struct {
	*huntFake
	modules  []serverapi.ShipModule
	cargo    map[string]int
	storage  map[string]int
	listings []game.MarketListing
	shipErr  error
	// reloadErr, when set, refuses every reload (the server refusing it while
	// docked, say) and loads nothing.
	reloadErr error
	calls     []string
}

func (p *huntProvisionFake) GetShip(context.Context) error { return p.shipErr }

func (p *huntProvisionFake) GetRawJSON(key string) []byte {
	switch key {
	case "ship":
		var resp serverapi.GetShipResponse
		resp.Modules = p.modules
		for id, n := range p.cargo {
			if n > 0 {
				resp.Ship.Cargo = append(resp.Ship.Cargo, serverapi.CargoItem{ItemID: id, Quantity: float64(n)})
			}
		}
		b, _ := json.Marshal(resp)
		return b
	case "storage":
		var items []map[string]any
		for id, n := range p.storage {
			items = append(items, map[string]any{"item_id": id, "quantity": n})
		}
		b, _ := json.Marshal(map[string]any{"base_id": "haven", "items": items})
		return b
	}
	return p.huntFake.GetRawJSON(key)
}

func (p *huntProvisionFake) ViewStorage(context.Context) error {
	p.calls = append(p.calls, "view_storage")
	return nil
}

func (p *huntProvisionFake) ViewMarket(context.Context, map[string]any) error {
	p.calls = append(p.calls, "view_market")
	return nil
}

func (p *huntProvisionFake) GetMarketListings() []game.MarketListing { return p.listings }

func (p *huntProvisionFake) WithdrawItems(_ context.Context, id string, qty float64) error {
	p.calls = append(p.calls, fmt.Sprintf("withdraw:%s:%.0f", id, qty))
	if int(qty) > p.storage[id] {
		return errors.New("not enough in storage")
	}
	p.storage[id] -= int(qty)
	p.cargo[id] += int(qty)
	return nil
}

func (p *huntProvisionFake) Buy(_ context.Context, id string, qty float64) error {
	p.calls = append(p.calls, fmt.Sprintf("buy:%s:%.0f", id, qty))
	for _, l := range p.listings {
		if l.ItemID == id && l.Type == "sell" {
			p.state.Credits -= l.PricePerUnit * qty
			p.cargo[id] += int(qty)
			return nil
		}
	}
	return errors.New("no such listing")
}

func (p *huntProvisionFake) Reload(_ context.Context, weapon, ammo string) error {
	p.calls = append(p.calls, "reload:"+weapon+"<-"+ammo)
	if p.reloadErr != nil {
		return p.reloadErr
	}
	if p.cargo[ammo] <= 0 {
		return errors.New("no ammo")
	}
	for i := range p.modules {
		if p.modules[i].ID == weapon {
			p.cargo[ammo]--
			p.modules[i].CurrentAmmo = p.modules[i].MagazineSize
			p.modules[i].LoadedAmmoID = ammo
		}
	}
	return nil
}

func (p *huntProvisionFake) called(prefix string) []string {
	var out []string
	for _, c := range p.calls {
		if strings.HasPrefix(c, prefix) {
			out = append(out, c)
		}
	}
	return out
}

func provCannons(n int) []serverapi.ShipModule {
	var ms []serverapi.ShipModule
	for i := range n {
		ms = append(ms, serverapi.ShipModule{
			ID: fmt.Sprintf("ac%d", i), TypeID: "autocannon_ii", Type: "weapon",
			AmmoType: "autocannon", MagazineSize: 20,
		})
	}
	return ms
}

func provLasers(n int) []serverapi.ShipModule {
	var ms []serverapi.ShipModule
	for i := range n {
		ms = append(ms, serverapi.ShipModule{ID: fmt.Sprintf("laser%d", i), TypeID: "pulse_laser_ii", Type: "weapon"})
	}
	return ms
}

type provEnv struct {
	fake *huntProvisionFake
	deps HuntDeps
	out  *strings.Builder
	dir  string
	job  huntJob
	s    *huntdecide.Sortie
}

func newProvEnv(t *testing.T, modules []serverapi.ShipModule) *provEnv {
	t.Helper()
	hf := newHuntFake(t, huntFakeOpts{})
	hf.state.Credits = 100000
	p := &huntProvisionFake{
		huntFake: hf, modules: modules,
		cargo: map[string]int{}, storage: map[string]int{},
	}
	out := &strings.Builder{}
	deps := huntDeps(hf, out)
	deps.Client = p
	if err := deps.KB.StoreItems(context.Background(), []knowledge.CatalogItem{
		{ID: "standard_rounds_box", Name: "standard_rounds_box", Ammo: &knowledge.ItemAmmo{AmmoType: "autocannon"}},
		{ID: "armor_piercing_rounds_box", Name: "armor_piercing_rounds_box", Ammo: &knowledge.ItemAmmo{AmmoType: "autocannon"}},
	}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	log := huntdecide.NewLog(dir)
	deps.Decisions = log
	deps.Decider = huntdecide.Recorder{Inner: huntRules{}, Log: log}
	deps.HomeStation = "haven_station"
	return &provEnv{fake: p, deps: deps, out: out, dir: dir, job: huntJob{home: true}, s: &huntdecide.Sortie{ID: "s1"}}
}

func (e *provEnv) run(reserve int) bool {
	return huntProvision(context.Background(), e.deps, e.out, "pirate-6", e.job, e.s, reserve)
}

func (e *provEnv) records(t *testing.T) []map[string]any {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(e.dir, huntdecide.DecisionsDir, "*.jsonl"))
	var recs []map[string]any
	for _, name := range files {
		f, err := os.Open(name)
		if err != nil {
			t.Fatal(err)
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(nil, 1<<20)
		for sc.Scan() {
			var m map[string]any
			if err = json.Unmarshal(sc.Bytes(), &m); err != nil {
				t.Fatal(err)
			}
			recs = append(recs, m)
		}
		_ = f.Close()
	}
	return recs
}

func (e *provEnv) record(t *testing.T, typ string) map[string]any {
	t.Helper()
	for _, r := range e.records(t) {
		if r["type"] == typ {
			return r
		}
	}
	t.Fatalf("no %s record in %v", typ, e.records(t))
	return nil
}

func (e *provEnv) outcomeData(t *testing.T) map[string]any {
	t.Helper()
	data, _ := e.record(t, "outcome")["data"].(map[string]any)
	return data
}

func (e *provEnv) outcomeMap(t *testing.T, key string) map[string]any {
	t.Helper()
	m, _ := e.outcomeData(t)[key].(map[string]any)
	return m
}

func eqCalls(t *testing.T, what string, got, want []string) {
	t.Helper()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

func optionIDs(t *testing.T, e *provEnv) []string {
	t.Helper()
	opts, _ := e.record(t, "decision")["options"].([]any)
	var ids []string
	for _, o := range opts {
		ids = append(ids, o.(map[string]any)["id"].(string))
	}
	return ids
}

func TestHuntProvisionWithdrawsToTwoReloadsPerWeapon(t *testing.T) {
	e := newProvEnv(t, provCannons(3))
	e.fake.storage["standard_rounds_box"] = 50
	if !e.run(0) {
		t.Fatalf("want armed\n%s", e.out)
	}
	eqCalls(t, "withdraws", e.fake.called("withdraw:"), []string{"withdraw:standard_rounds_box:6"})
	dec := e.record(t, "decision")
	if dec["kind"] != "provision" || dec["option_id"] != "reloads_2" {
		t.Errorf("decision = kind %v option %v", dec["kind"], dec["option_id"])
	}
	if got := e.outcomeMap(t, "from_storage")["standard_rounds_box"]; got != float64(6) {
		t.Errorf("from_storage = %v", got)
	}
	oc := e.record(t, "outcome")
	if oc["phase"] != "provision" || oc["seq"] != float64(1) || oc["sortie_id"] != "s1" {
		t.Errorf("outcome header = %v", oc)
	}
	if got := e.outcomeMap(t, "depot_left")["standard_rounds_box"]; got != float64(44) {
		t.Errorf("depot_left = %v", got)
	}
	if e.outcomeData(t)["armed"] != true {
		t.Errorf("armed = %v", e.outcomeData(t)["armed"])
	}
}

func TestHuntProvisionCountsAmmoAlreadyAboard(t *testing.T) {
	e := newProvEnv(t, provCannons(3))
	e.fake.cargo["standard_rounds_box"] = 4
	e.fake.storage["standard_rounds_box"] = 50
	e.run(0)
	eqCalls(t, "withdraws", e.fake.called("withdraw:"), []string{"withdraw:standard_rounds_box:2"})
}

func TestHuntProvisionBuysShortfallUnderCap(t *testing.T) {
	e := newProvEnv(t, provCannons(3))
	e.fake.storage["standard_rounds_box"] = 1
	e.fake.listings = []game.MarketListing{{ItemID: "standard_rounds_box", Type: "sell", Quantity: 100, PricePerUnit: 6}}
	e.deps.AmmoCreditCap = 5000
	if !e.run(0) {
		t.Fatalf("want armed\n%s", e.out)
	}
	eqCalls(t, "withdraws", e.fake.called("withdraw:"), []string{"withdraw:standard_rounds_box:1"})
	eqCalls(t, "buys", e.fake.called("buy:"), []string{"buy:standard_rounds_box:5"})
	if got := e.outcomeData(t)["credits_paid"]; got != float64(30) {
		t.Errorf("credits_paid = %v", got)
	}
	if got := e.outcomeMap(t, "from_market")["standard_rounds_box"]; got != float64(5) {
		t.Errorf("from_market = %v", got)
	}
}

func TestHuntProvisionStopsAtCreditCap(t *testing.T) {
	e := newProvEnv(t, provCannons(3))
	e.fake.listings = []game.MarketListing{{ItemID: "standard_rounds_box", Type: "sell", Quantity: 100, PricePerUnit: 2000}}
	e.deps.AmmoCreditCap = 5000
	e.run(0)
	eqCalls(t, "buys", e.fake.called("buy:"), []string{"buy:standard_rounds_box:2"})
}

func TestHuntProvisionKeepsCreditFloor(t *testing.T) {
	e := newProvEnv(t, provCannons(3))
	e.fake.state.Credits = 1010
	e.fake.listings = []game.MarketListing{{ItemID: "standard_rounds_box", Type: "sell", Quantity: 100, PricePerUnit: 6}}
	e.run(0)
	eqCalls(t, "buys", e.fake.called("buy:"), []string{"buy:standard_rounds_box:1"})
}

func TestHuntProvisionClampsToHold(t *testing.T) {
	e := newProvEnv(t, provCannons(3))
	e.fake.state.Ship.CargoCapacity, e.fake.state.Ship.CargoUsed = 13, 0
	e.fake.storage["standard_rounds_box"] = 50
	e.run(0)
	eqCalls(t, "options", optionIDs(t, e), []string{"reloads_1"})
	eqCalls(t, "withdraws", e.fake.called("withdraw:"), []string{"withdraw:standard_rounds_box:3"})

	e = newProvEnv(t, provCannons(3))
	e.fake.state.Ship.CargoCapacity, e.fake.state.Ship.CargoUsed = 10, 0
	e.fake.storage["standard_rounds_box"] = 50
	e.run(0)
	if recs := e.records(t); len(recs) != 0 {
		t.Errorf("a full hold must record nothing, got %v", recs)
	}
	eqCalls(t, "withdraws", e.fake.called("withdraw:"), nil)
}

func TestHuntProvisionRespectsNeedReserve(t *testing.T) {
	e := newProvEnv(t, provCannons(3))
	e.fake.state.Ship.CargoCapacity, e.fake.state.Ship.CargoUsed = 30, 0
	e.fake.storage["standard_rounds_box"] = 50
	e.run(16)
	eqCalls(t, "options", optionIDs(t, e), []string{"reloads_1"})
}

func TestHuntProvisionEnergyOnlyIsSilent(t *testing.T) {
	e := newProvEnv(t, provLasers(3))
	if !e.run(0) {
		t.Fatalf("want armed\n%s", e.out)
	}
	if recs := e.records(t); len(recs) != 0 {
		t.Errorf("energy-only must record nothing, got %v", recs)
	}
	eqCalls(t, "storage and market calls", append(e.fake.called("view_"), e.fake.called("withdraw:")...), nil)
}

func TestHuntProvisionFailsOpenWhenShipUnreadable(t *testing.T) {
	e := newProvEnv(t, provCannons(3))
	e.fake.shipErr = errors.New("get_ship: boom")
	if !e.run(0) {
		t.Fatalf("an unreadable ship must fail open\n%s", e.out)
	}
	if recs := e.records(t); len(recs) != 0 {
		t.Errorf("want zero records, got %v", recs)
	}
}

func TestHuntProvisionBlockedUnarmed(t *testing.T) {
	e := newProvEnv(t, provCannons(3))
	if e.run(0) {
		t.Fatalf("dry ship with nothing to load must not be armed\n%s", e.out)
	}
	if !strings.Contains(e.out.String(), "BLOCKED UNARMED") {
		t.Errorf("output lacks BLOCKED UNARMED:\n%s", e.out)
	}
	if e.outcomeData(t)["armed"] != false {
		t.Errorf("outcome armed = %v", e.outcomeData(t)["armed"])
	}
}

// A refused dock-side reload leaves the magazines dry, but fitting ammo is now
// aboard: the ship is armed (the reload gate at the ground loads it), exactly
// as huntCanFire judges it.
func TestHuntProvisionArmedByAmmoAboardWhenReloadRefused(t *testing.T) {
	e := newProvEnv(t, provCannons(3))
	e.fake.storage["standard_rounds_box"] = 50
	e.fake.reloadErr = errors.New("cannot reload while docked")
	if !e.run(0) {
		t.Fatalf("fitting ammo aboard must count as armed\n%s", e.out)
	}
	if len(e.fake.called("reload:")) == 0 {
		t.Fatalf("the test needs the dock-side reload to be tried and refused, calls %v", e.fake.calls)
	}
	if e.fake.cargo["standard_rounds_box"] <= 0 {
		t.Errorf("boxes aboard = %d, want the withdrawn boxes", e.fake.cargo["standard_rounds_box"])
	}
	if strings.Contains(e.out.String(), "BLOCKED UNARMED") {
		t.Errorf("must not block with ammo aboard:\n%s", e.out)
	}
	if e.outcomeData(t)["armed"] != true {
		t.Errorf("outcome armed = %v, want true", e.outcomeData(t)["armed"])
	}
}

func TestHuntProvisionMixedLoadoutArmedByLaser(t *testing.T) {
	e := newProvEnv(t, append(provCannons(1), provLasers(1)...))
	if !e.run(0) {
		t.Fatalf("an ammo-free weapon keeps the ship armed\n%s", e.out)
	}
	if strings.Contains(e.out.String(), "BLOCKED UNARMED") {
		t.Errorf("must not block:\n%s", e.out)
	}
}

func TestHuntProvisionFallsBackWhenLoadedItemUnavailable(t *testing.T) {
	mods := provCannons(3)
	for i := range mods {
		mods[i].LoadedAmmoID = "armor_piercing_rounds_box"
	}
	e := newProvEnv(t, mods)
	e.fake.storage["standard_rounds_box"] = 50
	if !e.run(0) {
		t.Fatalf("a stocked depot must keep the ship armed\n%s", e.out)
	}
	eqCalls(t, "withdraws", e.fake.called("withdraw:"), []string{"withdraw:standard_rounds_box:6"})
	if strings.Contains(e.out.String(), "BLOCKED UNARMED") {
		t.Errorf("must not block:\n%s", e.out)
	}
}

func TestHuntProvisionBlockedHoldFullWording(t *testing.T) {
	e := newProvEnv(t, provCannons(3))
	e.fake.state.Ship.CargoCapacity, e.fake.state.Ship.CargoUsed = 10, 0
	e.fake.storage["standard_rounds_box"] = 50
	if e.run(0) {
		t.Fatalf("dry ship with no room must not be armed\n%s", e.out)
	}
	want := "BLOCKED UNARMED at haven_station: no weapon can fire and the hold has no room for ammo (free 10, reserved 0)"
	if !strings.Contains(e.out.String(), want) {
		t.Errorf("output lacks %q:\n%s", want, e.out)
	}
	if strings.Contains(e.out.String(), "in storage or on the market") {
		t.Errorf("must not claim a missing supply:\n%s", e.out)
	}
}

func TestHuntProvisionDepotLowLineOnlyAtHome(t *testing.T) {
	e := newProvEnv(t, provCannons(3))
	e.fake.storage["standard_rounds_box"] = 14
	e.run(0)
	want := "DEPOT LOW: standard_rounds_box 8 left at haven_station (under 2 sorties)"
	if !strings.Contains(e.out.String(), want) {
		t.Errorf("output lacks %q:\n%s", want, e.out)
	}

	e = newProvEnv(t, provCannons(3))
	e.deps.HomeStation = "some_other_station"
	e.fake.storage["standard_rounds_box"] = 14
	e.run(0)
	if strings.Contains(e.out.String(), "DEPOT LOW") {
		t.Errorf("DEPOT LOW away from home:\n%s", e.out)
	}
}
