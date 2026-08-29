package game

import (
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// notifyPlayers builds ObservedPlayer records from a NearbyPlayer slice,
// stamps system/POI/source/time context, and dispatches to the registered
// observer. Silent no-op when no observer is registered.
func (c *Client) notifyPlayers(source string, players []serverapi.NearbyPlayer, poiID string) {
	if len(players) == 0 {
		return
	}
	c.playerObserverMu.RLock()
	cb := c.playerObserver
	c.playerObserverMu.RUnlock()
	if cb == nil {
		return
	}

	systemID := c.currentSystemID()

	now := time.Now().UTC()
	out := make([]ObservedPlayer, 0, len(players))
	for _, p := range players {
		out = append(out, ObservedPlayer{
			PlayerID:       p.PlayerID,
			Username:       p.Username,
			ShipClass:      p.ShipClass,
			FactionID:      p.FactionID,
			FactionTag:     p.FactionTag,
			ClanTag:        p.ClanTag,
			PrimaryColor:   p.PrimaryColor,
			SecondaryColor: p.SecondaryColor,
			StatusMessage:  p.StatusMessage,
			Anonymous:      p.Anonymous,
			InCombat:       p.InCombat,
			SystemID:       systemID,
			POIID:          poiID,
			Source:         source,
			SeenAt:         now,
		})
	}
	cb(out)
}

// notifyCombatSightings emits InCombat=true sightings for the given
// id/username/ship-class triples, stamping the current system and time. It is
// the shared core for the per-event battle adapters below. Faction info lives
// on the side, not the participant, so it is not stamped here.
func (c *Client) notifyCombatSightings(source string, players []combatSighting) {
	if len(players) == 0 {
		return
	}
	c.playerObserverMu.RLock()
	cb := c.playerObserver
	c.playerObserverMu.RUnlock()
	if cb == nil {
		return
	}

	systemID := c.currentSystemID()

	now := time.Now().UTC()
	out := make([]ObservedPlayer, 0, len(players))
	for _, p := range players {
		out = append(out, ObservedPlayer{
			PlayerID:  p.playerID,
			Username:  p.username,
			ShipClass: p.shipClass,
			InCombat:  true,
			SystemID:  systemID,
			Source:    source,
			SeenAt:    now,
		})
	}
	cb(out)
}

// combatSighting is the minimal identity carried across all battle participant
// shapes (combat_update, battle_alert, battle_started, battle_update).
type combatSighting struct {
	playerID  string
	username  string
	shipClass string
}

// notifyPlayersFromBattle adapts BattleParticipant records (combat_update /
// battle_alert) into combat sightings.
func (c *Client) notifyPlayersFromBattle(source string, parts []serverapi.BattleParticipant) {
	players := make([]combatSighting, 0, len(parts))
	for _, p := range parts {
		players = append(players, combatSighting{p.PlayerID, p.Username, p.ShipClass})
	}
	c.notifyCombatSightings(source, players)
}

// notifyPlayersFromBattleStart adapts battle_started participants (integer
// side_id, identity-only) into combat sightings.
func (c *Client) notifyPlayersFromBattleStart(source string, parts []serverapi.BattleAlertParticipant) {
	players := make([]combatSighting, 0, len(parts))
	for _, p := range parts {
		players = append(players, combatSighting{p.PlayerID, p.Username, p.ShipClass})
	}
	c.notifyCombatSightings(source, players)
}

// notifyPlayersFromBattleUpdate adapts battle_update participants (which carry
// live hull/shield percentages) into combat sightings.
func (c *Client) notifyPlayersFromBattleUpdate(source string, parts []serverapi.BattleUpdateParticipant) {
	players := make([]combatSighting, 0, len(parts))
	for _, p := range parts {
		players = append(players, combatSighting{p.PlayerID, p.Username, p.ShipClass})
	}
	c.notifyCombatSightings(source, players)
}

// notifyPlayersFromBattleEnd adapts battle_ended participants (identity + side
// + per-battle tally, no ship_class) into combat sightings. The battle has just
// concluded, but these players were combatants in our system moments ago, so
// they are recorded as in-combat sightings like the other battle adapters.
func (c *Client) notifyPlayersFromBattleEnd(source string, parts []serverapi.BattleEndedParticipant) {
	players := make([]combatSighting, 0, len(parts))
	for _, p := range parts {
		players = append(players, combatSighting{p.PlayerID, p.Username, ""})
	}
	c.notifyCombatSightings(source, players)
}

// notifyPlayerFromChat emits a single identity-only ObservedPlayer for
// the sender of a chat_message push. ShipClass / POIID / SystemID are
// intentionally left empty so the recorder upserts seen_players only and
// skips the sightings table — per the spec's identity-only decision for
// chat. (ChatMessage does carry SystemID/POIID but we deliberately
// ignore them here.)
func (c *Client) notifyPlayerFromChat(msg serverapi.ChatMessage) {
	if msg.SenderID == "" {
		return
	}
	c.playerObserverMu.RLock()
	cb := c.playerObserver
	c.playerObserverMu.RUnlock()
	if cb == nil {
		return
	}

	cb([]ObservedPlayer{{
		PlayerID: msg.SenderID,
		Username: msg.Sender,
		Source:   "chat_message",
		SeenAt:   time.Now().UTC(),
	}})
}

// notifyPlayerFromScan emits a single ObservedPlayer for the target revealed
// by a scan action_result. scan exposes the target's faction_id, username and
// ship_class, so unlike the identity-only chat path this carries full sighting
// context. systemID is passed in by the caller (parseActionResult holds c.mu,
// so this helper must not call currentSystemID, which would re-lock it).
func (c *Client) notifyPlayerFromScan(resp serverapi.ScanResponse, systemID string) {
	if resp.TargetID == "" {
		return
	}
	c.playerObserverMu.RLock()
	cb := c.playerObserver
	c.playerObserverMu.RUnlock()
	if cb == nil {
		return
	}

	cb([]ObservedPlayer{{
		PlayerID:  resp.TargetID,
		Username:  resp.Username,
		ShipClass: resp.ShipClass,
		FactionID: resp.FactionID,
		SystemID:  systemID,
		Source:    "scan",
		SeenAt:    time.Now().UTC(),
	}})
}

// currentSystemID returns the current system identifier from c.state,
// guarded by c.mu. Returns "" if state has not been initialized.
//
// It reads System.ID before CurrentSystem, and slugifies whichever it gets.
// Both are needed. CurrentSystem is documented as, and assigned from, the
// system NAME (see GetCurrentSystem, and `c.state.CurrentSystem = sys.Name`),
// so reading it stored "Bellatrix" and "Alpha Centauri" as system ids -- and
// 5,715 of 6,870 seen_player_sightings rows could not be joined to the systems
// table at all, 83% of our player intel silently invisible to any query that
// wanted a police level or an empire. System.ID is not sufficient on its own
// either: it falls back to the name when the server sends an empty id.
//
// Slugifying is safe and idempotent -- all 505 known system ids are lowercase
// with no spaces or dashes, so a genuine id passes through untouched.
func (c *Client) currentSystemID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.state == nil {
		return ""
	}
	if id := c.state.System.ID; id != "" {
		return slugSystemID(id)
	}
	return slugSystemID(c.state.CurrentSystem)
}

// slugSystemID converts a system display name to the id form the KB joins on:
// "Alpha Centauri" -> "alpha_centauri", "GSC-0002" -> "gsc_0002",
// "Trader's Rest" -> "traders_rest". A value that is already an id is returned
// unchanged.
//
// The apostrophe is DROPPED rather than replaced, unlike the space and dash.
// "Trader's Rest" is the one name in the galaxy that carries one, and its id is
// traders_rest -- so substituting an underscore would produce trader_s_rest and
// keep the row unjoinable.
func slugSystemID(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToLower(strings.NewReplacer(" ", "_", "-", "_", "'", "").Replace(s))
}
