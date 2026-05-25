package spar

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// battleClient is the minimal client surface the per-combatant loop needs. Both
// *game.Client (via game.GameClient) and the test fake satisfy it.
type battleClient interface {
	GetBattleStatus(ctx context.Context) error
	Battle(ctx context.Context, action string, payload map[string]any) error
	GetState() *game.State
}

// battleOver reports whether a battle has ended: nil/absent state, we are no
// longer a participant, or one or fewer sides still have a living (HullPct>0)
// participant.
func battleOver(b *game.BattleState) bool {
	if b == nil || !b.IsParticipant {
		return true
	}
	living := map[string]bool{}
	for _, p := range b.Participants {
		if p.HullPct > 0 {
			living[p.SideID] = true
		}
	}
	return len(living) <= 1
}

// runPolicyLoop drives one combatant: each tick it refreshes battle status,
// stops when the battle is over, otherwise applies its policy. tickSleep is the
// pause between iterations (game.SleepTick in production; tiny in tests).
func runPolicyLoop(ctx context.Context, bc battleClient, selfID string, p Policy, tickSleep time.Duration) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err := bc.GetBattleStatus(ctx); err != nil {
			return fmt.Errorf("get_battle_status: %w", err)
		}
		b := bc.GetState().BattleState
		if battleOver(b) {
			return nil
		}
		if v, ok := BuildView(b, selfID); ok {
			act := p.Decide(v)
			if act.Kind == "battle" {
				if err := bc.Battle(ctx, act.BattleAction, act.Payload); err != nil {
					return fmt.Errorf("battle %s: %w", act.BattleAction, err)
				}
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(tickSleep):
		}
	}
}

// Match orchestrates a sparring session.
type Match struct {
	Combatants   []*Combatant
	Arena        string // arena system id (must be set; auto-discovery deferred)
	Rendezvous   string // POI type, e.g. "asteroid_belt"
	AggressorIdx int    // index of the bot that initiates (botvbot mode)
	Partner      bool   // partner mode: don't initiate; wait for a human to attack
	MaxTicks     int
	Logger       *log.Logger
}

// Run sets up all combatants, initiates (or waits for) the fight, drives the
// bots, and prints telemetry plus a final summary.
func (m *Match) Run(ctx context.Context) error {
	// botvbot needs >=2 bots; partner mode needs >=1 bot (the human is an
	// external play_as session, not a combatant the harness logs in).
	minC := 2
	if m.Partner {
		minC = 1
	}
	if len(m.Combatants) < minC {
		return fmt.Errorf("need at least %d combatant(s), got %d", minC, len(m.Combatants))
	}

	// Auto-discovery is deferred (the client does not cache the full systems
	// list), so require an explicit --arena for now.
	arena := m.Arena
	if arena == "" {
		return fmt.Errorf("no arena specified: pass --arena <system> (e.g. ross_128); auto-discovery is not yet supported")
	}

	// Concurrent setup (WaitGroup + first-error; no errgroup dependency).
	if err := m.setupAll(ctx, arena); err != nil {
		return fmt.Errorf("combatant setup: %w", err)
	}
	m.Logger.Printf("All %d combatants staged in %s", len(m.Combatants), arena)

	// Validate the arena really is outside empire space (checked on arrival).
	if err := m.Combatants[0].Client.GetSystem(ctx); err != nil {
		return fmt.Errorf("validate arena: get_system: %w", err)
	}
	if err := ValidateArenaSystem(m.Combatants[0].Client.GetState().System); err != nil {
		return err
	}

	// Initiate.
	if m.Partner {
		opp := m.Combatants[0]
		m.Logger.Printf("PARTNER MODE: arena=%s rendezvous-type=%s", arena, m.Rendezvous)
		m.Logger.Printf("  Run: play_as <your-agent>, travel to the rendezvous POI, then: attack %s", opp.Username)
		m.Logger.Printf("  Waiting for a battle to start...")
		if err := m.waitForBattle(ctx, opp); err != nil {
			return err
		}
	} else {
		agg := m.Combatants[m.AggressorIdx]
		target := m.Combatants[(m.AggressorIdx+1)%len(m.Combatants)]
		m.Logger.Printf("%s attacking %s", agg.Username, target.Username)
		if err := agg.Client.Attack(ctx, target.Username); err != nil {
			return fmt.Errorf("initiate attack: %w", err)
		}
	}

	// Run bot policy loops + telemetry concurrently until the battle ends.
	loopCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if m.MaxTicks > 0 {
		go func() {
			select {
			case <-loopCtx.Done():
			case <-time.After(time.Duration(m.MaxTicks) * game.SleepTick):
				m.Logger.Printf("max-ticks reached; ending match")
				cancel()
			}
		}()
	}

	// Telemetry off the first combatant's client.
	go m.telemetryLoop(loopCtx, m.Combatants[0])

	var wg sync.WaitGroup
	for _, c := range m.Combatants {
		if c.Policy == nil {
			continue // human partner slot: not bot-driven
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := runPolicyLoop(loopCtx, c.Client, c.PlayerID(), c.Policy, game.SleepTick); err != nil && !errors.Is(err, context.Canceled) {
				m.Logger.Printf("%s policy loop ended: %v", c.Username, err)
			}
		}()
	}
	wg.Wait()
	cancel()

	// If the parent context was cancelled (Ctrl-C), the bots stopped mid-fight.
	// Best-effort tell each to flee so it disengages rather than dying idle,
	// then still print the partial summary. MaxTicks and natural battle-end
	// cancel only the child loopCtx, leaving the parent error nil.
	if ctx.Err() != nil {
		m.Logger.Printf("shutdown requested; telling bots to flee")
		m.fleeAll()
	}

	m.printSummary()
	return nil
}

