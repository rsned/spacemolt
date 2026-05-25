package spar

import (
	"fmt"

	"github.com/rsned/spacemolt/pkg/game"
)

// IsNonEmpireArena reports whether a system permits anything-goes PvP: it is
// not owned by an empire, or is lawless / unpoliced / a pirate stronghold.
func IsNonEmpireArena(s game.SystemData) bool {
	return s.Empire == "" || s.SecurityStatus == "Lawless" || s.PoliceLevel == 0 || s.IsStronghold
}

// hasSafeAdjacency reports whether s connects to at least one policed/empire
// system — somewhere to retreat to, equip at, or rebuild from.
func hasSafeAdjacency(s game.SystemData, byID map[string]game.SystemData) bool {
	for _, c := range s.Connections {
		if n, ok := byID[c.SystemID]; ok && !IsNonEmpireArena(n) {
			return true
		}
	}
	return false
}

// hasRendezvousPOI reports whether s has at least one POI to gather combatants at.
func hasRendezvousPOI(s game.SystemData) bool { return len(s.POIs) > 0 }

// SelectArena picks the best non-empire arena from systems: it must be a
// non-empire system, be reachable by all combatants (reachable(id) true), have
// a rendezvous POI, and have safe-space adjacency. Among candidates the first
// match (by input order) is returned. Pure — no network.
func SelectArena(systems []game.SystemData, byID map[string]game.SystemData, reachable func(systemID string) bool) (string, error) {
	for _, s := range systems {
		if !IsNonEmpireArena(s) {
			continue
		}
		if !hasRendezvousPOI(s) {
			continue
		}
		if !hasSafeAdjacency(s, byID) {
			continue
		}
		if !reachable(s.ID) {
			continue
		}
		return s.ID, nil
	}
	return "", fmt.Errorf("no reachable non-empire arena with a rendezvous POI and safe-space adjacency found; pass --arena explicitly")
}

// ValidateArenaSystem checks that an already-loaded system is a legal arena.
// Used at arrival to confirm a --arena value really is outside empire space.
func ValidateArenaSystem(s game.SystemData) error {
	if !IsNonEmpireArena(s) {
		return fmt.Errorf("system %s (%s) is empire space, not a valid arena", s.ID, s.SecurityStatus)
	}
	return nil
}
