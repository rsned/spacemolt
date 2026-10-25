// Package linedash builds a read-only production-line dashboard from the
// declarative line definitions in data/overmind/lines.yaml.
package linedash

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"time"

	"gopkg.in/yaml.v3"
)

// State is the health classification of a node or edge.
type State string

// Node and edge health states.
const (
	StateOptimal State = "optimal"
	StateStarved State = "starved"
	StateChoked  State = "choked"
	StateUnknown State = "unknown"
)

// NodeType is the role a node plays in a line.
type NodeType string

// The six node types.
const (
	NodeMine     NodeType = "mine"
	NodePurchase NodeType = "purchase"
	NodeStore    NodeType = "store"
	NodeConvert  NodeType = "convert"
	NodeProduce  NodeType = "produce"
	NodeSell     NodeType = "sell"
)

// EdgeKind is the kind of link between two nodes.
type EdgeKind string

// The three edge kinds.
const (
	EdgeHaul    EdgeKind = "haul"
	EdgeConsign EdgeKind = "consign"
	EdgeCraft   EdgeKind = "craft"
)

// forbiddenItems must never appear on a line (the Global Constraint).
var forbiddenItems = []string{"energy_crystal", "starshine"}

// Holder says where a tank's units sit.
type Holder struct {
	Agent   string `yaml:"agent,omitempty"`   // personal storage of this agent_id, OR
	Faction string `yaml:"faction,omitempty"` // faction storage of this faction tag (e.g. CRFT)
	Station string `yaml:"station"`           // base_id
}

// Node is one tank or agent group in a line.
type Node struct {
	ID           string         `yaml:"id"`
	Type         NodeType       `yaml:"type"`
	Name         string         `yaml:"name"`
	Items        []string       `yaml:"items"`                   // tank items shown on the node
	Agents       []string       `yaml:"agents,omitempty"`        // agent_ids attached
	Holders      []Holder       `yaml:"holders,omitempty"`       // tank locations
	Station      string         `yaml:"station,omitempty"`       // produce/sell/purchase station base_id
	Recipe       string         `yaml:"recipe,omitempty"`        // produce: recipe_id
	UnitsPerRun  int            `yaml:"units_per_run,omitempty"` // produce: output units per run
	InputsPerRun map[string]int `yaml:"inputs_per_run,omitempty"`
	BatchRuns    int            `yaml:"batch_runs,omitempty"` // produce: runs per craft command
}

// Edge is a link between two nodes of the same line.
type Edge struct {
	From   string   `yaml:"from"`
	To     string   `yaml:"to"`
	Kind   EdgeKind `yaml:"kind"`
	Item   string   `yaml:"item,omitempty"`   // the item that moves; empty on craft edges
	Agents []string `yaml:"agents,omitempty"` // the senders whose gift_sent rows are this link's movement
	Label  string   `yaml:"label,omitempty"`  // default by kind: "haul consigned" | "consign" | "craft"
}

// Line is one production line.
type Line struct {
	ID             string `yaml:"id"`
	Name           string `yaml:"name"`
	OutputItem     string `yaml:"output_item"`
	CapacityPerDay int    `yaml:"capacity_per_day"` // 100% facility figure, for "Avg Factory Eff"
	Nodes          []Node `yaml:"nodes"`
	Edges          []Edge `yaml:"edges"`
}

// Config is the whole lines file.
type Config struct {
	Thresholds Thresholds `yaml:"thresholds"`
	Lines      []Line     `yaml:"lines"`
}

// LoadLines reads the lines file, applies defaults and validates it.
func LoadLines(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read lines config: %w", err)
	}
	var c Config
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true) // a typo such as "holder:" is an error, not an empty field
	if err := dec.Decode(&c); err != nil && !errors.Is(err, io.EOF) {
		return Config{}, fmt.Errorf("parse lines config %s: %w", path, err)
	}
	c.Thresholds = c.Thresholds.withDefaults()
	for li := range c.Lines {
		for ei := range c.Lines[li].Edges {
			e := &c.Lines[li].Edges[ei]
			if e.Label == "" {
				e.Label = defaultEdgeLabel(e.Kind)
			}
		}
	}
	if err := c.Validate(); err != nil {
		return Config{}, fmt.Errorf("lines config %s: %w", path, err)
	}
	return c, nil
}

func defaultEdgeLabel(k EdgeKind) string {
	switch k {
	case EdgeHaul:
		return "haul consigned"
	case EdgeConsign:
		return "consign"
	case EdgeCraft:
		return "craft"
	}
	return ""
}

// Validate checks the structural rules of every line.
func (c Config) Validate() error {
	for _, l := range c.Lines {
		if err := l.validate(); err != nil {
			return err
		}
	}
	return nil
}

func (l Line) validate() error {
	if l.CapacityPerDay < 1 {
		return fmt.Errorf("line %q: capacity_per_day must be >= 1, got %d", l.ID, l.CapacityPerDay)
	}
	if err := checkItem(l.ID, "output_item", l.OutputItem); err != nil {
		return err
	}
	ids := make(map[string]bool, len(l.Nodes))
	for _, n := range l.Nodes {
		if ids[n.ID] {
			return fmt.Errorf("line %q: duplicate node id %q", l.ID, n.ID)
		}
		ids[n.ID] = true
		if err := validateNode(l.ID, n); err != nil {
			return err
		}
	}
	for _, e := range l.Edges {
		for _, end := range []string{e.From, e.To} {
			if !ids[end] {
				return fmt.Errorf("line %q: edge %s->%s names unknown node %q", l.ID, e.From, e.To, end)
			}
		}
		switch e.Kind {
		case EdgeHaul, EdgeConsign, EdgeCraft:
		default:
			return fmt.Errorf("line %q: edge %s->%s has unknown kind %q", l.ID, e.From, e.To, e.Kind)
		}
		if err := checkItem(l.ID, fmt.Sprintf("edge %s->%s item", e.From, e.To), e.Item); err != nil {
			return err
		}
	}
	return nil
}

