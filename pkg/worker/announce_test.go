package worker

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

// announceFake answers view_market with a fixed book and records chat posts.
type announceFake struct {
	game.GameClient
	docked string
	items  []map[string]any
	posts  []string
	chans  []string
}

func (f *announceFake) GetState() *game.State {
	s := &game.State{}
	s.Player.DockedAtBase = f.docked
	return s
}

func (f *announceFake) ViewMarket(context.Context, map[string]any) error { return nil }

func (f *announceFake) GetRawJSON(key string) []byte {
	if key != "market" {
		return nil
	}
	raw, _ := json.Marshal(map[string]any{"action": "view_market", "base": "Conjunction Junction", "items": f.items})
	return raw
}

func (f *announceFake) Chat(_ context.Context, channel, content, _ string) error {
	f.chans = append(f.chans, channel)
	f.posts = append(f.posts, content)
	return nil
}

func marketListing(id, name string, price, qty int) map[string]any {
	return map[string]any{"item_id": id, "item_name": name, "best_sell": price, "sell_quantity": qty}
}

func writeAnnouncements(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "announcements.txt"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

const cryerFile = `# items: pirate_moonshine, void_dust, nerve_burn
# channel: system
Fresh {items} at {station}, best prices in the Galaxy!
Void dust only {price:void_dust} a unit at {station}!
`

// The crier posts one message per run, filling {items} with only the
// advertised goods actually listed for sale here.
func TestAnnounceFillsItemsFromListings(t *testing.T) {
	dir := writeAnnouncements(t, cryerFile)
	c := &announceFake{docked: "cj", items: []map[string]any{
		marketListing("pirate_moonshine", "Pirate Moonshine", 300, 500),
		marketListing("void_dust", "Void Dust", 6500, 40),
		marketListing("nerve_burn", "Nerve Burn", 7000, 0), // none for sale
		marketListing("iron_ore", "Iron Ore", 5, 9999),     // not advertised
	}}

	if err := Announce(context.Background(), c, dir, nil); err != nil {
		t.Fatal(err)
	}
	if len(c.posts) != 1 || c.chans[0] != "system" {
		t.Fatalf("posts=%v chans=%v, want one system post", c.posts, c.chans)
	}
	want := "Fresh Pirate Moonshine and Void Dust at Conjunction Junction, best prices in the Galaxy!"
	if c.posts[0] != want {
		t.Errorf("post = %q\nwant  %q", c.posts[0], want)
	}
}

// Messages rotate, one per run, so the channel does not hear the same line twice
// in a row.
func TestAnnounceRotates(t *testing.T) {
	dir := writeAnnouncements(t, cryerFile)
	c := &announceFake{docked: "cj", items: []map[string]any{
		marketListing("pirate_moonshine", "Pirate Moonshine", 300, 500),
		marketListing("void_dust", "Void Dust", 6500, 40),
	}}

	for range 3 {
		if err := Announce(context.Background(), c, dir, nil); err != nil {
			t.Fatal(err)
		}
	}
	if len(c.posts) != 3 || c.posts[0] == c.posts[1] || c.posts[0] != c.posts[2] {
		t.Fatalf("posts = %q, want A, B, A", c.posts)
	}
	if !strings.Contains(c.posts[1], "6,500") {
		t.Errorf("price message = %q, want the void dust price 6,500", c.posts[1])
	}
}

// A message quoting a price for something not for sale is skipped in favour of
// the next one that can be said truthfully.
func TestAnnounceSkipsMessageForMissingItem(t *testing.T) {
	dir := writeAnnouncements(t, `# items: pirate_moonshine, void_dust
Void dust only {price:void_dust} at {station}!
Moonshine on tap at {station}!
`)
	c := &announceFake{docked: "cj", items: []map[string]any{marketListing("pirate_moonshine", "Pirate Moonshine", 300, 500)}}

	if err := Announce(context.Background(), c, dir, nil); err != nil {
		t.Fatal(err)
	}
	if len(c.posts) != 1 || c.posts[0] != "Moonshine on tap at Conjunction Junction!" {
		t.Errorf("posts = %q, want the moonshine line", c.posts)
	}
}

// Empty shelves: say nothing rather than advertise what we do not have.
func TestAnnounceSilentWhenNothingListed(t *testing.T) {
	dir := writeAnnouncements(t, cryerFile)
	c := &announceFake{docked: "cj", items: []map[string]any{marketListing("iron_ore", "Iron Ore", 5, 9999)}}

	if err := Announce(context.Background(), c, dir, nil); err != nil {
		t.Fatal(err)
	}
	if len(c.posts) != 0 {
		t.Errorf("posted %q with nothing advertised in stock", c.posts)
	}
}

// Undocked, the market cannot be read, so the crier waits for the next run.
func TestAnnounceUndockedIsQuiet(t *testing.T) {
	dir := writeAnnouncements(t, cryerFile)
	c := &announceFake{items: []map[string]any{marketListing("void_dust", "Void Dust", 6500, 40)}}

	if err := Announce(context.Background(), c, dir, nil); err != nil {
		t.Fatal(err)
	}
	if len(c.posts) != 0 {
		t.Errorf("posted %q while undocked", c.posts)
	}
}

func TestAnnounceIsSupported(t *testing.T) {
	if !supported["announce"] {
		t.Error(`"announce" missing from the supported command set`)
	}
}
