package linedash

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/assets"
)

type seedEvent struct {
	player string
	id     int
	typ    string
	at     string
	data   string
}

func seedEvents(t *testing.T, events []seedEvent, extra ...string) *Events {
	t.Helper()
	path := filepath.Join(t.TempDir(), "assets.db")
	store, err := assets.Open(assets.Config{DBPath: path})
	if err != nil {
		t.Fatalf("assets.Open: %v", err)
	}
	stmts := []string{
		`INSERT INTO agents(player_id, agent_id, username) VALUES ('p10', 'craftsman-10', 'Harrison ''Handiwork'' Hay')`,
		`INSERT INTO agents(player_id, agent_id, username) VALUES ('p9', 'trader-9', 'Trader Nine')`,
		`INSERT INTO agents(player_id, agent_id, username) VALUES ('pm', 'miningbot_0000', 'miningbot_0000')`,
	}
	stmts = append(stmts, extra...)
	for _, s := range stmts {
		if _, execErr := store.DB().Exec(s); execErr != nil {
			t.Fatalf("seed %q: %v", s, execErr)
		}
	}
	for _, e := range events {
		_, execErr := store.DB().Exec(
			`INSERT INTO action_log_events(player_id, event_id, event_type, created_at, data_json) VALUES (?, ?, ?, ?, ?)`,
			e.player, e.id, e.typ, e.at, e.data)
		if execErr != nil {
			t.Fatalf("seed event %d: %v", e.id, execErr)
		}
	}
	if closeErr := store.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	ev, err := OpenEvents(context.Background(), path)
	if err != nil {
		t.Fatalf("OpenEvents: %v", err)
	}
	t.Cleanup(func() { _ = ev.Close() })

	return ev
}

var eventsT0 = time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)

func TestWindowParsesStringPayloads(t *testing.T) {
	ev := seedEvents(t, []seedEvent{
		{"p10", 1, "crafting.completed", "2026-10-24T10:00:01Z", `{"recipe_id":"brew","runs":"3","facility_id":"f"}`},
		{"p9", 2, "trading.exchange_fill", "2026-10-24T10:00:02Z", `{"base_id":"voss","item_id":"moonshine","price":"450","quantity":"117","role":"seller","order_id":"o"}`},
		{"p9", 3, "trading.gift_sent", "2026-10-24T10:00:03Z", `{"item_id":"water_ice","quantity":"20","recipient":"x","sender":"y"}`},
		{"p9", 4, "trading.gift_received", "2026-10-24T10:00:04Z", `{"item_id":"water_ice","quantity":"21","recipient":"x","sender":"y"}`},
		{"p10", 5, "faction.withdraw", "2026-10-24T10:00:05Z", `{"base_id":"voss","faction_id":"f1","item_id":"grain","quantity":"500"}`},
		{"p10", 6, "storage.deposit_items", "2026-10-24T10:00:06Z", `{"item_id":"moonshine","quantity":"7"}`},
		{"p10", 8, "storage.deposit_items", "garbage", `{"item_id":"zzz","quantity":"1"}`},
		{"p10", 7, "trading.other", "2026-10-24T10:00:07Z", `{}`},
	})
	got, err := ev.Window(context.Background(), []string{"craftsman-10", "trader-9"}, eventsT0)
	if err != nil {
		t.Fatal(err)
	}
	// the garbage-created_at row is dropped; its neighbours are present.
	if len(got) != 6 {
		t.Fatalf("got %d events, want 6: %+v", len(got), got)
	}
	if g := got[0]; g.Type != "crafting.completed" || g.Runs != 3 || g.Recipe != "brew" || g.AgentID != "craftsman-10" || g.Units != 0 {
		t.Errorf("crafting = %+v", g)
	}
	if g := got[1]; g.Price != 450 || g.Units != 117 || g.Role != "seller" || g.Station != "voss" || g.Item != "moonshine" {
		t.Errorf("fill = %+v", g)
	}
	if g := got[2]; g.Units != 20 || g.Item != "water_ice" {
		t.Errorf("gift_sent = %+v", g)
	}
	if g := got[4]; g.Units != 500 || g.Station != "voss" || g.Item != "grain" {
		t.Errorf("withdraw = %+v", g)
	}
	if g := got[5]; g.Units != 7 || g.Item != "moonshine" || g.Station != "" {
		t.Errorf("deposit = %+v", g)
	}
	if !got[0].At.Equal(time.Date(2026, 10, 24, 10, 0, 1, 0, time.UTC)) {
		t.Errorf("At = %v", got[0].At)
	}
}

func TestEventsGiftSentAttributedBySenderPlayer(t *testing.T) {
	ev := seedEvents(t, []seedEvent{
		{"p10", 1, "trading.gift_sent", "2026-10-24T10:00:01Z", `{"item_id":"moonshine","quantity":"5","recipient":"Trader Nine","sender":"Harrison 'Handiwork' Hay"}`},
	})
	got, err := ev.Window(context.Background(), []string{"craftsman-10"}, eventsT0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].AgentID != "craftsman-10" {
		t.Fatalf("got %+v", got)
	}
}

