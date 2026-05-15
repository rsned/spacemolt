# request_id WebSocket Pipeline — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace classifier-based response correlation with `request_id` echo from server v0.296.1, enabling concurrent mutations, removing `CommandQueue` / `waiters` / `mutationMu`, and surfacing close-frame lifecycle handling.

**Architecture:** New `Submit(ctx, msg)` primitive returns a `*RequestHandle` with separate `Ack()` and `Result()` channels. The response router gains a primary `byReqID` map; classifier-based matching survives only as a fallback for untagged frames (server pushes). Concurrency is gated by a soft global cap (16 in-flight) plus a per-action lock keyed by `msg.Type`. `close=1000` triggers transparent replay with fresh UUIDv7 IDs; other close codes fail-fast.

**Tech Stack:** Go 1.24, `github.com/coder/websocket`, `github.com/google/uuid` v1.6.0 (already indirect, promote to direct).

**Spec:** `docs/superpowers/specs/2026-05-15-request-id-websocket-design.md`

---

## File map

**New files:**
- `pkg/game/submit.go` — `Submit`, `RequestHandle`, `Result`, `SubmitOption`, cleanup goroutine
- `pkg/game/submit_test.go` — Submit + handle lifecycle tests
- `pkg/game/inflight.go` — Soft-cap semaphore wrapper
- `pkg/game/inflight_test.go`
- `pkg/game/actionlock.go` — Context-aware per-action mutex map
- `pkg/game/actionlock_test.go`
- `pkg/game/orphan.go` — Orphan-stats counter + rate-limited WARN logger
- `pkg/game/orphan_test.go`
- `pkg/game/close_codes.go` — Close-code policy registry
- `pkg/game/close_codes_test.go`
- `pkg/game/connection_errors.go` — `ConnectionClosed`, `ConnectionLost` error types
- `pkg/game/connection_errors_test.go`
- `pkg/game/concurrency_stress_test.go` — Race + soak tests

**Modified files:**
- `internal/protocol/messages.go` — Add `RequestID` to `Message` and `Response`
- `pkg/game/response_router.go` — Add `byReqID` map; route by ID first
- `pkg/game/response_router_test.go` — Add request_id correlation cases
- `pkg/game/client.go` — Wire close-code handling, replay, orphan dispatch, soft-cap init
- `pkg/version/checker.go` — Bump `BuiltForAPIVersion` to `v0.296.1`
- `go.mod` — Promote `google/uuid` from indirect to direct
- All command method files (`client.go`, `client_commands.go`, `crafting.go`, etc.) — convert `execMutation` / `execQuery` / `waitFor*` calls to `Submit`

**Deleted files (final phase):**
- `pkg/game/client_queue.go`, `pkg/game/client_queue_test.go`
- `pkg/game/QUEUE.md`, `pkg/game/MIGRATION_EXAMPLE.md`
- `pkg/game/response_exec.go` (after all migrations)

---

## Phase 1 — Foundations (no behavior change)

### Task 1: Add RequestID to protocol structs

**Files:**
- Modify: `internal/protocol/messages.go`

- [ ] **Step 1: Add RequestID to Message and Response**

Replace the `Message` and `Response` structs in `internal/protocol/messages.go`:

```go
// Message represents a message sent to the server
type Message struct {
	Type      string         `json:"type"`
	Payload   map[string]any `json:"payload,omitempty"`
	Timestamp int64          `json:"timestamp,omitempty"`
	// RequestID is set by Client.Submit to correlate responses (server
	// v0.296.1+). Echoed by the server on the pending:true ack, terminal
	// action_result, and any error/action_error tied to this request.
	RequestID string `json:"request_id,omitempty"`
}

// Response represents a message received from the server
type Response struct {
	Type    string         `json:"type"`
	Payload map[string]any `json:"payload,omitempty"`
	// RequestID, when non-empty, identifies the client request this
	// response correlates to. Empty on server-initiated pushes
	// (welcome, tick, chat_message, pirate_warning, ...).
	RequestID string `json:"request_id,omitempty"`
}
```

- [ ] **Step 2: Verify build**

Run: `go build ./...`
Expected: clean build, no errors.

- [ ] **Step 3: Verify existing tests still pass**

Run: `go test ./internal/protocol/... ./pkg/game/... -count=1 -short`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/protocol/messages.go
git commit -m "feat(protocol): add RequestID field to Message and Response

Optional field that Submit will stamp on every outgoing frame to
correlate responses (server v0.296.1 echo). No behavior change yet
— no caller stamps it."
```

---

### Task 2: Add ConnectionClosed and ConnectionLost error types

**Files:**
- Create: `pkg/game/connection_errors.go`
- Test: `pkg/game/connection_errors_test.go`

- [ ] **Step 1: Write failing test**

Create `pkg/game/connection_errors_test.go`:

```go
package game

import (
	"errors"
	"testing"

	"github.com/coder/websocket"
)

