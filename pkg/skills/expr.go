package skills

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/rsned/spacemolt/pkg/game"
)

// EvalExpr evaluates a condition expression against game state.
// Supported forms:
//   - Bare booleans: "docked", "has_cargo", "cargo_full", "fuel_low"
//   - Negation: "not docked"
//   - Comparisons: "fuel_pct < 0.1", "credits >= 5000"
//   - String comparisons: "current_poi == poi-123", "system_name == Alpha"
//   - Functions: "at_poi_type(station, asteroid_belt)", "has_module_type(mining)"
//   - Special: "default" (always true)
func EvalExpr(expr string, state *game.State) (bool, error) {
	expr = strings.TrimSpace(expr)

	if expr == "default" {
		return true, nil
	}

	// Negation
	if inner, ok := strings.CutPrefix(expr, "not "); ok {
		result, err := EvalExpr(inner, state)
		if err != nil {
			return false, err
		}
		return !result, nil
	}

	// Function-style: at_poi_type(a, b)
	if strings.HasPrefix(expr, "at_poi_type(") && strings.HasSuffix(expr, ")") {
		args := strings.TrimSuffix(strings.TrimPrefix(expr, "at_poi_type("), ")")
		types := parseArgs(args)
		poiType := resolveCurrentPOIType(state)
		return slices.Contains(types, poiType), nil
	}

	// Function-style: has_module_type(type)
	if strings.HasPrefix(expr, "has_module_type(") && strings.HasSuffix(expr, ")") {
		args := strings.TrimSuffix(strings.TrimPrefix(expr, "has_module_type("), ")")
		moduleType := strings.TrimSpace(args)
		return hasModuleType(state, moduleType), nil
	}

	// Try comparison operators (ordered by length to avoid prefix issues)
	for _, op := range []string{">=", "<=", "!=", "==", ">", "<"} {
		if parts := strings.SplitN(expr, " "+op+" ", 2); len(parts) == 2 {
			return evalComparison(parts[0], op, parts[1], state)
		}
	}

	// Bare boolean
	val, err := resolveVar(expr, state)
	if err != nil {
		return false, err
	}
	return val.asBool()
}

type exprValue struct {
	floatVal  float64
	stringVal string
	boolVal   bool
	kind      string // "float", "string", "bool"
}

func (v exprValue) asBool() (bool, error) {
	switch v.kind {
	case "bool":
		return v.boolVal, nil
	case "float":
		return v.floatVal != 0, nil
	default:
		return false, fmt.Errorf("cannot use %q as boolean", v.stringVal)
	}
}

func resolveVar(name string, state *game.State) (exprValue, error) {
	name = strings.TrimSpace(name)

	fuelPct := safeDivide(state.Fuel, state.MaxFuel)
	hullPct := safeDivide(state.Hull, state.MaxHull)
	cargoPct := safeDivide(state.Ship.CargoUsed, state.Ship.CargoCapacity)

	switch name {
	case "fuel_pct":
		return exprValue{floatVal: fuelPct, kind: "float"}, nil
	case "hull_pct":
		return exprValue{floatVal: hullPct, kind: "float"}, nil
	case "cargo_pct":
		return exprValue{floatVal: cargoPct, kind: "float"}, nil
	case "cargo_count":
		return exprValue{floatVal: float64(len(state.Ship.Cargo)), kind: "float"}, nil
	case "credits":
		return exprValue{floatVal: state.Credits, kind: "float"}, nil
	case "docked":
		return exprValue{boolVal: state.Doc, kind: "bool"}, nil
	case "has_cargo":
		return exprValue{boolVal: len(state.Ship.Cargo) > 0, kind: "bool"}, nil
	case "cargo_full":
		return exprValue{boolVal: cargoPct >= 0.97, kind: "bool"}, nil
	case "fuel_low":
		return exprValue{boolVal: fuelPct < 0.1, kind: "bool"}, nil
	case "current_poi":
		return exprValue{stringVal: state.CurrentPOI, kind: "string"}, nil
	case "current_poi_type":
		return exprValue{stringVal: resolveCurrentPOIType(state), kind: "string"}, nil
	case "system_name":
		return exprValue{stringVal: state.System.Name, kind: "string"}, nil
	default:
		return exprValue{}, fmt.Errorf("unknown variable: %q", name)
	}
}

func evalComparison(lhs, op, rhs string, state *game.State) (bool, error) {
	left, err := resolveVar(lhs, state)
	if err != nil {
		return false, err
	}

	// Float comparison
	if left.kind == "float" {
		rightVal, parseErr := strconv.ParseFloat(strings.TrimSpace(rhs), 64)
		if parseErr != nil {
			return false, fmt.Errorf("cannot parse %q as number for comparison with %s", rhs, lhs)
		}
		return compareFloat(left.floatVal, op, rightVal)
	}

	// String comparison
	rightStr := strings.TrimSpace(rhs)
	switch op {
	case "==":
		return left.stringVal == rightStr, nil
	case "!=":
		return left.stringVal != rightStr, nil
	default:
		return false, fmt.Errorf("operator %s not supported for string comparison", op)
	}
}

func compareFloat(left float64, op string, right float64) (bool, error) {
	switch op {
	case "<":
		return left < right, nil
	case ">":
		return left > right, nil
	case "<=":
		return left <= right, nil
	case ">=":
		return left >= right, nil
	case "==":
		return left == right, nil
	case "!=":
		return left != right, nil
	default:
		return false, fmt.Errorf("unknown operator: %s", op)
	}
}

func resolveCurrentPOIType(state *game.State) string {
	for _, poi := range state.System.POIs {
		if poi.ID == state.CurrentPOI {
			return poi.Type
		}
	}
	return ""
}

func hasModuleType(state *game.State, moduleType string) bool {
	for _, moduleID := range state.Ship.Modules {
		if def, ok := state.ModuleDefinitions[moduleID]; ok {
			if strings.EqualFold(def.Type, moduleType) {
				return true
			}
		}
	}
	return false
}

func safeDivide(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}

func parseArgs(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
