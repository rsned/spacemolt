package worker

import (
	"sync"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// huntBoardGate decides whether a hunt pass spends its three mission queries
// (completed_missions, get_active_missions, get_missions).
//
// Why it exists. On 2026-09-11 pirate-6 through pirate-10 each emitted, in the
// same five-minute window and byte-identical:
//
//	send_tally total=116 completed_missions=29 get_active_missions=29 get_missions=29 get_status=29
//
// Four queries at the SAME count means one pass issues exactly one of each and
// achieves nothing: accept_mission was 0. Across the fleet those three queries
// were 43.9% of ALL outbound traffic. The per-bot game_query ceiling (300/min)
// was never close — but the counter that earns an IP a temporary block
// aggregates every request from the address, so a loop that looks harmless for
// one agent is a real cost at fleet scale.
//
// Why skipping is SAFE, per the server team: board entries are computed from
// the agent's own state at the moment of the query and do not rotate on a
// timer, while separately-posted contracts turn over on a schedule the server
// owns. A query never triggers a refresh. So re-asking sooner than turnover
// hands back the board already held.
//
// What therefore bypasses the backoff entirely:
//
//   - A DIFFERENT STATION. A new dock is a different board; read it at once.
//   - A PASS THAT FOUND WORK. An agent holding an active mission is not dry,
//     and huntResumeJob needs get_active_missions to find that mission. Gating
//     a working agent would strand its job, so the backoff applies only after
//     a pass that found nothing to resume AND nothing admissible to accept.
//
// While genuinely dry the agent holds no mission, so nothing of ours can
// change except our location — which the pass already learns from the
// get_status it issues anyway. That leaves server-side turnover as the only
// unobservable, and SleepMissionBoardPoll is the backstop for it.
//
// One instance per worker process, threaded in via HuntDeps. A nil gate always
// polls, so callers that do not thread one through (tests, play_as) behave
// exactly as before.
type huntBoardGate struct {
	mu          sync.Mutex
	lastPoll    time.Time
	lastStation string
	dry         bool
}

// shouldPoll reports whether this pass should issue its mission queries.
func (g *huntBoardGate) shouldPoll(station string, now time.Time) bool {
	if g == nil {
		return true
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	// Not dry: the agent has work in flight and must keep reading it.
	if !g.dry {
		return true
	}
	// Never polled, or the board under us changed.
	if g.lastPoll.IsZero() || station != g.lastStation {
		return true
	}

	return now.Sub(g.lastPoll) >= game.SleepMissionBoardPoll
}

// record stamps the outcome of a pass that actually queried. dry means the
// pass found nothing to resume and nothing admissible to accept.
func (g *huntBoardGate) record(station string, now time.Time, dry bool) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.lastPoll = now
	g.lastStation = station
	g.dry = dry
}
