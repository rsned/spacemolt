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

// Duel modes. A duel is fought either at a real arena POI (server
// v0.586.0: knockout instead of death, everything restored on the spot) or
// in lawless space the old way (destruction is real, respawn at home).
//
// Arena is the right default for new scenarios -- it takes repair, respawn
// and third-party interference out of the loop entirely. Two measurement
// classes CANNOT move there, because the arena changes the very mechanic
// they measure: fleeing forfeits the match rather than escaping it, and
// emergency warp / emergency cloak never trigger. Those stay lawless.
const (
	ModeArena   = "arena"
	ModeLawless = "lawless"
)

var validModes = map[string]bool{ModeArena: true, ModeLawless: true}

// Duel is one scenario entry; it runs Repeats times.
type Duel struct {
	ID       string `json:"id"`
	Purpose  string `json:"purpose"`
	Attacker string `json:"attacker"`
	Guest    string `json:"guest,omitempty"` // replaces bot B when set (S6c)
	// Mode is "arena" or "lawless"; empty takes the campaign's
	// DefaultMode. See Campaign.ModeOf.
	Mode     string  `json:"mode,omitempty"`
	FitA     FitSpec `json:"fit_a"`
	FitB     FitSpec `json:"fit_b"`
	Script   []Phase `json:"script"`
	MaxTicks int     `json:"max_ticks"`
	// RequireFull makes preflight wait for full shield+hull pools (regen
	// and armor-law scenarios need clean starting state).
	RequireFull bool `json:"require_full,omitempty"`
	Repeats     int  `json:"repeats"`
	ReloadEvery int  `json:"reload_every,omitempty"` // reload every N ticks (0 = no reload)
}

// Campaign is the whole scenario matrix plus its geography.
//
// Two fighting locations, picked per duel by mode:
//
//   - arena duels are fought at ArenaPOI in ArenaSystem (the Blood Arena,
//     `blood_arena` in `krynn`). Both sides must be undocked AT that POI
//     for the challenge/accept handshake.
//   - lawless duels are fought anywhere in LawlessSystem, by attacking.
//
// LawlessSystem falls back to ArenaSystem when unset, which is what makes
// pre-v0.586.0 campaign files -- where `arena_system` named the lawless
// duelling system, e.g. ashford -- keep running unchanged. That fallback
// is refused for a campaign that mixes both modes, where it would silently
// send the flee scenarios into the real arena; see LoadCampaign.
type Campaign struct {
	ArenaSystem    string `json:"arena_system"`
	ArenaPOI       string `json:"arena_poi,omitempty"`
	LawlessSystem  string `json:"lawless_system,omitempty"`
	StagingSystem  string `json:"staging_system"`
	StagingStation string `json:"staging_station"`
	// DefaultMode applies to every duel that does not name its own.
	// Empty means lawless, so legacy campaigns behave as they always did.
	DefaultMode string `json:"default_mode,omitempty"`
	Duels       []Duel `json:"duels"`
}

// ModeOf resolves the mode in force for d: the duel's own Mode, else the
// campaign DefaultMode, else lawless.
func (c *Campaign) ModeOf(d Duel) string {
	if d.Mode != "" {
		return d.Mode
	}
	if c.DefaultMode != "" {
		return c.DefaultMode
	}
	return ModeLawless
}

// FightSystem is the system d is fought in.
func (c *Campaign) FightSystem(d Duel) (string, error) {
	if c.ModeOf(d) == ModeArena {
		if c.ArenaSystem == "" {
			return "", fmt.Errorf("duel %q is arena mode but campaign has no arena_system", d.ID)
		}
		return c.ArenaSystem, nil
	}
	if c.LawlessSystem != "" {
		return c.LawlessSystem, nil
	}
	// Legacy: arena_system named the lawless duelling system.
	if c.ArenaSystem == "" {
		return "", fmt.Errorf("duel %q is lawless mode but campaign has no lawless_system", d.ID)
	}
	return c.ArenaSystem, nil
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
	if c.DefaultMode != "" && !validModes[c.DefaultMode] {
		return nil, fmt.Errorf("%s: default_mode %q must be %q or %q", path, c.DefaultMode, ModeArena, ModeLawless)
	}
	seen := map[string]bool{}
	var anyArena, anyLawless bool
	for i, d := range c.Duels {
		if d.ID == "" {
			return nil, fmt.Errorf("duel %d: missing id", i)
		}
		if seen[d.ID] {
			return nil, fmt.Errorf("duel %q: duplicate id", d.ID)
		}
		seen[d.ID] = true
		if d.Mode != "" && !validModes[d.Mode] {
			return nil, fmt.Errorf("duel %q: mode %q must be %q or %q", d.ID, d.Mode, ModeArena, ModeLawless)
		}
		if c.ModeOf(d) == ModeArena {
			anyArena = true
			if c.ArenaPOI == "" {
				return nil, fmt.Errorf("duel %q is arena mode: campaign needs arena_poi (e.g. %q) and arena_system (e.g. %q)",
					d.ID, "blood_arena", "krynn")
			}
		} else {
			anyLawless = true
		}
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
	// A half-migrated campaign is the dangerous case: arena_system has been
	// repointed at the real arena for the arena duels, but lawless_system
	// was never added -- so the legacy fallback would send the lawless
	// duels there too, where fleeing forfeits and the flee measurement is
	// silently garbage. Refuse to load rather than run it.
	if anyArena && anyLawless && c.LawlessSystem == "" {
		return nil, fmt.Errorf("%s: campaign mixes arena and lawless duels but sets no lawless_system; "+
			"arena_system %q is the arena's own system and cannot double as the lawless one", path, c.ArenaSystem)
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
