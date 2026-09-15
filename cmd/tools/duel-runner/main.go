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
	"sync"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// arenaAcceptPolls bounds the wait for a challenge to reach the defender.
// Each poll sleeps one tick, so this is ~2 minutes -- long enough for a
// slow tick, short enough that a lapsed challenge fails the run instead of
// hanging it.
const arenaAcceptPolls = 12

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
			logger.Printf("  [%s] %s repeat %d (%s)", camp.ModeOf(r.duel), r.duel.ID, r.repeat, r.duel.Purpose)
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
				switch {
				case botB.Name() == r.duel.Guest:
					// The guest is already logged in as side B (e.g. run with
					// --b <guest>). Reuse that session -- logging the same
					// agent in a second time replace-wars with the first and
					// trips the contention detector.
					g = botB
				case botA.Name() == r.duel.Guest:
					g = botA
				default:
					// A genuinely separate third agent. Same 36s
					// session-contention window as the startup logins.
					time.Sleep(2 * game.SleepLong)
					g, err = Login(ctx, r.duel.Guest, logger)
					if err != nil {
						logger.Fatal(err)
					}
					defer g.Close()
				}
				guests[r.duel.Guest] = g
			}
			bSide = g
		}
		logger.Printf("=== %s repeat %d [%s]: %s", r.duel.ID, r.repeat, camp.ModeOf(r.duel), r.duel.Purpose)
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
	// Preflight: bring each side to its correct fit and into the arena. Each
	// bot drives its own game session, so the two sides prep CONCURRENTLY --
	// when both need a staging refit their round-trips overlap instead of
	// stacking, and a bot that can skip staging doesn't idle while the other
	// refits. Barrier on both before the attack.
	mode := camp.ModeOf(d)
	rec.Mode = mode
	fightSystem, err := camp.FightSystem(d)
	if err != nil {
		return rec, err
	}
	fitFor := func(bot *Bot) FitSpec {
		if bot == a {
			return d.FitA
		}
		return d.FitB
	}
	prep := func(bot *Bot) error {
		// The bots deal no real damage and attack works from anywhere in the
		// arena system, so the staging round-trip is pure overhead EXCEPT
		// when a bot needs something only a station provides: a fit change,
		// ammo, or a full-pool repair. A duel's guest keeps their own
		// ship/fit and never visits staging.
		isGuest := d.Guest != "" && bot.Name() == d.Guest
		if isGuest {
			logger.Printf("%s: guest side for %s, skipping refit (keeps own fit)", bot.Name(), d.ID)
		} else {
			fit := fitFor(bot)
			needsFit, err := bot.NeedsFit(fit)
			if err != nil {
				return err
			}
			if d.RequireFull || len(fit.Ammo) > 0 || needsFit {
				// Refits require a dock: get the bot to the staging station
				// first, tolerating every already-there condition (a prior
				// run's recovery may have failed any leg of the trip).
				if err := bot.Jump(camp.StagingSystem); err != nil && !strings.Contains(err.Error(), "already in") {
					logger.Printf("%s staging jump: %v (continuing)", bot.Name(), err)
				}
				if err := bot.Dock(camp.StagingStation); err != nil && !strings.Contains(err.Error(), "already docked") {
					logger.Printf("%s staging dock: %v (may already be docked)", bot.Name(), err)
				}
				// Top off while docked: the detour itself costs fuel, and an
				// un-refuelled bot eventually strands itself. Non-fatal.
				if err := bot.Refuel(); err != nil {
					logger.Printf("%s refuel: %v (continuing)", bot.Name(), err)
				}
				if err := bot.EnsureFit(fit); err != nil {
					return err
				}
				if err := bot.EnsureAmmo(fit); err != nil {
					return err
				}
				if d.RequireFull {
					if err := bot.WaitReady(60); err != nil {
						return err
					}
				}
			} else {
				logger.Printf("%s: fit already current, no ammo/repair needed -- skipping staging", bot.Name())
			}
		}
		// Into the fighting system. Idempotent: a bot already there
		// (survivor of the last duel, or one that skipped staging) just
		// reports "already in".
		if err := bot.Undock(); err != nil {
			logger.Printf("%s undock: %v (may already be in space)", bot.Name(), err)
		}
		if err := bot.Jump(fightSystem); err != nil {
			if strings.Contains(err.Error(), "already in") {
				logger.Printf("%s: already in %s", bot.Name(), fightSystem)
			} else {
				return err
			}
		}
		if mode != ModeArena {
			return nil
		}
		// Arena mode: the challenge handshake requires both sides undocked
		// AT the arena POI, so a system-level arrival is not enough.
		if err := bot.Travel(camp.ArenaPOI); err != nil {
			return fmt.Errorf("travel to arena poi %s: %w", camp.ArenaPOI, err)
		}
		st, err := bot.ArenaStatus()
		if err != nil {
			return err
		}
		if !st.AtArena {
			return fmt.Errorf("%s: at_arena is false after travelling to %s -- that POI does not host arena matches",
				bot.Name(), camp.ArenaPOI)
		}
		// Clear a challenge left pending by a killed run; only one is
		// allowed per player, so a stale one blocks this duel's.
		if st.Outgoing != nil {
			logger.Printf("%s: cancelling stale outgoing challenge %s", bot.Name(), st.Outgoing.ChallengeID)
			if err := bot.ArenaCancel(); err != nil {
				logger.Printf("%s: cancel stale challenge: %v (continuing)", bot.Name(), err)
			}
		}
		if capped := st.CappedSkills(); len(capped) > 0 {
			logger.Printf("%s: WARNING arena XP cap reached today for %v -- these bots have been levelling; "+
				"re-check skills before trusting further runs", bot.Name(), capped)
		}
		return nil
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var prepErr error
	for _, bot := range []*Bot{a, b} {
		wg.Add(1)
		go func(bot *Bot) {
			defer wg.Done()
			if err := prep(bot); err != nil {
				mu.Lock()
				if prepErr == nil {
					prepErr = fmt.Errorf("%s preflight: %w", bot.Name(), err)
				}
				mu.Unlock()
			}
		}(bot)
	}
	wg.Wait()
	if prepErr != nil {
		return rec, prepErr
	}
	attacker, defender := a, b
	if d.Attacker == b.Name() {
		attacker, defender = b, a
	}
	target := defender.PlayerID()
	if target == "" {
		target = defender.Username()
	}
	var startedBattleID string
	if mode == ModeArena {
		startedBattleID, rec.XPUsedToday, err = startArenaMatch(attacker, defender, target, logger)
	} else {
		logger.Printf("%s attacking %s (player %q, target_id %q)", attacker.Name(), defender.Name(), defender.Username(), target)
		err = attacker.Attack(target)
	}
	if err != nil {
		return rec, err
	}
	res, err := runDuel(a, b, d, func() { time.Sleep(game.SleepQuick) }, logger)
	if err != nil {
		return rec, err
	}
	rec.BattleID, rec.Outcome, rec.Void, rec.Ended = res.BattleID, res.Outcome, res.Void, time.Now().UTC()
	// The accept reply names the battle authoritatively; the view-scraped
	// id is a fallback that can be empty if the fight ended before the
	// first poll landed.
	if startedBattleID != "" {
		if res.BattleID != "" && res.BattleID != startedBattleID {
			logger.Printf("%s: battle id mismatch -- accept said %s, views said %s; keeping the accept id",
				d.ID, startedBattleID, res.BattleID)
		}
		rec.BattleID = startedBattleID
	}
	// No forced return to staging: survivors stay in the arena ready for the
	// next duel (the next preflight pulls them to staging only if it needs a
	// refit), and a destroyed bot has already respawned at its home station
	// with a free starter hull -- the next preflight's arena jump collects it
	// from there. Skipping the round-trip is the whole point of the
	// conditional staging above.
	return rec, nil
}

