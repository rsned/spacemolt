package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rsned/spacemolt/pkg/game"
)

// announceFile and announceStateFile live in the agent's directory
// (data/agents/<agent-id>/). The operator edits the first; the second only
// remembers which message comes next.
const (
	announceFile      = "announcements.txt"
	announceStateFile = "announcements-state.json"
)

// announceConfig is a parsed announcements.txt.
//
// Format, one entry per line:
//
//	# items: pirate_moonshine, void_dust     -- goods to advertise (item ids)
//	# channel: system                        -- system (default) | local | faction
//	# any other comment line is ignored
//	Fresh {items} at {station}, best prices in the Galaxy!
//
// Placeholders: {items} = the advertised goods listed for sale here right now,
// {station} = the station name, {price:<item_id>} = that item's lowest ask.
type announceConfig struct {
	Channel  string
	Items    []string
	Messages []string
}

func parseAnnouncements(text string) announceConfig {
	cfg := announceConfig{Channel: "system"}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if rest, ok := strings.CutPrefix(line, "#"); ok {
			rest = strings.TrimSpace(rest)
			if v, ok := strings.CutPrefix(rest, "items:"); ok {
				for _, it := range strings.Split(v, ",") {
					if it = strings.TrimSpace(it); it != "" {
						cfg.Items = append(cfg.Items, it)
					}
				}
			} else if v, ok := strings.CutPrefix(rest, "channel:"); ok {
				if v = strings.TrimSpace(v); v != "" {
					cfg.Channel = v
				}
			}
			continue
		}
		cfg.Messages = append(cfg.Messages, line)
	}

	return cfg
}

// announceListing is one advertised item that is for sale here now.
type announceListing struct {
	Name  string
	Price int
}

// Announce is the town crier: it posts one rotating message, filled in from the
// station's live market, to the configured chat channel. Conjunction Junction
// lets contraband trade, and a broadcast every ten minutes in system chat tells
// traffic passing through Cloverfield what is on our shelves (operator,
// 2026-10-31).
//
// It says nothing rather than something untrue: undocked (no market to read),
// none of the advertised goods listed, or no message that can be filled in all
// mean no post. A message quoting {price:x} for an item not for sale is skipped
// in favour of the next one. The rotation advances past every message it
// considered, so the channel does not hear one line over and over.
func Announce(ctx context.Context, client game.GameClient, agentDir string, out io.Writer) error {
	if out == nil {
		out = io.Discard
	}
	raw, err := os.ReadFile(filepath.Join(agentDir, announceFile)) // #nosec G304 -- the agent's own directory
	if err != nil {
		return fmt.Errorf("announce: %w", err)
	}
	cfg := parseAnnouncements(string(raw))
	if len(cfg.Messages) == 0 || len(cfg.Items) == 0 {
		return fmt.Errorf("announce: %s needs '# items:' and at least one message", announceFile)
	}
	state := client.GetState()
	if state == nil || state.Player.DockedAtBase == "" {
		return nil
	}
	if err := client.ViewMarket(ctx, nil); err != nil {
		return fmt.Errorf("announce: view_market: %w", err)
	}
	station, stock := announceStock(client.GetRawJSON("market"), cfg.Items)
	if len(stock) == 0 {
		fmt.Fprintln(out, "announce: none of the advertised goods are listed; staying quiet") //nolint:errcheck
		return nil
	}

	next := loadAnnounceNext(agentDir)
	for i := range len(cfg.Messages) {
		idx := (next + i) % len(cfg.Messages)
		msg, ok := renderAnnouncement(cfg.Messages[idx], cfg.Items, stock, station)
		if !ok {
			continue
		}
		if err := client.Chat(ctx, cfg.Channel, msg, ""); err != nil {
			return fmt.Errorf("announce: chat: %w", err)
		}
		saveAnnounceNext(agentDir, idx+1)
		fmt.Fprintf(out, "announce [%s]: %s\n", cfg.Channel, msg) //nolint:errcheck
		return nil
	}
	fmt.Fprintln(out, "announce: no message could be filled from current listings") //nolint:errcheck

	return nil
}

// announceStock reads a view_market reply: the station name and, for each
// advertised item with sell orders, its name and lowest ask.
func announceStock(raw []byte, items []string) (string, map[string]announceListing) {
	var resp struct {
		Base  string `json:"base"`
		Items []struct {
			ItemID       string  `json:"item_id"`
			ItemName     string  `json:"item_name"`
			BestSell     float64 `json:"best_sell"`
			SellQuantity float64 `json:"sell_quantity"`
		} `json:"items"`
	}
	if len(raw) == 0 || json.Unmarshal(unwrapActionResult(raw), &resp) != nil {
		return "", nil
	}
	want := make(map[string]bool, len(items))
	for _, it := range items {
		want[it] = true
	}
	stock := map[string]announceListing{}
	for _, it := range resp.Items {
		if want[it.ItemID] && it.SellQuantity > 0 && it.BestSell > 0 {
			name := it.ItemName
			if name == "" {
				name = it.ItemID
			}
			stock[it.ItemID] = announceListing{Name: name, Price: int(it.BestSell)}
		}
	}

	return resp.Base, stock
}

// renderAnnouncement fills a template. ok is false when it names a price for an
// item not for sale, so the caller tries the next message instead.
func renderAnnouncement(tmpl string, items []string, stock map[string]announceListing, station string) (string, bool) {
	var names []string
	for _, it := range items { // the operator's order, not the market's
		if l, ok := stock[it]; ok {
			names = append(names, l.Name)
		}
	}
	msg := strings.ReplaceAll(tmpl, "{items}", joinAnd(names))
	msg = strings.ReplaceAll(msg, "{station}", station)
	for {
		i := strings.Index(msg, "{price:")
		if i < 0 {
			break
		}
		j := strings.Index(msg[i:], "}")
		if j < 0 {
			break
		}
		id := msg[i+len("{price:") : i+j]
		l, ok := stock[id]
		if !ok {
			return "", false
		}
		msg = msg[:i] + groupThousands(l.Price) + msg[i+j+1:]
	}

	return msg, true
}

// joinAnd renders "a", "a and b", "a, b and c".
func joinAnd(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	}

	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// groupThousands renders 6500 as "6,500".
func groupThousands(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}

	return s
}

func loadAnnounceNext(agentDir string) int {
	var st struct {
		Next int `json:"next"`
	}
	raw, err := os.ReadFile(filepath.Join(agentDir, announceStateFile)) // #nosec G304 -- the agent's own directory
	if err != nil || json.Unmarshal(raw, &st) != nil || st.Next < 0 {
		return 0
	}

	return st.Next
}

// saveAnnounceNext records the rotation position. A failed write costs one
// repeated message, so it is not an error.
func saveAnnounceNext(agentDir string, next int) {
	raw, err := json.Marshal(map[string]int{"next": next})
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(agentDir, announceStateFile), raw, 0o644) //nolint:gosec // operator-readable by design
}
