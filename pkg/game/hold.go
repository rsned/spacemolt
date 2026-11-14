package game

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// ActiveHold is a hold on the ship from a hold_update "started" push, e.g. a
// customs station's warp scrambler for an inspection. While it is set the
// server refuses jump, travel and dock.
type ActiveHold struct {
	HolderID   string
	HolderName string
	Source     string
	StartTick  int64
	Message    string
	// At is the wall-clock time the start push arrived.
	At time.Time
}

// clone copies the hold so State.Clone never shares it.
func (h *ActiveHold) clone() *ActiveHold {
	if h == nil {
		return nil
	}
	c := *h
	return &c
}

// HoldStaleAfter is how long a hold counts without its end push. The fleet
// logs hold 74 "started" against 1 "ended", so an end can be missed; past
// this the hold is treated as over and a move is simply tried (the server
// refuses it if the inspection is really still running).
const HoldStaleAfter = SleepJumpMaxWait

// Held reports whether a hold is active on the ship.
func (s *State) Held() bool { return s.HeldAt(time.Now()) }

// HeldAt is Held as of now: a hold set less than HoldStaleAfter before now.
func (s *State) HeldAt(now time.Time) bool {
	return s.ActiveHold != nil && now.Sub(s.ActiveHold.At) < HoldStaleAfter
}

// holdEndEvents are the hold_update events that close a hold. Only "started"
// has been seen live; these are the plausible closers. Any other event is
// logged and leaves State untouched.
var holdEndEvents = map[string]struct{}{
	"ended": {}, "released": {}, "completed": {}, "cleared": {},
	"cancelled": {}, "canceled": {}, "expired": {}, "broken": {},
}

// isHoldEnd reports whether a hold_update event closes a hold.
func isHoldEnd(ev string) bool {
	_, end := holdEndEvents[ev]
	return end
}

// formatHoldUpdate renders the one-line operational log entry for a push.
// other names the held ship when the hold is not on this player.
func formatHoldUpdate(h serverapi.HoldUpdate, other, styled bool) string {
	what := "hold"
	if h.Source != "" {
		what = h.Source + " hold"
	}
	ev := h.Event
	if ev == "" {
		ev = "update"
	}
	line := alertTag("HOLD", styled) + what + " " + ev
	if other && h.TargetName != "" {
		line += " on " + h.TargetName
	}
	if h.Reason != "" {
		line += " (" + h.Reason + ")"
	}
	if h.HolderName != "" {
		line += " by " + h.HolderName
	}
	if h.Message != "" {
		line += ": " + h.Message
	}
	return line
}

// handleHoldUpdate records a hold_update push on State.ActiveHold (set on
// "started", cleared on an end event) and logs it to the operational log.
func (c *Client) handleHoldUpdate(payload map[string]any) {
	var h serverapi.HoldUpdate
	if data, err := json.Marshal(payload); err == nil {
		_ = json.Unmarshal(data, &h)
	}
	ev := strings.ToLower(h.Event)
	c.mu.Lock()
	// Customs broadcasts holds on every ship at the POI. Only one aimed at
	// this player (or one whose target we cannot tell) is ours.
	me := c.state.Player.ID
	ours := h.TargetID == "" || me == "" || h.TargetID == me
	switch {
	case !ours:
		// Someone else's inspection: log it, leave our state alone.
	case ev == "started":
		c.state.ActiveHold = &ActiveHold{
			HolderID:   h.HolderID,
			HolderName: h.HolderName,
			Source:     h.Source,
			StartTick:  h.StartTick,
			Message:    h.Message,
			At:         time.Now(),
		}
	case isHoldEnd(ev):
		c.state.ActiveHold = nil
	}
	c.mu.Unlock()
	if !ours {
		h.Message = "" // the text says "holds you"; it is not us
	}
	c.logEvent(formatHoldUpdate(h, !ours, stderrIsTTY()))
}
