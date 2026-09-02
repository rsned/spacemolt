package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// Bot wraps a logged-in client as a duel `side` plus the out-of-battle
// logistics the campaign needs (refit, travel, recovery).
type Bot struct {
	agentID  string
	client   *game.Client
	ctx      context.Context
	logger   *log.Logger
	username string // this bot's in-game Player.Username, captured once at login

	// battle-view tracking (see View): once a battle is seen, a later poll
	// with InBattle==false means the battle ended.
	seenBattle      bool
	battleFirstSeen time.Time

	// ammo tracking: cached after EnsureFit for use in Reload()
	weaponInstances map[string][]string // type_id -> instance ids (weapons only)
	ammoMap         map[string]string   // type_id -> ammo item_id
}

// settle mirrors battle-export: a reply lands in the raw cache ~2s after
// Submit returns.
func (b *Bot) settle() { time.Sleep(game.SleepQuick) }

// Login logs in agentID and waits for the session to settle before the
// caller issues its first command.
func Login(ctx context.Context, agentID string, logger *log.Logger) (*Bot, error) {
	client, _, err := game.InitializeAgent(agentID, logger, ctx, false)
	if err != nil {
		return nil, fmt.Errorf("login %s: %w", agentID, err)
	}
	b := &Bot{agentID: agentID, client: client, ctx: ctx, logger: logger}
	b.settle()
	b.username = client.GetState().Player.Username
	return b, nil
}

// ResetBattleTracking clears the seen-battle bookkeeping. Callers MUST call
// this on both bots before issuing the Attack that starts a new duel:
// without it, seenBattle stays true from the previous duel and View() will
// instantly report Ended (with the PREVIOUS duel's battle id) on the very
// first poll, because State.InBattle does not flip true until the new
// battle's first server push arrives.
func (b *Bot) ResetBattleTracking() {
	b.seenBattle = false
	b.battleFirstSeen = time.Time{}
}

func (b *Bot) Close() { _ = b.client.Close() }

func (b *Bot) Name() string { return b.agentID }

// Username is the in-game player name from the server's own login state.
// It can differ from the agent id (craftsman-1 plays as "Arthur
// 'Artificer' Artis"), and attack's target_id resolves against it — an
// agent id that isn't also the username yields "Target not in this
// system" even with both ships in the arena.
func (b *Bot) Username() string { return b.username }

// PlayerID is the server's player id hash (get_nearby's player_id), the
// canonical attack target_id. Empty only if login state never arrived.
func (b *Bot) PlayerID() string { return b.client.GetState().Player.ID }

// Raw issues a free-form command and waits for it to settle in the raw
// response cache.
func (b *Bot) Raw(cmd string, args map[string]any) error {
	if err := b.client.RawCommand(b.ctx, cmd, args); err != nil {
		return fmt.Errorf("%s %s: %w", b.agentID, cmd, err)
	}
	b.settle()
	return nil
}

func (b *Bot) Battle(action string, kv map[string]any) error {
	args := map[string]any{"action": action}
	for k, v := range kv {
		args[k] = v
	}
	// Battle actions are free and queue for the next tick; no settle needed
	// beyond the client's own send path.
	return b.client.RawCommand(b.ctx, "battle", args)
}

// battleStatusPayload is the slice of a get_battle_status reply View() needs.
// Parsed from the raw "_last" cache rather than client.GetState().BattleState
// (or any push-derived field), per the ruling: the client's battle_update
// push handler only sets State.InBattle/State.LastBattleID and discards
// zone/stance/tick, so those must come from this direct query instead.
type battleStatusPayload struct {
	BattleID     string `json:"battle_id"`
	Tick         int    `json:"tick"`
	TickDuration int    `json:"tick_duration"`
	Participants []struct {
		PlayerID string `json:"player_id"`
		Username string `json:"username"`
		Zone     string `json:"zone"`
		Stance   string `json:"stance"`
	} `json:"participants"`
}

