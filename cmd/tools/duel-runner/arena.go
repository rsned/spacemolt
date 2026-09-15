package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// Arena support (server v0.586.0). A match at an arena POI -- the Blood
// Arena in Krynn -- is fought on the normal battle engine but has no
// permanent consequences: a ship at 0 hull is knocked out and restored on
// the spot, with hull, shields, armor, crew casualties and module disables
// all reversed. Ammo, fuel and consumables ARE really spent, so preflight
// still has to visit staging for those.
//
// Two things the arena does NOT do, which is why lawless duels survive
// alongside it (see Campaign.Mode): fleeing forfeits the match instead of
// escaping it, and emergency warp / emergency cloak never trigger. Every
// scenario that MEASURES flee or the emergency modules must stay in
// lawless space.
//
// Wire shapes are transcribed from server_docs/openapi.json (/arena,
// ArenaStatusResponse / ArenaChallengeResponse / ArenaAcceptResponse); the
// field names are the server's, not guesses. Both sides must be undocked
// at the arena POI for the whole challenge/accept handshake.

// arenaChallengeInfo is a challenge pending in either direction.
type arenaChallengeInfo struct {
	ChallengeID  string `json:"challenge_id"`
	OpponentID   string `json:"opponent_id"`
	OpponentName string `json:"opponent_name"`
	POIID        string `json:"poi_id"`
	MaxSideSize  int    `json:"max_side_size"`
	ExpiresTick  int    `json:"expires_tick"`
}

// arenaStatus is the `arena status` reply. Every field except at_arena is
// optional, so pointers mark "absent" for the two challenge slots.
type arenaStatus struct {
	AtArena       bool                `json:"at_arena"`
	BattleID      string              `json:"battle_id"`
	ArenaWins     int                 `json:"arena_wins"`
	ArenaLosses   int                 `json:"arena_losses"`
	ArenaKOs      int                 `json:"arena_knockouts"`
	XPCapPerSkill int                 `json:"xp_cap_per_skill"`
	XPUsedToday   map[string]int      `json:"xp_used_today"`
	Incoming      *arenaChallengeInfo `json:"incoming"`
	Outgoing      *arenaChallengeInfo `json:"outgoing"`
}

// CappedSkills lists the skills that have hit today's arena XP cap, sorted
// for a stable log line. Phase B's measurements assume the duelling bots
// stay at known skill levels; arena fights grant combat XP normally up to
// this cap, so a campaign long enough to cap a skill has almost certainly
// levelled it mid-matrix and the affected runs need re-checking.
func (s arenaStatus) CappedSkills() []string {
	if s.XPCapPerSkill <= 0 {
		return nil
	}
	var out []string
	for skill, used := range s.XPUsedToday {
		if used >= s.XPCapPerSkill {
			out = append(out, skill)
		}
	}
	sort.Strings(out)
	return out
}

// arenaChallenge is the `arena challenge` reply.
type arenaChallenge struct {
	ChallengeID string `json:"challenge_id"`
	TargetID    string `json:"target_id"`
	TargetName  string `json:"target_name"`
	POIID       string `json:"poi_id"`
	MaxSideSize int    `json:"max_side_size"`
	ExpiresTick int    `json:"expires_tick"`
}

// arenaParticipant is one pilot in a started match.
type arenaParticipant struct {
	PlayerID string `json:"player_id"`
	Username string `json:"username"`
	SideID   int    `json:"side_id"`
}

// arenaAccept is the `arena accept` reply. Its battle_id is authoritative:
// in arena mode the runner takes the duel's battle id from here rather
// than scraping it off the first get_battle_status poll.
type arenaAccept struct {
	BattleID     string             `json:"battle_id"`
	YourSide     int                `json:"your_side"`
	OpponentSide int                `json:"opponent_side"`
	Participants []arenaParticipant `json:"participants"`
}

func parseArenaStatus(raw []byte) (arenaStatus, error) {
	var s arenaStatus
	if len(raw) == 0 {
		return s, fmt.Errorf("arena status: empty reply (nothing in the raw cache)")
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return s, fmt.Errorf("arena status: %w", err)
	}
	return s, nil
}

