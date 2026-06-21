package worker

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/rsned/spacemolt/pkg/game"
)

// TokenError indicates a $TOKEN$ in a command could not be resolved from game
// state. Inside a loop it is fatal: the loop aborts immediately, even under -f.
type TokenError struct{ msg string }

func (e *TokenError) Error() string { return e.msg }

// knownPOITypes is the set of POI types a $TYPE$ token may name. It lets the
// resolver tell an unresolvable-but-valid type ("no station in system") apart
// from a typo ("unknown token"). Mirrors the types enumerated in
// pkg/game/constants.go POIFreshnessThreshold.
var knownPOITypes = map[string]bool{
	"asteroid_belt": true, "asteroid": true, "asteroid_field": true,
	"gas_cloud": true, "ice_field": true, "nebula": true,
	"station": true, "base": true, "planet": true, "moon": true,
	"sun": true, "relic": true, "jump_gate": true, "wreck": true,
}

// tokenRe matches $NAME$ where NAME starts with a letter/underscore.
var tokenRe = regexp.MustCompile(`\$([A-Za-z_][A-Za-z0-9_]*)\$`)

// ResolveTokens replaces every $TOKEN$ occurrence in each argument with a value
// derived from live game state. It returns the substituted arguments, or a
// *TokenError if any token cannot be resolved.
func ResolveTokens(args []string, state *game.State) ([]string, error) {
	out := make([]string, len(args))
	for i, a := range args {
		var rerr error
		out[i] = tokenRe.ReplaceAllStringFunc(a, func(m string) string {
			name := m[1 : len(m)-1] // strip surrounding '$'
			val, err := resolveOneToken(name, state)
			if err != nil {
				if rerr == nil {
					rerr = err
				}
				return m
			}
			return val
		})
		if rerr != nil {
			return nil, rerr
		}
	}
	return out, nil
}

// resolveOneToken resolves a single token name (without the surrounding '$').
// State tokens (SYSTEM, SHIP, CREDITS) take precedence; any other name is
// treated as a POI type and looked up in the current system.
func resolveOneToken(name string, state *game.State) (string, error) {
	switch strings.ToUpper(name) {
	case "SYSTEM":
		if state == nil || state.System.ID == "" {
			return "", &TokenError{"$SYSTEM$: no current system in state"}
		}
		return state.System.ID, nil
	case "SHIP":
		if state == nil || state.Ship.ID == "" {
			return "", &TokenError{"$SHIP$: no active ship in state"}
		}
		return state.Ship.ID, nil
	case "CREDITS":
		if state == nil {
			return "", &TokenError{"$CREDITS$: no state available"}
		}
		return strconv.FormatInt(int64(state.Credits), 10), nil
	}

	poiType := strings.ToLower(name)
	if !knownPOITypes[poiType] {
		return "", &TokenError{fmt.Sprintf("unknown token $%s$", name)}
	}
	if state == nil {
		return "", &TokenError{fmt.Sprintf("$%s$: no state available", name)}
	}
	var matches []string
	for _, p := range state.System.POIs {
		if p.Type == poiType {
			matches = append(matches, p.ID)
		}
	}
	if len(matches) == 0 {
		return "", &TokenError{fmt.Sprintf("no %s POI in system %s (%s)",
			poiType, state.System.Name, state.System.ID)}
	}
	slices.Sort(matches)
	return matches[0], nil
}