// battleEnded is the pure ended-decision extracted from View() so it is
// testable without a live client: once a battle has been seen (seenBattle),
// a poll reporting InBattle==false means that battle has ended. Before the
// first battle is seen (seenBattle==false), InBattle==false just means no
// battle has started yet -- not "ended". This is the crux of the
// cross-duel bug: without Bot.ResetBattleTracking() between duels,
// seenBattle stays true from the prior duel and this returns true on the
// very first poll of the next one.
func battleEnded(seenBattle, inBattle bool) bool {
	return seenBattle && !inBattle
}

// View reads the latest battle state via a fresh get_battle_status query
// (a free info query, not a mutation). It runs once per control-loop
// iteration; the loop's own pacing keeps the poll rate around 1 per 2-4s.
func (b *Bot) View() (BattleView, bool) {
	if err := b.client.RawCommand(b.ctx, "get_battle_status", nil); err != nil {
		// "No active battle" after we HAVE seen one is the end signal:
		// once every participant escapes or dies, the status query itself
		// errors (measured live in the S0 probe) and no battle_update push
		// ever announces the end to a side that already disengaged.
		if b.seenBattle && strings.Contains(err.Error(), "No active battle") {
			return BattleView{
				BattleID: b.client.GetState().LastBattleID,
				Ended:    true,
				Outcome:  "ended",
			}, true
		}
		b.logger.Printf("%s: get_battle_status: %v", b.agentID, err)
		return BattleView{}, false
	}
	time.Sleep(game.SleepQuick) // settle: the reply lands in the raw cache a moment after Submit returns.

	var payload battleStatusPayload
	if raw := b.client.GetRawJSON("_last"); raw != nil {
		if err := json.Unmarshal(raw, &payload); err != nil {
			b.logger.Printf("%s: parse get_battle_status: %v", b.agentID, err)
		}
	}
	st := b.client.GetState()

	if payload.BattleID != "" && !b.seenBattle {
		b.seenBattle = true
		b.battleFirstSeen = time.Now()
	}

	// Ended: once a battle has been seen, InBattle flipping false means the
	// fight is over. The authoritative outcome comes later from the
	// exported battle log; "ended" is a placeholder the manifest can use.
	if battleEnded(b.seenBattle, st.InBattle) {
		return BattleView{BattleID: st.LastBattleID, Ended: true, Outcome: "ended"}, true
	}

	if payload.BattleID == "" && !st.InBattle {
		return BattleView{}, false
	}

	tickDuration := payload.TickDuration
	if tickDuration <= 0 {
		tickDuration = 10
	}
	tick := payload.Tick
	if tick <= 0 {
		tick = 1 + int(time.Since(b.battleFirstSeen)/(time.Duration(tickDuration)*time.Second))
	}

	v := BattleView{
		BattleID:         payload.BattleID,
		Tick:             tick,
		ParticipantCount: len(payload.Participants),
	}
	for _, p := range payload.Participants {
		if p.Username == b.username {
			v.MyZone, v.MyStance = p.Zone, p.Stance
		}
	}
	return v, true
}

// --- logistics -----------------------------------------------------------

// computeFitActions diffs the current module list against the wanted one,
// respecting duplicates (multiset difference in both directions).
func computeFitActions(current, want []string) (toRemove, toInstall []string) {
	need := map[string]int{}
	for _, m := range want {
		need[m]++
	}
	for _, m := range current {
		if need[m] > 0 {
			need[m]--
		} else {
			toRemove = append(toRemove, m)
		}
	}
	for m, n := range need {
		for range n {
			toInstall = append(toInstall, m)
		}
	}
	return toRemove, toInstall
}

