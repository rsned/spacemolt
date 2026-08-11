package worker

import (
	"context"
	"sync"
)

// empireBasesCache memoises the base ids belonging to an empire.
//
// The station roster is fixed galaxy data — empires do not gain or lose
// stations while a worker runs — so this is resolved once and held for the
// process lifetime. Without it the mission pass would re-query the whole
// empire's bases every ~11s, on every one of ~110 workers, to compute a number
// that cannot have changed.
//
// A negative result is cached too: an empire we have no bases for is a KB gap,
// not a transient, and re-asking every pass would not fix it.
var empireBases = struct {
	mu sync.RWMutex
	m  map[string][]string
}{m: map[string][]string{}}

// missionPayoutScope reports the empire whose board this pass is reading, and
// the origin bases the payout ratio should be measured over.
//
// The purse is per-empire and the payer is the empire that ISSUED the mission,
// so the right sample is "missions accepted at a station of this empire", not
// the whole galaxy — a galaxy-wide ratio over-discounts the solvent empires and
// under-discounts the broke ones simultaneously.
//
// Returns ("galaxy", nil) when the empire cannot be determined, which restores
// exactly the previous galaxy-wide behaviour: undocked, an unknown station, or
// a KB with no base row for it. Falling back to the wider sample is the safe
// direction — the alternative is a tiny sample that trips the MinSamples floor
// and prices everything at face value.
func missionPayoutScope(ctx context.Context, deps MissionDeps) (empire string, fromBases []string) {
	if deps.KB == nil || deps.Client == nil {
		return "galaxy", nil
	}
	st := deps.Client.GetState()
	if st == nil || st.Player.DockedAtBase == "" {
		return "galaxy", nil
	}
	base, err := deps.KB.GetBase(ctx, st.Player.DockedAtBase)
	if err != nil || base == nil || base.Empire == "" {
		return "galaxy", nil
	}

	return base.Empire, empireBaseIDs(ctx, deps, base.Empire)
}

// empireBaseIDs returns every known base id of an empire, from cache.
func empireBaseIDs(ctx context.Context, deps MissionDeps, empire string) []string {
	empireBases.mu.RLock()
	ids, ok := empireBases.m[empire]
	empireBases.mu.RUnlock()
	if ok {
		return ids
	}

	ids, err := deps.KB.GetBaseIDsByEmpire(ctx, empire)
	if err != nil {
		// Do not cache a query failure — that is transient in a way a missing
		// row is not.
		return nil
	}

	empireBases.mu.Lock()
	empireBases.m[empire] = ids
	empireBases.mu.Unlock()

	return ids
}
