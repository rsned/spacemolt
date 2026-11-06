package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
)

// tourStatusFiles are where a tour's recipients report their location.
var tourStatusFiles = []string{"data/overmind/mb-status.json", "data/overmind/unlock-status.json"}

// TourStop is one recipient on a deliver_tour manifest.
type TourStop struct {
	StationID  string         `json:"station_id"`
	System     string         `json:"system"`
	Recipient  string         `json:"recipient"`
	Items      map[string]int `json:"items"`
	Stronghold bool           `json:"stronghold,omitempty"`
	// FromStation sends the parts from this agent's storage there instead of
	// from cargo. A hub bot uses it: the gift lands in the recipient's storage
	// at the hub, so the recipient's location and the hold do not matter.
	FromStation string `json:"from_station,omitempty"`
}

// DeliverTour gifts a reviewed manifest from cargo, stop by stop. Gifts cannot
// be taken back, so each one is written to <manifest>.progress as pending before
// it is sent and done after. A restarted tour skips done gifts and halts on a
// pending one, which may or may not have landed, until an operator settles it.
// A stop is checked before its first gift: the recipient must still be in that
// system and the cargo must cover the stop.
func (d *WorkerDispatch) DeliverTour(ctx context.Context, manifestPath string) error {
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("deliver_tour: %w", err)
	}
	var stops []TourStop
	if err = json.Unmarshal(raw, &stops); err != nil {
		return fmt.Errorf("deliver_tour: decode %s: %w", manifestPath, err)
	}
	if !smugglingUnlocked(d.Client.GetState()) {
		for _, s := range stops {
			if s.Stronghold {
				return fmt.Errorf("deliver_tour: %s is in a stronghold and this agent lacks the pirate unlock", s.Recipient)
			}
		}
	}
	progressPath := manifestPath + ".progress"
	done, err := readTourProgress(progressPath)
	if err != nil {
		return fmt.Errorf("deliver_tour: %w", err)
	}

	for _, stop := range stops {
		items := slices.Sorted(maps.Keys(stop.Items))
		for _, item := range items {
			if done[tourKey(stop.Recipient, item)] == tourPending {
				return fmt.Errorf("deliver_tour: %s %s may already be gifted; check its storage and mark the progress line done or remove it", stop.Recipient, item)
			}
		}
		items = slices.DeleteFunc(items, func(item string) bool { return done[tourKey(stop.Recipient, item)] == tourDone })
		if len(items) == 0 {
			continue
		}
		if stop.FromStation == "" {
			if err = d.checkTourStop(ctx, stop, items); err != nil {
				return fmt.Errorf("deliver_tour: %w", err)
			}
		}
		// Travel first: it fails often and sends nothing, so it stays outside
		// the pending window and a retry needs no operator.
		toSys, toPOI, rerr := resolveBase(ctx, d.KB, stop.StationID)
		if rerr != nil {
			return fmt.Errorf("deliver_tour: %s: %w", stop.Recipient, rerr)
		}
		if err = d.autopilotAndDock(ctx, toSys, toPOI); err != nil {
			return fmt.Errorf("deliver_tour: %s: travel: %w", stop.Recipient, err)
		}
		for _, item := range items {
			key := tourKey(stop.Recipient, item)
			if err = appendTourProgress(progressPath, key, tourPending); err != nil {
				return fmt.Errorf("deliver_tour: %s %s not sent: %w", stop.Recipient, item, err)
			}
			if err = d.Deliver(ctx, item, stop.Items[item], stop.FromStation, stop.StationID, stop.Recipient); err != nil {
				return fmt.Errorf("deliver_tour: %s: %w", stop.Recipient, err)
			}
			if err = appendTourProgress(progressPath, key, tourDone); err != nil {
				return fmt.Errorf("deliver_tour: %s %s gifted but not recorded: %w", stop.Recipient, item, err)
			}
		}
		fmt.Fprintf(d.Out, "deliver_tour: %s done\n", stop.Recipient) //nolint:errcheck
	}

	return nil
}

// checkTourStop refuses a stop whose recipient moved or whose cargo is short.
// Deliver alone would hand over a short quantity and report success.
func (d *WorkerDispatch) checkTourStop(ctx context.Context, stop TourStop, items []string) error {
	st := d.Client.GetState()
	for _, item := range items {
		if have := cargoCount(st, item); have < stop.Items[item] {
			return fmt.Errorf("%s needs %d %s, %d aboard", stop.Recipient, stop.Items[item], item, have)
		}
	}
	locate := d.TourRecipientSystem
	if locate == nil {
		locate = statusRecipientSystem
	}
	system, err := locate(stop.Recipient)
	if err != nil {
		return fmt.Errorf("locate %s: %w", stop.Recipient, err)
	}
	if strings.EqualFold(system, stop.System) {
		return nil
	}
	if d.KB != nil {
		systems, kerr := d.KB.GetSystems(ctx)
		if kerr != nil {
			return fmt.Errorf("locate %s: %w", stop.Recipient, kerr)
		}
		for _, s := range systems {
			if s.ID == stop.System && strings.EqualFold(s.Name, system) {
				return nil
			}
		}
	}

	return fmt.Errorf("%s is in %s, manifest says %s", stop.Recipient, system, stop.System)
}

// statusRecipientSystem reads the recipient's system from the fleet status
// files, which carry the system's display name.
func statusRecipientSystem(agent string) (string, error) {
	for _, path := range tourStatusFiles {
		raw, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		var status struct {
			Workers []struct {
				AgentID string `json:"agent_id"`
				System  string `json:"system"`
			} `json:"workers"`
		}
		if err = json.Unmarshal(raw, &status); err != nil {
			return "", fmt.Errorf("decode %s: %w", path, err)
		}
		for _, w := range status.Workers {
			if w.AgentID == agent && w.System != "" {
				return w.System, nil
			}
		}
	}

	return "", fmt.Errorf("%s not in any status file", agent)
}

func tourKey(recipient, item string) string { return recipient + "\t" + item }

// Gift states in the progress file; the last line for a key wins.
const (
	tourPending = "pending"
	tourDone    = "done"
)

func readTourProgress(path string) (map[string]string, error) {
	done := map[string]string{}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return done, nil
	}
	if err != nil {
		return nil, err
	}
	for line := range strings.Lines(string(raw)) {
		key, state, ok := cutLastTab(strings.TrimRight(line, "\n"))
		if !ok {
			return nil, fmt.Errorf("bad progress line %q", line)
		}
		done[key] = state
	}

	return done, nil
}

func cutLastTab(line string) (before, after string, ok bool) {
	i := strings.LastIndexByte(line, '\t')
	if i < 0 {
		return "", "", false
	}

	return line[:i], line[i+1:], true
}

func appendTourProgress(path, key, state string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err = fmt.Fprintln(f, key+"\t"+state); err != nil {
		_ = f.Close()
		return err
	}

	return f.Close()
}
