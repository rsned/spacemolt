package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// operatorLease is play_as's operator-presence lease for one agent. While the
// file exists, that agent's displaced worker waits instead of taking the
// session back (game.ReconnectingHandler.SetDisplacedHold). Release is
// idempotent and safe from any goroutine, so every exit path can call it.
type operatorLease struct {
	path string
	once sync.Once
	log  *log.Logger
}

// acquireOperatorLease writes the lease (ISO-8601 UTC timestamp + pid). A write
// failure is logged, not fatal: without the lease the worker simply takes its
// session back after its displaced interval.
func acquireOperatorLease(agentID string, logger *log.Logger) *operatorLease {
	l := &operatorLease{path: game.DisplacedLeasePath(agentID), log: logger}
	content := fmt.Sprintf("%s %d\n", time.Now().UTC().Format(time.RFC3339), os.Getpid())
	if err := os.MkdirAll(filepath.Dir(l.path), 0o750); err != nil {
		logger.Printf("WARN: play_as lease: %v", err)
		return l
	}
	if err := os.WriteFile(l.path, []byte(content), 0o600); err != nil {
		logger.Printf("WARN: play_as lease: %v", err)
	}
	return l
}

// release removes the lease file once.
func (l *operatorLease) release() {
	l.once.Do(func() {
		if err := os.Remove(l.path); err != nil && !os.IsNotExist(err) {
			l.log.Printf("WARN: play_as lease: remove: %v", err)
		}
	})
}

// releaseOnSignal removes the lease and exits on SIGTERM or SIGHUP for the
// life of the process, and on SIGINT until the returned stop is called. The
// REPL owns SIGINT once it starts (it cancels the running command), so main
// calls stop just before entering it; the REPL's own exits return normally and
// run main's deferred release.
func (l *operatorLease) releaseOnSignal() (stop func()) {
	termCh := make(chan os.Signal, 1)
	signal.Notify(termCh, syscall.SIGTERM, syscall.SIGHUP)
	intCh := make(chan os.Signal, 1)
	signal.Notify(intCh, os.Interrupt)
	go func() {
		var sig os.Signal
		select {
		case sig = <-termCh:
		case sig = <-intCh:
		}
		l.release()
		code := 143
		if sig == os.Interrupt {
			code = 130
		}
		os.Exit(code)
	}()
	return func() { signal.Stop(intCh) }
}