func TestWindowResolvesGiftReceivedSenderByUsername(t *testing.T) {
	ev := seedEvents(t, []seedEvent{
		{"p9", 1, "trading.gift_received", "2026-10-24T10:00:01Z", `{"item_id":"moonshine","quantity":"5","sender":"miningbot_0000"}`},
		{"p9", 2, "trading.gift_received", "2026-10-24T10:00:02Z", `{"item_id":"moonshine","quantity":"6","sender":"Harrison 'Handiwork' Hay"}`},
		{"p9", 3, "trading.gift_received", "2026-10-24T10:00:03Z", `{"item_id":"moonshine","quantity":"7","sender":"Stranger"}`},
		{"p9", 4, "trading.gift_received", "2026-10-24T10:00:04Z", `{"item_id":"moonshine","quantity":"8","sender":"trader-9"}`},
	})
	got, err := ev.Window(context.Background(), []string{"trader-9"}, eventsT0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("got %d, want 4", len(got))
	}
	want := []string{"miningbot_0000", "craftsman-10", "Stranger", "trader-9"}
	for i, w := range want {
		if got[i].Sender != w {
			t.Errorf("event %d Sender = %q, want %q", i, got[i].Sender, w)
		}
		if got[i].AgentID != "trader-9" {
			t.Errorf("event %d AgentID = %q", i, got[i].AgentID)
		}
	}
}

func TestWindowSkipsCreditGifts(t *testing.T) {
	ev := seedEvents(t, []seedEvent{
		{"p9", 1, "trading.gift_received", "2026-10-24T10:00:01Z", `{"credits":"1000","sender":"y"}`},
		{"p9", 2, "trading.gift_sent", "2026-10-24T10:00:02Z", `{"credits":"1000","recipient":"y"}`},
		{"p9", 3, "trading.gift_sent", "2026-10-24T10:00:03Z", `{"item_id":"grain","quantity":"1"}`},
	})
	got, err := ev.Window(context.Background(), []string{"trader-9"}, eventsT0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Item != "grain" {
		t.Fatalf("got %+v", got)
	}
}

func TestWindowFiltersAgentsAndSince(t *testing.T) {
	ev := seedEvents(t, []seedEvent{
		{"p10", 1, "storage.deposit_items", "2026-10-24T09:59:59Z", `{"item_id":"a","quantity":"1"}`},
		{"p10", 3, "storage.deposit_items", "2026-10-24T10:00:00Z", `{"item_id":"b","quantity":"2"}`},
		{"p10", 2, "storage.deposit_items", "2026-10-24T10:00:00Z", `{"item_id":"c","quantity":"3"}`},
		{"p9", 4, "storage.deposit_items", "2026-10-24T11:00:00Z", `{"item_id":"d","quantity":"4"}`},
		{"pm", 5, "storage.deposit_items", "2026-10-24T12:00:00Z", `{"item_id":"e","quantity":"bad"}`},
	})
	got, err := ev.Window(context.Background(), []string{"craftsman-10", "miningbot_0000"}, eventsT0)
	if err != nil {
		t.Fatal(err)
	}
	var items []string
	for _, g := range got {
		items = append(items, g.Item)
	}
	// created_at ASC, event_id ASC; p9 excluded; before-since excluded; bad qty -> 0.
	if fmt.Sprint(items) != "[c b e]" {
		t.Fatalf("items = %v", items)
	}
	if got[2].Units != 0 {
		t.Errorf("bad quantity Units = %d", got[2].Units)
	}
	none, err := ev.Window(context.Background(), nil, eventsT0)
	if err != nil || len(none) != 0 {
		t.Errorf("empty agents: %v %v", none, err)
	}
}

func TestCursorAges(t *testing.T) {
	ev := seedEvents(t, nil,
		`INSERT INTO action_log_cursor(player_id, captured_at) VALUES ('p10', '2026-10-24T10:30:00Z')`,
	)
	ages, err := ev.CursorAges(context.Background(), []string{"craftsman-10", "trader-9"})
	if err != nil {
		t.Fatal(err)
	}
	if !ages["craftsman-10"].Equal(time.Date(2026, 10, 24, 10, 30, 0, 0, time.UTC)) {
		t.Errorf("craftsman-10 = %v", ages["craftsman-10"])
	}
	if a, ok := ages["trader-9"]; !ok || !a.IsZero() {
		t.Errorf("trader-9 = %v ok=%v, want present zero", a, ok)
	}
}

func TestOpenEventsMissingFileErrors(t *testing.T) {
	if _, err := OpenEvents(context.Background(), filepath.Join(t.TempDir(), "none.db")); err == nil {
		t.Fatal("want error for missing db")
	}
}
