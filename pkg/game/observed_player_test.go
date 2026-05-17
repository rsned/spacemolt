package game

import (
	"sync"
	"testing"
)

func TestSetPlayerObserver_StoresCallback(t *testing.T) {
	c := &Client{}

	var mu sync.Mutex
	var fired bool
	c.SetPlayerObserver(func(_ []ObservedPlayer) {
		mu.Lock()
		fired = true
		mu.Unlock()
	})

	c.playerObserverMu.RLock()
	cb := c.playerObserver
	c.playerObserverMu.RUnlock()

	if cb == nil {
		t.Fatal("playerObserver not registered")
	}
	cb(nil)

	mu.Lock()
	defer mu.Unlock()
	if !fired {
		t.Error("callback not invoked")
	}
}
