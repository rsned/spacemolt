package main

import (
	"fmt"
	"log"
)

// BattleView is the per-tick snapshot the control loop consumes. session.go
// builds it from the client's battle_update-derived state; tests fake it.
type BattleView struct {
	BattleID         string
	Tick             int
	Ended            bool
	Outcome          string
	MyZone           string
	MyStance         string
	ParticipantCount int
}

type side interface {
	Name() string
	Battle(action string, kv map[string]any) error
	View() (BattleView, bool)
}

type duelResult struct {
	BattleID string
	Outcome  string
	Void     bool
	Ticks    int
}

var ringOf = map[string]int{"engaged": 0, "inner": 1, "mid": 2, "outer": 3}

// applyOrders issues the phase's stance for one side and, when a ring hold
// is set, an advance/retreat correction toward it. Stance orders are
// re-issued only on change (they queue for the next tick and are free).
func applyOrders(s side, stance string, hold *int, lastStance *string, v BattleView, logger *log.Logger) error {
	if *lastStance != stance {
		if err := s.Battle("stance", map[string]any{"stance": stance}); err != nil {
			return err
		}
		*lastStance = stance
	}
	if hold == nil || stance == "flee" { // flee auto-retreats; never fight it
		return nil
	}
	ring, ok := ringOf[v.MyZone]
	if !ok {
		return nil // unknown zone label: skip correction this tick
	}
	switch {
	case ring > *hold:
		return s.Battle("advance", nil)
	case ring < *hold:
		return s.Battle("retreat", nil)
	}
	return nil
}

// runDuel drives one battle from first view to battle end. The attack that
// creates the battle has already been issued by the caller; runDuel only
// applies the script, voids on interference, and flees out past MaxTicks.
func runDuel(a, b side, d Duel, wait func(), logger *log.Logger) (duelResult, error) {
	var res duelResult
	lastA, lastB := "", ""
	voided := false
	for i := 0; ; i++ {
		if i > d.MaxTicks*20+200 {
			return res, fmt.Errorf("duel %s: no battle end after %d polls", d.ID, i)
		}
		va, okA := a.View()
		if !okA {
			wait()
			continue
		}
		res.BattleID = va.BattleID
		res.Ticks = va.Tick
		if va.Ended {
			res.Outcome = va.Outcome
			res.Void = voided
			return res, nil
		}
		phase := d.PhaseAt(va.Tick)
		stanceA, stanceB, hold := phase.StanceA, phase.StanceB, phase.HoldRing
		if va.ParticipantCount > 2 && !voided {
			voided = true
			logger.Printf("duel %s: %d participants — voiding, fleeing out", d.ID, va.ParticipantCount)
		}
		if voided || va.Tick > d.MaxTicks {
			stanceA, stanceB, hold = "flee", "flee", nil
		}
		if err := applyOrders(a, stanceA, hold, &lastA, va, logger); err != nil {
			return res, fmt.Errorf("side A orders: %w", err)
		}
		vb, okB := b.View()
		if okB {
			if err := applyOrders(b, stanceB, hold, &lastB, vb, logger); err != nil {
				return res, fmt.Errorf("side B orders: %w", err)
			}
		}
		wait()
	}
}