// shipInfo is the slice of get_ship the logistics code needs. The reply
// shape is {ship: {...}, modules: [...], cargo_used, cargo_max, class?} --
// the fitted-module detail (id + type_id) is a TOP-LEVEL "modules" array,
// not nested under "ship" (verified against pkg/game/serverapi/responses.go
// GetShipResponse and pkg/game/serverapi/types.go ShipModule; ship.modules
// is a separate, flatter []string of the same type ids used only for the
// legacy view). ShipModule.ID is the fitted instance id (what uninstall_mod
// takes); ShipModule.TypeID is the catalog item id (what buy/install_mod
// take) -- the brief's "item_id" json tag does not exist on the wire.
type shipInfo struct {
	Ship struct {
		ClassID string `json:"class_id"`
	} `json:"ship"`
	Modules []struct {
		ID     string `json:"id"`
		TypeID string `json:"type_id"`
	} `json:"modules"`
}

// NeedsFit reports whether the ship's current modules differ from the
// FitSpec (or the hull is wrong). get_ship is a free info query that works
// undocked, so the preflight can call this from the arena to decide whether
// a staging detour is even required — most consecutive duels reuse the same
// fit and can skip staging entirely. A hull mismatch counts as "needs fit"
// so the caller routes to staging and surfaces EnsureFit's clear error.
func (b *Bot) NeedsFit(fit FitSpec) (bool, error) {
	if err := b.Raw("get_ship", nil); err != nil {
		return false, err
	}
	var info shipInfo
	if err := json.Unmarshal(b.client.GetRawJSON("ship"), &info); err != nil {
		return false, fmt.Errorf("%s: parse get_ship: %w", b.agentID, err)
	}
	if fit.Hull != "" && info.Ship.ClassID != fit.Hull {
		return true, nil
	}
	current := make([]string, 0, len(info.Modules))
	for _, m := range info.Modules {
		current = append(current, m.TypeID)
	}
	toRemove, toInstall := computeFitActions(current, fit.Modules)
	if len(toRemove)+len(toInstall) > 0 {
		return true, nil
	}
	// Fit already matches: cache weapon instances so Reload() works without
	// EnsureFit having run this duel.
	byType := map[string][]string{}
	for _, m := range info.Modules {
		byType[m.TypeID] = append(byType[m.TypeID], m.ID)
	}
	b.weaponInstances = byType
	b.ammoMap = fit.Ammo
	return false, nil
}

// EnsureFit docks (caller guarantees at staging), buys and installs until
// get_ship matches the FitSpec exactly, and errors on any mismatch it
// cannot resolve (wrong hull requires manual intervention — hull swaps are
// campaign setup, not per-duel work).
func (b *Bot) EnsureFit(fit FitSpec) error {
	if err := b.Raw("get_ship", nil); err != nil {
		return err
	}
	var info shipInfo
	if err := json.Unmarshal(b.client.GetRawJSON("ship"), &info); err != nil {
		return fmt.Errorf("%s: parse get_ship: %w", b.agentID, err)
	}
	if fit.Hull != "" && info.Ship.ClassID != fit.Hull {
		return fmt.Errorf("%s: hull is %q, scenario needs %q (swap hulls manually or via campaign setup)",
			b.agentID, info.Ship.ClassID, fit.Hull)
	}
	current := make([]string, 0, len(info.Modules))
	byType := map[string][]string{} // type_id -> instance ids
	for _, m := range info.Modules {
		current = append(current, m.TypeID)
		byType[m.TypeID] = append(byType[m.TypeID], m.ID)
	}
	toRemove, toInstall := computeFitActions(current, fit.Modules)
	for _, typeID := range toRemove {
		inst := byType[typeID][0]
		byType[typeID] = byType[typeID][1:]
		if err := b.Raw("uninstall_mod", map[string]any{"module_id": inst}); err != nil {
			return err
		}
	}
	for _, typeID := range toInstall {
		// buy is a no-op cost-wise if cargo already holds one from a spare.
		if err := b.Raw("buy", map[string]any{"item_id": typeID, "quantity": 1}); err != nil {
			b.logger.Printf("%s: buy %s failed (may already own one): %v", b.agentID, typeID, err)
			// Fall back to station storage: campaign hardware is often
			// ferried in by the owner rather than sold on a lawless market.
			if werr := b.Raw("withdraw_items", map[string]any{"item_id": typeID, "quantity": 1}); werr != nil {
				b.logger.Printf("%s: withdraw %s from storage also failed: %v (install may still find cargo)", b.agentID, typeID, werr)
			}
		}
		if err := b.Raw("install_mod", map[string]any{"module_id": typeID}); err != nil {
			return err
		}
	}
	// Verify: re-read and diff again; any residue is a hard error.
	if err := b.Raw("get_ship", nil); err != nil {
		return err
	}
	var after shipInfo
	if err := json.Unmarshal(b.client.GetRawJSON("ship"), &after); err != nil {
		return err
	}
	now := make([]string, 0, len(after.Modules))
	afterByType := map[string][]string{}
	for _, m := range after.Modules {
		now = append(now, m.TypeID)
		afterByType[m.TypeID] = append(afterByType[m.TypeID], m.ID)
	}
	if rem, inst := computeFitActions(now, fit.Modules); len(rem)+len(inst) > 0 {
		return fmt.Errorf("%s: fit verify failed: extra=%v missing=%v", b.agentID, rem, inst)
	}
	// Cache weapon instances and ammo map for later use in Reload().
	b.weaponInstances = afterByType
	b.ammoMap = fit.Ammo
	return nil
}

