package fitting

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Catalog holds the loaded ships, modules, and Engineering efficiency values.
type Catalog struct {
	ships            map[string]Ship
	modules          map[string]Module
	cpuEffPerLevel   float64
	powerEffPerLevel float64
}

// rawListFile is the common shape of the catalog JSON files: a top-level
// "items" array (other keys are ignored).
type rawListFile struct {
	Items []json.RawMessage `json:"items"`
}

type rawShip struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	CPUCapacity    int      `json:"cpu_capacity"`
	PowerCapacity  int      `json:"power_capacity"`
	WeaponSlots    int      `json:"weapon_slots"`
	DefenseSlots   int      `json:"defense_slots"`
	UtilitySlots   int      `json:"utility_slots"`
	Tier           int      `json:"tier"`
	DefaultModules []string `json:"default_modules"`
}

type rawModule struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Type           string         `json:"type"`
	Slot           string         `json:"slot"`
	CPUUsage       int            `json:"cpu_usage"`
	PowerUsage     int            `json:"power_usage"`
	CPUBonus       int            `json:"cpu_bonus"`
	PowerBonus     int            `json:"power_bonus"`
	RequiredSkills map[string]int `json:"required_skills"`
}

type rawSkill struct {
	ID            string             `json:"id"`
	BonusPerLevel map[string]float64 `json:"bonus_per_level"`
}

// LoadCatalog reads catalog_ships.json, catalog_items.json, and
// catalog_skills.json from dir and returns a populated Catalog. Items without a
// "slot" field are not modules and are skipped. Engineering efficiency defaults
// to 1%/level if the skill or its bonus keys are absent.
func LoadCatalog(dir string) (*Catalog, error) {
	c := &Catalog{
		ships:            map[string]Ship{},
		modules:          map[string]Module{},
		cpuEffPerLevel:   1,
		powerEffPerLevel: 1,
	}

	if err := c.loadShips(filepath.Join(dir, "catalog_ships.json")); err != nil {
		return nil, err
	}
	if err := c.loadModules(filepath.Join(dir, "catalog_items.json")); err != nil {
		return nil, err
	}
	if err := c.loadSkills(filepath.Join(dir, "catalog_skills.json")); err != nil {
		return nil, err
	}
	return c, nil
}

func readItems(path string) ([]json.RawMessage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var f rawListFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return f.Items, nil
}

func (c *Catalog) loadShips(path string) error {
	items, err := readItems(path)
	if err != nil {
		return err
	}
	for _, raw := range items {
		var rs rawShip
		if err := json.Unmarshal(raw, &rs); err != nil {
			return fmt.Errorf("parse ship in %s: %w", path, err)
		}
		// Direct conversion relies on rawShip and Ship having identical field
		// layouts; a divergence is caught at compile time. Keep them in sync.
		c.ships[rs.ID] = Ship(rs)
	}
	return nil
}

func (c *Catalog) loadModules(path string) error {
	items, err := readItems(path)
	if err != nil {
		return err
	}
	for _, raw := range items {
		var rm rawModule
		if err := json.Unmarshal(raw, &rm); err != nil {
			return fmt.Errorf("parse item in %s: %w", path, err)
		}
		if rm.Slot == "" { // not a module
			continue
		}
		// Same identical-layout requirement as rawShip/Ship above.
		c.modules[rm.ID] = Module(rm)
	}
	return nil
}

func (c *Catalog) loadSkills(path string) error {
	items, err := readItems(path)
	if err != nil {
		return err
	}
	for _, raw := range items {
		var rsk rawSkill
		if err := json.Unmarshal(raw, &rsk); err != nil {
			return fmt.Errorf("parse skill in %s: %w", path, err)
		}
		if rsk.ID != "engineering" {
			continue
		}
		if v, ok := rsk.BonusPerLevel["cpuEfficiency"]; ok {
			c.cpuEffPerLevel = v
		}
		if v, ok := rsk.BonusPerLevel["powerEfficiency"]; ok {
			c.powerEffPerLevel = v
		}
	}
	return nil
}

// Ship returns the ship with the given id.
func (c *Catalog) Ship(id string) (Ship, bool) {
	s, ok := c.ships[id]
	return s, ok
}

// Module returns the module with the given id.
func (c *Catalog) Module(id string) (Module, bool) {
	m, ok := c.modules[id]
	return m, ok
}

// Ships returns all loaded ships sorted by tier then name.
func (c *Catalog) Ships() []Ship {
	out := make([]Ship, 0, len(c.ships))
	for _, s := range c.ships {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Tier != out[j].Tier {
			return out[i].Tier < out[j].Tier
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// Engineering returns an Engineering at the given level using the catalog's
// efficiency-per-level values.
func (c *Catalog) Engineering(level int) Engineering {
	return Engineering{Level: level, CPUEffPerLevel: c.cpuEffPerLevel, PowerEffPerLevel: c.powerEffPerLevel}
}
