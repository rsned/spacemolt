package main

import (
	"context"
	"os"
	"sync"
	"testing"
)

// TestInterrupter_TriggerCancelsArmedContext verifies that triggering the
// interrupter cancels the context whose cancel func is currently armed — the
// mechanism that lets Ctrl+C abort a running loop/command back to the prompt.
func TestInterrupter_TriggerCancelsArmedContext(t *testing.T) {
	it := &interrupter{}
	ctx, cancel := context.WithCancel(context.Background())
	it.arm(cancel)

	if !it.trigger() {
		t.Fatal("trigger() returned false while a cancel was armed")
	}
	if ctx.Err() == nil {
		t.Fatal("armed context was not cancelled by trigger()")
	}
}

// TestInterrupter_TriggerNoopWhenDisarmed verifies that a trigger with nothing
// armed (the idle prompt, where liner handles Ctrl+C itself) is a safe no-op.
func TestInterrupter_TriggerNoopWhenDisarmed(t *testing.T) {
	it := &interrupter{}
	if it.trigger() {
		t.Fatal("trigger() returned true with nothing armed")
	}

	ctx, cancel := context.WithCancel(context.Background())
	it.arm(cancel)
	it.disarm()
	if it.trigger() {
		t.Fatal("trigger() returned true after disarm()")
	}
	if ctx.Err() != nil {
		t.Fatal("disarmed context should not have been cancelled")
	}
	cancel()
}

// TestInterrupter_WatchCancelsOnSignal verifies the watch loop cancels the
// armed context when a signal arrives on the channel and invokes the notice
// callback exactly when a cancel actually fired.
func TestInterrupter_WatchCancelsOnSignal(t *testing.T) {
	it := &interrupter{}
	ctx, cancel := context.WithCancel(context.Background())
	it.arm(cancel)

	sigCh := make(chan os.Signal, 1)
	var noticed sync.WaitGroup
	noticed.Add(1)
	go it.watch(sigCh, func() { noticed.Done() })

	sigCh <- os.Interrupt
	noticed.Wait() // blocks until the watch loop fired the notice callback

	if ctx.Err() == nil {
		t.Fatal("watch did not cancel the armed context on signal")
	}
	close(sigCh)
}