// fleeBot issues a best-effort flee stance so a bot disengages on shutdown.
func fleeBot(ctx context.Context, bc battleClient) error {
	return bc.Battle(ctx, "stance", map[string]any{"stance": "flee"})
}

// fleeAll tells every bot combatant to flee, on a fresh short-lived context
// since the match context is already cancelled by the time this runs.
func (m *Match) fleeAll() {
	fctx, cancel := context.WithTimeout(context.Background(), game.SleepShort)
	defer cancel()
	for _, c := range m.Combatants {
		if c.Policy == nil {
			continue // human partner slot: not bot-driven
		}
		if err := fleeBot(fctx, c.Client); err != nil {
			m.Logger.Printf("%s flee on shutdown: %v", c.Username, err)
		}
	}
}

// setupAll runs Setup for every combatant concurrently, returning the first
// error encountered.
func (m *Match) setupAll(ctx context.Context, arena string) error {
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
	)
	for _, c := range m.Combatants {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := c.Setup(ctx, arena, m.Rendezvous); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return firstErr
}

// waitForBattle polls until the given combatant is in a battle (partner mode).
func (m *Match) waitForBattle(ctx context.Context, c *Combatant) error {
	deadline := time.Now().Add(game.SleepPartnerJoinWindow) // human join window
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		_ = c.Client.GetBattleStatus(ctx)
		if !battleOver(c.Client.GetState().BattleState) {
			return nil
		}
		time.Sleep(game.SleepTick)
	}
	return fmt.Errorf("no battle started within timeout; did the human attack %s?", c.Username)
}

// telemetryLoop prints a per-participant row whenever the battle state changes.
func (m *Match) telemetryLoop(ctx context.Context, c *Combatant) {
	var lastSig string
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		_ = c.Client.GetBattleStatus(ctx)
		st := c.Client.GetState()
		if st.BattleState != nil {
			if sig := battleSignature(st.BattleState); sig != lastSig {
				lastSig = sig
				for _, p := range st.BattleState.Participants {
					m.Logger.Print(formatTickRow(st.CurrentTick, p))
				}
			}
		}
		time.Sleep(game.SleepTick)
	}
}

// battleSignature returns a string that changes whenever any participant's
// tactical state (zone/stance/hull/shield) changes. Used to dedup telemetry
// rows without relying on CurrentTick, which get_battle_status parsing does not
// advance.
func battleSignature(b *game.BattleState) string {
	var sb strings.Builder
	for _, p := range b.Participants {
		fmt.Fprintf(&sb, "%s:%s:%s:%d:%d;", p.PlayerID, p.Zone, p.Stance, p.HullPct, p.ShieldPct)
	}
	return sb.String()
}

// printSummary prints final hull/shield per combatant.
func (m *Match) printSummary() {
	m.Logger.Printf("──── match summary ────")
	for _, c := range m.Combatants {
		st := c.Client.GetState()
		m.Logger.Print(formatSummary(c.Username, st))
	}
}
