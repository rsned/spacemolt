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

// Held reports whether a hold is active on the ship.
func (s *State) Held() bool { return s.ActiveHold != nil }

// holdEndEvents are the hold_update events that close a hold. Only "started"
// has been seen live; these are the plausible closers. Any other event is
// logged and leaves State untouched.
var holdEndEvents = map[string]struct{}{
	"ended": {}, "released": {}, "completed": {}, "cleared": {},
	"cancelled": {}, "canceled": {}, "expired": {}, "broken": {},
}

// formatHoldUpdate renders the one-line operational log entry for a push.
func formatHoldUpdate(h serverapi.HoldUpdate, styled bool) string {
	what := "hold"
	if h.Source != "" {
		what = h.Source + " hold"
	}
	ev := h.Event
	if ev == "" {
		ev = "update"
	}
	line := alertTag("HOLD", styled) + what + " " + ev
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
	if ev == "started" {
		c.state.ActiveHold = &ActiveHold{
			HolderID:   h.HolderID,
			HolderName: h.HolderName,
			Source:     h.Source,
			StartTick:  h.StartTick,
			Message:    h.Message,
			At:         time.Now(),
		}
	} else if _, end := holdEndEvents[ev]; end {
		c.state.ActiveHold = nil
	}
	c.mu.Unlock()
	c.logEvent(formatHoldUpdate(h, stderrIsTTY()))
}
