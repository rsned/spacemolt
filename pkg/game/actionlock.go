package game

import (
	"context"
	"sync"
)

// ctxMutex is a context-aware 1-slot mutex. sync.Mutex doesn't honor
// context, so we wrap a buffered channel.
type ctxMutex struct {
	ch chan struct{}
}

func newCtxMutex() *ctxMutex { return &ctxMutex{ch: make(chan struct{}, 1)} }

// lockCtx blocks until the mutex is acquired or ctx fires. Returns nil
// on success; ctx.Err() if cancelled before acquisition (lock not held).
func (m *ctxMutex) lockCtx(ctx context.Context) error {
	// Fast path: don't race the select if ctx is already cancelled.
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case m.ch <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// unlock releases the mutex. Caller MUST have called lockCtx and received nil first.
func (m *ctxMutex) unlock() { <-m.ch }

// actionLockMap holds one ctxMutex per action name (msg.Type). Maps
// are protected by RWMutex — lookups are common, key creation rare.
type actionLockMap struct {
	mu    sync.RWMutex
	locks map[string]*ctxMutex
}

func newActionLockMap() *actionLockMap {
	return &actionLockMap{locks: make(map[string]*ctxMutex)}
}

// lock acquires the named action's mutex. Returns a release func the
// caller MUST call exactly once.
func (m *actionLockMap) lock(ctx context.Context, action string) (release func(), err error) {
	lk := m.getOrCreate(action)
	if err := lk.lockCtx(ctx); err != nil {
		return nil, err
	}
	return lk.unlock, nil
}

func (m *actionLockMap) getOrCreate(action string) *ctxMutex {
	m.mu.RLock()
	lk, ok := m.locks[action]
	m.mu.RUnlock()
	if ok {
		return lk
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// Re-check under write lock.
	if lk, ok = m.locks[action]; ok {
		return lk
	}
	lk = newCtxMutex()
	m.locks[action] = lk
	return lk
}
