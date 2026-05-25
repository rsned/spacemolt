package spar

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// weaponPrefixes are the module-id prefixes that count as a fitted weapon.
var weaponPrefixes = []string{
	"pulse_laser_", "autocannon_", "focused_beam_", "railgun_",
	"missile_launcher_", "ion_cannon_", "plasma_cannon_",
}

func isWeaponModule(id string) bool {
	for _, p := range weaponPrefixes {
		if strings.HasPrefix(id, p) {
			return true
		}
	}
	return false
}

// neededModules returns the module ids to buy+install so the ship has at least
// one weapon and one shield. Pure — order is weapon then shield.
func neededModules(installed []string, weaponID, shieldID string) []string {
	haveWeapon, haveShield := false, false
	for _, m := range installed {
		if isWeaponModule(m) {
			haveWeapon = true
		}
		if strings.HasPrefix(m, "shield_") {
			haveShield = true
		}
	}
	var need []string
	if !haveWeapon && weaponID != "" {
		need = append(need, weaponID)
	}
	if !haveShield && shieldID != "" {
		need = append(need, shieldID)
	}
	return need
}

// Combatant is one logged-in agent plus its assigned policy and setup config.
type Combatant struct {
	AgentID  string
	Username string
	Client   game.GameClient
	Policy   Policy // nil for the human partner slot
	WeaponID string
	ShieldID string
	NoEquip  bool
	Logger   *log.Logger // must be non-nil: game.NavigateToSystem dereferences it
}

// PlayerID returns the combatant's player id from live state.
func (c *Combatant) PlayerID() string { return c.Client.GetState().Player.ID }

// Setup equips the combatant (if needed), navigates it to the arena system, and
// travels to the rendezvous POI type. Equip runs first because stations are
// typically one or more jumps from lawless arenas.
func (c *Combatant) Setup(ctx context.Context, arenaSystem, rendezvousType string) error {
	if !c.NoEquip {
		if err := c.equip(ctx); err != nil {
			return fmt.Errorf("%s equip: %w", c.AgentID, err)
		}
	}
	if err := game.NavigateToSystem(c.Client, ctx, arenaSystem, c.Logger); err != nil {
		return fmt.Errorf("%s navigate to %s: %w", c.AgentID, arenaSystem, err)
	}
	if err := c.Client.GetSystem(ctx); err != nil {
		return fmt.Errorf("%s get_system: %w", c.AgentID, err)
	}
	poi := firstPOIOfType(c.Client.GetState().System, rendezvousType)
	if poi == "" {
		return fmt.Errorf("%s: arena %s has no rendezvous POI", c.AgentID, arenaSystem)
	}
	if _, err := c.Client.Travel(ctx, poi); err != nil {
		return fmt.Errorf("%s travel to rendezvous %s: %w", c.AgentID, poi, err)
	}
	return nil
}

// equip docks at a station, buys+installs any missing weapon/shield, refuels and
// repairs, then undocks. If the ship is already armed and shielded it is a no-op.
func (c *Combatant) equip(ctx context.Context) error {
	st := c.Client.GetState()
	need := neededModules(st.Ship.Modules, c.WeaponID, c.ShieldID)
	if len(need) == 0 {
		return nil
	}
	if err := game.NavigateAndDock(c.Client, ctx, c.Logger); err != nil {
		return fmt.Errorf("dock for equip: %w", err)
	}
	for _, mod := range need {
		if err := c.Client.Buy(ctx, mod, 1); err != nil {
			return fmt.Errorf("buy %s: %w", mod, err)
		}
		time.Sleep(game.SleepQuick)
		if err := c.Client.InstallMod(ctx, mod); err != nil {
			return fmt.Errorf("install %s: %w", mod, err)
		}
		time.Sleep(game.SleepQuick)
	}
	if err := c.Client.Refuel(ctx); err != nil {
		c.Logger.Printf("%s: refuel before spar failed (continuing): %v", c.AgentID, err)
	}
	if err := c.Client.Repair(ctx); err != nil {
		c.Logger.Printf("%s: repair before spar failed (continuing): %v", c.AgentID, err)
	}
	if err := c.Client.Undock(ctx); err != nil {
		return fmt.Errorf("undock after equip: %w", err)
	}
	return nil
}

// firstPOIOfType returns the first POI id of the given type. If none match, it
// falls back to the first non-station POI so combatants rendezvous in open space
// rather than auto-docking; only if every POI is a station does it return the
// first one. Returns "" if the system has no POIs.
func firstPOIOfType(s game.SystemData, poiType string) string {
	for _, p := range s.POIs {
		if p.Type == poiType {
			return p.ID
		}
	}
	// Fallback: prefer a non-station POI so combatants meet in open space
	// rather than auto-docking.
	for _, p := range s.POIs {
		if p.Type != "station" {
			return p.ID
		}
	}
	if len(s.POIs) > 0 {
		return s.POIs[0].ID
	}
	return ""
}
