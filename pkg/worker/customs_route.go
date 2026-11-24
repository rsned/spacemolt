package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/customs"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// DefaultCustomsAlertsPath records contraband runs that found no
// customs-free route, for the operator (customs-map report lists them).
const DefaultCustomsAlertsPath = "data/overmind/customs-alerts.json"

// ErrNoCustomsFreeRoute is returned when a ship carrying contraband has no
// route to its destination that stays out of every monitored customs
// system. The ship stays where it is with its cargo (operator decision
// 2026-11-23: fail and alert, never fly the monitored route, never unload).
var ErrNoCustomsFreeRoute = fmt.Errorf("%w: no customs-free route", ErrRouteThroughAvoided)

// contrabandAboard lists the KB-category "contraband" items in cargo as
// "id xN". A nil kb finds none.
func contrabandAboard(ctx context.Context, kb knowledge.Base, cargo []game.CargoItem) []string {
	if kb == nil {
		return nil
	}
	var out []string
	for _, c := range cargo {
		if c.Quantity <= 0 {
			continue
		}
		it, err := kb.GetItem(ctx, c.ItemID)
		if err != nil || it == nil || it.Category != "contraband" {
			continue
		}
		out = append(out, fmt.Sprintf("%s x%g", c.ItemID, c.Quantity))
	}
	return out
}

// customsAvoid returns the monitored customs systems this route must stay
// out of, or nil when there is no map, nothing is monitored, or the hold
// carries no contraband. The map is read BEFORE the KB is touched, so a
// worker without a map never pays for (or trips over) item lookups. The
// route's own endpoints are never avoided: the ship cannot leave where it
// is or skip where it must go.
func customsAvoid(ctx context.Context, deps AutopilotDeps, client game.GameClient, origin, dest string, out io.Writer) map[string]string {
	path := deps.CustomsMapPath
	if path == "" {
		path = customs.DefaultMapPath
	}
	m, err := customs.Load(path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			fmt.Fprintf(out, "   customs: map unreadable, not avoiding customs: %v\n", err) //nolint:errcheck
		}
		return nil
	}
	monitored := m.Monitored()
	if len(monitored) == 0 {
		return nil
	}
	st := client.GetState()
	if st == nil {
		return nil
	}
	items := contrabandAboard(ctx, deps.KB, st.Ship.Cargo)
	if len(items) == 0 {
		return nil
	}
	for _, end := range []string{origin, dest} {
		end = strings.ToLower(end)
		if _, ok := monitored[end]; ok {
			fmt.Fprintf(out, "   customs: %s is monitored but is the route endpoint; flying anyway\n", end) //nolint:errcheck
			delete(monitored, end)
		}
	}
	ids := slices.Sorted(maps.Keys(monitored))
	fmt.Fprintf(out, "   customs: contraband aboard (%s); avoiding %d monitored systems: %s\n", //nolint:errcheck
		strings.Join(items, ", "), len(ids), strings.Join(ids, ","))
	return monitored
}

// customsAlert is one blocked (agent, destination) pair.
type customsAlert struct {
	Agent     string    `json:"agent"`
	Dest      string    `json:"dest"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
	Count     int       `json:"count"`
	Monitored []string  `json:"monitored"`
}

type customsAlertsFile struct {
	Alerts map[string]customsAlert `json:"alerts"`
}

// recordCustomsAlert bumps the alert for (agent, dest) and reports whether
// it is new, so only the first failure logs the full line. A write error
// still counts as first, so a broken file never silences the alert.
func recordCustomsAlert(path, agent, dest string, monitored []string, now time.Time) bool {
	if path == "" {
		path = DefaultCustomsAlertsPath
	}
	var f customsAlertsFile
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &f) //nolint:errcheck // a corrupt file starts over
	}
	if f.Alerts == nil {
		f.Alerts = map[string]customsAlert{}
	}
	key := agent + "|" + dest
	a, seen := f.Alerts[key]
	if !seen {
		a = customsAlert{Agent: agent, Dest: dest, FirstSeen: now}
	}
	a.LastSeen, a.Monitored = now, monitored
	a.Count++
	f.Alerts[key] = a
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return true
	}
	tmp := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tmp")
	if os.WriteFile(tmp, data, 0o644) != nil || os.Rename(tmp, path) != nil {
		return true
	}
	return !seen
}

// customsBlocked turns a failed detour that hit monitored customs systems
// into ErrNoCustomsFreeRoute, logging the full line once per (agent, dest)
// and a short line on repeats.
func customsBlocked(deps AutopilotDeps, client game.GameClient, dest string, hits []string, out io.Writer) error {
	agent := ""
	if st := client.GetState(); st != nil {
		agent = st.Player.Username
	}
	if recordCustomsAlert(deps.CustomsAlertsPath, agent, dest, hits, time.Now()) {
		fmt.Fprintf(out, "   customs: no customs-free route to %s (monitored on every path: %s)\n", dest, strings.Join(hits, ",")) //nolint:errcheck
	} else {
		fmt.Fprintf(out, "   customs: %s still blocked by customs (alert repeat)\n", dest) //nolint:errcheck
	}
	return fmt.Errorf("%w: to %s (monitored on every path: %s)", ErrNoCustomsFreeRoute, dest, strings.Join(hits, ","))
}

// CustomsAlertLines formats the alerts file for the customs-map report,
// one line per blocked (agent, destination), newest first. A missing file
// is no alerts.
func CustomsAlertLines(path string) []string {
	if path == "" {
		path = DefaultCustomsAlertsPath
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var f customsAlertsFile
	if json.Unmarshal(data, &f) != nil {
		return nil
	}
	alerts := slices.SortedFunc(maps.Values(f.Alerts), func(a, b customsAlert) int { return b.LastSeen.Compare(a.LastSeen) })
	lines := make([]string, 0, len(alerts))
	for _, a := range alerts {
		lines = append(lines, fmt.Sprintf("  %-22s -> %-20s x%-3d last %s  monitored on every path: %s",
			a.Agent, a.Dest, a.Count, a.LastSeen.Format("01-02 15:04"), strings.Join(a.Monitored, ",")))
	}
	return lines
}