func parseArenaChallenge(raw []byte) (arenaChallenge, error) {
	var c arenaChallenge
	if len(raw) == 0 {
		return c, fmt.Errorf("arena challenge: empty reply (nothing in the raw cache)")
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return c, fmt.Errorf("arena challenge: %w", err)
	}
	if c.ChallengeID == "" {
		return c, fmt.Errorf("arena challenge: reply carries no challenge_id: %s", raw)
	}
	return c, nil
}

func parseArenaAccept(raw []byte) (arenaAccept, error) {
	var a arenaAccept
	if len(raw) == 0 {
		return a, fmt.Errorf("arena accept: empty reply (nothing in the raw cache)")
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return a, fmt.Errorf("arena accept: %w", err)
	}
	if a.BattleID == "" {
		// No battle id means the match did not start (a lapsed or already
		// answered challenge). Driving a battle that isn't there would
		// burn the whole duel budget polling an empty status.
		return a, fmt.Errorf("arena accept: reply carries no battle_id, match did not start: %s", raw)
	}
	return a, nil
}

// --- Bot arena commands ---------------------------------------------------
//
// These go through Bot.Raw and read the reply out of the client's "_last"
// raw-JSON slot, the same path View() uses for get_battle_status: the
// client has no typed arena support, and the calibration harness is its
// only consumer today.

// arenaRaw issues one arena action and returns the settled raw reply.
func (b *Bot) arenaRaw(action string, args map[string]any) ([]byte, error) {
	payload := map[string]any{"action": action}
	for k, v := range args {
		payload[k] = v
	}
	// Drop the previous reply first: without this a failed round-trip
	// leaves the PRIOR arena reply in "_last" and the parser happily
	// reads it as this command's answer.
	b.client.ClearRawJSON("_last")
	if err := b.Raw("arena", payload); err != nil {
		return nil, err
	}
	return b.client.GetRawJSON("_last"), nil
}

// ArenaStatus reads pending challenges, the current arena battle and
// today's arena XP. status is a query, not a mutation -- it is not subject
// to the 1-per-tick mutation limit, so the accept-side poll can run at the
// control loop's own pace.
func (b *Bot) ArenaStatus() (arenaStatus, error) {
	raw, err := b.arenaRaw("status", nil)
	if err != nil {
		return arenaStatus{}, err
	}
	return parseArenaStatus(raw)
}

// ArenaChallenge challenges playerID to a match. maxSide caps ships per
// side; pass 1 for the solo duel every calibration scenario wants (it also
// makes a third-party join impossible, which is what the lawless campaign
// needed its interference-void rule for).
func (b *Bot) ArenaChallenge(playerID string, maxSide int) (arenaChallenge, error) {
	raw, err := b.arenaRaw("challenge", map[string]any{
		"player_id":     playerID,
		"max_side_size": maxSide,
	})
	if err != nil {
		return arenaChallenge{}, err
	}
	return parseArenaChallenge(raw)
}

// ArenaAccept answers the pending incoming challenge. The wire payload
// carries no challenge id -- a player has at most one pending challenge.
func (b *Bot) ArenaAccept() (arenaAccept, error) {
	raw, err := b.arenaRaw("accept", nil)
	if err != nil {
		return arenaAccept{}, err
	}
	return parseArenaAccept(raw)
}

// ArenaCancel withdraws a challenge this bot issued. Used to clear a
// stale outgoing challenge left by a killed run before issuing a new one.
func (b *Bot) ArenaCancel() error {
	_, err := b.arenaRaw("cancel", nil)
	return err
}

// Travel moves to a POI within the current system without docking. The
// arena handshake requires both sides undocked AT the arena POI, which is
// a relic (`blood_arena`), not a station -- so this is travel without the
// dock that Bot.Dock appends.
func (b *Bot) Travel(poi string) error {
	if err := b.Raw("travel", map[string]any{"target_poi": poi}); err != nil {
		return err
	}
	time.Sleep(game.SleepTravel)
	return nil
}
