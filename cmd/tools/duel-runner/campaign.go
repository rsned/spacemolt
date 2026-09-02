// Command duel-runner executes scripted 1v1 calibration duels between two
// owned agents from a campaign file, per the Phase B design:
// kb/docs/superpowers/specs/2026-09-01-phase-b-calibration-duels-design.md
package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// FitSpec is the exact hull + module list a bot must carry for a duel.
type FitSpec struct {
	Hull    string            `json:"hull"`
	Modules []string          `json:"modules"`
	Ammo    map[string]string `json:"ammo,omitempty"` // weapon type_id -> ammo item_id
}

// Phase is one segment of a duel's stance script. HoldRing, when set,
// pins the shared separation ring (0=engaged .. 3=outer) by issuing
// advance/retreat corrections to both sides.
//
// HoldRingA / HoldRingB independently override HoldRing for one side only
// (needed for S1-odd's asymmetric-hold duels, where the two sides must
// converge on different rings to land on an odd total zone_distance). When
// set, a per-side field always wins for that side; HoldRing is the
// both-sides default.
type Phase struct {
	FromTick  int    `json:"from_tick"`
	StanceA   string `json:"stance_a"`
	StanceB   string `json:"stance_b"`
	HoldRing  *int   `json:"hold_ring,omitempty"`
	HoldRingA *int   `json:"hold_ring_a,omitempty"`
	HoldRingB *int   `json:"hold_ring_b,omitempty"`
}

// HoldA returns the effective ring-hold for side A: HoldRingA if set,
// otherwise the shared HoldRing (nil if neither is set).
func (p Phase) HoldA() *int {
	if p.HoldRingA != nil {
		return p.HoldRingA
	}
	return p.HoldRing
}

// HoldB returns the effective ring-hold for side B: HoldRingB if set,
// otherwise the shared HoldRing (nil if neither is set).
func (p Phase) HoldB() *int {
	if p.HoldRingB != nil {
		return p.HoldRingB
	}
	return p.HoldRing
}

// Duel is one scenario entry; it runs Repeats times.
type Duel struct {
	ID          string  `json:"id"`
	Purpose     string  `json:"purpose"`
	Attacker    string  `json:"attacker"`
	Guest       string  `json:"guest,omitempty"` // replaces bot B when set (S6c)
	FitA        FitSpec `json:"fit_a"`
	FitB        FitSpec `json:"fit_b"`
	Script      []Phase `json:"script"`
	MaxTicks    int     `json:"max_ticks"`
	Repeats     int     `json:"repeats"`
	ReloadEvery int     `json:"reload_every,omitempty"` // reload every N ticks (0 = no reload)
}

// Campaign is the whole scenario matrix plus its geography.
type Campaign struct {
	ArenaSystem    string `json:"arena_system"`
	StagingSystem  string `json:"staging_system"`
	StagingStation string `json:"staging_station"`
	Duels          []Duel `json:"duels"`
}

var validStances = map[string]bool{"fire": true, "brace": true, "evade": true, "flee": true}

// LoadCampaign reads and validates a campaign file. Every defect found is
// an error naming the duel — a campaign typo must never surface mid-run.
func LoadCampaign(path string) (*Campaign, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Campaign
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if c.ArenaSystem == "" || c.StagingSystem == "" || c.StagingStation == "" {
		return nil, fmt.Errorf("%s: arena_system, staging_system, staging_station are all required", path)
	}
	if len(c.Duels) == 0 {
		return nil, fmt.Errorf("%s: campaign has no duels", path)
	}
	seen := map[string]bool{}
	for i, d := range c.Duels {
		if d.ID == "" {
			return nil, fmt.Errorf("duel %d: missing id", i)
		}
		if seen[d.ID] {
			return nil, fmt.Errorf("duel %q: duplicate id", d.ID)
		}
		seen[d.ID] = true
		if d.MaxTicks <= 0 {
			return nil, fmt.Errorf("duel %q: max_ticks must be > 0", d.ID)
		}
		if d.Repeats <= 0 {
			return nil, fmt.Errorf("duel %q: repeats must be > 0", d.ID)
		}
		if d.ReloadEvery < 0 {
			return nil, fmt.Errorf("duel %q: reload_every must be >= 0", d.ID)
		}
		if len(d.Script) == 0 {
			return nil, fmt.Errorf("duel %q: empty script", d.ID)
		}
		for _, p := range d.Script {
			if !validStances[p.StanceA] || !validStances[p.StanceB] {
				return nil, fmt.Errorf("duel %q: invalid stance %q/%q", d.ID, p.StanceA, p.StanceB)
			}
			if p.HoldRing != nil && (*p.HoldRing < 0 || *p.HoldRing > 3) {
				return nil, fmt.Errorf("duel %q: hold_ring %d out of range 0..3", d.ID, *p.HoldRing)
			}
			if p.HoldRingA != nil && (*p.HoldRingA < 0 || *p.HoldRingA > 3) {
				return nil, fmt.Errorf("duel %q: hold_ring_a %d out of range 0..3", d.ID, *p.HoldRingA)
			}
			if p.HoldRingB != nil && (*p.HoldRingB < 0 || *p.HoldRingB > 3) {
				return nil, fmt.Errorf("duel %q: hold_ring_b %d out of range 0..3", d.ID, *p.HoldRingB)
			}
		}
	}
	return &c, nil
}

// PhaseAt returns the script phase in force at tick (the last phase whose
// FromTick <= tick; before the first phase, the first phase applies).
func (d *Duel) PhaseAt(tick int) Phase {
	cur := d.Script[0]
	for _, p := range d.Script {
		if p.FromTick <= tick {
			cur = p
		}
	}
	return cur
}