// startArenaMatch runs the v0.586.0 challenge/accept handshake and returns
// the started battle's id plus the challenger's arena XP ledger.
//
// Both sides must already be undocked at the arena POI (preflight puts
// them there). max_side_size 1 makes it a solo duel, which also makes a
// third-party join impossible -- the interference case the lawless
// campaign needed its void rule for.
func startArenaMatch(attacker, defender *Bot, target string, logger *log.Logger) (string, map[string]int, error) {
	logger.Printf("%s challenging %s to an arena match (player %q, player_id %q)",
		attacker.Name(), defender.Name(), defender.Username(), target)
	ch, err := attacker.ArenaChallenge(target, 1)
	if err != nil {
		return "", nil, fmt.Errorf("%s challenge %s: %w", attacker.Name(), defender.Name(), err)
	}
	logger.Printf("%s: challenge %s issued to %s at %s (expires tick %d)",
		attacker.Name(), ch.ChallengeID, ch.TargetName, ch.POIID, ch.ExpiresTick)

	// The challenge is a mutation (1 per tick), and the defender's view of
	// it only appears once the server has processed it -- so poll status
	// rather than accepting blind.
	var incoming bool
	for i := range arenaAcceptPolls {
		st, err := defender.ArenaStatus()
		if err != nil {
			return "", nil, fmt.Errorf("%s arena status: %w", defender.Name(), err)
		}
		if st.Incoming != nil {
			incoming = true
			break
		}
		if i == 0 {
			logger.Printf("%s: waiting for the challenge to appear", defender.Name())
		}
		time.Sleep(game.SleepTick)
	}
	if !incoming {
		return "", nil, fmt.Errorf("%s: no incoming challenge after %d polls (it may have lapsed, or the bots are at different POIs)",
			defender.Name(), arenaAcceptPolls)
	}
	acc, err := defender.ArenaAccept()
	if err != nil {
		return "", nil, fmt.Errorf("%s accept: %w", defender.Name(), err)
	}
	logger.Printf("%s accepted: battle %s, sides %d vs %d, %d participants",
		defender.Name(), acc.BattleID, acc.YourSide, acc.OpponentSide, len(acc.Participants))

	// XP ledger snapshot for the manifest: arena fights grant combat XP
	// normally, so a campaign long enough to level a bot invalidates the
	// zero-skill baseline the measurements assume.
	var xp map[string]int
	if st, err := attacker.ArenaStatus(); err != nil {
		logger.Printf("%s: arena XP snapshot: %v (continuing)", attacker.Name(), err)
	} else {
		xp = st.XPUsedToday
	}
	return acc.BattleID, xp, nil
}
