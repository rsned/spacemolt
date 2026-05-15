package game

import (
	"fmt"
	"strings"

	"github.com/coder/websocket"
)

type closeAction int

const (
	// closeReplay: snapshot pending mutations, reconnect, re-send with
	// fresh request_ids. Used for documented graceful server-side
	// closes where the server has no memory of prior in-flight work.
	closeReplay closeAction = iota
	// closeFailFast: deliver ConnectionClosed to all pending handles.
	// Used for abnormal or ambiguous closes where re-sending risks
	// double-execution.
	closeFailFast
)

// String renders the action name so log lines read "action=replay"
// instead of "action=0".
func (a closeAction) String() string {
	switch a {
	case closeReplay:
		return "replay"
	case closeFailFast:
		return "fail-fast"
	default:
		return fmt.Sprintf("closeAction(%d)", int(a))
	}
}

type closeCodePolicy struct {
	code         websocket.StatusCode
	knownReasons []string // empty = any reason matches
	action       closeAction
	note         string
}

// knownCloseCodes captures every close-code/reason combination we
// expect from the server. Unknown combinations log a WARN and fall
// through to the "unknown" default. Updated when the server adds new
// close conditions to api.md.
var knownCloseCodes = []closeCodePolicy{
	{
		code:         websocket.StatusNormalClosure, // 1000
		knownReasons: []string{"max session age", "inactivity", "rolling restart"},
		action:       closeReplay,
		note:         "documented graceful close (server v0.296.1)",
	},
	{code: websocket.StatusGoingAway, action: closeFailFast, note: "going away"},
	{code: websocket.StatusAbnormalClosure, action: closeFailFast, note: "abnormal close (no close frame)"},
	{code: websocket.StatusInternalError, action: closeFailFast, note: "server internal error"},
}

// lookupClosePolicy returns the policy for (code, reason). The second
// return is true only when the combination is fully documented;
// callers should log a WARN when it's false. For code=1000 with an
// undocumented reason, the policy still falls through to replay.
func lookupClosePolicy(code websocket.StatusCode, reason string) (closeCodePolicy, bool) {
	for _, p := range knownCloseCodes {
		if p.code != code {
			continue
		}
		if len(p.knownReasons) == 0 {
			return p, true
		}
		for _, r := range p.knownReasons {
			if strings.HasPrefix(reason, r) {
				return p, true
			}
		}
		// Code known, reason not. Per spec, code=1000 still replays.
		return p, false
	}
	return closeCodePolicy{code: code, action: closeFailFast, note: "unknown close code"}, false
}