// EnsureAmmo ensures one ammo item is on board for each weapon in fit.Ammo,
// buying if needed (with fallback to withdraw from storage), then reloading
// all cached weapon instances with their ammo. Errors on buy and withdraw
// are logged non-fatally if either succeeds; if both fail, a hard error is
// returned naming the ammo item. Reload errors within the same ammo type are
// logged non-fatally (a full magazine errors harmlessly).
func (b *Bot) EnsureAmmo(fit FitSpec) error {
	if len(fit.Ammo) == 0 {
		return nil
	}
	for weaponType, ammoItem := range fit.Ammo {
		// Ensure one ammo item is on board.
		buyErr := b.Raw("buy", map[string]any{"item_id": ammoItem, "quantity": 1})
		if buyErr == nil {
			b.logger.Printf("%s: bought ammo %s for weapon %s", b.agentID, ammoItem, weaponType)
		} else {
			b.logger.Printf("%s: buy ammo %s failed: %v (trying withdraw)", b.agentID, ammoItem, buyErr)
			withdrawErr := b.Raw("withdraw_items", map[string]any{"item_id": ammoItem, "quantity": 1})
			if withdrawErr == nil {
				b.logger.Printf("%s: withdrew ammo %s for weapon %s from storage", b.agentID, ammoItem, weaponType)
			} else {
				b.logger.Printf("%s: withdraw ammo %s also failed: %v", b.agentID, ammoItem, withdrawErr)
				if buyErr != nil && withdrawErr != nil {
					return fmt.Errorf("%s: could not obtain ammo %s for weapon %s (buy and withdraw both failed)",
						b.agentID, ammoItem, weaponType)
				}
			}
		}
		// Reload all instances of this weapon type with the ammo.
		if instances, ok := b.weaponInstances[weaponType]; ok {
			for _, inst := range instances {
				if err := b.Raw("reload", map[string]any{"weapon_instance_id": inst, "ammo_item_id": ammoItem}); err != nil {
					b.logger.Printf("%s: reload weapon %s instance %s: %v", b.agentID, weaponType, inst, err)
				}
			}
		}
	}
	return nil
}

// Reload reloads all cached weapon instances using the cached ammo map
// (called from within a battle). Returns the first error whose message
// contains "not in a battle" and logs other errors non-fatally.
func (b *Bot) Reload() error {
	if b.weaponInstances == nil || b.ammoMap == nil {
		return nil
	}
	for weaponType, ammoItem := range b.ammoMap {
		if instances, ok := b.weaponInstances[weaponType]; ok {
			for _, inst := range instances {
				if err := b.Raw("reload", map[string]any{"weapon_instance_id": inst, "ammo_item_id": ammoItem}); err != nil {
					if strings.Contains(err.Error(), "not in a battle") {
						return err
					}
					b.logger.Printf("%s: mid-battle reload weapon %s instance %s: %v", b.agentID, weaponType, inst, err)
				}
			}
		}
	}
	return nil
}

