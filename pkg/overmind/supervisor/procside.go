package supervisor

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

// procScan reads /proc and returns each process's argv as a single
// space-separated string keyed by pid. Unreadable entries are skipped: a process
// that exits mid-scan is not an error, it is the normal case.
func procScan() map[int]string {
	out := map[int]string{}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return out
	}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue // not a process directory
		}
		raw, err := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
		if err != nil {
			continue
		}
		out[pid] = string(bytes.ReplaceAll(bytes.TrimRight(raw, "\x00"), []byte{0}, []byte{' '}))
	}
	return out
}

// WorkerRunning reports whether a worker process for agentID is attached to the
// fleet at socketPath.
//
// The process table is the authority here, not the status file. A status file is
// written on a timer, so a worker that died a second ago still looks alive in it
// and — worse — one that is alive can look stale. Reading /proc asks the only
// question that matters before starting the agent somewhere else: is there still
// a process holding this agent's game session?
func WorkerRunning(agentID, socketPath string) (bool, error) {
	needleAgent := "--agent " + agentID + " "
	needleSock := "--socket " + socketPath
	for _, cmd := range procScan() {
		// Match the worker binary specifically: the reconciler's own argv, or an
		// operator's grep, must never be mistaken for a live worker.
		if !bytes.Contains([]byte(cmd), []byte("worker ")) {
			continue
		}
		if bytes.Contains([]byte(cmd), []byte(needleAgent)) && bytes.Contains([]byte(cmd), []byte(needleSock)) {
			return true, nil
		}
	}
	return false, nil
}

// SignalOvermindReload sends SIGHUP to the overmind process owning socketPath,
// which makes it re-read its fleet yaml and overrides sidecar and start/stop
// workers to match. Returns an error when no such overmind is found, because a
// membership change nobody applied must never be mistaken for one that landed.
func SignalOvermindReload(socketPath string) error {
	for pid, cmd := range procScan() {
		if !bytes.Contains([]byte(cmd), []byte("overmind")) {
			continue
		}
		if !bytes.Contains([]byte(cmd), []byte("--socket "+socketPath)) {
			continue
		}
		if bytes.Contains([]byte(cmd), []byte("--agent ")) {
			continue // a worker attached to this socket, not the overmind
		}
		if err := syscall.Kill(pid, syscall.SIGHUP); err != nil {
			return fmt.Errorf("SIGHUP overmind pid %d: %w", pid, err)
		}
		return nil
	}
	return fmt.Errorf("no overmind found for socket %s", socketPath)
}

// ProcFleetSide builds a FleetSide backed by the live process table and signals.
func ProcFleetSide(name, overridesPath, socketPath string) FleetSide {
	return FleetSide{
		Name:          name,
		OverridesPath: overridesPath,
		Reload:        func() error { return SignalOvermindReload(socketPath) },
		Running:       func(agentID string) (bool, error) { return WorkerRunning(agentID, socketPath) },
	}
}
