package assets

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"
)

// staleNote renders " (hulls stale 3h0m0s)" for a reason string, or "" when the
// data came from this pass.
func staleNote(source string, age time.Duration) string {
	if age <= 0 {
		return ""
	}

	return fmt.Sprintf(" (%s stale %s)", source, age.Round(time.Minute))
}

// Thresholds mirrored from pkg/worker. They are redeclared rather than
// imported because pkg/worker will import pkg/assets, and the reverse edge
// would be a cycle. The divergence is deliberate and documented in the spec:
// agent_capability is a SCREENING FILTER and the worker's own gate stays
// authoritative at accept time.
const (
	skillSmuggling  = "smuggling" // pkg/worker/mission_select.go
	pirateFactionID = "pirates"   // pkg/worker/mission_standing.go
	smugglingLevel3 = 3           // pkg/worker: smugglingXPExemptLevel
	strongholdBase  = 10          // pkg/worker: smugglingUnlocked baseline
)

// haulMinCredits is a pkg/assets-only screening heuristic, NOT mirrored from
// pkg/worker: pkg/worker has no flat credit floor for haul at all. It gates
// haul per-opportunity on margin instead — haulMinMargin (0.03),
// haulMinNetProfit (1000.0), and haulSmallHoldNetProfit (250.0) in
// pkg/worker/haul.go, evaluated by haulGate/netProfitFloor against live
// price and cargo capacity. That can't be reduced to one flat credit number,
// so this rule uses a cheap standalone proxy (enough buying power to attempt
// an arbitrage leg) instead. An agent above this floor can still be refused
// by haulGate on margin grounds; the ledger does not attempt to predict
// that — consistent with agent_capability being a screening filter whose
// worker-side gate stays authoritative at accept time.
const haulMinCredits = 20000

// pkg/worker's freightPackageFootprint (100) is deliberately NOT mirrored yet:
// the v1 freight rule has no ship-class capacity to compare it against, and an
// unused constant fails the linter. It comes back with the catalog lookup in
// follow-on item 3.

// AgentSnapshot is everything the rules see. CarrierKnown distinguishes "no
// debt" from "never captured" -- missing data must never read as capability.
//
// CarrierAge/HullsAge are non-zero when the value came from storage rather than
// from this pass. Rules never silently trust old data; they visibly trust it,
// by appending the age to the blocking reason.
type AgentSnapshot struct {
	Profile      Profile
	Skills       map[string]SkillRow
	Standings    map[string]StandingRow
	Carrier      Carrier
	CarrierKnown bool
	CarrierAge   time.Duration
	Hulls        []Hull
	HullsAge     time.Duration
}

// activeHull returns the agent's currently flown hull.
func (s AgentSnapshot) activeHull() (Hull, bool) {
	for _, h := range s.Hulls {
		if h.IsActive {
			return h, true
		}
	}

	return Hull{}, false
}

// Capability is one derived eligibility verdict.
type Capability struct {
	Capability     string
	Eligible       bool
	BlockingReason string
}

// Rules is the eligibility registry. Adding a capability is adding a function
// and a key: no schema change, no migration. This is the layer that grows as
// needs change, wrapping tables that stay pinned to the wire format.
func Rules() map[string]func(AgentSnapshot) (bool, string) {
	return map[string]func(AgentSnapshot) (bool, string){
		"haul": func(s AgentSnapshot) (bool, string) {
			if _, ok := s.activeHull(); !ok {
				return false, "no active hull captured"
			}
			if s.Profile.Credits < haulMinCredits {
				return false, fmt.Sprintf("credits %.0f < %d%s",
					s.Profile.Credits, haulMinCredits, staleNote("hulls", s.HullsAge))
			}

			return true, ""
		},
		"freight": func(s AgentSnapshot) (bool, string) {
			if !s.CarrierKnown {
				return false, "carrier profile not captured"
			}
			if s.Carrier.DebtBlocksAcceptance || s.Carrier.OutstandingDebt > 0 {
				return false, fmt.Sprintf("outstanding_debt %d%s",
					s.Carrier.OutstandingDebt, staleNote("carrier", s.CarrierAge))
			}
			if _, ok := s.activeHull(); !ok {
				return false, "no active hull captured"
			}
			// v1 does NOT check cargo capacity: OwnedShip carries CargoUsed,
			// not capacity, and resolving capacity needs a ship-class catalog
			// lookup (follow-on 3). Until then this rule over-reports freight
			// eligibility for tiny hulls. That is the documented weakness of
			// the screening filter; freightCandidate still refuses them.

			return true, ""
		},
		"mission_delivery": func(s AgentSnapshot) (bool, string) {
			if _, ok := s.activeHull(); !ok {
				return false, "no active hull captured"
			}

			return true, ""
		},
		skillSmuggling: func(s AgentSnapshot) (bool, string) {
			lvl := s.Skills[skillSmuggling].Level
			if lvl < smugglingLevel3 {
				return false, fmt.Sprintf("level %d, needs %d", lvl, smugglingLevel3)
			}

			return true, ""
		},
		"stronghold_access": func(s AgentSnapshot) (bool, string) {
			base := s.Standings[pirateFactionID].Baseline
			if base < strongholdBase {
				return false, fmt.Sprintf("baseline %d, needs %d", base, strongholdBase)
			}

			return true, ""
		},
	}
}

// Evaluate runs every registered rule, returning one verdict per capability in
// a deterministic order.
func Evaluate(s AgentSnapshot) []Capability {
	rules := Rules()
	names := make([]string, 0, len(rules))
	for name := range rules {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]Capability, 0, len(names))
	for _, name := range names {
		ok, reason := rules[name](s)
		out = append(out, Capability{Capability: name, Eligible: ok, BlockingReason: reason})
	}

	return out
}

// ReplaceCapabilities swaps in the agent's full verdict set.
func (s *Store) ReplaceCapabilities(ctx context.Context, playerID string, rows []Capability, now time.Time) error {
	ts := rfc3339(now)

	return s.replaceSet(ctx, `DELETE FROM agent_capability WHERE player_id = ?`, playerID, func(tx *sql.Tx) error {
		for _, c := range rows {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO agent_capability (player_id, capability, eligible, blocking_reason, as_of)
				 VALUES (?,?,?,?,?)`,
				playerID, c.Capability, c.Eligible, c.BlockingReason, ts); err != nil {
				return fmt.Errorf("assets: insert capability %s/%s: %w", playerID, c.Capability, err)
			}
		}

		return nil
	})
}
