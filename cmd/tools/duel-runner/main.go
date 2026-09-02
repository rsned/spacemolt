// Command duel-runner executes scripted 1v1 calibration duels between two
// owned agents from a campaign file, per the Phase B design:
// kb/docs/superpowers/specs/2026-09-01-phase-b-calibration-duels-design.md
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

func main() {
	campaignPath := flag.String("campaign", "", "campaign JSON (required)")
	manifestPath := flag.String("manifest", "", "manifest JSONL to append to (required)")
	agentA := flag.String("a", "battle_bot1", "side A agent id")
	agentB := flag.String("b", "battle_bot2", "side B agent id (per-duel guest overrides)")
	// guest is accepted for CLI-surface compatibility with the brief's
	// documented flag set, but per-duel guests come from the campaign
	// file's Duel.Guest field, not a global CLI override; ignored here.
	_ = flag.String("guest", "", "ignored: per-duel guest agent id comes from each duel's campaign \"guest\" field, not this flag")
	only := flag.String("only", "", "comma-separated scenario ids to run (default: all)")
	dryRun := flag.Bool("dry-run", false, "print the run list and exit without logging in")
	flag.Parse()
	logger := log.New(os.Stderr, "[duel-runner] ", log.LstdFlags)
	if *campaignPath == "" || *manifestPath == "" {
		fmt.Fprintln(os.Stderr, "usage: duel-runner --campaign c.json --manifest m.jsonl [--only S0]")
		os.Exit(2)
	}
	camp, err := LoadCampaign(*campaignPath)
	if err != nil {
		logger.Fatal(err)
	}
	done, err := LoadDone(*manifestPath)
	if err != nil {
		logger.Fatal(err)
	}
	filter := map[string]bool{}
	for _, id := range strings.Split(*only, ",") {
		if id != "" {
			filter[id] = true
		}
	}
	type run struct {
		duel   Duel
		repeat int
	}
	var runs []run
	for _, d := range camp.Duels {
		if len(filter) > 0 && !filter[d.ID] {
			continue
		}
		for r := 1; r <= d.Repeats; r++ {
			if !done[DoneKey(d.ID, r)] {
				runs = append(runs, run{d, r})
			}
		}
	}
	logger.Printf("%d runs pending", len(runs))
	if *dryRun {
		for _, r := range runs {
			logger.Printf("  %s repeat %d (%s)", r.duel.ID, r.repeat, r.duel.Purpose)
		}
		return
	}

	ctx := context.Background()
	botA, err := Login(ctx, *agentA, logger)
	if err != nil {
		logger.Fatal(err)
	}
	defer botA.Close()
	// Login gap: the client enforces a 36s session-contention window
	// between logins. game.SleepLong alone is only 20s (2*SleepTick), so
	// this composes named constants to clear the window -- do not
	// simplify this back down to a single SleepLong.
	time.Sleep(2 * game.SleepLong)
	botB, err := Login(ctx, *agentB, logger)
	if err != nil {
		logger.Fatal(err)
	}
	defer botB.Close()

	guests := map[string]*Bot{}
	for _, r := range runs {
		bSide := botB
		if r.duel.Guest != "" {
			g, ok := guests[r.duel.Guest]
			if !ok {
				// Same 36s session-contention window as the startup logins.
				time.Sleep(2 * game.SleepLong)
				g, err = Login(ctx, r.duel.Guest, logger)
				if err != nil {
					logger.Fatal(err)
				}
				defer g.Close()
				guests[r.duel.Guest] = g
			}
			bSide = g
		}
		logger.Printf("=== %s repeat %d: %s", r.duel.ID, r.repeat, r.duel.Purpose)
		rec, err := executeDuel(camp, botA, bSide, r.duel, r.repeat, logger)
		if err != nil {
			logger.Fatalf("%s repeat %d: %v (manifest is consistent; re-run to resume)", r.duel.ID, r.repeat, err)
		}
		if err := AppendRecord(*manifestPath, rec); err != nil {
			logger.Fatal(err)
		}
		logger.Printf("=== %s repeat %d: %s (battle %s)%s", r.duel.ID, r.repeat, rec.Outcome, rec.BattleID,
			map[bool]string{true: " VOID", false: ""}[rec.Void])
	}
	logger.Printf("campaign section complete")
}

// executeDuel runs preflight, the battle, and recovery for one repeat.
func executeDuel(camp *Campaign, a, b *Bot, d Duel, repeat int, logger *log.Logger) (Record, error) {
	rec := Record{ScenarioID: d.ID, Repeat: repeat, Started: time.Now().UTC()}
	// Reset battle-view tracking on both bots before the new fight starts:
	// without this, a bot's seenBattle stays true from the previous duel and
	// View() reports the new duel as instantly "Ended" (carrying the
	// PREVIOUS duel's battle id) on its very first poll, because
	// State.InBattle does not flip true until the new battle's first server
	// push arrives.
	a.ResetBattleTracking()
	b.ResetBattleTracking()
	// Preflight: both at staging, correct fits, then into the arena. A
	// duel's guest (e.g. S6c's craftsman-1) keeps their own ship/fit --
	// the runner must never refit them, so EnsureFit is skipped for
	// whichever side carries the guest agent.
	for _, bot := range []*Bot{a, b} {
		if d.Guest != "" && bot.Name() == d.Guest {
			logger.Printf("%s: guest side for %s, skipping EnsureFit (keeps own fit)", bot.Name(), d.ID)
			continue
		}
		fit := d.FitA
		if bot != a {
			fit = d.FitB
		}
		if err := bot.EnsureFit(fit); err != nil {
			return rec, err
		}
		// Ensure ammo is on board if needed.
		if err := bot.EnsureAmmo(fit); err != nil {
			return rec, err
		}
	}
	for _, bot := range []*Bot{a, b} {
		if err := bot.Undock(); err != nil {
			logger.Printf("%s undock: %v (may already be in space)", bot.Name(), err)
		}
		if err := bot.Jump(camp.ArenaSystem); err != nil {
			// Idempotent preflight: a bot left in the arena by an aborted
			// run is already where it needs to be.
			if strings.Contains(err.Error(), "already in") {
				logger.Printf("%s: already in the arena", bot.Name())
			} else {
				return rec, err
			}
		}
	}
	attacker, defender := a, b
	if d.Attacker == b.Name() {
		attacker, defender = b, a
	}
	if err := attacker.Attack(defender.Name()); err != nil {
		return rec, err
	}
	res, err := runDuel(a, b, d, func() { time.Sleep(game.SleepQuick) }, logger)
	if err != nil {
		return rec, err
	}
	rec.BattleID, rec.Outcome, rec.Void, rec.Ended = res.BattleID, res.Outcome, res.Void, time.Now().UTC()
	// Recovery: both bots return to staging and dock (a destroyed bot has
	// already respawned there with a free starter hull).
	for _, bot := range []*Bot{a, b} {
		if err := bot.Jump(camp.StagingSystem); err != nil {
			logger.Printf("%s return jump: %v (bot may have respawned at staging already)", bot.Name(), err)
		}
		if err := bot.Dock(camp.StagingStation); err != nil {
			logger.Printf("%s dock: %v", bot.Name(), err)
		}
	}
	return rec, nil
}
