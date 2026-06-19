package main

import (
	"context"
	"os"
	"sync"
)

// interrupter routes a SIGINT received while a foreground command is running to
// that command's context cancellation, so Ctrl+C aborts a long-running loop or
// blocking command back to the REPL prompt instead of killing the process.
//
// At the idle prompt liner reads in raw mode and handles Ctrl+C itself (no
// SIGINT is generated), so the interrupter is disarmed there and its trigger is
// a no-op. The REPL arms it with the active command's cancel func around each
// foreground execution and disarms it afterward.
type interrupter struct {
	mu     sync.Mutex
	cancel context.CancelFunc
}

// arm registers cancel as the function to call on the next interrupt. It
// replaces any previously-armed cancel (commands run one at a time under the
// REPL's execMu, so there is only ever one).
func (it *interrupter) arm(cancel context.CancelFunc) {
	it.mu.Lock()
	it.cancel = cancel
	it.mu.Unlock()
}

// disarm clears the armed cancel so a later interrupt is a no-op.
func (it *interrupter) disarm() {
	it.mu.Lock()
	it.cancel = nil
	it.mu.Unlock()
}

// trigger cancels the armed context, if any, and reports whether it did. A
// false return means nothing was armed (idle prompt) and the interrupt should
// be ignored.
func (it *interrupter) trigger() bool {
	it.mu.Lock()
	cancel := it.cancel
	it.mu.Unlock()
	if cancel == nil {
		return false
	}
	cancel()
	return true
}

// watch consumes signals from sigCh and triggers a cancellation for each. When
// a signal actually cancels an armed command, notify is invoked (used to print
// an "interrupting" notice); it is skipped when nothing was armed. watch
// returns when sigCh is closed.
func (it *interrupter) watch(sigCh <-chan os.Signal, notify func()) {
	for range sigCh {
		if it.trigger() && notify != nil {
			notify()
		}
	}
}
