package main

import (
	"fmt"
	"log"
	"strings"
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
	Reload() error
	View() (BattleView, bool)
}

type duelResult struct {
	BattleID string
	Outcome  string
	Void     bool
	Ticks    int
}

var ringOf = map[string]int{"engaged": 0, "inner": 1, "mid": 2, "outer": 3}

// battleGone reports whether an order failed because the battle is already
// over (both sides escaped/disengaged) — a terminal outcome, not an error.
// Measured live: the server answers a post-battle stance order with
// "You are not in a battle. Use attack to engage a target."
func battleGone(err error) bool {
	return err != nil && strings.Contains(err.Error(), "not in a battle")
}

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
		if va.BattleID != "" { // never clobber a captured id with an empty end-view
			res.BattleID = va.BattleID
		}
		res.Ticks = va.Tick
		if va.Ended {
			res.Outcome = va.Outcome
			res.Void = voided
			return res, nil
		}
		phase := d.PhaseAt(va.Tick)
		stanceA, stanceB := phase.StanceA, phase.StanceB
		holdA, holdB := phase.HoldA(), phase.HoldB()
		if va.ParticipantCount > 2 && !voided {
			voided = true
			logger.Printf("duel %s: %d participants — voiding, fleeing out", d.ID, va.ParticipantCount)
		}
		if voided || va.Tick > d.MaxTicks {
			stanceA, stanceB, holdA, holdB = "flee", "flee", nil, nil
		}
		if err := applyOrders(a, stanceA, holdA, &lastA, va, logger); err != nil {
			if battleGone(err) {
				res.Outcome, res.Void = "ended", voided
				return res, nil
			}
			return res, fmt.Errorf("side A orders: %w", err)
		}
		vb, okB := b.View()
		if okB {
			if err := applyOrders(b, stanceB, holdB, &lastB, vb, logger); err != nil {
				if battleGone(err) {
					res.Outcome, res.Void = "ended", voided
					return res, nil
				}
				return res, fmt.Errorf("side B orders: %w", err)
			}
		}
		// Reload logic: trigger on every tick where tick % reload_every == 0,
		// but only if not voided and not fleeing.
		if d.ReloadEvery > 0 && va.Tick > 0 && va.Tick%d.ReloadEvery == 0 {
			if !voided && stanceA != "flee" {
				if err := a.Reload(); err != nil {
					if battleGone(err) {
						res.Outcome, res.Void = "ended", voided
						return res, nil
					}
					logger.Printf("duel %s: side A reload: %v", d.ID, err)
				}
			}
			if okB && !voided && stanceB != "flee" {
				if err := b.Reload(); err != nil {
					if battleGone(err) {
						res.Outcome, res.Void = "ended", voided
						return res, nil
					}
					logger.Printf("duel %s: side B reload: %v", d.ID, err)
				}
			}
		}
		wait()
	}
}
