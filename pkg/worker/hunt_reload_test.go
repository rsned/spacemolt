package worker

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// huntReloadFake serves a get_ship reply with one autocannon and records the
// reloads it is sent.
type huntReloadFake struct {
	game.GameClient
	rounds  int
	loaded  string
	cargo   []serverapi.CargoItem
	reloads []string
}

func (w *huntReloadFake) GetShip(context.Context) error { return nil }

func (w *huntReloadFake) GetRawJSON(key string) []byte {
	if key != "ship" {
		return w.GameClient.GetRawJSON(key)
	}
	var resp serverapi.GetShipResponse
	resp.Modules = []serverapi.ShipModule{{
		ID: "mod1", TypeID: "autocannon_i", Name: "Autocannon I", Type: "weapon",
		AmmoType: "autocannon", MagazineSize: 20, CurrentAmmo: w.rounds, LoadedAmmoID: w.loaded,
	}}
	resp.Ship.Cargo = w.cargo
	b, _ := json.Marshal(resp)
	return b
}

func (w *huntReloadFake) Reload(_ context.Context, weapon, ammo string) error {
	w.reloads = append(w.reloads, weapon+"<-"+ammo)
	return nil
}

func huntReloadDeps(t *testing.T, w *huntReloadFake) (HuntDeps, *strings.Builder) {
	t.Helper()
	f := newHuntFake(t, huntFakeOpts{})
	w.GameClient = f
	var log strings.Builder
	deps := huntDeps(f, &log)
	deps.Client = w
	if err := deps.KB.StoreItems(context.Background(), []knowledge.CatalogItem{
		{ID: "standard_rounds_box", Name: "standard_rounds_box", Ammo: &knowledge.ItemAmmo{AmmoType: "autocannon"}},
		{ID: "armor_piercing_rounds_box", Name: "armor_piercing_rounds_box", Ammo: &knowledge.ItemAmmo{AmmoType: "autocannon"}},
	}); err != nil {
		t.Fatal(err)
	}
	return deps, &log
}

func TestHuntReload(t *testing.T) {
	std := serverapi.CargoItem{ItemID: "standard_rounds_box", Quantity: 5}
	ap := serverapi.CargoItem{ItemID: "armor_piercing_rounds_box", Quantity: 9}
	for _, tc := range []struct {
		name      string
		fake      huntReloadFake
		wantArmed bool
		want      []string
	}{
		{"dry magazine is filled", huntReloadFake{cargo: []serverapi.CargoItem{std}}, true, []string{"mod1<-standard_rounds_box"}},
		{"dry with nothing aboard cannot fight", huntReloadFake{}, false, nil},
		{"full magazine is left alone", huntReloadFake{rounds: 20, loaded: "standard_rounds_box", cargo: []serverapi.CargoItem{std}}, true, nil},
		{"partial is topped up with the loaded ammo", huntReloadFake{rounds: 4, loaded: "standard_rounds_box", cargo: []serverapi.CargoItem{std, ap}}, true, []string{"mod1<-standard_rounds_box"}},
		{"partial is not swapped to another ammo", huntReloadFake{rounds: 4, loaded: "standard_rounds_box", cargo: []serverapi.CargoItem{ap}}, true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := tc.fake
			deps, log := huntReloadDeps(t, &w)
			armed := huntReload(context.Background(), deps, log, "pirate-policed")
			if armed != tc.wantArmed {
				t.Errorf("armed = %v, want %v\n%s", armed, tc.wantArmed, log)
			}
			if strings.Join(w.reloads, ",") != strings.Join(tc.want, ",") {
				t.Errorf("reloads = %v, want %v\n%s", w.reloads, tc.want, log)
			}
			if !tc.wantArmed && !strings.Contains(log.String(), "NO AMMO") {
				t.Errorf("an unarmed ship must be logged, got:\n%s", log)
			}
		})
	}
}
