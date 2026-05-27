// Package craftplan computes what an agent can craft right now and what is
// missing to craft a target recipe. The Engine consumes a Source (live game
// client + crafting DB in production; fakes in tests) and returns plain-data
// results that callers render however they like.
package craftplan

import "github.com/rsned/spacemolt/pkg/game/serverapi"

// Inventory is the count of items the agent has access to, split by where
// they live. Plan/Craftable sum the buckets the user opted into; the split
// is preserved so the per-recipe display can show which bucket a material
// is currently sitting in.
type Inventory struct {
	Cargo   map[string]int // item_id → quantity in ship cargo
	Storage map[string]int // item_id → quantity in personal storage at current station
	Faction map[string]int // item_id → quantity in faction storage at current station (may be nil)
}

// total returns the sum across the buckets the caller opted into. Pass
// includeFaction=false to ignore the faction bucket entirely (the default
// when --include-faction is not set).
func (i Inventory) total(itemID string, includeFaction bool) int {
	n := i.Cargo[itemID] + i.Storage[itemID]
	if includeFaction {
		n += i.Faction[itemID]
	}
	return n
}

// BOMRow is one base-material row from the crafting DB's bill_of_materials
// table for a single target recipe. RecipePath lists the recipe_ids that
// connect the target down to this base material (ordered output → leaf;
// length 1 means the target recipe itself consumes the base directly).
type BOMRow struct {
	BaseItemID string
	Quantity   int
	RecipePath []string
}

// CraftableRow is one row in the craftable table.
type CraftableRow struct {
	Recipe         serverapi.Recipe // pulled through so the renderer can reach inputs/outputs without re-lookup
	CanMake        int              // integer batches buildable from the current inventory; math.MaxInt for ∞
	OutputItemID   string           // primary output item_id (recipe.Outputs[0])
	OutputQuantity int              // primary output quantity per batch
	Depth          int              // 1 = direct, 2+ = via N-1 intermediate crafts (only meaningful in --reachable)
}

// PlanInputRow is one row in the plan output. For --reachable mode each row
// represents a base material; for direct mode each row is a direct ingredient.
type PlanInputRow struct {
	ItemID      string
	Need        int
	HaveCargo   int
	HaveStorage int
	HaveFaction int
	Short       int // max(0, Need - (HaveCargo + HaveStorage [+ HaveFaction]))
}

// IntermediateCraft describes one of the sub-crafts the operator would have
// to run to reach the target via --reachable mode.
type IntermediateCraft struct {
	RecipeID       string
	OutputItemID   string
	OutputQuantity int
	BatchesNeeded  int
}

// PlanResult is the full output of Engine.Plan. Ready==true means every
// Inputs row has Short==0 and the agent meets the recipe's skill + legality
// gates.
type PlanResult struct {
	Recipe         serverapi.Recipe
	Quantity       int
	StationID      string
	Inputs         []PlanInputRow
	Intermediates  []IntermediateCraft // populated only in --reachable mode
	BlockedSkill   map[string]int      // skill_id → level shortfall; empty if no skill block
	BlockedIllegal bool                // true if recipe is illegal at this station
	Ready          bool
}

// CraftableOpts controls Engine.Craftable.
type CraftableOpts struct {
	Reachable           bool
	IncludeFaction      bool
	IncludeFacilityOnly bool   // include recipes flagged facility_only (default: hide; their ∞ can_make floods the top)
	IncludeHidden       bool   // include recipes flagged hidden (default: hide)
	CategoryFilter      string // substring match, case-insensitive; empty = no filter
	SearchFilter        string // substring match against name + output item_ids, case-insensitive
	Refresh             bool   // bypass session recipe-catalog cache
	Max                 int    // hard cap on rows returned; 0 = engine default (100)
	OneRecipe           string // if non-empty, return only this recipe (still in CraftableRow form); used by --detail --recipe X
	SortBy              SortMode
}

// SortMode controls Craftable row ordering. Unknown / zero value = default
// (can_make desc, then depth asc, then recipe_id asc).
type SortMode int

const (
	SortDefault    SortMode = iota // can_make desc, depth asc, recipe_id asc
	SortCanMakeAsc                 // can_make asc (lowest first — what's running out)
	SortName                       // recipe.Name asc, then can_make desc
	SortCategory                   // recipe.Category asc, then can_make desc
	SortRecipeID                   // recipe.ID asc
)

// String renders a SortMode for the footer.
func (s SortMode) String() string {
	switch s {
	case SortCanMakeAsc:
		return "can_make asc"
	case SortName:
		return "name asc"
	case SortCategory:
		return "category asc"
	case SortRecipeID:
		return "recipe_id asc"
	default:
		return "can_make desc"
	}
}

// ParseSortMode maps a user-supplied --sort value to a SortMode. Empty or
// unknown values map to SortDefault.
func ParseSortMode(s string) SortMode {
	switch s {
	case "can_make_asc", "can_make-asc", "lowest":
		return SortCanMakeAsc
	case "name":
		return SortName
	case "category":
		return SortCategory
	case "id", "recipe", "recipe_id":
		return SortRecipeID
	default:
		return SortDefault
	}
}

// PlanOpts controls Engine.Plan.
type PlanOpts struct {
	ID             string // recipe_id OR item_id; recipe wins on tie
	Quantity       int    // batches to plan for; must be ≥ 1
	Reachable      bool
	IncludeFaction bool
	Refresh        bool
}
