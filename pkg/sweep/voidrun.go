package sweep

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"time"
)

// VoidLease is how long a zone claim lasts without saved progress; after it
// the next free explorer takes the zone over at its saved waypoint.
const VoidLease = 2 * time.Hour

// voidSmallTank is the launches-per-tank below which an explorer takes the
// largest zone it can fly on about two tanks, not simply the largest zone.
const voidSmallTank = 30

// VoidZone is a zone in a run: its square wave and who is flying it.
// Next is the index of the next waypoint to reach; the zone is Done when
// every waypoint has been reached. Need is the launch count, one per leg.
type VoidZone struct {
	Zone
	Waypoints []Point  `json:"waypoints"`
	Claimant  string   `json:"claimant,omitempty"`
	Next      int      `json:"next"`
	Done      bool     `json:"done"`
	LeaseUTC  string   `json:"lease_utc,omitempty"`
	Found     []string `json:"found,omitempty"`
}

// Need is the launches the zone's square wave takes: one per waypoint.
func (z *VoidZone) Need() int { return len(z.Waypoints) }

// VoidRun is the shared ledger of one interior void sweep.
type VoidRun struct {
	Created string      `json:"created"`
	Zones   []*VoidZone `json:"zones"`
}

// NewVoidRun lays a square wave with lanes spacing apart over each zone.
func NewVoidRun(zones []Zone, spacing float64, now time.Time) *VoidRun {
	r := &VoidRun{Created: now.UTC().Format(time.RFC3339)}
	for _, z := range zones {
		r.Zones = append(r.Zones, &VoidZone{Zone: z, Waypoints: Waypoints(Lanes(z, spacing))})
	}
	return r
}

// Active reports whether any zone has been claimed or flown.
func (r *VoidRun) Active() bool {
	return slices.ContainsFunc(r.Zones, func(z *VoidZone) bool { return z.Claimant != "" || z.Next > 0 || z.Done })
}

// LoadVoidRun reads a run ledger.
func LoadVoidRun(path string) (*VoidRun, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("void run read: %w", err)
	}
	var r VoidRun
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("void run decode %s: %w", path, err)
	}
	return &r, nil
}

// SaveVoidRun writes a run ledger atomically (temp file + rename).
func SaveVoidRun(path string, r *VoidRun) error {
	data, err := json.MarshalIndent(r, "", " ")
	if err != nil {
		return fmt.Errorf("void run marshal: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("void run mkdir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("void run write: %w", err)
	}
	return os.Rename(tmp, path)
}

// withVoidRun runs fn on the ledger under an exclusive flock on path.lock
// and saves it when fn reports a change: worker processes share the file.
func withVoidRun(path string, fn func(r *VoidRun) (bool, error)) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("void run mkdir: %w", err)
	}
	lock, err := os.OpenFile(path+".lock", os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return fmt.Errorf("void run lock: %w", err)
	}
	defer lock.Close() //nolint:errcheck
	if ferr := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); ferr != nil {
		return fmt.Errorf("void run flock: %w", ferr)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) //nolint:errcheck
	r, err := LoadVoidRun(path)
	if err != nil {
		return err
	}
	changed, err := fn(r)
	if err != nil || !changed {
		return err
	}
	return SaveVoidRun(path, r)
}

// leaseLive reports whether z's claim was refreshed within VoidLease of now.
func leaseLive(z *VoidZone, now time.Time) bool {
	at, err := time.Parse(time.RFC3339, z.LeaseUTC)
	return err == nil && now.Sub(at) < VoidLease
}

// ClaimZone hands agent a zone to fly: its own unfinished claim if it holds
// one (a restart), else the largest free zone that ok accepts (nil ok
// accepts all). A zone is free when unclaimed or its lease has lapsed; a
// lapsed zone keeps its Next. An explorer with fewer than voidSmallTank
// launches per tank prefers the largest zone needing at most two tanks.
// Nil, nil when nothing is left.
func ClaimZone(path, agent string, launchesPerTank float64, ok func(*VoidZone) bool, now time.Time) (*VoidZone, error) {
	var got *VoidZone
	err := withVoidRun(path, func(r *VoidRun) (bool, error) {
		stamp := now.UTC().Format(time.RFC3339)
		for _, z := range r.Zones {
			if z.Claimant == agent && !z.Done {
				z.LeaseUTC = stamp
				got = z
				return true, nil
			}
		}
		var free []*VoidZone
		for _, z := range r.Zones {
			if z.Done || (z.Claimant != "" && leaseLive(z, now)) || (ok != nil && !ok(z)) {
				continue
			}
			free = append(free, z)
		}
		slices.SortStableFunc(free, func(a, b *VoidZone) int {
			switch {
			case a.Area > b.Area:
				return -1
			case a.Area < b.Area:
				return 1
			}
			return 0
		})
		if len(free) == 0 {
			return false, nil
		}
		pick := free[0]
		if launchesPerTank < voidSmallTank {
			if i := slices.IndexFunc(free, func(z *VoidZone) bool { return float64(z.Need()) <= 2*launchesPerTank }); i >= 0 {
				pick = free[i]
			}
		}
		pick.Claimant, pick.LeaseUTC = agent, stamp
		got = pick
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	if got == nil {
		return nil, nil
	}
	c := *got
	c.Waypoints = slices.Clone(got.Waypoints)
	return &c, nil
}

// SaveProgress records that agent's zone has reached waypoint next and the
// hidden systems found in it, and refreshes the lease. Reaching the last
// waypoint marks the zone done and releases it.
func SaveProgress(path, agent, zoneID string, next int, found []string, now time.Time) error {
	return withVoidRun(path, func(r *VoidRun) (bool, error) {
		i := slices.IndexFunc(r.Zones, func(z *VoidZone) bool { return z.ID == zoneID })
		if i < 0 {
			return false, fmt.Errorf("void run: no zone %s", zoneID)
		}
		z := r.Zones[i]
		if z.Claimant != agent {
			return false, errors.New("void run: " + agent + " does not hold " + zoneID + " (held by " + z.Claimant + ")")
		}
		z.Next = max(z.Next, next)
		for _, f := range found {
			if !slices.Contains(z.Found, f) {
				z.Found = append(z.Found, f)
			}
		}
		z.LeaseUTC = now.UTC().Format(time.RFC3339)
		if z.Next >= len(z.Waypoints) {
			z.Done, z.Claimant, z.LeaseUTC = true, "", ""
		}
		return true, nil
	})
}
