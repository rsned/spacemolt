package worker

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

type tourFixture struct {
	d        *WorkerDispatch
	client   *deliverFakeClient
	manifest string
}

func newTour(t *testing.T, state *game.State, stops ...TourStop) tourFixture {
	t.Helper()
	agents := t.TempDir()
	for _, s := range stops {
		writeDeliverCreds(t, agents, s.Recipient, s.Recipient+"-name")
	}
	state.Ship.CargoCapacity = 1900
	state.Ship.Cargo = []game.CargoItem{{ItemID: "advanced_drone_bay", Quantity: 10}, {ItemID: "mining_drone", Quantity: 50}}
	client := &deliverFakeClient{fakeClient: &fakeClient{state: state}, storageStock: map[string]float64{}}
	d := NewWorkerDispatch(client, newDeliverTestKB(t), nil, io.Discard)
	d.AgentID = "courier"
	d.AgentsDir = agents
	d.TourRecipientSystem = func(string) (string, error) { return "to_sys", nil }

	raw, err := json.Marshal(stops)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "tour.json")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	return tourFixture{d, client, path}
}

func fitStop(recipient string) TourStop {
	return TourStop{StationID: "to_base", System: "to_sys", Recipient: recipient,
		Items: map[string]int{"advanced_drone_bay": 1, "mining_drone": 5}}
}

func giftedTo(c *deliverFakeClient) []string {
	out := make([]string, 0, len(c.giftCalls))
	for _, g := range c.giftCalls {
		out = append(out, g["recipient"].(string)+":"+g["item_id"].(string))
	}

	return out
}

func TestDeliverTourGiftsInOrder(t *testing.T) {
	f := newTour(t, &game.State{}, fitStop("bot-a"), fitStop("bot-b"))

	if err := f.d.DeliverTour(context.Background(), f.manifest); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(giftedTo(f.client), " ")
	want := "bot-a-name:advanced_drone_bay bot-a-name:mining_drone bot-b-name:advanced_drone_bay bot-b-name:mining_drone"
	if got != want {
		t.Fatalf("got %s", got)
	}
}

// A second gift cannot be taken back, so a stop that died between its two items
// resumes on the item it had not sent.
func TestDeliverTourResumeSkipsFinishedStops(t *testing.T) {
	f := newTour(t, &game.State{}, fitStop("bot-a"), fitStop("bot-b"))
	done := "bot-a\tadvanced_drone_bay\nbot-a\tmining_drone\nbot-b\tadvanced_drone_bay\n"
	if err := os.WriteFile(f.manifest+".progress", []byte(done), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := f.d.DeliverTour(context.Background(), f.manifest); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(giftedTo(f.client), " "); got != "bot-b-name:mining_drone" {
		t.Fatalf("got %s", got)
	}
}

func TestDeliverTourStopsWhenRecipientMoved(t *testing.T) {
	f := newTour(t, &game.State{}, fitStop("bot-a"))
	f.d.TourRecipientSystem = func(string) (string, error) { return "elsewhere", nil }

	err := f.d.DeliverTour(context.Background(), f.manifest)
	if err == nil || !strings.Contains(err.Error(), "bot-a") {
		t.Fatalf("want an error naming the bot, got %v", err)
	}
	if len(f.client.giftCalls) != 0 {
		t.Fatalf("no gift to a moved bot: %v", giftedTo(f.client))
	}
}

func TestDeliverTourStrongholdNeedsUnlock(t *testing.T) {
	stop := fitStop("bot-a")
	stop.Stronghold = true
	f := newTour(t, &game.State{}, fitStop("bot-b"), stop)

	if err := f.d.DeliverTour(context.Background(), f.manifest); err == nil {
		t.Fatal("a locked courier must refuse a stronghold tour")
	}
	if len(f.client.calls) != 0 || len(f.client.giftCalls) != 0 {
		t.Fatalf("refused before any action: %v", f.client.calls)
	}
}

func TestDeliverTourShortCargoStops(t *testing.T) {
	stop := fitStop("bot-a")
	stop.Items["mining_drone"] = 500
	f := newTour(t, &game.State{}, stop)

	if err := f.d.DeliverTour(context.Background(), f.manifest); err == nil {
		t.Fatal("short cargo must stop the tour")
	}
	if len(f.client.giftCalls) != 0 {
		t.Fatalf("no partial gift: %v", giftedTo(f.client))
	}
}
