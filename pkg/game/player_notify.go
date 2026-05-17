package game

import (
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

// notifyPlayersFromBattle adapts BattleParticipant records (which lack
// some NearbyPlayer fields and always imply InCombat=true). Faction info
// lives on BattleSide, not the participant, so it is not stamped here.
func (c *Client) notifyPlayersFromBattle(source string, parts []serverapi.BattleParticipant) {
	if len(parts) == 0 {
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
	out := make([]ObservedPlayer, 0, len(parts))
	for _, p := range parts {
		out = append(out, ObservedPlayer{
			PlayerID:  p.PlayerID,
			Username:  p.Username,
			ShipClass: p.ShipClass,
			InCombat:  true,
			SystemID:  systemID,
			Source:    source,
			SeenAt:    now,
		})
	}
	cb(out)
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

// currentSystemID returns the current system identifier from c.state,
// guarded by c.mu. Returns "" if state has not been initialized.
func (c *Client) currentSystemID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.state == nil {
		return ""
	}
	return c.state.CurrentSystem
}