// Attack attacks target (a player, pirate, or NPC id). The wire payload key
// is target_id (not target), and the server also wants a weapon_idx --
// verified against pkg/game/client.go's Attack method.
func (b *Bot) Attack(target string) error {
	return b.Raw("attack", map[string]any{"target_id": target, "weapon_idx": 0})
}

// Jump jumps to system. The wire payload key is target_system (not
// system) -- verified against pkg/game/client.go's Jump method.
func (b *Bot) Jump(system string) error {
	if err := b.Raw("jump", map[string]any{"target_system": system}); err != nil {
		return err
	}
	time.Sleep(game.SleepJump)
	return nil
}

// Dock travels to poi (a station POI id) and docks there. dock() itself
// takes no payload -- you dock at whatever POI you are currently at -- so
// getting to a specific station is a travel(target_poi) first, verified
// against pkg/game/client.go's Travel/Dock methods and server_docs/api.md
// ("dock() -- Dock at a base", "travel(target_poi)").
func (b *Bot) Dock(poi string) error {
	if err := b.Raw("travel", map[string]any{"target_poi": poi}); err != nil {
		return err
	}
	time.Sleep(game.SleepTravel)
	if err := b.Raw("dock", nil); err != nil {
		return err
	}
	time.Sleep(game.SleepDock)
	return nil
}

func (b *Bot) Undock() error { return b.Raw("undock", nil) }

// Refuel tops off the tank at the current station. Each duel spans two
// jumps plus arena manoeuvring, so a bot that is never refuelled slowly
// drains and eventually strands itself in the arena with too little fuel
// to jump back to staging (observed: battle_bot2 hit 0 in ashford). Call
// it in preflight while docked at staging; harmless when already full.
func (b *Bot) Refuel() error { return b.Raw("refuel", nil) }

// WaitReady polls get_ship until shield and hull both read full (or the
// budget runs out), issuing a station "repair" once if hull is short.
// Scenarios that measure regen (S4/S5) or per-volley hull damage (S7)
// need full pools at tick one; hit-table scenarios do not care.
func (b *Bot) WaitReady(maxPolls int) error {
	repaired := false
	for i := range maxPolls {
		if err := b.Raw("get_ship", nil); err != nil {
			return err
		}
		var st struct {
			Ship struct {
				Hull      float64 `json:"hull"`
				MaxHull   float64 `json:"max_hull"`
				Shield    float64 `json:"shield"`
				MaxShield float64 `json:"max_shield"`
			} `json:"ship"`
		}
		if err := json.Unmarshal(b.client.GetRawJSON("ship"), &st); err != nil {
			return fmt.Errorf("%s: parse get_ship: %w", b.agentID, err)
		}
		if st.Ship.Shield >= st.Ship.MaxShield && st.Ship.Hull >= st.Ship.MaxHull {
			return nil
		}
		if st.Ship.Hull < st.Ship.MaxHull && !repaired {
			repaired = true
			if err := b.Raw("repair", nil); err != nil {
				b.logger.Printf("%s: repair: %v", b.agentID, err)
			}
			continue
		}
		if i%10 == 0 {
			b.logger.Printf("%s: waiting for full pools (hull %.0f/%.0f shield %.0f/%.0f)",
				b.agentID, st.Ship.Hull, st.Ship.MaxHull, st.Ship.Shield, st.Ship.MaxShield)
		}
		time.Sleep(game.SleepTick)
	}
	return fmt.Errorf("%s: pools not full after %d polls", b.agentID, maxPolls)
}
