package main

import (
	"sort"
	"time"
)

// Chain is the pirate-unlock smuggling chain in order. Completing the last rung
// is what raises the pirate baseline from the -30 hostile default to 10, which
// is the whole point of the unlock pool.
//
// The order is not guessed: every agent that holds the unlock walked these three
// in this sequence, and the server names the next rung in each completion
// payload (across_the_line reports chain_next=an_introduction, which in turn
// reports chain_next=supply_run — supply_run is past the unlock and so is not
// tracked here).
var Chain = []string{"no_questions_asked", "across_the_line", "an_introduction"}

// UnlockRung is the index in Chain whose completion grants the unlock.
const UnlockRung = 2

// Completion is one finished chain mission, as recorded in market.db.
type Completion struct {
	AgentID    string
	TemplateID string
	FinishedAt time.Time
}

// Progress is one agent's position on the chain.
type Progress struct {
	AgentID  string
	Step     int       // rungs cleared, 0..len(Chain)
	Last     string    // highest rung cleared ("" when none)
	LastAt   time.Time // when that rung was cleared
	Next     string    // the rung still owed ("" when unlocked)
	Unlocked bool
}

// Compute places every pool agent on the chain.
//
// An agent's step is its HIGHEST cleared rung, not the number of rungs it has
// records for. Those differ: mission_results only goes back so far, and an agent
// that cleared an_introduction before the table existed would otherwise be
// reported as not started — the one reading that would send an already-unlocked
// agent back to the giver. Rungs are strictly ordered, so reaching rung N means
// N-1 was cleared whether or not we hold the row.
//
// Completions for agents outside the pool are ignored, and a pool agent with no
// completions at all still gets a row: "has not started" is the single most
// useful thing this report can say, and dropping the row hides it.
func Compute(pool []string, comps []Completion) []Progress {
	rung := make(map[string]int, len(Chain))
	for i, t := range Chain {
		rung[t] = i
	}
	inPool := make(map[string]bool, len(pool))
	for _, a := range pool {
		inPool[a] = true
	}

	best := map[string]int{}         // agent -> highest rung index cleared
	when := map[string]time.Time{}   // agent -> when that rung was cleared
	for _, c := range comps {
		if !inPool[c.AgentID] {
			continue
		}
		i, ok := rung[c.TemplateID]
		if !ok {
			continue
		}
		if cur, seen := best[c.AgentID]; !seen || i > cur {
			best[c.AgentID] = i
			when[c.AgentID] = c.FinishedAt
		}
	}

	out := make([]Progress, 0, len(pool))
	for _, a := range pool {
		p := Progress{AgentID: a, Next: Chain[0]}
		if i, ok := best[a]; ok {
			p.Step = i + 1
			p.Last = Chain[i]
			p.LastAt = when[a]
			p.Unlocked = i >= UnlockRung
			if !p.Unlocked {
				p.Next = Chain[i+1]
			} else {
				p.Next = ""
			}
		}
		out = append(out, p)
	}
	// Furthest along first, then most recent progress, then name — so the agents
	// about to land the unlock are what you see at the top.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Step != out[j].Step {
			return out[i].Step > out[j].Step
		}
		if !out[i].LastAt.Equal(out[j].LastAt) {
			return out[i].LastAt.After(out[j].LastAt)
		}
		return out[i].AgentID < out[j].AgentID
	})

	return out
}
