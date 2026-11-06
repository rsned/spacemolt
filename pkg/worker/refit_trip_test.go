package worker

import (
	"context"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

func refitDispatch(t *testing.T, ship []byte, drones string) (*WorkerDispatch, *deliverFakeClient) {
	t.Helper()
	kb := newDeliverTestKB(t)
	t.Chdir(filepath.Join("..", ".."))
	st := havenState()
	st.Doc = true
	st.Ship.CargoCapacity = 100
	client := &deliverFakeClient{
		fakeClient:   &fakeClient{state: st, raw: map[string][]byte{"ship": ship, "drones": []byte(drones)}},
		storageStock: map[string]float64{"advanced_drone_bay": 1, "mining_drone": 5},
	}
	d := NewWorkerDispatch(client, kb, nil, io.Discard)
	d.AgentID = "bot"
	d.Role = "resident"

	return d, client
}

const noDrones = `{"drones":[]}`

func indexOfPrefix(calls []string, prefix string) int {
	return slices.IndexFunc(calls, func(c string) bool { return strings.HasPrefix(c, prefix) })
}

func TestRefitTripCollectsThenFitsAtHome(t *testing.T) {
	d, c := refitDispatch(t, shipJSON([2]string{"a", "mining_laser_i"}), noDrones)

	if err := d.RefitTrip(context.Background(), "from_base", "to_base", "mine_asteroid", []string{"mining_laser_i"}); err != nil {
		t.Fatal(err)
	}
	withdraw, strip := indexOfPrefix(c.calls, "withdraw:advanced_drone_bay"), indexOfPrefix(c.calls, "uninstall_mod:a")
	if withdraw < 0 || strip < 0 || withdraw > strip {
		t.Fatalf("collect at the hub, then strip: %v", c.calls)
	}
	// Scripts bind to the system they are fitted in, so the bay goes in after
	// the second leg, not at the hub.
	home := slices.Index(c.calls, "travel:to_poi")
	if install := indexOfPrefix(c.calls, "install_mod:"); home < 0 || install < home {
		t.Fatalf("fit must happen at home: %v", c.calls)
	}
}

func TestRefitTripAlreadyFittedDoesNotTravel(t *testing.T) {
	d, c := refitDispatch(t, shipJSON([2]string{"b", "advanced_drone_bay"}),
		`{"drones":[{"id":"1","type":"mining_drone"},{"id":"2","type":"mining_drone"},{"id":"3","type":"mining_drone"},{"id":"4","type":"mining_drone"},{"id":"5","type":"mining_drone"}]}`)
	c.suppressRoster = true

	if err := d.RefitTrip(context.Background(), "from_base", "to_base", "mine_asteroid", nil); err != nil {
		t.Fatal(err)
	}
	if indexOfPrefix(c.calls, "travel:") >= 0 || indexOfPrefix(c.calls, "find_route") >= 0 {
		t.Fatalf("a fitted bot stays put: %v", c.calls)
	}
}

func TestRefitTripPartsAboardSkipsTheHub(t *testing.T) {
	d, c := refitDispatch(t, shipJSON(), noDrones)
	c.state.Ship.Cargo = []game.CargoItem{{ItemID: "advanced_drone_bay", Quantity: 1}, {ItemID: "mining_drone", Quantity: 5}}

	if err := d.RefitTrip(context.Background(), "from_base", "to_base", "mine_asteroid", nil); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(c.calls, "travel:from_poi") {
		t.Fatalf("parts aboard means no hub leg: %v", c.calls)
	}
}

func TestRefitTripNoRoomStaysHome(t *testing.T) {
	d, c := refitDispatch(t, shipJSON(), noDrones)
	c.state.Ship.CargoCapacity = 20

	err := d.RefitTrip(context.Background(), "from_base", "to_base", "mine_asteroid", nil)
	if err == nil || !strings.Contains(err.Error(), "cargo") {
		t.Fatalf("want a cargo error, got %v", err)
	}
	if indexOfPrefix(c.calls, "travel:") >= 0 {
		t.Fatalf("must not leave: %v", c.calls)
	}
}

// A hub that has not received the gifts yet must not strand the bot off post.
func TestRefitTripEmptyHubReturnsHome(t *testing.T) {
	d, c := refitDispatch(t, shipJSON(), noDrones)
	c.storageStock = map[string]float64{}
	c.atomicWithdraw = true

	if err := d.RefitTrip(context.Background(), "from_base", "to_base", "mine_asteroid", nil); err == nil {
		t.Fatal("want the withdraw error")
	}
	if !slices.Contains(c.calls, "travel:to_poi") {
		t.Fatalf("must fly home: %v", c.calls)
	}
	if indexOfPrefix(c.calls, "install_mod:") >= 0 {
		t.Fatalf("no fit without parts: %v", c.calls)
	}
}

// staleSystemClient models the live client after a jump: state names the new
// system but holds no POIs until get_system refreshes it.
type staleSystemClient struct {
	*deliverFakeClient
	pois []game.POI
}

func (c *staleSystemClient) GetSystem(ctx context.Context) error {
	c.state.System.POIs = c.pois
	return c.fakeClient.GetSystem(ctx)
}

// Live 2026-11-06, marketbot_node_alpha: the fit failed at home with "no
// asteroid_belt POI" because a jump leaves the cached system empty.
func TestRefitTripRefreshesSystemBeforeFitting(t *testing.T) {
	d, c := refitDispatch(t, shipJSON(), noDrones)
	stale := &staleSystemClient{deliverFakeClient: c, pois: c.state.System.POIs}
	c.state.System.POIs = nil
	d.Client = stale

	if err := d.RefitTrip(context.Background(), "from_base", "to_base", "mine_asteroid", nil); err != nil {
		t.Fatal(err)
	}
	if indexOfPrefix(c.calls, "install_mod:") < 0 {
		t.Fatalf("no fit: %v", c.calls)
	}
}
