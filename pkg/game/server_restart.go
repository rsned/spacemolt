package game

import "time"

// ServerRestart is an announced server restart from a server_restart_warning
// push (a version deploy). While it is pending, workers hold off starting a
// jump or travel: one started into the restart window hangs for the full
// SleepJumpMaxWait and fails (2026-11-22, v0.733.0: explorer-3 at Adhara,
// craftsman-2 at Wazn).
type ServerRestart struct {
	// At is when the server said it would restart: the warning's arrival plus
	// seconds_until_restart.
	At time.Time
	// Target is the version being deployed, e.g. "0.733.0".
	Target string
}

// clone copies the restart so State.Clone never shares it.
func (r *ServerRestart) clone() *ServerRestart {
	if r == nil {
		return nil
	}
	c := *r
	return &c
}

// defaultRestartLead is the warning lead time assumed when a warning omits
// seconds_until_restart; every warning seen live has said 60s.
const defaultRestartLead = 60 * time.Second

// RestartPending reports whether an announced restart is not over yet: a
// warning has arrived, no reconnect has cleared it since, and it is within
// SleepServerRestartMaxWait of the announced restart time. The cap means a
// restart that was announced and never happened cannot hold a worker forever.
func (s *State) RestartPending(now time.Time) bool {
	return s.ServerRestart != nil && now.Before(s.ServerRestart.At.Add(SleepServerRestartMaxWait))
}

// RestartRecent reports whether a restart is pending or happened within
// SleepServerRestartMaxWait of now. A move that fails while this is true was
// most likely broken by the restart, so it is worth resuming.
func (s *State) RestartRecent(now time.Time) bool {
	if s.RestartPending(now) {
		return true
	}
	return !s.LastRestartAt.IsZero() && now.Before(s.LastRestartAt.Add(SleepServerRestartMaxWait))
}

// recordServerRestart notes an announced restart on State.
func (c *Client) recordServerRestart(lead time.Duration, target string) {
	if lead <= 0 {
		lead = defaultRestartLead
	}
	at := time.Now().Add(lead)
	c.mu.Lock()
	c.state.ServerRestart = &ServerRestart{At: at, Target: target}
	c.state.LastRestartAt = at
	c.mu.Unlock()
}

// clearServerRestart ends a pending restart. A successful reconnect after the
// warning means the restart is over. LastRestartAt is kept for RestartRecent.
func (c *Client) clearServerRestart() {
	c.mu.Lock()
	c.state.ServerRestart = nil
	c.mu.Unlock()
}