func validateNode(lineID string, n Node) error {
	switch n.Type {
	case NodeMine, NodePurchase, NodeStore, NodeConvert, NodeProduce, NodeSell:
	default:
		return fmt.Errorf("line %q node %q: unknown type %q", lineID, n.ID, n.Type)
	}
	switch n.Type {
	case NodeProduce, NodeSell, NodePurchase:
		if n.Station == "" {
			return fmt.Errorf("line %q node %q: %s node requires station", lineID, n.ID, n.Type)
		}
	}
	if n.Type == NodeProduce {
		switch {
		case n.Recipe == "":
			return fmt.Errorf("line %q node %q: produce node requires recipe", lineID, n.ID)
		case n.UnitsPerRun < 1:
			return fmt.Errorf("line %q node %q: units_per_run must be >= 1", lineID, n.ID)
		case n.BatchRuns < 1:
			return fmt.Errorf("line %q node %q: batch_runs must be >= 1", lineID, n.ID)
		case len(n.InputsPerRun) == 0:
			return fmt.Errorf("line %q node %q: produce node requires non-empty inputs_per_run", lineID, n.ID)
		}
	}
	for i, h := range n.Holders {
		if (h.Agent == "") == (h.Faction == "") {
			return fmt.Errorf("line %q node %q: holder %d must set exactly one of agent or faction", lineID, n.ID, i)
		}
	}
	for _, it := range n.Items {
		if err := checkItem(lineID, fmt.Sprintf("node %q items", n.ID), it); err != nil {
			return err
		}
	}
	for it := range n.InputsPerRun {
		if err := checkItem(lineID, fmt.Sprintf("node %q inputs_per_run", n.ID), it); err != nil {
			return err
		}
	}
	return nil
}

func checkItem(lineID, where, item string) error {
	if slices.Contains(forbiddenItems, item) {
		return fmt.Errorf("line %q %s: item %q is forbidden on a line", lineID, where, item)
	}
	return nil
}

// Thresholds tune the state classification. Zero values are replaced by
// DefaultThresholds field by field at load time.
type Thresholds struct {
	HaulWindow   time.Duration `yaml:"haul_window"`    // default 4h: a haul edge with no gift_sent in this window has not moved
	MineWindow   time.Duration `yaml:"mine_window"`    // default 4h: a Mine node with no drop in this window is dry
	CraftWindow  time.Duration `yaml:"craft_window"`   // default 1h: a Produce node with no crafting.completed in this window is stalled
	SellWindow   time.Duration `yaml:"sell_window"`    // default 6h: a Sell node with no exchange_fill in this window is not selling
	SellStale    time.Duration `yaml:"sell_stale"`     // default 6h: an ask older than this at the floor = market saturated
	StaleData    time.Duration `yaml:"stale_data"`     // default 2h: a source captured longer ago than this is stale
	RateWindow   time.Duration `yaml:"rate_window"`    // default 24h: units/day window
	SellLot      int           `yaml:"sell_lot"`       // default 50: one shopkeeper ask lot
	ChokeUnits   int           `yaml:"choke_units"`    // default 1000: a drop/output tank at or above this with no outflow is choked
	BrokeCredits int           `yaml:"broke_credits"`  // default 500 (spec D6)
	FuelFloorPct int           `yaml:"fuel_floor_pct"` // default 10: fuel below this % while undocked = stranded
}

// DefaultThresholds returns the documented defaults.
func DefaultThresholds() Thresholds {
	return Thresholds{
		HaulWindow:   4 * time.Hour,
		MineWindow:   4 * time.Hour,
		CraftWindow:  time.Hour,
		SellWindow:   6 * time.Hour,
		SellStale:    6 * time.Hour,
		StaleData:    2 * time.Hour,
		RateWindow:   24 * time.Hour,
		SellLot:      50,
		ChokeUnits:   1000,
		BrokeCredits: 500,
		FuelFloorPct: 10,
	}
}

func (t Thresholds) withDefaults() Thresholds {
	d := DefaultThresholds()
	fillDur := func(v *time.Duration, def time.Duration) {
		if *v == 0 {
			*v = def
		}
	}
	fillInt := func(v *int, def int) {
		if *v == 0 {
			*v = def
		}
	}
	fillDur(&t.HaulWindow, d.HaulWindow)
	fillDur(&t.MineWindow, d.MineWindow)
	fillDur(&t.CraftWindow, d.CraftWindow)
	fillDur(&t.SellWindow, d.SellWindow)
	fillDur(&t.SellStale, d.SellStale)
	fillDur(&t.StaleData, d.StaleData)
	fillDur(&t.RateWindow, d.RateWindow)
	fillInt(&t.SellLot, d.SellLot)
	fillInt(&t.ChokeUnits, d.ChokeUnits)
	fillInt(&t.BrokeCredits, d.BrokeCredits)
	fillInt(&t.FuelFloorPct, d.FuelFloorPct)
	return t
}