func TestConnectionClosed_Error(t *testing.T) {
	e := &ConnectionClosed{Code: websocket.StatusGoingAway, Reason: "rolling restart"}
	got := e.Error()
	want := "websocket closed: code=1001 reason=\"rolling restart\""
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestConnectionLost_Error_Unwrap(t *testing.T) {
	root := errors.New("dial tcp: connection refused")
	e := &ConnectionLost{Cause: root}
	if !errors.Is(e, root) {
		t.Errorf("errors.Is should match wrapped cause")
	}
	if e.Error() == "" {
		t.Errorf("Error() must not be empty")
	}
}

func TestConnectionLost_NilCause(t *testing.T) {
	e := &ConnectionLost{}
	if e.Error() == "" {
		t.Errorf("Error() must not be empty when Cause is nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/game/ -run "TestConnectionClosed|TestConnectionLost" -count=1`
Expected: FAIL — `ConnectionClosed` and `ConnectionLost` undefined.

- [ ] **Step 3: Write implementation**

Create `pkg/game/connection_errors.go`:

```go
package game

import (
	"fmt"

	"github.com/coder/websocket"
)

// ConnectionClosed signals that the WebSocket received a close frame
// while a request was outstanding, and the close-code policy (see
// close_codes.go) was fail-fast rather than replay. Callers can
// inspect Code/Reason via errors.As.
type ConnectionClosed struct {
	Code   websocket.StatusCode
	Reason string
}

func (e *ConnectionClosed) Error() string {
	return fmt.Sprintf("websocket closed: code=%d reason=%q", int(e.Code), e.Reason)
}

// ConnectionLost signals that the WebSocket disconnected and the
// client failed to re-establish a working session (network down,
// auth failed during reconnect, etc.). The Cause is the last
// reconnect-attempt error.
type ConnectionLost struct {
	Cause error
}

func (e *ConnectionLost) Error() string {
	if e.Cause == nil {
		return "websocket connection lost"
	}
	return "websocket connection lost: " + e.Cause.Error()
}

func (e *ConnectionLost) Unwrap() error {
	return e.Cause
}
```

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./pkg/game/ -run "TestConnectionClosed|TestConnectionLost" -count=1 -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/game/connection_errors.go pkg/game/connection_errors_test.go
git commit -m "feat(game): add ConnectionClosed and ConnectionLost error types

Returned by Submit when a close frame interrupts a pending request
(non-replay codes) or when reconnect fails after a graceful close."
```

---

### Task 3: Orphan stats + rate-limited logger

**Files:**
- Create: `pkg/game/orphan.go`
- Test: `pkg/game/orphan_test.go`

- [ ] **Step 1: Write failing tests**

Create `pkg/game/orphan_test.go`:

```go
package game

import (
	"sync"
	"testing"
	"time"
)

func TestOrphanStats_Record(t *testing.T) {
	o := newOrphanStats()
	o.record("req-1", "action_result")
	o.record("req-2", "error")

	if got := o.Count(); got != 2 {
		t.Errorf("Count = %d, want 2", got)
	}
	if id, _, _ := o.Last(); id != "req-2" {
		t.Errorf("LastID = %q, want req-2", id)
	}
}

func TestOrphanStats_RecordConcurrent(t *testing.T) {
	o := newOrphanStats()
	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			o.record("id", "type")
		}()
	}
	wg.Wait()
	if got := o.Count(); got != 1000 {
		t.Errorf("Count = %d, want 1000", got)
	}
}

func TestOrphanStats_LogRateLimit(t *testing.T) {
	o := newOrphanStats()
	o.logInterval = 100 * time.Millisecond
	var mu sync.Mutex
	logged := 0
	o.logFunc = func(format string, args ...any) {
		mu.Lock()
		logged++
		mu.Unlock()
	}
	// 100 events in ~10ms must produce at most 1 log line.
	for i := 0; i < 100; i++ {
		o.record("id", "type")
	}
	mu.Lock()
	got := logged
	mu.Unlock()
	if got > 1 {
		t.Errorf("logged = %d, want <= 1 in burst", got)
	}
}
```

- [ ] **Step 2: Run tests to verify fail**

Run: `go test ./pkg/game/ -run TestOrphanStats -count=1`
Expected: FAIL — `newOrphanStats` undefined.

- [ ] **Step 3: Write implementation**

Create `pkg/game/orphan.go`:

```go
package game

import (
	"log"
	"sync"
	"sync/atomic"
	"time"
)

// orphanStats tracks responses that arrived tagged with a request_id
// the router did not recognize. See spec: "Orphan handling".
type orphanStats struct {
	count      atomic.Int64
	mu         sync.Mutex
	lastID     string
	lastType   string
	lastSeen   time.Time
	lastLogged time.Time

	// logInterval is the minimum gap between WARN log lines. Defaults
	// to 1s; tests override.
	logInterval time.Duration
	// logFunc is overridable for tests. Defaults to log.Printf.
	logFunc func(string, ...any)
}

func newOrphanStats() *orphanStats {
	return &orphanStats{
		logInterval: time.Second,
		logFunc:     log.Printf,
	}
}

// record bumps the counter, stores the most-recent ID/type, and emits
// a rate-limited WARN line.
func (o *orphanStats) record(requestID, respType string) {
	o.count.Add(1)
	o.mu.Lock()
	o.lastID = requestID
	o.lastType = respType
	o.lastSeen = time.Now()
	shouldLog := o.lastLogged.IsZero() || time.Since(o.lastLogged) >= o.logInterval
	if shouldLog {
		o.lastLogged = time.Now()
	}
	o.mu.Unlock()

	if shouldLog {
		o.logFunc("WARN: orphan response request_id=%s type=%s", requestID, respType)
	}
}

// Count returns the cumulative orphan count.
func (o *orphanStats) Count() int64 { return o.count.Load() }

// Last returns the most-recent orphan's ID, type, and timestamp. Used by
// Client.DiagnosticStats.
func (o *orphanStats) Last() (id, respType string, at time.Time) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.lastID, o.lastType, o.lastSeen
}
```

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./pkg/game/ -run TestOrphanStats -count=1 -race -v`
Expected: PASS, no race.

- [ ] **Step 5: Commit**

```bash
git add pkg/game/orphan.go pkg/game/orphan_test.go
git commit -m "feat(game): orphan-response stats with rate-limited WARN

Counts responses tagged with unknown request_ids and logs the first
in each 1-second window. Surfaced via Client.DiagnosticStats."
```

---

### Task 4: Soft-cap semaphore

**Files:**
- Create: `pkg/game/inflight.go`
- Test: `pkg/game/inflight_test.go`

- [ ] **Step 1: Write failing tests**

Create `pkg/game/inflight_test.go`:

```go
package game

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestInflight_AcquireRelease(t *testing.T) {
	s := newInflight(2)
	ctx := context.Background()
	if err := s.Acquire(ctx); err != nil {
		t.Fatalf("Acquire 1: %v", err)
	}
	if err := s.Acquire(ctx); err != nil {
		t.Fatalf("Acquire 2: %v", err)
	}
	s.Release()
	if err := s.Acquire(ctx); err != nil {
		t.Fatalf("Acquire after Release: %v", err)
	}
}

func TestInflight_BlocksAtCap(t *testing.T) {
	s := newInflight(1)
	ctx := context.Background()
	if err := s.Acquire(ctx); err != nil {
		t.Fatalf("Acquire 1: %v", err)
	}

	ctx2, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := s.Acquire(ctx2); err == nil {
		t.Fatal("expected Acquire to fail (ctx expired)")
	}
}

func TestInflight_CapInvariant(t *testing.T) {
	const cap = 16
	s := newInflight(cap)

	var current atomic.Int32
	var peak atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.Acquire(context.Background()); err != nil {
				t.Errorf("Acquire: %v", err)
				return
			}
			defer s.Release()
			n := current.Add(1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			time.Sleep(time.Millisecond)
			current.Add(-1)
		}()
	}
	wg.Wait()
	if got := peak.Load(); got > cap {
		t.Errorf("peak concurrent = %d, want <= %d", got, cap)
	}
}

func TestInflight_NoLeakOnCancel(t *testing.T) {
	s := newInflight(2)
	for i := 0; i < 1000; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // pre-cancel
		_ = s.Acquire(ctx)
	}
	// All cancelled acquires should have returned an error without
	// consuming a slot. Cap should still be 2 free.
	ctx := context.Background()
	if err := s.Acquire(ctx); err != nil {
		t.Errorf("Acquire after cancel storm: %v", err)
	}
	if err := s.Acquire(ctx); err != nil {
		t.Errorf("Acquire 2 after cancel storm: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify fail**

Run: `go test ./pkg/game/ -run TestInflight -count=1`
Expected: FAIL — `newInflight` undefined.

- [ ] **Step 3: Write implementation**

Create `pkg/game/inflight.go`:

```go
package game

import "context"

// inflight is a counting semaphore implemented over a buffered channel.
// Used by Submit to cap the number of outstanding mutations. Acquire
// honors context cancellation and never consumes a slot on failure.
type inflight struct {
	slots chan struct{}
}

func newInflight(capacity int) *inflight {
	return &inflight{slots: make(chan struct{}, capacity)}
}

// Acquire takes a slot. Returns ctx.Err() if ctx fires first, in which
// case no slot is consumed.
func (s *inflight) Acquire(ctx context.Context) error {
	select {
	case s.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Release returns a slot. Caller MUST balance every successful Acquire
// with exactly one Release.
func (s *inflight) Release() {
	<-s.slots
}

// InFlight returns the current number of held slots (advisory; may be
// stale by the time the caller reads it).
func (s *inflight) InFlight() int { return len(s.slots) }

// Cap returns the configured capacity.
func (s *inflight) Cap() int { return cap(s.slots) }
```

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./pkg/game/ -run TestInflight -count=1 -race -v`
Expected: PASS, no race.

- [ ] **Step 5: Commit**

```bash
git add pkg/game/inflight.go pkg/game/inflight_test.go
git commit -m "feat(game): soft-cap semaphore for in-flight Submit mutations

Buffered-channel counting semaphore. Acquire respects context
cancellation and does not consume a slot on failure."
```

---

### Task 5: Context-aware per-action lock

**Files:**
- Create: `pkg/game/actionlock.go`
- Test: `pkg/game/actionlock_test.go`

- [ ] **Step 1: Write failing tests**

Create `pkg/game/actionlock_test.go`:

```go
package game

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestActionLock_DistinctActionsDoNotBlock(t *testing.T) {
	m := newActionLockMap()
	ctx := context.Background()

	rel1, err := m.lock(ctx, "mine")
	if err != nil {
		t.Fatalf("lock mine: %v", err)
	}
	rel2, err := m.lock(ctx, "travel")
	if err != nil {
		t.Fatalf("lock travel must not block: %v", err)
	}
	rel1()
	rel2()
}

func TestActionLock_SameActionSerializes(t *testing.T) {
	m := newActionLockMap()

	rel, err := m.lock(context.Background(), "mine")
	if err != nil {
		t.Fatalf("lock 1: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := m.lock(ctx, "mine"); err == nil {
		t.Fatal("expected second lock(mine) to block until ctx expires")
	}
	rel()
}

func TestActionLock_FIFOConcurrent(t *testing.T) {
	m := newActionLockMap()

	var counter atomic.Int32
	var wg sync.WaitGroup
	const N = 100
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rel, err := m.lock(context.Background(), "mine")
			if err != nil {
				t.Errorf("lock: %v", err)
				return
			}
			defer rel()
			// Critical section: counter must be incremented atomically
			// with no parallel entrants.
			before := counter.Load()
			time.Sleep(time.Microsecond)
			after := counter.Load()
			if before != after {
				t.Errorf("interleaving detected: before=%d after=%d", before, after)
			}
			counter.Add(1)
		}()
	}
	wg.Wait()
	if got := counter.Load(); got != N {
		t.Errorf("counter = %d, want %d", got, N)
	}
}

func TestActionLock_LazyCreationConcurrent(t *testing.T) {
	m := newActionLockMap()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			action := "action-" + string(rune('A'+i%26))
			rel, err := m.lock(context.Background(), action)
			if err != nil {
				t.Errorf("lock: %v", err)
				return
			}
			rel()
		}()
	}
	wg.Wait()
}
```

- [ ] **Step 2: Run tests to verify fail**

Run: `go test ./pkg/game/ -run TestActionLock -count=1`
Expected: FAIL — `newActionLockMap` undefined.

- [ ] **Step 3: Write implementation**

Create `pkg/game/actionlock.go`:

```go
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
	select {
	case m.ch <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

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
```

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./pkg/game/ -run TestActionLock -count=1 -race -v`
Expected: PASS, no race.

- [ ] **Step 5: Commit**

```bash
git add pkg/game/actionlock.go pkg/game/actionlock_test.go
git commit -m "feat(game): context-aware per-action lock map

Used by Submit to serialize same-typed mutations (two 'mine' calls
queue) while allowing distinct actions to proceed concurrently."
```

---

### Task 6: Close-code policy registry

**Files:**
- Create: `pkg/game/close_codes.go`
- Test: `pkg/game/close_codes_test.go`

- [ ] **Step 1: Write failing tests**

Create `pkg/game/close_codes_test.go`:

```go
package game

import (
	"testing"

	"github.com/coder/websocket"
)

func TestClosePolicy_Known1000_DocumentedReason(t *testing.T) {
	p, known := lookupClosePolicy(websocket.StatusNormalClosure, "max session age")
	if !known {
		t.Fatal("expected known=true for documented close=1000")
	}
	if p.action != closeReplay {
		t.Errorf("action = %v, want closeReplay", p.action)
	}
}

func TestClosePolicy_Known1000_UndocumentedReason(t *testing.T) {
	p, known := lookupClosePolicy(websocket.StatusNormalClosure, "region migration")
	if known {
		t.Fatal("expected known=false for undocumented reason on 1000")
	}
	// Even unknown reason on 1000 still defaults to replay (per spec).
	if p.action != closeReplay {
		t.Errorf("action = %v, want closeReplay fallback", p.action)
	}
}

func TestClosePolicy_KnownFailFast(t *testing.T) {
	cases := []websocket.StatusCode{
		websocket.StatusGoingAway,
		websocket.StatusAbnormalClosure,
		websocket.StatusInternalError,
	}
	for _, code := range cases {
		p, known := lookupClosePolicy(code, "")
		if !known {
			t.Errorf("code=%d should be known", code)
		}
		if p.action != closeFailFast {
			t.Errorf("code=%d action = %v, want closeFailFast", code, p.action)
		}
	}
}

func TestClosePolicy_Unknown(t *testing.T) {
	p, known := lookupClosePolicy(websocket.StatusCode(4999), "wat")
	if known {
		t.Fatal("expected known=false for code=4999")
	}
	if p.action != closeFailFast {
		t.Errorf("unknown code action = %v, want closeFailFast", p.action)
	}
}
```

- [ ] **Step 2: Run tests to verify fail**

Run: `go test ./pkg/game/ -run TestClosePolicy -count=1`
Expected: FAIL — `lookupClosePolicy` undefined.

- [ ] **Step 3: Write implementation**

Create `pkg/game/close_codes.go`:

```go
package game

import (
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
```

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./pkg/game/ -run TestClosePolicy -count=1 -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/game/close_codes.go pkg/game/close_codes_test.go
git commit -m "feat(game): close-code policy registry

Documents server v0.296.1 graceful-close conditions and assigns
replay vs fail-fast actions. Unknown codes default to fail-fast."
```

---

## Phase 2 — Router & Submit primitive

### Task 7: Add byReqID to responseRouter

**Files:**
- Modify: `pkg/game/response_router.go`
- Modify: `pkg/game/response_router_test.go`

- [ ] **Step 1: Write failing tests**

Add to `pkg/game/response_router_test.go`:

```go
func TestRouter_Dispatch_ByRequestID_Hit(t *testing.T) {
	r := newResponseRouter()
	ch := make(chan protocol.Response, 1)
	r.registerByID("req-7", ch, nil)

	r.dispatch(protocol.Response{Type: "ok", RequestID: "req-7"})

	select {
	case resp := <-ch:
		if resp.RequestID != "req-7" {
			t.Errorf("got RequestID=%q, want req-7", resp.RequestID)
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatal("response not delivered")
	}
}

func TestRouter_Dispatch_ByRequestID_OrphanCounted(t *testing.T) {
	r := newResponseRouter()
	orphans := r.orphans()

	r.dispatch(protocol.Response{Type: "ok", RequestID: "no-such-id"})

	if got := orphans.Count(); got != 1 {
		t.Errorf("orphan count = %d, want 1", got)
	}
}

func TestRouter_Dispatch_UntaggedFallsThroughToPush(t *testing.T) {
	r := newResponseRouter()
	got := make(chan string, 1)
	r.registerPush(matchType(protocol.TypeChatMessage), func(resp protocol.Response) {
		got <- "fired"
	})
	r.dispatch(protocol.Response{Type: protocol.TypeChatMessage}) // no request_id

	select {
	case <-got:
	case <-time.After(50 * time.Millisecond):
		t.Fatal("push handler did not fire")
	}
}

func TestRouter_Dispatch_TaggedDoesNotFallThrough(t *testing.T) {
	r := newResponseRouter()
	got := make(chan string, 1)
	r.registerPush(matchType(protocol.TypeChatMessage), func(resp protocol.Response) {
		got <- "fired"
	})
	// Tagged + unknown ID -> orphan, must NOT fall through to push.
	r.dispatch(protocol.Response{Type: protocol.TypeChatMessage, RequestID: "tagged-but-unknown"})

	select {
	case <-got:
		t.Fatal("push handler must not fire for tagged-but-unknown frame")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestRouter_Dispatch_ByRequestID_TerminatorIntermediate(t *testing.T) {
	r := newResponseRouter()
	ch := make(chan protocol.Response, 1)
	term := func(resp protocol.Response) (bool, error) {
		return resp.Type == protocol.TypeActionResult, nil
	}
	r.registerByID("req-9", ch, term)

	// Intermediate pending ack: must NOT close out, must NOT deliver to ch.
	r.dispatch(protocol.Response{Type: protocol.TypeOK, RequestID: "req-9",
		Payload: map[string]any{"pending": true}})
	select {
	case <-ch:
		t.Fatal("intermediate must not deliver to result chan")
	case <-time.After(20 * time.Millisecond):
	}

	// Terminal frame: must deliver and unregister.
	r.dispatch(protocol.Response{Type: protocol.TypeActionResult, RequestID: "req-9"})
	select {
	case <-ch:
	case <-time.After(50 * time.Millisecond):
		t.Fatal("terminal not delivered")
	}
	if r.subCount() != 0 {
		t.Errorf("subscription not cleaned up: subCount=%d", r.subCount())
	}
}
```

Note: `registerByID` and `orphans` are new APIs introduced in this task.

- [ ] **Step 2: Run tests to verify fail**

Run: `go test ./pkg/game/ -run TestRouter_Dispatch_ByRequestID -count=1`
Expected: FAIL — undefined methods.

- [ ] **Step 3: Modify responseRouter struct and dispatch**

Replace the contents of `pkg/game/response_router.go`:

```go
package game

import (
	"log"
	"sync"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
)

// subscription is a single registration with the response router.
// idSub is set for request_id-correlated entries; legacy classifier
// entries use match/terminate/respCh/handler.
type subscription struct {
	// Request-ID correlation (preferred). When id != "", match,
	// terminate, and registered are unused. The router routes via
	// byReqID before scanning subs.
	id        string

	match      Classifier
	terminate  Terminator
	respCh     chan protocol.Response // one-shot result delivery
	ackCh      chan protocol.Response // optional: pending ack delivery (id subs only)
	handler    func(protocol.Response)
	registered time.Time
}

// responseRouter dispatches incoming responses to registered subscribers.
// It has no WebSocket awareness — callers feed it responses via dispatch.
type responseRouter struct {
	mu      sync.Mutex
	byReqID map[string]*subscription
	subs    []*subscription
	orphan  *orphanStats
}

func newResponseRouter() *responseRouter {
	return &responseRouter{
		byReqID: make(map[string]*subscription),
		orphan:  newOrphanStats(),
	}
}

// orphans returns the orphan-stats counter (for diagnostics + tests).
func (r *responseRouter) orphans() *orphanStats { return r.orphan }

// registerByID registers a subscription keyed by request_id. If
// terminate is nil the next matching frame is treated as terminal. If
// terminate is non-nil, the router delivers only when terminate
// reports done=true; intermediate frames pass through to ackCh (if
// caller set it via the returned subscription).
func (r *responseRouter) registerByID(id string, ch chan protocol.Response, terminate Terminator) *subscription {
	r.mu.Lock()
	defer r.mu.Unlock()
	sub := &subscription{
		id:         id,
		respCh:     ch,
		terminate:  terminate,
		registered: time.Now(),
	}
	r.byReqID[id] = sub
	return sub
}

// setAckChannel attaches an ack channel to an existing id-subscription.
// Must be called before any frames for this id can arrive.
func (r *responseRouter) setAckChannel(sub *subscription, ackCh chan protocol.Response) {
	r.mu.Lock()
	defer r.mu.Unlock()
	sub.ackCh = ackCh
}

// registerQuery adds a one-shot classifier-based query subscription.
// Used only for untagged frames (fallback path).
func (r *responseRouter) registerQuery(match Classifier, ch chan protocol.Response) *subscription {
	return r.register(&subscription{
		match:      match,
		respCh:     ch,
		registered: time.Now(),
	})
}

// registerMutation adds a one-shot classifier-based mutation subscription.
// Used only for untagged frames (fallback path).
func (r *responseRouter) registerMutation(match Classifier, term Terminator, ch chan protocol.Response) *subscription {
	return r.register(&subscription{
		match:      match,
		terminate:  term,
		respCh:     ch,
		registered: time.Now(),
	})
}

// registerPush adds a long-lived push subscription.
func (r *responseRouter) registerPush(match Classifier, handler func(protocol.Response)) *subscription {
	return r.register(&subscription{
		match:      match,
		handler:    handler,
		registered: time.Now(),
	})
}

func (r *responseRouter) register(sub *subscription) *subscription {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.subs = append(r.subs, sub)
	return sub
}

// unregister removes sub from the router. No-op if sub was never
// registered or already removed.
func (r *responseRouter) unregister(sub *subscription) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if sub.id != "" {
		if r.byReqID[sub.id] == sub {
			delete(r.byReqID, sub.id)
		}
		return
	}
	for i, s := range r.subs {
		if s == sub {
			copy(r.subs[i:], r.subs[i+1:])
			r.subs[len(r.subs)-1] = nil
			r.subs = r.subs[:len(r.subs)-1]
			return
		}
	}
}

// snapshotByID returns all live id-keyed subscriptions. Used by the
// replay path on close=1000.
func (r *responseRouter) snapshotByID() []*subscription {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*subscription, 0, len(r.byReqID))
	for _, s := range r.byReqID {
		out = append(out, s)
	}
	return out
}

// rekey changes an id-subscription's request_id. Used by the replay
// path when re-sending under a fresh UUID.
func (r *responseRouter) rekey(sub *subscription, newID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byReqID[sub.id] == sub {
		delete(r.byReqID, sub.id)
	}
	sub.id = newID
	r.byReqID[newID] = sub
}

// subCount returns the number of live subscriptions (for tests). Counts
// both byReqID and legacy subs.
func (r *responseRouter) subCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.subs) + len(r.byReqID)
}

// dispatch routes a single response. Order:
//  1. resp.RequestID != "": look up byReqID. Hit = deliver (with
//     terminator gate for intermediates). Miss = orphan and DROP.
//     Tagged frames never fall through.
//  2. resp.RequestID == "": fan out to matching push handlers, then
//     deliver to the earliest matching classifier subscription.
func (r *responseRouter) dispatch(resp protocol.Response) {
	if resp.RequestID != "" {
		r.dispatchByID(resp)
		return
	}
	r.dispatchUntagged(resp)
}

func (r *responseRouter) dispatchByID(resp protocol.Response) {
	r.mu.Lock()
	sub, ok := r.byReqID[resp.RequestID]
	r.mu.Unlock()

	if !ok {
		r.orphan.record(resp.RequestID, resp.Type)
		return
	}

	// Terminator gate: intermediate frames go to ackCh, terminals to respCh.
	if sub.terminate != nil {
		done, _ := r.safeRunTerminator(sub, resp)
		if !done {
			// Intermediate (e.g. pending:true ack). Deliver to ackCh if set.
			if sub.ackCh != nil {
				select {
				case sub.ackCh <- resp:
				default:
					log.Printf("response router: dropped ack (id=%s, full ackCh)", resp.RequestID)
				}
			}
			return
		}
	}

	// Terminal: deliver and unregister.
	select {
	case sub.respCh <- resp:
	default:
		log.Printf("response router: dropped terminal (id=%s, full respCh)", resp.RequestID)
	}
	r.unregister(sub)
}

func (r *responseRouter) dispatchUntagged(resp protocol.Response) {
	r.mu.Lock()
	snapshot := make([]*subscription, len(r.subs))
	copy(snapshot, r.subs)
	r.mu.Unlock()

	// 1. Push fan-out
	for _, s := range snapshot {
		if s.handler != nil && s.match(resp) {
			r.safeFireHandler(s, resp)
		}
	}

	// 2. Mutation classifier
	for _, s := range snapshot {
		if s.terminate == nil || s.respCh == nil {
			continue
		}
		if !s.match(resp) {
			continue
		}
		done, _ := r.safeRunTerminator(s, resp)
		if !done {
			return
		}
		select {
		case s.respCh <- resp:
		default:
			log.Printf("response router: dropped mutation terminal (type=%s)", resp.Type)
		}
		r.unregister(s)
		return
	}

	// 3. Query FIFO
	var winner *subscription
	for _, s := range snapshot {
		if s.handler != nil || s.terminate != nil || s.respCh == nil {
			continue
		}
		if !s.match(resp) {
			continue
		}
		if winner == nil || s.registered.Before(winner.registered) {
			winner = s
		}
	}
	if winner != nil {
		select {
		case winner.respCh <- resp:
		default:
			log.Printf("response router: dropped query reply (type=%s)", resp.Type)
		}
		r.unregister(winner)
	}
}

func (r *responseRouter) safeFireHandler(s *subscription, resp protocol.Response) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("response router: push handler panic (type=%s): %v", resp.Type, rec)
		}
	}()
	s.handler(resp)
}

func (r *responseRouter) safeRunTerminator(s *subscription, resp protocol.Response) (done bool, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("response router: terminator panic (type=%s): %v", resp.Type, rec)
			done = false
		}
	}()
	return s.terminate(resp)
}
```

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./pkg/game/ -run "TestRouter" -count=1 -race -v`
Expected: PASS (all existing router tests + new ones).

- [ ] **Step 5: Commit**

```bash
git add pkg/game/response_router.go pkg/game/response_router_test.go
git commit -m "feat(game): router routes by request_id with classifier fallback

Adds byReqID map and dispatchByID path. Tagged frames with unknown
IDs are orphaned (counted + logged), not delivered to classifier
subscribers. Untagged frames flow through the existing fan-out path."
```

---

### Task 8: Submit + RequestHandle (happy path)

**Files:**
- Create: `pkg/game/submit.go`
- Test: `pkg/game/submit_test.go`
- Modify: `pkg/game/client.go` (initialize new fields)
- Modify: `go.mod` (promote google/uuid to direct)

- [ ] **Step 1: Write failing tests**

Create `pkg/game/submit_test.go`:

```go
package game

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
)

// newTestClient returns a Client with stubbed connection so Submit can
// run without a real WebSocket. sendCh receives every Send call.
func newTestClient(t *testing.T) (*Client, chan protocol.Message) {
	t.Helper()
	c := newClientSkeleton()
	sendCh := make(chan protocol.Message, 16)
	c.sendOverride = func(ctx context.Context, msg protocol.Message) error {
		sendCh <- msg
		return nil
	}
	return c, sendCh
}

func TestSubmit_AckOnly_QueryReturnsTerminal(t *testing.T) {
	c, sendCh := newTestClient(t)
	ctx := context.Background()

	h, err := c.Submit(ctx, protocol.Message{Type: "get_status"}, WithAckOnly())
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	sent := <-sendCh
	if sent.RequestID == "" {
		t.Fatal("sent message missing RequestID")
	}
	if h.ID != sent.RequestID {
		t.Errorf("handle.ID = %q, want %q", h.ID, sent.RequestID)
	}

	go c.router.dispatch(protocol.Response{
		Type: protocol.TypeOK, RequestID: sent.RequestID,
		Payload: map[string]any{"status": "ok"},
	})

	resp, err := h.Result(ctx)
	if err != nil {
		t.Fatalf("Result: %v", err)
	}
	if resp.RequestID != sent.RequestID {
		t.Errorf("resp.RequestID = %q, want %q", resp.RequestID, sent.RequestID)
	}
}

func TestSubmit_Mutation_PendingThenTerminal(t *testing.T) {
	c, sendCh := newTestClient(t)
	ctx := context.Background()

	h, err := c.Submit(ctx,
		protocol.Message{Type: "mine"},
		WithTerminator(terminateOnAction),
	)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	sent := <-sendCh

	go func() {
		c.router.dispatch(protocol.Response{
			Type: protocol.TypeOK, RequestID: sent.RequestID,
			Payload: map[string]any{"pending": true, "command": "mine"},
		})
		time.Sleep(5 * time.Millisecond)
		c.router.dispatch(protocol.Response{
			Type: protocol.TypeActionResult, RequestID: sent.RequestID,
			Payload: map[string]any{"command": "mine", "yield": 3},
		})
	}()

	ack, err := h.Ack(ctx)
	if err != nil {
		t.Fatalf("Ack: %v", err)
	}
	if p, _ := ack.Payload["pending"].(bool); !p {
		t.Errorf("expected pending=true on ack")
	}

	resp, err := h.Result(ctx)
	if err != nil {
		t.Fatalf("Result: %v", err)
	}
	if resp.Type != protocol.TypeActionResult {
		t.Errorf("Result type = %q, want action_result", resp.Type)
	}
}

func TestSubmit_ServerError(t *testing.T) {
	c, sendCh := newTestClient(t)
	ctx := context.Background()

	h, err := c.Submit(ctx, protocol.Message{Type: "buy"},
		WithTerminator(terminateOnAction))
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	sent := <-sendCh

	go c.router.dispatch(protocol.Response{
		Type: protocol.TypeError, RequestID: sent.RequestID,
		Payload: map[string]any{"code": "insufficient_credits", "message": "not enough credits"},
	})

	_, err = h.Result(ctx)
	if err == nil {
		t.Fatal("expected ServerError, got nil")
	}
	var se *ServerError
	if !errors.As(err, &se) {
		t.Fatalf("err type = %T, want *ServerError", err)
	}
	if se.Code != "insufficient_credits" {
		t.Errorf("se.Code = %q, want insufficient_credits", se.Code)
	}
}

func TestSubmit_ResultCalledTwiceReturnsSameValue(t *testing.T) {
	c, sendCh := newTestClient(t)
	ctx := context.Background()

	h, err := c.Submit(ctx, protocol.Message{Type: "get_status"}, WithAckOnly())
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	sent := <-sendCh

	go c.router.dispatch(protocol.Response{
		Type: protocol.TypeOK, RequestID: sent.RequestID,
		Payload: map[string]any{"status": "ok"},
	})

	r1, err1 := h.Result(ctx)
	r2, err2 := h.Result(ctx)
	if err1 != nil || err2 != nil {
		t.Fatalf("err1=%v err2=%v", err1, err2)
	}
	if r1.RequestID != r2.RequestID {
		t.Errorf("Result/Result mismatch: %q vs %q", r1.RequestID, r2.RequestID)
	}
}

func TestSubmit_CtxCancelReleasesResources(t *testing.T) {
	c, sendCh := newTestClient(t)
	ctx, cancel := context.WithCancel(context.Background())

	h, err := c.Submit(ctx, protocol.Message{Type: "mine"},
		WithTerminator(terminateOnAction))
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	<-sendCh

	cancel()
	_, err = h.Result(context.Background())
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}

	// Resources released: cap should drain to 0.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if c.inflight.InFlight() == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Errorf("inflight slot not released, count=%d", c.inflight.InFlight())
}

func TestSubmit_UsesUUIDv7(t *testing.T) {
	c, sendCh := newTestClient(t)
	ctx := context.Background()

	_, err := c.Submit(ctx, protocol.Message{Type: "get_status"}, WithAckOnly())
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	sent := <-sendCh
	// UUIDv7 string is 36 chars with version "7" at position 14.
	if len(sent.RequestID) != 36 || sent.RequestID[14] != '7' {
		t.Errorf("RequestID = %q, want UUIDv7 (36 chars, version 7)", sent.RequestID)
	}
}

// newClientSkeleton returns a minimal Client suitable for Submit unit
// tests — no real WebSocket, no listener goroutine. Mirrors the
// constructor's field init for the bits Submit/router touch.
func newClientSkeleton() *Client {
	c := &Client{
		debugLogger: testLogger(),
	}
	c.router = newResponseRouter()
	c.inflight = newInflight(16)
	c.actionLocks = newActionLockMap()
	c.connected = true
	return c
}

func testLogger() *log.Logger {
	return log.New(io.Discard, "", 0)
}

// Avoid unused-imports churn in the skeleton: stitch the log and io
// imports onto the test file when this helper is introduced.
var _ = strings.Builder{}
var _ sync.Mutex
```

Note: the helper above references `log` and `io`; ensure your test file imports `log` and `io`.

- [ ] **Step 2: Run tests to verify fail**

Run: `go test ./pkg/game/ -run TestSubmit -count=1`
Expected: FAIL — `Submit`, `RequestHandle`, etc. undefined.

- [ ] **Step 3: Promote google/uuid to direct dependency**

Run: `go get github.com/google/uuid@v1.6.0`
Expected: `go.mod` now lists `github.com/google/uuid v1.6.0` without `// indirect`.

- [ ] **Step 4: Write Submit implementation**

Create `pkg/game/submit.go`:

```go
package game

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rsned/spacemolt/internal/protocol"
)

// Result wraps the terminal response or terminal error from a Submit.
type Result struct {
	Response protocol.Response
	Err      error
}

// RequestHandle is returned by Submit. Callers read Ack() for the
// pending:true ack (multi-tick mutations only) and Result() for the
// terminal action_result / error. Result is idempotent.
type RequestHandle struct {
	ID     string
	ack    chan protocol.Response
	result chan Result
	// done is closed once the cleanup goroutine has finalized
	// (released slot + lock). Used by tests / DiagnosticStats.
	done chan struct{}
	// cached holds the resolved Result so Result() can be called
	// repeatedly.
	cached *Result
}

// Ack waits for the server's pending:true ack. Multi-tick actions
// (travel, jump, dock, mine, attack, self_destruct) emit one; queries
// and instant mutations may not. Returns ctx.Err() on cancellation.
// Returns the same response that triggered the ack, or the empty
// Response if Result() has already resolved without an ack.
func (h *RequestHandle) Ack(ctx context.Context) (protocol.Response, error) {
	select {
	case resp, ok := <-h.ack:
		if !ok {
			return protocol.Response{}, errors.New("no ack: request resolved without pending")
		}
		return resp, nil
	case <-ctx.Done():
		return protocol.Response{}, ctx.Err()
	}
}

// Result waits for the terminal response. Idempotent: subsequent
// calls return the same value. Returns ctx.Err() on cancellation;
// the underlying subscription stays live so a late terminal does not
// become an orphan.
func (h *RequestHandle) Result(ctx context.Context) (protocol.Response, error) {
	if h.cached != nil {
		return h.cached.Response, h.cached.Err
	}
	select {
	case r := <-h.result:
		h.cached = &r
		return r.Response, r.Err
	case <-ctx.Done():
		return protocol.Response{}, ctx.Err()
	}
}

// SubmitOption customizes Submit. See WithTerminator, WithTimeout,
// WithAckOnly.
type SubmitOption func(*submitConfig)

type submitConfig struct {
	terminator Terminator
	timeout    time.Duration
	ackOnly    bool
}

// WithTerminator overrides the default terminateOnAction.
func WithTerminator(t Terminator) SubmitOption {
	return func(c *submitConfig) { c.terminator = t }
}

// WithTimeout overrides the default SleepTick*3.
func WithTimeout(d time.Duration) SubmitOption {
	return func(c *submitConfig) { c.timeout = d }
}

// WithAckOnly marks this Submit as a query: the first response (any
// type) is treated as terminal. Skips the per-action lock entirely.
// Overrides any WithTerminator passed earlier or later.
func WithAckOnly() SubmitOption {
	return func(c *submitConfig) { c.ackOnly = true; c.terminator = nil }
}

// Submit sends msg with a fresh request_id and returns a handle for
// retrieving the ack and terminal response. Blocks while acquiring
// the in-flight slot and (for mutations) the per-action lock.
//
// Concurrency: queries (WithAckOnly) acquire only the in-flight slot.
// Mutations acquire in-flight then per-action lock; release in
// reverse order on resolution.
func (c *Client) Submit(ctx context.Context, msg protocol.Message, opts ...SubmitOption) (*RequestHandle, error) {
	cfg := submitConfig{
		terminator: terminateOnAction,
		timeout:    SleepTick * 3,
	}
	for _, o := range opts {
		o(&cfg)
	}
	if cfg.ackOnly {
		cfg.terminator = nil
	}

	// 1. Acquire global cap.
	if err := c.inflight.Acquire(ctx); err != nil {
		return nil, fmt.Errorf("inflight: %w", err)
	}

	// 2. Acquire per-action lock (mutations only).
	var releaseAction func()
	if !cfg.ackOnly {
		rel, err := c.actionLocks.lock(ctx, msg.Type)
		if err != nil {
			c.inflight.Release()
			return nil, fmt.Errorf("action lock %q: %w", msg.Type, err)
		}
		releaseAction = rel
	}

	// 3. Stamp request_id and register subscription BEFORE Send so the
	//    response can't arrive in the gap.
	id := uuid.Must(uuid.NewV7()).String()
	msg.RequestID = id

	h := &RequestHandle{
		ID:     id,
		ack:    make(chan protocol.Response, 1),
		result: make(chan Result, 1),
		done:   make(chan struct{}),
	}
	sub := c.router.registerByID(id, make(chan protocol.Response, 1), cfg.terminator)
	c.router.setAckChannel(sub, h.ack)

	// 4. Spawn cleanup goroutine. Owns: resolving result, releasing
	//    lock + slot, unregistering subscription.
	go c.runSubmit(sub, h, cfg, releaseAction)

	// 5. Send. If Send fails, signal cleanup via a special error frame.
	if err := c.send(ctx, msg); err != nil {
		// Inject a synthetic error so the cleanup goroutine completes.
		c.router.dispatch(protocol.Response{
			Type:      protocol.TypeError,
			RequestID: id,
			Payload:   map[string]any{"code": "send_failed", "message": err.Error()},
		})
		return h, nil
	}

	return h, nil
}

// runSubmit drains sub.respCh (terminal or error) and the per-Submit
// timeout/ctx, then releases resources. Runs exactly once per Submit.
//
// ctx is intentionally NOT plumbed in here; the Submit-caller's ctx
// is observed via h.Result() reads. Cancelling the caller's ctx does
// NOT abort the underlying subscription (so a late server reply does
// not become an orphan) — it only affects the caller's Result wait.
func (c *Client) runSubmit(sub *subscription, h *RequestHandle, cfg submitConfig, releaseAction func()) {
	defer close(h.done)
	defer c.inflight.Release()
	if releaseAction != nil {
		defer releaseAction()
	}

	timer := time.NewTimer(cfg.timeout)
	defer timer.Stop()

	select {
	case resp := <-sub.respCh:
		var err error
		if cfg.terminator != nil {
			// Surface error from terminator (router discards it).
			if _, e := cfg.terminator(resp); e != nil {
				err = e
			}
		} else if resp.Type == protocol.TypeError || resp.Type == protocol.TypeActionError {
			err = serverErrorFromPayload(resp.Payload)
		}
		h.result <- Result{Response: resp, Err: err}
	case <-timer.C:
		c.router.unregister(sub)
		h.result <- Result{Err: fmt.Errorf("timeout waiting for %s (request_id=%s)", "submit", h.ID)}
	}
}
```

- [ ] **Step 5: Add Client fields**

In `pkg/game/client.go`, find the `Client` struct (around line 28). After the `router` field, add:

```go
	inflight    *inflight
	actionLocks *actionLockMap
```

In the client constructor (search for `c.router = newResponseRouter()` or where it's initialized), add right after:

```go
	client.inflight = newInflight(16)
	client.actionLocks = newActionLockMap()
```

- [ ] **Step 6: Add private send wrapper**

Add to `pkg/game/client.go` (near the public Send function):

```go
// send is the private wire primitive used by Submit. Exposes the same
// semantics as Send (currently Deprecated) without the deprecation
// warning, since Submit IS the new public path.
func (c *Client) send(ctx context.Context, msg protocol.Message) error {
	return c.Send(ctx, msg)
}
```

(Final phase will invert this: `Send` becomes a wrapper around `send`.)

- [ ] **Step 7: Run tests**

Run: `go test ./pkg/game/ -run TestSubmit -count=1 -race -v`
Expected: PASS

- [ ] **Step 8: Run full test suite**

Run: `go build ./... && go test ./... -count=1 -short`
Expected: clean build, all tests pass.

- [ ] **Step 9: Commit**

```bash
git add pkg/game/submit.go pkg/game/submit_test.go pkg/game/client.go go.mod go.sum
git commit -m "feat(game): introduce Submit primitive with RequestHandle

Submit stamps a UUIDv7 request_id, registers a byReqID subscription,
acquires the in-flight cap and (for mutations) the per-action lock,
sends, and returns a handle exposing Ack/Result channels. Cleanup
goroutine handles resource release. No existing caller migrated yet."
```

---

## Phase 3 — Close handling & reconnect-replay

### Task 9: Wire close-code detection in the read loop

**Files:**
- Modify: `pkg/game/client.go` (the listener at ~line 1717)

- [ ] **Step 1: Read the existing listener context**

Open `pkg/game/client.go` and find the `if closeErr, ok := err.(*websocket.CloseError); ok {` block (around line 1718). Note the goroutineID variable and the `c.debugLogger` is in scope.

- [ ] **Step 2: Add handleClose method**

Add to `pkg/game/client.go`:

```go
// handleClose runs the close-code policy: replay outstanding mutations
// on graceful closes, fail-fast otherwise. Called by the listen loop
// after a close frame is observed.
func (c *Client) handleClose(closeErr *websocket.CloseError) {
	policy, known := lookupClosePolicy(closeErr.Code, closeErr.Reason)
	if !known {
		log.Printf("WARN: unknown WebSocket close: code=%d reason=%q — please document; default action=%v",
			int(closeErr.Code), closeErr.Reason, policy.action)
	} else {
		c.debugLogger.Printf("close policy: %s (code=%d reason=%q)", policy.note, int(closeErr.Code), closeErr.Reason)
	}

	switch policy.action {
	case closeReplay:
		c.replayPending(closeErr)
	default: // closeFailFast
		c.failPending(&ConnectionClosed{Code: closeErr.Code, Reason: closeErr.Reason})
	}
}

// failPending delivers err to every outstanding id-keyed subscription
// and unregisters them. Used on fail-fast closes.
func (c *Client) failPending(err error) {
	subs := c.router.snapshotByID()
	for _, sub := range subs {
		c.router.dispatch(protocol.Response{
			Type:      protocol.TypeError,
			RequestID: sub.id,
			Payload:   map[string]any{"code": "connection_closed", "message": err.Error()},
		})
	}
}

// replayPending is implemented in Task 10.
func (c *Client) replayPending(closeErr *websocket.CloseError) {
	// Task 10 fills this in. Until then, fall back to fail-fast so the
	// caller doesn't hang.
	c.failPending(&ConnectionClosed{Code: closeErr.Code, Reason: closeErr.Reason})
}
```

- [ ] **Step 3: Wire handleClose into the listen loop**

In the listener (around line 1718), replace:

```go
// Check if this is a server close frame
if closeErr, ok := err.(*websocket.CloseError); ok {
    c.debugLogger.Printf("[listen-%d] Server close frame | Status: %s (%d) | Reason: %q",
        goroutineID, closeErr.Code, closeErr.Code, closeErr.Reason)
}
```

with:

```go
// Check if this is a server close frame and run the close policy.
if closeErr, ok := err.(*websocket.CloseError); ok {
    c.debugLogger.Printf("[listen-%d] Server close frame | Status: %s (%d) | Reason: %q",
        goroutineID, closeErr.Code, closeErr.Code, closeErr.Reason)
    c.handleClose(closeErr)
}
```

- [ ] **Step 4: Write a close-handling test**

Add to `pkg/game/close_codes_test.go`:

```go
func TestHandleClose_FailFastPropagates(t *testing.T) {
	c := newClientSkeleton()
	// Register a fake pending subscription.
	ch := make(chan protocol.Response, 1)
	sub := c.router.registerByID("req-fast", ch, terminateOnAction)
	_ = sub

	closeErr := &websocket.CloseError{Code: websocket.StatusGoingAway, Reason: "test"}
	c.handleClose(closeErr)

	select {
	case resp := <-ch:
		if resp.Type != protocol.TypeError {
			t.Errorf("resp.Type = %q, want error", resp.Type)
		}
		if code, _ := resp.Payload["code"].(string); code != "connection_closed" {
			t.Errorf("code = %q, want connection_closed", code)
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatal("pending request not failed on close")
	}
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./pkg/game/ -run "TestHandleClose|TestClosePolicy" -count=1 -race -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add pkg/game/client.go pkg/game/close_codes_test.go
git commit -m "feat(game): wire close-code policy into the listen loop

Close frames now consult the policy registry. Fail-fast closes
deliver ConnectionClosed to every pending Submit; replay path is
stubbed (Task 10)."
```

---

### Task 10: Reconnect-replay path

**Files:**
- Modify: `pkg/game/client.go`
- Modify: `pkg/game/submit.go` (retain original message on sub)
- Test: `pkg/game/submit_test.go`

- [ ] **Step 1: Write failing test**

Add to `pkg/game/submit_test.go`:

```go
func TestReplay_OnNormalClose_FreshUUIDAndDeliver(t *testing.T) {
	c, sendCh := newTestClient(t)
	ctx := context.Background()

	h, err := c.Submit(ctx, protocol.Message{Type: "mine"},
		WithTerminator(terminateOnAction))
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	first := <-sendCh
	origID := first.RequestID

	// Simulate a graceful close: replay should re-send with a new
	// request_id and update the handle.
	c.replayPending(&websocket.CloseError{Code: websocket.StatusNormalClosure, Reason: "max session age"})

	second := <-sendCh
	if second.RequestID == "" || second.RequestID == origID {
		t.Errorf("replayed RequestID = %q, want fresh non-empty", second.RequestID)
	}
	if h.ID != second.RequestID {
		t.Errorf("handle.ID = %q, want %q (post-replay)", h.ID, second.RequestID)
	}

	// Deliver terminal under the new ID; Result must succeed.
	go c.router.dispatch(protocol.Response{
		Type: protocol.TypeActionResult, RequestID: second.RequestID,
		Payload: map[string]any{"command": "mine"},
	})
	if _, err := h.Result(ctx); err != nil {
		t.Errorf("Result after replay: %v", err)
	}
}

func TestReplay_LateOriginalResponseIsOrphan(t *testing.T) {
	c, sendCh := newTestClient(t)
	ctx := context.Background()

	h, err := c.Submit(ctx, protocol.Message{Type: "mine"},
		WithTerminator(terminateOnAction))
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	first := <-sendCh
	origID := first.RequestID

	c.replayPending(&websocket.CloseError{Code: websocket.StatusNormalClosure, Reason: "rolling restart"})
	<-sendCh // consume replayed send

	// Late response under the old ID must be orphaned, not delivered.
	before := c.router.orphans().Count()
	c.router.dispatch(protocol.Response{
		Type: protocol.TypeActionResult, RequestID: origID,
		Payload: map[string]any{"command": "mine"},
	})
	if got := c.router.orphans().Count(); got != before+1 {
		t.Errorf("orphan count delta = %d, want 1", got-before)
	}

	// Cleanup: deliver under the new id so the handle resolves.
	go c.router.dispatch(protocol.Response{
		Type: protocol.TypeActionResult, RequestID: h.ID,
		Payload: map[string]any{"command": "mine"},
	})
	if _, err := h.Result(ctx); err != nil {
		t.Errorf("Result: %v", err)
	}
}
```

- [ ] **Step 2: Add original-message retention to subscription**

In `pkg/game/response_router.go`, modify the `subscription` struct to add:

```go
	// replayMsg holds the original outgoing Message (without
	// RequestID) so the replay path can re-send under a fresh UUID.
	// Set only for id-keyed subscriptions created by Submit.
	replayMsg *protocol.Message
```

Add a setter:

```go
// setReplayMsg attaches the original outgoing message for replay.
func (r *responseRouter) setReplayMsg(sub *subscription, msg protocol.Message) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Strip RequestID so replay can stamp a fresh one.
	msg.RequestID = ""
	sub.replayMsg = &msg
}
```

- [ ] **Step 3: Wire replayMsg in Submit**

In `pkg/game/submit.go`, after `sub := c.router.registerByID(...)`:

```go
	c.router.setReplayMsg(sub, msg)
```

- [ ] **Step 4: Implement replayPending**

Replace the stub in `pkg/game/client.go`:

```go
// replayPending re-sends every outstanding mutation under a fresh
// UUIDv7. Caller has already torn down the connection; this assumes
// the existing reconnect machinery will run before send().
//
// Per spec: server v0.296.1 graceful closes lose all in-flight state,
// so no double-execution risk. The caller's handle.Result() never
// observes the close.
func (c *Client) replayPending(closeErr *websocket.CloseError) {
	subs := c.router.snapshotByID()
	if len(subs) == 0 {
		return
	}
	c.debugLogger.Printf("replay: %d pending mutation(s) under fresh request_ids (close code=%d reason=%q)",
		len(subs), int(closeErr.Code), closeErr.Reason)

	ctx, cancel := context.WithTimeout(context.Background(), SleepReconnect)
	defer cancel()

	for _, sub := range subs {
		if sub.replayMsg == nil {
			// No retained message (subscription created outside Submit).
			// Fail it instead.
			c.router.dispatch(protocol.Response{
				Type:      protocol.TypeError,
				RequestID: sub.id,
				Payload:   map[string]any{"code": "connection_closed", "message": "no replay payload"},
			})
			continue
		}
		newID := uuid.Must(uuid.NewV7()).String()
		msg := *sub.replayMsg
		msg.RequestID = newID
		c.router.rekey(sub, newID)

		if err := c.send(ctx, msg); err != nil {
			c.debugLogger.Printf("replay: send failed for new id=%s: %v", newID, err)
			c.router.dispatch(protocol.Response{
				Type:      protocol.TypeError,
				RequestID: newID,
				Payload:   map[string]any{"code": "connection_lost", "message": err.Error()},
			})
		}
	}
}
```

Add the `uuid` import to `client.go` (`"github.com/google/uuid"`).

- [ ] **Step 5: Update handle.ID on rekey**

The current rekey only updates the router's map and the subscription's id, but `RequestHandle.ID` is stored on the handle. The handle needs to track rekeys.

Modify `RequestHandle`:

```go
type RequestHandle struct {
	mu     sync.Mutex
	id     string  // protected by mu; was: ID string
	ack    chan protocol.Response
	result chan Result
	done   chan struct{}
	cached *Result
}

// ID returns the current request_id. May change across reconnect-replay.
func (h *RequestHandle) ID() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.id
}

func (h *RequestHandle) setID(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.id = id
}
```

In `Submit`, replace `h.ID = id` with `h.setID(id)` and the initializer `ID: id,` with nothing (set via setID immediately after construction).

In `replayPending`, after `c.router.rekey(sub, newID)`, the handle must also learn the new ID. Subscription needs a back-reference to handle, or replay needs a different lookup. Simplest: add a field `handle *RequestHandle` to subscription (set in Submit), and update it in replayPending:

In `subscription`:
```go
	handle *RequestHandle // set for id-subs created by Submit
```

In `Submit`, after creating `h`:
```go
	sub.handle = h
```

In `replayPending`, after computing `newID`:
```go
	if sub.handle != nil {
		sub.handle.setID(newID)
	}
```

Update tests that read `h.ID` to call `h.ID()` instead.

- [ ] **Step 6: Run tests**

Run: `go test ./pkg/game/ -run "TestReplay|TestSubmit" -count=1 -race -v`
Expected: PASS

Run: `go build ./...`
Expected: clean build.

- [ ] **Step 7: Commit**

```bash
git add pkg/game/client.go pkg/game/submit.go pkg/game/submit_test.go pkg/game/response_router.go
git commit -m "feat(game): replay pending mutations on close=1000

Snapshots outstanding id-subscriptions, generates fresh UUIDv7s, and
re-sends under the new IDs. Handle.ID() returns the current id;
late responses under the original id are orphaned."
```

---

## Phase 4 — Concurrency stress tests

### Task 11: Concurrency stress + soak tests

**Files:**
- Create: `pkg/game/concurrency_stress_test.go`

- [ ] **Step 1: Write the test file**

Create `pkg/game/concurrency_stress_test.go`:

```go
package game

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
)

func TestStress_ManyDistinctActionsResolveCorrectly(t *testing.T) {
	if testing.Short() {
		t.Skip("stress test; -short")
	}
	c, sendCh := newTestClient(t)
	// Consume and reply automatically.
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case m := <-sendCh:
				go c.router.dispatch(protocol.Response{
					Type:      protocol.TypeActionResult,
					RequestID: m.RequestID,
					Payload:   map[string]any{"command": m.Type},
				})
			case <-stop:
				return
			}
		}
	}()
	defer close(stop)

	var wg sync.WaitGroup
	const N = 1000
	for i := 0; i < N; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			action := fmt.Sprintf("action-%d", i%50) // 50 distinct actions
			h, err := c.Submit(context.Background(),
				protocol.Message{Type: action})
			if err != nil {
				t.Errorf("Submit: %v", err)
				return
			}
			if _, err := h.Result(context.Background()); err != nil {
				t.Errorf("Result: %v", err)
			}
		}()
	}
	wg.Wait()

	if got := c.inflight.InFlight(); got != 0 {
		t.Errorf("inflight leak: %d slots held", got)
	}
}

func TestStress_SameActionSerializes(t *testing.T) {
	if testing.Short() {
		t.Skip("stress test; -short")
	}
	c, sendCh := newTestClient(t)
	stop := make(chan struct{})
	var current atomic.Int32
	var peak atomic.Int32
	go func() {
		for {
			select {
			case m := <-sendCh:
				go func(m protocol.Message) {
					n := current.Add(1)
					for {
						p := peak.Load()
						if n <= p || peak.CompareAndSwap(p, n) {
							break
						}
					}
					time.Sleep(time.Millisecond)
					current.Add(-1)
					c.router.dispatch(protocol.Response{
						Type:      protocol.TypeActionResult,
						RequestID: m.RequestID,
						Payload:   map[string]any{"command": m.Type},
					})
				}(m)
			case <-stop:
				return
			}
		}
	}()
	defer close(stop)

	var wg sync.WaitGroup
	const N = 100
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h, err := c.Submit(context.Background(), protocol.Message{Type: "mine"})
			if err != nil {
				t.Errorf("Submit: %v", err)
				return
			}
			if _, err := h.Result(context.Background()); err != nil {
				t.Errorf("Result: %v", err)
			}
		}()
	}
	wg.Wait()

	if got := peak.Load(); got > 1 {
		t.Errorf("peak concurrent 'mine' = %d, want 1 (serialized)", got)
	}
}

func TestStress_NoSlotLeakUnderRandomOutcomes(t *testing.T) {
	if testing.Short() {
		t.Skip("stress test; -short")
	}
	c, sendCh := newTestClient(t)

	stop := make(chan struct{})
	go func() {
		for {
			select {
			case m := <-sendCh:
				go func(m protocol.Message) {
					switch rand.IntN(4) {
					case 0:
						// success
						c.router.dispatch(protocol.Response{
							Type:      protocol.TypeActionResult,
							RequestID: m.RequestID,
							Payload:   map[string]any{"command": m.Type},
						})
					case 1:
						// server error
						c.router.dispatch(protocol.Response{
							Type:      protocol.TypeError,
							RequestID: m.RequestID,
							Payload:   map[string]any{"code": "no_target", "message": "no target"},
						})
					case 2:
						// action error
						c.router.dispatch(protocol.Response{
							Type:      protocol.TypeActionError,
							RequestID: m.RequestID,
							Payload:   map[string]any{"code": "blocked"},
						})
					case 3:
						// drop — timeout path
					}
				}(m)
			case <-stop:
				return
			}
		}
	}()
	defer close(stop)

	var wg sync.WaitGroup
	const N = 500
	for i := 0; i < N; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			action := fmt.Sprintf("a%d", i%20)
			h, err := c.Submit(ctx, protocol.Message{Type: action},
				WithTimeout(50*time.Millisecond))
			if err != nil {
				return
			}
			_, _ = h.Result(ctx)
		}()
	}
	wg.Wait()

	// Allow cleanup goroutines to finish.
	time.Sleep(200 * time.Millisecond)
	if got := c.inflight.InFlight(); got != 0 {
		t.Errorf("inflight leak: %d slots held", got)
	}
}

func TestStress_ReplayMidLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("stress test; -short")
	}
	c, sendCh := newTestClient(t)

	stop := make(chan struct{})
	go func() {
		for {
			select {
			case m := <-sendCh:
				go c.router.dispatch(protocol.Response{
					Type:      protocol.TypeActionResult,
					RequestID: m.RequestID,
					Payload:   map[string]any{"command": m.Type},
				})
			case <-stop:
				return
			}
		}
	}()
	defer close(stop)

	// Force a replay every 50ms while submits are firing.
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			case <-time.After(50 * time.Millisecond):
				c.replayPending(&websocket.CloseError{
					Code:   websocket.StatusNormalClosure,
					Reason: "rolling restart",
				})
			}
		}
	}()

	var wg sync.WaitGroup
	const N = 200
	for i := 0; i < N; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()
			action := fmt.Sprintf("a%d", i%10)
			h, err := c.Submit(ctx, protocol.Message{Type: action})
			if err != nil {
				return
			}
			_, _ = h.Result(ctx)
		}()
	}
	wg.Wait()
	close(done)

	time.Sleep(200 * time.Millisecond)
	if got := c.inflight.InFlight(); got != 0 {
		t.Errorf("inflight leak after replay storm: %d slots", got)
	}
}
```

Note: imports needed at the top of the file:

```go
import (
    "context"
    "fmt"
    "math/rand/v2"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    "github.com/coder/websocket"
    "github.com/rsned/spacemolt/internal/protocol"
)
```

- [ ] **Step 2: Run stress tests**

Run: `go test ./pkg/game/ -run TestStress -count=1 -race -timeout=120s -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add pkg/game/concurrency_stress_test.go
git commit -m "test(game): concurrency stress + replay-mid-load coverage

Verifies distinct-action parallelism, same-action serialization,
slot release under random outcomes, and replay storm robustness.
Skipped under -short."
```

---

## Phase 5 — Bump version and migrate callers

### Task 12: Bump BuiltForAPIVersion

**Files:**
- Modify: `pkg/version/checker.go`

- [ ] **Step 1: Update the constant**

In `pkg/version/checker.go`, change:

```go
const BuiltForAPIVersion = "v0.294.0"
```

to:

```go
const BuiltForAPIVersion = "v0.296.1"
```

- [ ] **Step 2: Verify build**

Run: `go build ./...`
Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add pkg/version/checker.go
git commit -m "feat(version): bump BuiltForAPIVersion to v0.296.1

Targets the request_id echo introduced in this server release."
```

---

### Task 13: Migrate Login / Register / Claim to Submit + WithAckOnly

**Files:**
- Modify: `pkg/game/client.go`

These three methods use `waitForAuthResponse` and `execMutation` respectively. They're the only non-mechanical auth paths.

- [ ] **Step 1: Migrate Login**

In `pkg/game/client.go`, find `func (c *Client) Login(ctx context.Context) error`. Replace its body:

```go
func (c *Client) Login(ctx context.Context) error {
	if c.password == "" {
		return fmt.Errorf("no password available")
	}

	msg := protocol.Message{
		Type: "login",
		Payload: map[string]any{
			"username": c.username,
			"password": c.password,
		},
		Timestamp: time.Now().UnixMilli(),
	}

	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(10*time.Second))
	if err != nil {
		return fmt.Errorf("failed to send login: %w", err)
	}
	resp, err := h.Result(ctx)
	if err != nil {
		return fmt.Errorf("login failed: %w", err)
	}
	if resp.Type != protocol.TypeLoggedIn {
		return fmt.Errorf("login: unexpected response type %q", resp.Type)
	}
	return nil
}
```

- [ ] **Step 2: Migrate Register**

In the same file, replace `func (c *Client) Register(ctx context.Context, empire, registrationCode string) error`'s body similarly:

```go
func (c *Client) Register(ctx context.Context, empire, registrationCode string) error {
	payload := map[string]any{
		"username": c.username,
		"empire":   empire,
	}
	if registrationCode != "" {
		payload["registration_code"] = registrationCode
	}

	msg := protocol.Message{
		Type:      "register",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}

	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(10*time.Second))
	if err != nil {
		return fmt.Errorf("failed to send register: %w", err)
	}
	resp, err := h.Result(ctx)
	if err != nil {
		return fmt.Errorf("registration failed: %w", err)
	}
	if resp.Type != protocol.TypeRegistered {
		return fmt.Errorf("register: unexpected response type %q", resp.Type)
	}
	return nil
}
```

- [ ] **Step 3: Migrate Claim**

Replace `func (c *Client) Claim(ctx context.Context, registrationCode string) error`'s body:

```go
func (c *Client) Claim(ctx context.Context, registrationCode string) error {
	msg := protocol.Message{
		Type: "claim",
		Payload: map[string]any{
			"registration_code": registrationCode,
		},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTerminator(terminateOnAction), WithTimeout(10*time.Second))
	if err != nil {
		return fmt.Errorf("claim: submit: %w", err)
	}
	if _, err := h.Result(ctx); err != nil {
		return fmt.Errorf("claim failed: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Build + test**

Run: `go build ./... && go test ./pkg/game/... -count=1 -short -race`
Expected: clean build, tests pass.

- [ ] **Step 5: Commit**

```bash
git add pkg/game/client.go
git commit -m "feat(game): migrate Login/Register/Claim to Submit

First batch of caller migrations — auth handshake. Login and
Register use WithAckOnly since the server replies immediately with
logged_in/registered (no pending:true). Claim uses terminateOnAction."
```

---

### Task 14: Migrate execMutation callers in client_commands.go

**Files:**
- Modify: `pkg/game/client_commands.go`

The file has 90 `execMutation` callers, all following the exact same shape. They convert mechanically.

- [ ] **Step 1: Apply the conversion pattern**

For every occurrence of:

```go
_, err := c.execMutation(ctx, msg, matchCommand("X"), terminateOnAction, SleepTick*3)
```

Replace with:

```go
h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
if err == nil {
    _, err = h.Result(ctx)
}
```

(The default terminator is `terminateOnAction`, so no `WithTerminator` needed.)

For occurrences with a custom classifier or terminator, e.g.:

```go
_, err := c.execMutation(ctx, msg, classifier, terminate, SleepActionStartTimeout)
```

Replace with:

```go
h, err := c.Submit(ctx, msg,
    WithTerminator(terminate),
    WithTimeout(SleepActionStartTimeout))
if err == nil {
    _, err = h.Result(ctx)
}
```

The custom `classifier` argument is no longer needed because request_id correlation is exact. Drop it.

- [ ] **Step 2: Sed-style sweep with verification**

Use this command to identify all call sites first:

```bash
grep -n "c.execMutation" pkg/game/client_commands.go
```

Convert each manually — do NOT run a blind sed because the surrounding error-wrapping varies.

- [ ] **Step 3: Build + test after each batch of ~20 conversions**

Commit batches of ~20 method conversions to keep the diff reviewable.

```bash
go build ./...
go test ./pkg/game/... -count=1 -short
```

- [ ] **Step 4: Commit per batch**

```bash
git add pkg/game/client_commands.go
git commit -m "refactor(game): migrate client_commands.go to Submit (batch N of 5)

Converts ~20 execMutation callers per batch; pattern is identical
across the file. Custom classifiers drop out since request_id
correlation is exact."
```

Expected: ~5 commits to drain all 90 sites.

---

### Task 15: Migrate execMutation / execQuery callers in client.go

**Files:**
- Modify: `pkg/game/client.go`

30 callers in this file.

- [ ] **Step 1: Apply the mutation pattern**

For every:

```go
_, err := c.execMutation(ctx, msg, matchCommand("X"), terminateOnAction, SleepTick*3)
```

replace with:

```go
h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
if err == nil {
    _, err = h.Result(ctx)
}
```

For execMutation calls with a custom terminator:

```go
_, err := c.execMutation(ctx, msg, classifier, terminate, timeout)
```

replace with:

```go
h, err := c.Submit(ctx, msg, WithTerminator(terminate), WithTimeout(timeout))
if err == nil {
    _, err = h.Result(ctx)
}
```

The custom classifier is dropped — request_id correlation makes it redundant.

- [ ] **Step 2: Apply the query pattern**

For every `c.execQuery(ctx, msg, classifier, timeout)`:

```go
h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(timeout))
if err == nil {
    _, err = h.Result(ctx)
}
```

Queries skip the per-action lock and don't have a terminator (first response is terminal).

- [ ] **Step 2: Build + test**

Run: `go build ./... && go test ./pkg/game/... -count=1 -short`
Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add pkg/game/client.go
git commit -m "refactor(game): migrate client.go remaining execMutation/execQuery to Submit"
```

---

### Task 16: Migrate crafting.go

**Files:**
- Modify: `pkg/game/crafting.go`

One `execMutation` call.

- [ ] **Step 1: Apply the pattern**

For:

```go
_, err := c.execMutation(ctx, msg, matchCommand("craft"), terminateOnAction, SleepTick*3)
```

replace with:

```go
h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
if err == nil {
    _, err = h.Result(ctx)
}
```

(Adjust timeout and command name to match the actual call site.)

- [ ] **Step 2: Build + test**

Run: `go build ./... && go test ./pkg/game/... -count=1 -short`
Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add pkg/game/crafting.go
git commit -m "refactor(game): migrate crafting.go to Submit"
```

---

### Task 17: Replace waitFor* callers

**Files:**
- Modify: any remaining file using `waitForResponse`, `waitForAuthResponse`, `waitForActionResponse`

After Tasks 13–16 only stragglers remain.

- [ ] **Step 1: Identify all call sites**

```bash
grep -rn "waitForResponse\|waitForAuthResponse\|waitForActionResponse" pkg/game/ --include="*.go" | grep -v _test.go
```

- [ ] **Step 2: Convert each**

For `waitForResponse(ctx, msgType, timeout)`:

Find the matching `c.Send(ctx, msg)` call just above. Replace the Send + waitForResponse pair with:

```go
h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(timeout))
if err != nil {
    return err
}
resp, err := h.Result(ctx)
if err != nil {
    return err
}
// resp now holds what waitForResponse used to return
```

For `waitForActionResponse(ctx, timeout)`: replace the surrounding Send + wait pair with the mutation pattern:

```go
h, err := c.Submit(ctx, msg, WithTimeout(timeout))
if err != nil {
    return err
}
if _, err := h.Result(ctx); err != nil {
    return err
}
```

For `waitForAuthResponse(ctx, successType, timeout)`: this is the Login/Register/Claim path, already migrated in Task 13. Any remaining caller follows the Task 13 pattern (Submit with WithAckOnly + verify resp.Type matches successType).

- [ ] **Step 3: Build + test**

Run: `go build ./... && go test ./pkg/game/... -count=1 -short -race`
Expected: clean.

- [ ] **Step 4: Commit**

```bash
git add -A pkg/game/
git commit -m "refactor(game): migrate remaining waitFor* callers to Submit"
```

---

## Phase 6 — Cleanup

### Task 18: Delete CommandQueue

**Files:**
- Delete: `pkg/game/client_queue.go`, `pkg/game/client_queue_test.go`
- Delete: `pkg/game/QUEUE.md`, `pkg/game/MIGRATION_EXAMPLE.md`
- Modify: `pkg/game/client.go` (remove `CmdQueue` field + init + dispatch + wrapper)

- [ ] **Step 1: Verify no live callers remain**

```bash
grep -rn "CmdQueue\|CommandQueue\|\.Enqueue(" pkg/ cmd/ internal/ --include="*.go" | grep -v _test.go | grep -v client_queue.go
```

Expected: empty output. If anything matches, migrate it first.

- [ ] **Step 2: Remove field, init, dispatch, wrapper**

In `pkg/game/client.go`:

- Remove the `CmdQueue *CommandQueue` field (~line 74).
- Remove `CmdQueue: NewCommandQueue(nil),` and the subsequent `client.CmdQueue.client = client` in the constructor (~lines 331-335).
- Remove the dispatch call (~lines 1796-1797):
  ```go
  if c.CmdQueue != nil {
      c.CmdQueue.handleResponse(resp)
  }
  ```
- Remove the deprecated `EnqueueCommand` wrapper (~lines 4745-4753).

- [ ] **Step 3: Delete files**

```bash
git rm pkg/game/client_queue.go pkg/game/client_queue_test.go pkg/game/QUEUE.md pkg/game/MIGRATION_EXAMPLE.md
```

- [ ] **Step 4: Build + test**

Run: `go build ./... && go test ./... -count=1 -short -race`
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add -A pkg/game/
git commit -m "refactor(game): delete CommandQueue and supporting docs

Superseded by Submit/request_id correlation. No remaining callers."
```

---

### Task 19: Delete waiters map + waitFor* helpers + mutationMu

**Files:**
- Modify: `pkg/game/client.go`

- [ ] **Step 1: Verify no callers**

```bash
grep -rn "c\.waiters\|waitForResponse\|waitForAuthResponse\|waitForActionResponse\|c\.mutationMu" pkg/ cmd/ --include="*.go" | grep -v _test.go
```

Expected: only matches inside `pkg/game/client.go` definitions, no callers.

- [ ] **Step 2: Remove fields and methods**

In `pkg/game/client.go`:

- Remove `waiterMu sync.Mutex` and `waiters map[string]chan protocol.Response` fields (~lines 70-71).
- Remove `mutationMu sync.Mutex` field (~line 107).
- Remove the `waiters` init in the constructor (search for `c.waiters = make(`).
- Remove the `waiters[...]` dispatch leg in `handleResponse` (around line 1802 — grep for `c.waiters[`).
- Remove `func (c *Client) waitForResponse`, `waitForAuthResponse`, `waitForActionResponse` (lines ~4087-4500).

- [ ] **Step 3: Build + test**

Run: `go build ./... && go test ./... -count=1 -short -race`
Expected: clean.

- [ ] **Step 4: Commit**

```bash
git add pkg/game/client.go
git commit -m "refactor(game): remove waiters map, waitFor* helpers, mutationMu

Replaced by router byReqID, Submit, and per-action lock + soft cap."
```

---

### Task 20: Delete execMutation, execQuery, response_exec.go

**Files:**
- Delete: `pkg/game/response_exec.go`
- Modify: `pkg/game/response_exec_test.go` (drop or rewrite tests)

- [ ] **Step 1: Verify no callers**

```bash
grep -rn "execMutation\|execQuery" pkg/ cmd/ --include="*.go"
```

Expected: only matches inside `response_exec.go` and tests.

- [ ] **Step 2: Decide on response_exec_test.go**

Open the file. If the tests cover behavior now tested via `submit_test.go`, delete it. If any tests cover the router or terminator independent of exec helpers, move them to `response_router_test.go` or `terminator_test.go`.

- [ ] **Step 3: Delete**

```bash
git rm pkg/game/response_exec.go
# If you decided to delete the test file:
git rm pkg/game/response_exec_test.go
```

- [ ] **Step 4: Keep subscribePush**

`subscribePush` lived in `response_exec.go`. Move it to a new file `pkg/game/subscribe.go`:

```go
package game

import (
	"github.com/rsned/spacemolt/internal/protocol"
)

// subscribePush registers a long-lived handler for responses
// satisfying match. Handlers run synchronously in the router's
// dispatch path — keep them fast. Returns a cancel function the
// caller must invoke to stop delivery; idempotent.
//
// Used by OnChatMessage, OnStorageUpdate, and other event listeners.
// Untagged server pushes (no request_id) flow through this path.
func (c *Client) subscribePush(
	match Classifier,
	handler func(protocol.Response),
) func() {
	sub := c.router.registerPush(match, handler)
	return func() {
		c.router.unregister(sub)
	}
}
```

- [ ] **Step 5: Build + test**

Run: `go build ./... && go test ./... -count=1 -short -race`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add -A pkg/game/
git commit -m "refactor(game): delete execMutation/execQuery; move subscribePush

Submit is now the only request/response primitive. subscribePush
relocated to subscribe.go for clarity."
```

---

### Task 21: Make Send private

**Files:**
- Modify: `pkg/game/client.go`
- Modify: `pkg/game/submit.go` (the private `send` wrapper goes away)

- [ ] **Step 1: Verify no external callers**

```bash
grep -rn "\.Send(" pkg/ cmd/ internal/ --include="*.go" | grep -v "client\.Send" | grep -v "_test.go" | grep -v "// Deprecated"
```

Expected: only matches inside `pkg/game/client.go` (and the test override hook).

- [ ] **Step 2: Rename**

In `pkg/game/client.go`:

- Rename `func (c *Client) Send(...)` to `func (c *Client) send(...)`.
- Remove the `Deprecated:` doc comment block.

In `pkg/game/submit.go`:

- Remove the `send` wrapper function added in Task 8. Callers already use `c.send` directly after this rename.

- [ ] **Step 3: Build + test**

Run: `go build ./... && go test ./... -count=1 -short -race`
Expected: clean.

- [ ] **Step 4: Run full lint**

Run: `golangci-lint run ./...`
Expected: no new findings.

- [ ] **Step 5: Commit**

```bash
git add pkg/game/client.go pkg/game/submit.go
git commit -m "refactor(game): make Send private; Submit is the only public path

Closes out the request_id refactor."
```

---

### Task 22: Final integration + push

- [ ] **Step 1: Full race-detector test pass**

Run: `go test ./... -count=1 -race -timeout=180s`
Expected: PASS

- [ ] **Step 2: Stress tests not skipped**

Run: `go test ./pkg/game/ -run TestStress -count=1 -race -timeout=180s -v`
Expected: PASS

- [ ] **Step 3: Lint**

Run: `golangci-lint run ./...`
Expected: 0 issues.

- [ ] **Step 4: Manual smoke**

Run an `auto-explorer` or `play_as` session against a real server briefly; confirm a few mutations + queries land and resolve.

- [ ] **Step 5: Push**

```bash
git push
```
