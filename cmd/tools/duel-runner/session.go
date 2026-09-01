package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
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

func (b *Bot) Close() { _ = b.client.Close() }

func (b *Bot) Name() string { return b.agentID }

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

// View reads the latest battle state via a fresh get_battle_status query
// (a free info query, not a mutation). It runs once per control-loop
// iteration; the loop's own pacing keeps the poll rate around 1 per 2-4s.
func (b *Bot) View() (BattleView, bool) {
	if err := b.client.RawCommand(b.ctx, "get_battle_status", nil); err != nil {
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
	if b.seenBattle && !st.InBattle {
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
	for _, m := range after.Modules {
		now = append(now, m.TypeID)
	}
	if rem, inst := computeFitActions(now, fit.Modules); len(rem)+len(inst) > 0 {
		return fmt.Errorf("%s: fit verify failed: extra=%v missing=%v", b.agentID, rem, inst)
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
