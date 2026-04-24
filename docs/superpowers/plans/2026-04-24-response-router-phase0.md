# Response Router Phase 0 — Infrastructure + Batch 0 Migration

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the client-side response router, classifier/terminator libraries, three exec primitives, and migrate `GetCargo` + `GetChatHistory` as validation. Remove the `time.Sleep` workarounds those two methods required.

**Architecture:** New `responseRouter` hangs off `*Client`. Response dispatch walks push subscribers (fan-out), then the active mutation (if classifier + terminator match), then FIFO query subscribers. Existing state parsers and `CommandQueue` stay live in parallel during migration.

**Tech Stack:** Go 1.24, `pkg/game` (existing WebSocket client), `pkg/mbox` (backfill consumer), `cmd/tools/play_as` (REPL consumer).

**Spec:** `docs/superpowers/specs/2026-04-24-response-router-design.md`

---

## File Structure

**New:**
- `pkg/game/classifier.go` — `Classifier` type, `matchType`/`matchAction`/`matchCommand`/`matchChannel`/`matchPayloadKey`/`matchAll`
- `pkg/game/classifier_test.go`
- `pkg/game/terminator.go` — `Terminator` type, `terminateOnAction`, `terminateOnTypes`
- `pkg/game/terminator_test.go`
- `pkg/game/response_router.go` — `responseRouter`, `subscription`, register/unregister/dispatch
- `pkg/game/response_router_test.go`
- `pkg/game/response_exec.go` — `execQuery`/`execMutation`/`subscribePush` methods on `*Client`
- `pkg/game/response_exec_test.go`
- `scripts/check_legacy_response_api.sh` — CI gate
- `docs/migration/response-router.md` — migration tracking

**Modified:**
- `pkg/game/client.go` — construct `responseRouter`, call `dispatch` in the read loop, add `mutationMu`
- `pkg/game/client_commands.go` — `GetCargo` and `GetChatHistory` use `execQuery`
- `pkg/game/client_queue.go` — `// Deprecated:` on `Enqueue`
- `pkg/mbox/ingest.go` — remove the `waitForChatHistoryResponse` workaround
- `cmd/tools/play_as/main.go` — remove the `SleepQuick` workaround in `chatPoller.fetchMessages`

---

## Section 1 — Classifier library

### Task 1: Classifier library

**Files:**
- Create: `pkg/game/classifier.go`
- Test: `pkg/game/classifier_test.go`

- [ ] **Step 1: Write the failing tests**

Create `pkg/game/classifier_test.go`:

```go
package game

import (
	"testing"

	"github.com/rsned/spacemolt/internal/protocol"
)

func TestMatchType(t *testing.T) {
	m := matchType(protocol.TypeOK)
	if !m(protocol.Response{Type: protocol.TypeOK}) {
		t.Error("expected OK to match")
	}
	if m(protocol.Response{Type: protocol.TypeError}) {
		t.Error("expected Error not to match")
	}
}

func TestMatchAction(t *testing.T) {
	m := matchAction("get_system")
	ok := protocol.Response{Payload: map[string]any{"action": "get_system"}}
	if !m(ok) {
		t.Error("expected get_system action to match")
	}
	if m(protocol.Response{Payload: map[string]any{"action": "get_poi"}}) {
		t.Error("expected get_poi not to match")
	}
	if m(protocol.Response{Payload: map[string]any{}}) {
		t.Error("expected missing action not to match")
	}
	if m(protocol.Response{}) {
		t.Error("expected nil payload not to match")
	}
}

func TestMatchCommand(t *testing.T) {
	m := matchCommand("deposit_items")
	if !m(protocol.Response{Payload: map[string]any{"command": "deposit_items"}}) {
		t.Error("expected deposit_items to match")
	}
	if m(protocol.Response{Payload: map[string]any{"command": "withdraw_items"}}) {
		t.Error("expected withdraw_items not to match")
	}
}

func TestMatchChannel(t *testing.T) {
	m := matchChannel("system")
	if !m(protocol.Response{Payload: map[string]any{"channel": "system"}}) {
		t.Error("expected system channel to match")
	}
	if m(protocol.Response{Payload: map[string]any{"channel": "local"}}) {
		t.Error("expected local channel not to match")
	}
}

func TestMatchPayloadKey(t *testing.T) {
	m := matchPayloadKey("cargo")
	if !m(protocol.Response{Payload: map[string]any{"cargo": []any{}}}) {
		t.Error("expected cargo key presence to match")
	}
	if m(protocol.Response{Payload: map[string]any{"ship": nil}}) {
		t.Error("expected missing cargo key not to match")
	}
}

func TestMatchAll(t *testing.T) {
	m := matchAll(matchType(protocol.TypeOK), matchPayloadKey("cargo"))
	ok := protocol.Response{Type: protocol.TypeOK, Payload: map[string]any{"cargo": []any{}}}
	if !m(ok) {
		t.Error("expected composite match")
	}
	missingType := protocol.Response{Payload: map[string]any{"cargo": []any{}}}
	if m(missingType) {
		t.Error("expected missing type to fail composite")
	}
	missingKey := protocol.Response{Type: protocol.TypeOK}
	if m(missingKey) {
		t.Error("expected missing key to fail composite")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```
go test ./pkg/game/ -run 'TestMatch' -v
```
Expected: FAIL with "undefined: matchType" etc.

- [ ] **Step 3: Write the implementation**

Create `pkg/game/classifier.go`:

```go
package game

import "github.com/rsned/spacemolt/internal/protocol"

// Classifier decides whether a given response should be delivered to a
// subscriber. Used by queries, pushes, and as the "match mine" gate for
// mutations. Zero allocations by convention: classifiers are cheap closures.
type Classifier func(resp protocol.Response) bool

// matchType matches responses whose top-level Type equals t.
func matchType(t string) Classifier {
	return func(resp protocol.Response) bool {
		return resp.Type == t
	}
}

// matchAction matches responses whose Payload["action"] equals name.
// Returns false for nil/missing payload or non-string action values.
func matchAction(name string) Classifier {
	return func(resp protocol.Response) bool {
		v, ok := resp.Payload["action"].(string)
		return ok && v == name
	}
}

// matchCommand matches responses whose Payload["command"] equals name.
// Used to correlate mutation replies to the issuing command.
func matchCommand(name string) Classifier {
	return func(resp protocol.Response) bool {
		v, ok := resp.Payload["command"].(string)
		return ok && v == name
	}
}

// matchChannel matches responses whose Payload["channel"] equals channel.
// Used for per-channel correlation (chat_history, chat_message).
func matchChannel(channel string) Classifier {
	return func(resp protocol.Response) bool {
		v, ok := resp.Payload["channel"].(string)
		return ok && v == channel
	}
}

// matchPayloadKey matches responses whose Payload contains key. Used when the
// response carries neither an "action" nor a "command" field, so we fall back
// to payload shape (e.g., get_cargo is identified by the "cargo" key).
func matchPayloadKey(key string) Classifier {
	return func(resp protocol.Response) bool {
		_, ok := resp.Payload[key]
		return ok
	}
}

// matchAll returns a Classifier that matches only when every supplied
// classifier matches. Short-circuits on the first non-match.
func matchAll(cs ...Classifier) Classifier {
	return func(resp protocol.Response) bool {
		for _, c := range cs {
			if !c(resp) {
				return false
			}
		}
		return true
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

```
go test ./pkg/game/ -run 'TestMatch' -v
```
Expected: PASS, all six tests green.

- [ ] **Step 5: Commit**

```bash
git add pkg/game/classifier.go pkg/game/classifier_test.go
git commit -m "feat(game): response classifier library"
```

---

## Section 2 — Terminator library

### Task 2: Terminator library

**Files:**
- Create: `pkg/game/terminator.go`
- Test: `pkg/game/terminator_test.go`

- [ ] **Step 1: Write the failing tests**

Create `pkg/game/terminator_test.go`:

```go
package game

import (
	"errors"
	"testing"

	"github.com/rsned/spacemolt/internal/protocol"
)

func TestTerminateOnAction_Result(t *testing.T) {
	done, err := terminateOnAction(protocol.Response{Type: protocol.TypeActionResult})
	if !done {
		t.Error("expected action_result to terminate")
	}
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestTerminateOnAction_Error(t *testing.T) {
	resp := protocol.Response{
		Type:    protocol.TypeActionError,
		Payload: map[string]any{"message": "boom"},
	}
	done, err := terminateOnAction(resp)
	if !done {
		t.Error("expected action_error to terminate")
	}
	if err == nil {
		t.Error("expected non-nil error")
	}
}

func TestTerminateOnAction_Ok(t *testing.T) {
	// ok with pending:true is intermediate, NOT terminal
	resp := protocol.Response{
		Type:    protocol.TypeOK,
		Payload: map[string]any{"pending": true},
	}
	done, err := terminateOnAction(resp)
	if done {
		t.Error("expected pending ok not to terminate")
	}
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestTerminateOnTypes(t *testing.T) {
	term := terminateOnTypes(protocol.TypeArrived, protocol.TypeActionError)
	if done, _ := term(protocol.Response{Type: protocol.TypeArrived}); !done {
		t.Error("expected arrived to terminate")
	}
	err := errors.New("x")
	_ = err
	if done, _ := term(protocol.Response{Type: protocol.TypeTick}); done {
		t.Error("expected tick not to terminate")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```
go test ./pkg/game/ -run 'TestTerminate' -v
```
Expected: FAIL with "undefined: terminateOnAction".

- [ ] **Step 3: Write the implementation**

Create `pkg/game/terminator.go`:

```go
package game

import (
	"fmt"

	"github.com/rsned/spacemolt/internal/protocol"
)

// Terminator reports whether a response resolves a mutation. It runs only
// against responses that have already passed the mutation's Classifier.
// done=true means the mutation is finished; err non-nil means it failed.
type Terminator func(resp protocol.Response) (done bool, err error)

// terminateOnAction is the default terminator for mutations whose terminal
// server message is a TypeActionResult. TypeActionError / TypeError also
// terminate, with a ServerError describing the failure. An "ok" with
// pending:true is intermediate and does NOT terminate.
func terminateOnAction(resp protocol.Response) (bool, error) {
	switch resp.Type {
	case protocol.TypeActionResult:
		return true, nil
	case protocol.TypeActionError, protocol.TypeError:
		return true, serverErrorFromPayload(resp.Payload)
	}
	return false, nil
}

// terminateOnTypes builds a Terminator that returns done=true on any of the
// named response types. ActionError/Error in the list terminate with an
// error; others (e.g. Arrived, Docked) terminate successfully.
func terminateOnTypes(types ...string) Terminator {
	errTypes := map[string]struct{}{
		protocol.TypeActionError: {},
		protocol.TypeError:       {},
	}
	set := make(map[string]struct{}, len(types))
	for _, t := range types {
		set[t] = struct{}{}
	}
	return func(resp protocol.Response) (bool, error) {
		if _, ok := set[resp.Type]; !ok {
			return false, nil
		}
		if _, isErr := errTypes[resp.Type]; isErr {
			return true, serverErrorFromPayload(resp.Payload)
		}
		return true, nil
	}
}

// serverErrorFromPayload builds a descriptive error from a server error
// payload. Prefers the "message" field, falls back to a generic message.
func serverErrorFromPayload(p map[string]any) error {
	if msg, ok := p["message"].(string); ok && msg != "" {
		return fmt.Errorf("server error: %s", msg)
	}
	return fmt.Errorf("server error (no message)")
}
```

- [ ] **Step 4: Run tests to verify they pass**

```
go test ./pkg/game/ -run 'TestTerminate' -v
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/game/terminator.go pkg/game/terminator_test.go
git commit -m "feat(game): response terminator library"
```

---

## Section 3 — Router core

### Task 3: Router struct, register & unregister

**Files:**
- Create: `pkg/game/response_router.go`
- Test: `pkg/game/response_router_test.go`

- [ ] **Step 1: Write the failing tests**

Create `pkg/game/response_router_test.go`:

```go
package game

import (
	"testing"

	"github.com/rsned/spacemolt/internal/protocol"
)

func TestRouter_RegisterUnregister(t *testing.T) {
	r := newResponseRouter()

	ch := make(chan protocol.Response, 1)
	sub := r.registerQuery(matchType(protocol.TypeOK), ch)
	if r.subCount() != 1 {
		t.Fatalf("expected 1 subscription, got %d", r.subCount())
	}
	r.unregister(sub)
	if r.subCount() != 0 {
		t.Fatalf("expected 0 subscriptions after unregister, got %d", r.subCount())
	}
}

func TestRouter_UnregisterMissingIsNoop(t *testing.T) {
	r := newResponseRouter()
	// Build a subscription that was never registered
	sub := &subscription{}
	r.unregister(sub) // must not panic
}
```

- [ ] **Step 2: Run tests to verify they fail**

```
go test ./pkg/game/ -run 'TestRouter_Register' -v
```
Expected: FAIL with "undefined: newResponseRouter".

- [ ] **Step 3: Write the implementation**

Create `pkg/game/response_router.go`:

```go
package game

import (
	"sync"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
)

// subscription is a single registration with the response router. Exactly
// one of respCh/handler is set: respCh for query/mutation one-shot,
// handler for push fan-out.
type subscription struct {
	match      Classifier
	terminate  Terminator              // nil for queries and pushes
	respCh     chan protocol.Response  // one-shot
	handler    func(protocol.Response) // push
	registered time.Time
}

// responseRouter dispatches incoming responses to registered subscribers.
// It has no WebSocket awareness — callers feed it responses via Dispatch.
type responseRouter struct {
	mu   sync.Mutex
	subs []*subscription
}

// newResponseRouter constructs an empty router.
func newResponseRouter() *responseRouter {
	return &responseRouter{}
}

// registerQuery adds a one-shot query subscription. Returns the handle
// the caller must pass to unregister.
func (r *responseRouter) registerQuery(match Classifier, ch chan protocol.Response) *subscription {
	return r.register(&subscription{
		match:      match,
		respCh:     ch,
		registered: time.Now(),
	})
}

// registerMutation adds a one-shot mutation subscription (respCh delivers
// the terminal response; terminator decides when that is).
func (r *responseRouter) registerMutation(match Classifier, term Terminator, ch chan protocol.Response) *subscription {
	return r.register(&subscription{
		match:      match,
		terminate:  term,
		respCh:     ch,
		registered: time.Now(),
	})
}

// registerPush adds a long-lived push subscription. Caller keeps the
// returned handle so it can call unregister to cancel.
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

// unregister removes sub from the router. No-op if sub was never registered
// or already removed.
func (r *responseRouter) unregister(sub *subscription) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, s := range r.subs {
		if s == sub {
			r.subs = append(r.subs[:i], r.subs[i+1:]...)
			return
		}
	}
}

// subCount returns the number of live subscriptions (for tests).
func (r *responseRouter) subCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.subs)
}
```

- [ ] **Step 4: Run tests to verify they pass**

```
go test ./pkg/game/ -run 'TestRouter_Register' -v
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/game/response_router.go pkg/game/response_router_test.go
git commit -m "feat(game): response router scaffold with register/unregister"
```

---

### Task 4: Router dispatch — push fan-out, query FIFO, mutation gate

**Files:**
- Modify: `pkg/game/response_router.go`
- Modify: `pkg/game/response_router_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `pkg/game/response_router_test.go`:

```go
func TestRouter_Dispatch_PushFanout(t *testing.T) {
	r := newResponseRouter()
	got := []string{}
	var mu sync.Mutex
	record := func(label string) func(protocol.Response) {
		return func(resp protocol.Response) {
			mu.Lock()
			got = append(got, label)
			mu.Unlock()
		}
	}
	r.registerPush(matchType(protocol.TypeChatMessage), record("a"))
	r.registerPush(matchType(protocol.TypeChatMessage), record("b"))
	r.registerPush(matchType(protocol.TypeTick), record("tick-only"))

	r.dispatch(protocol.Response{Type: protocol.TypeChatMessage})

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("expected 2 handlers to fire, got %d: %v", len(got), got)
	}
}

func TestRouter_Dispatch_QueryOneShot(t *testing.T) {
	r := newResponseRouter()
	ch := make(chan protocol.Response, 1)
	r.registerQuery(matchType(protocol.TypeOK), ch)

	r.dispatch(protocol.Response{Type: protocol.TypeOK})
	select {
	case resp := <-ch:
		if resp.Type != protocol.TypeOK {
			t.Errorf("got type %q, want %q", resp.Type, protocol.TypeOK)
		}
	default:
		t.Fatal("expected response on channel")
	}

	// Second dispatch should not reach an already-consumed one-shot
	// (subscription was removed after first delivery).
	r.dispatch(protocol.Response{Type: protocol.TypeOK})
	if r.subCount() != 0 {
		t.Errorf("expected 0 subs after delivery, got %d", r.subCount())
	}
}

func TestRouter_Dispatch_QueryFIFO(t *testing.T) {
	r := newResponseRouter()
	ch1 := make(chan protocol.Response, 1)
	ch2 := make(chan protocol.Response, 1)
	// Register in order: ch1 first.
	r.registerQuery(matchType(protocol.TypeOK), ch1)
	time.Sleep(time.Millisecond) // ensure distinct timestamps
	r.registerQuery(matchType(protocol.TypeOK), ch2)

	r.dispatch(protocol.Response{Type: protocol.TypeOK, Payload: map[string]any{"n": 1}})
	r.dispatch(protocol.Response{Type: protocol.TypeOK, Payload: map[string]any{"n": 2}})

	r1 := <-ch1
	r2 := <-ch2
	if r1.Payload["n"].(int) != 1 || r2.Payload["n"].(int) != 2 {
		t.Errorf("FIFO violated: ch1=%v ch2=%v", r1.Payload["n"], r2.Payload["n"])
	}
}

func TestRouter_Dispatch_MutationTerminates(t *testing.T) {
	r := newResponseRouter()
	ch := make(chan protocol.Response, 1)
	r.registerMutation(matchCommand("deposit_items"), terminateOnAction, ch)

	// Pending ok with matching command → classifier matches, terminator fails → no delivery
	r.dispatch(protocol.Response{
		Type:    protocol.TypeOK,
		Payload: map[string]any{"command": "deposit_items", "pending": true},
	})
	if r.subCount() != 1 {
		t.Fatalf("mutation subscription should still be live, have %d", r.subCount())
	}

	// Terminal action_result with matching command → delivers and unregisters
	r.dispatch(protocol.Response{
		Type:    protocol.TypeActionResult,
		Payload: map[string]any{"command": "deposit_items"},
	})
	select {
	case resp := <-ch:
		if resp.Type != protocol.TypeActionResult {
			t.Errorf("got type %q", resp.Type)
		}
	default:
		t.Fatal("expected terminal on channel")
	}
	if r.subCount() != 0 {
		t.Errorf("expected mutation sub removed after terminal, have %d", r.subCount())
	}
}
```

Also add the import for `sync` at the top of the test file if not already present.

- [ ] **Step 2: Run tests to verify they fail**

```
go test ./pkg/game/ -run 'TestRouter_Dispatch' -v
```
Expected: FAIL with "undefined: r.dispatch".

- [ ] **Step 3: Write the implementation**

Append to `pkg/game/response_router.go`:

```go
// dispatch routes a single response through the subscriber list. Order:
//   1. Fan out to every matching push subscriber.
//   2. Deliver to the active mutation subscriber if classifier matches AND
//      terminator fires; remove the subscription on delivery.
//   3. Deliver to the earliest-registered matching query subscriber; remove
//      the subscription on delivery.
// Responses that match nothing fall through silently — the caller's legacy
// _last-slot capture still runs in Client.handleResponse.
func (r *responseRouter) dispatch(resp protocol.Response) {
	// Snapshot live subs under the lock, then run handlers without holding
	// it so slow push handlers can't block register/unregister.
	r.mu.Lock()
	snapshot := make([]*subscription, len(r.subs))
	copy(snapshot, r.subs)
	r.mu.Unlock()

	// 1. Push fan-out — handlers run synchronously; document "don't block".
	for _, s := range snapshot {
		if s.handler != nil && s.match(resp) {
			s.handler(resp)
		}
	}

	// 2. Active mutation: there is at most one; if its classifier matches
	//    and terminator fires, deliver.
	for _, s := range snapshot {
		if s.terminate == nil || s.respCh == nil {
			continue
		}
		if !s.match(resp) {
			continue
		}
		done, _ := s.terminate(resp)
		if !done {
			// Intermediate message for this mutation; do not deliver.
			return
		}
		// Deliver terminal and unregister.
		select {
		case s.respCh <- resp:
		default:
		}
		r.unregister(s)
		return
	}

	// 3. Query FIFO: deliver to earliest-registered matching query.
	var winner *subscription
	for _, s := range snapshot {
		if s.handler != nil || s.terminate != nil || s.respCh == nil {
			continue // skip pushes and mutations
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
		}
		r.unregister(winner)
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

```
go test ./pkg/game/ -run 'TestRouter_Dispatch' -v
```
Expected: PASS, all four tests green.

- [ ] **Step 5: Commit**

```bash
git add pkg/game/response_router.go pkg/game/response_router_test.go
git commit -m "feat(game): response router dispatch with push/mutation/query routing"
```

---

## Section 4 — Exec primitives

### Task 5: execQuery primitive

**Files:**
- Create: `pkg/game/response_exec.go`
- Create: `pkg/game/response_exec_test.go`
- Modify: `pkg/game/client.go` — add `router` field + construct

- [ ] **Step 1: Wire the router onto Client**

Edit `pkg/game/client.go`. Find the `Client` struct (around line 22) and add a field:

```go
	router *responseRouter
```

Find `NewClient` (or wherever the struct is constructed; search for `waiters: make(map[string]chan protocol.Response)`) and add the router initialization right next to it:

```go
	router: newResponseRouter(),
```

- [ ] **Step 2: Write the failing test**

Create `pkg/game/response_exec_test.go`:

```go
package game

import (
	"context"
	"testing"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
)

// newTestClient returns a *Client with only the response-router pieces
// wired up — enough to exercise exec primitives without a WebSocket.
// The client's Send is stubbed via sendOverride.
func newTestClient(send func(ctx context.Context, msg protocol.Message) error) *Client {
	c := &Client{
		router:       newResponseRouter(),
		sendOverride: send,
	}
	return c
}

func TestExecQuery_DeliversMatchingResponse(t *testing.T) {
	var sent protocol.Message
	c := newTestClient(func(_ context.Context, msg protocol.Message) error {
		sent = msg
		// Simulate the server replying asynchronously.
		go func() {
			time.Sleep(5 * time.Millisecond)
			c.router.dispatch(protocol.Response{
				Type:    protocol.TypeOK,
				Payload: map[string]any{"cargo": []any{}},
			})
		}()
		return nil
	})

	resp, err := c.execQuery(
		context.Background(),
		protocol.Message{Type: "get_cargo"},
		matchAll(matchType(protocol.TypeOK), matchPayloadKey("cargo")),
		1*time.Second,
	)
	if err != nil {
		t.Fatalf("execQuery: %v", err)
	}
	if sent.Type != "get_cargo" {
		t.Errorf("sent wrong message: %+v", sent)
	}
	if _, ok := resp.Payload["cargo"]; !ok {
		t.Errorf("response missing cargo key: %+v", resp.Payload)
	}
	if c.router.subCount() != 0 {
		t.Errorf("subscription leaked: %d live", c.router.subCount())
	}
}

func TestExecQuery_Timeout(t *testing.T) {
	c := newTestClient(func(_ context.Context, _ protocol.Message) error {
		return nil // never reply
	})
	_, err := c.execQuery(
		context.Background(),
		protocol.Message{Type: "get_cargo"},
		matchType(protocol.TypeOK),
		20*time.Millisecond,
	)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if c.router.subCount() != 0 {
		t.Errorf("subscription leaked on timeout: %d live", c.router.subCount())
	}
}

func TestExecQuery_ContextCancel(t *testing.T) {
	c := newTestClient(func(_ context.Context, _ protocol.Message) error { return nil })
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	_, err := c.execQuery(ctx, protocol.Message{Type: "x"}, matchType(protocol.TypeOK), 1*time.Second)
	if err == nil {
		t.Fatal("expected ctx error")
	}
	if c.router.subCount() != 0 {
		t.Errorf("subscription leaked on ctx cancel: %d live", c.router.subCount())
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

```
go test ./pkg/game/ -run 'TestExecQuery' -v
```
Expected: FAIL with "undefined: c.execQuery".

- [ ] **Step 4: Write the implementation**

Create `pkg/game/response_exec.go`:

```go
package game

import (
	"context"
	"fmt"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
)

// execQuery sends msg and blocks until a response satisfying match arrives,
// timeout elapses, or ctx is cancelled. Safe to call concurrently: multiple
// queries with the same classifier resolve FIFO by registration time.
func (c *Client) execQuery(
	ctx context.Context,
	msg protocol.Message,
	match Classifier,
	timeout time.Duration,
) (protocol.Response, error) {
	ch := make(chan protocol.Response, 1)
	sub := c.router.registerQuery(match, ch)
	defer c.router.unregister(sub)

	if err := c.Send(ctx, msg); err != nil {
		return protocol.Response{}, fmt.Errorf("send: %w", err)
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case resp := <-ch:
		return resp, nil
	case <-timer.C:
		return protocol.Response{}, fmt.Errorf("timeout waiting for response to %s", msg.Type)
	case <-ctx.Done():
		return protocol.Response{}, ctx.Err()
	}
}
```

- [ ] **Step 5: Run tests to verify they pass**

```
go test ./pkg/game/ -run 'TestExecQuery' -v
```
Expected: PASS, all three tests green.

- [ ] **Step 6: Commit**

```bash
git add pkg/game/client.go pkg/game/response_exec.go pkg/game/response_exec_test.go
git commit -m "feat(game): execQuery primitive with router wiring"
```

---

### Task 6: execMutation primitive + mutationMu

**Files:**
- Modify: `pkg/game/client.go` — add `mutationMu sync.Mutex`
- Modify: `pkg/game/response_exec.go` — add `execMutation`
- Modify: `pkg/game/response_exec_test.go`

- [ ] **Step 1: Add the mutex field**

Edit `pkg/game/client.go`. In the `Client` struct, add (next to `router`):

```go
	mutationMu sync.Mutex
```

- [ ] **Step 2: Write the failing tests**

Append to `pkg/game/response_exec_test.go`:

```go
func TestExecMutation_WaitsForTerminal(t *testing.T) {
	c := newTestClient(func(_ context.Context, _ protocol.Message) error {
		// Simulate: first ok pending, then the action_result terminal.
		go func() {
			time.Sleep(5 * time.Millisecond)
			c.router.dispatch(protocol.Response{
				Type:    protocol.TypeOK,
				Payload: map[string]any{"command": "deposit_items", "pending": true},
			})
			time.Sleep(5 * time.Millisecond)
			c.router.dispatch(protocol.Response{
				Type:    protocol.TypeActionResult,
				Payload: map[string]any{"command": "deposit_items", "quantity": 5.0},
			})
		}()
		return nil
	})

	resp, err := c.execMutation(
		context.Background(),
		protocol.Message{Type: "deposit_items"},
		matchCommand("deposit_items"),
		terminateOnAction,
		1*time.Second,
	)
	if err != nil {
		t.Fatalf("execMutation: %v", err)
	}
	if resp.Type != protocol.TypeActionResult {
		t.Errorf("expected action_result, got %q", resp.Type)
	}
}

func TestExecMutation_SerializesConcurrent(t *testing.T) {
	var active int32
	var peak int32
	c := newTestClient(func(_ context.Context, msg protocol.Message) error {
		n := atomic.AddInt32(&active, 1)
		if n > atomic.LoadInt32(&peak) {
			atomic.StoreInt32(&peak, n)
		}
		go func() {
			time.Sleep(10 * time.Millisecond)
			c.router.dispatch(protocol.Response{
				Type:    protocol.TypeActionResult,
				Payload: map[string]any{"command": msg.Type},
			})
			atomic.AddInt32(&active, -1)
		}()
		return nil
	})

	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = c.execMutation(
				context.Background(),
				protocol.Message{Type: "deposit_items"},
				matchCommand("deposit_items"),
				terminateOnAction,
				1*time.Second,
			)
		}()
	}
	wg.Wait()
	if atomic.LoadInt32(&peak) > 1 {
		t.Errorf("mutations ran in parallel (peak=%d)", peak)
	}
}
```

Ensure the test file imports `sync` and `sync/atomic`.

- [ ] **Step 3: Run tests to verify they fail**

```
go test ./pkg/game/ -run 'TestExecMutation' -v
```
Expected: FAIL with "undefined: c.execMutation".

- [ ] **Step 4: Write the implementation**

Append to `pkg/game/response_exec.go`:

```go
// execMutation sends msg, holds c.mutationMu for the entire duration, and
// blocks until a response satisfies both match AND terminate — or timeout /
// ctx cancellation. Concurrent calls serialize on the mutex.
func (c *Client) execMutation(
	ctx context.Context,
	msg protocol.Message,
	match Classifier,
	terminate Terminator,
	timeout time.Duration,
) (protocol.Response, error) {
	c.mutationMu.Lock()
	defer c.mutationMu.Unlock()

	ch := make(chan protocol.Response, 1)
	sub := c.router.registerMutation(match, terminate, ch)
	defer c.router.unregister(sub)

	if err := c.Send(ctx, msg); err != nil {
		return protocol.Response{}, fmt.Errorf("send: %w", err)
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case resp := <-ch:
		// The router only delivers when terminate returned done=true. Re-run
		// the terminator here to surface any error it produced.
		if _, err := terminate(resp); err != nil {
			return resp, err
		}
		return resp, nil
	case <-timer.C:
		return protocol.Response{}, fmt.Errorf("timeout waiting for %s to complete", msg.Type)
	case <-ctx.Done():
		return protocol.Response{}, ctx.Err()
	}
}
```

- [ ] **Step 5: Run tests to verify they pass**

```
go test ./pkg/game/ -run 'TestExecMutation' -v
```
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add pkg/game/client.go pkg/game/response_exec.go pkg/game/response_exec_test.go
git commit -m "feat(game): execMutation primitive with serialization"
```

---

### Task 7: subscribePush primitive

**Files:**
- Modify: `pkg/game/response_exec.go`
- Modify: `pkg/game/response_exec_test.go`

- [ ] **Step 1: Write the failing test**

Append to `pkg/game/response_exec_test.go`:

```go
func TestSubscribePush_FiresForever(t *testing.T) {
	c := newTestClient(nil)
	var count int32
	cancel := c.subscribePush(matchType(protocol.TypeChatMessage), func(_ protocol.Response) {
		atomic.AddInt32(&count, 1)
	})
	defer cancel()

	c.router.dispatch(protocol.Response{Type: protocol.TypeChatMessage})
	c.router.dispatch(protocol.Response{Type: protocol.TypeChatMessage})
	c.router.dispatch(protocol.Response{Type: protocol.TypeTick}) // ignored
	if got := atomic.LoadInt32(&count); got != 2 {
		t.Errorf("expected 2 handler calls, got %d", got)
	}
}

func TestSubscribePush_CancelStopsDelivery(t *testing.T) {
	c := newTestClient(nil)
	var count int32
	cancel := c.subscribePush(matchType(protocol.TypeChatMessage), func(_ protocol.Response) {
		atomic.AddInt32(&count, 1)
	})

	c.router.dispatch(protocol.Response{Type: protocol.TypeChatMessage})
	cancel()
	c.router.dispatch(protocol.Response{Type: protocol.TypeChatMessage}) // must not fire

	if got := atomic.LoadInt32(&count); got != 1 {
		t.Errorf("expected 1 call, got %d", got)
	}
	if c.router.subCount() != 0 {
		t.Errorf("push sub leaked: %d", c.router.subCount())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```
go test ./pkg/game/ -run 'TestSubscribePush' -v
```
Expected: FAIL with "undefined: c.subscribePush".

- [ ] **Step 3: Write the implementation**

Append to `pkg/game/response_exec.go`:

```go
// subscribePush registers a long-lived handler for responses satisfying
// match. Handlers run synchronously in the router's dispatch path — keep
// them fast; if you need to do real work, copy the payload and hand off to
// your own goroutine. Returns a cancel function the caller must invoke to
// stop delivery; idempotent.
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

- [ ] **Step 4: Run tests to verify they pass**

```
go test ./pkg/game/ -run 'TestSubscribePush' -v
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/game/response_exec.go pkg/game/response_exec_test.go
git commit -m "feat(game): subscribePush primitive"
```

---

## Section 5 — Wire router into the read loop

### Task 8: Dispatch every response through the router

**Files:**
- Modify: `pkg/game/client.go` — add one line in the read loop

- [ ] **Step 1: Locate the read loop**

Read `pkg/game/client.go` around the existing line `c.handleResponse(resp)` (near line 1621). Current structure:

```go
				c.handleResponse(resp)

				// Route to command queue first
				if c.CmdQueue != nil {
					c.CmdQueue.handleResponse(resp)
				}
```

- [ ] **Step 2: Insert router dispatch**

Add the router dispatch right after `c.handleResponse(resp)` (and before `CmdQueue.handleResponse`), so state parsers run first and populate `State` before any query wakes up and reads it:

```go
				c.handleResponse(resp)

				// Fan out through the new response router. Runs after state
				// parsers so callers reading State inside their response
				// handler see fresh data. Legacy CmdQueue/waiters remain
				// below until the last method finishes migrating.
				if c.router != nil {
					c.router.dispatch(resp)
				}

				// Route to command queue first
				if c.CmdQueue != nil {
					c.CmdQueue.handleResponse(resp)
				}
```

- [ ] **Step 3: Run full package tests**

```
go build ./... && go test ./pkg/game/... -run 'TestRouter|TestExec|TestMatch|TestTerminate' -v
```
Expected: all green. No regressions in existing tests either:

```
go test ./pkg/game/... -short
```
Expected: PASS (may be slow; `-short` skips network tests).

- [ ] **Step 4: Commit**

```bash
git add pkg/game/client.go
git commit -m "feat(game): dispatch responses through router from read loop"
```

---

## Section 6 — Migrate GetCargo (batch 0, part 1)

### Task 9: GetCargo uses execQuery, remove deposit_all workaround

**Files:**
- Modify: `pkg/game/client_commands.go:549` — `GetCargo`
- Modify: `pkg/game/client.go` — remove the `SleepQuick` workaround in `DepositAllItems`
- Test: existing `TestDepositAllItems*` and manual REPL

- [ ] **Step 1: Rewrite GetCargo**

Find `GetCargo` in `pkg/game/client_commands.go:549`. Replace:

```go
func (c *Client) GetCargo(ctx context.Context) error {
	return c.Send(ctx, protocol.Message{
		Type:      "get_cargo",
		Timestamp: time.Now().UnixMilli(),
	})
}
```

with:

```go
func (c *Client) GetCargo(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "get_cargo",
		Timestamp: time.Now().UnixMilli(),
	}
	// get_cargo returns type=ok with a "cargo" array; no "action" field.
	match := matchAll(matchType(protocol.TypeOK), matchPayloadKey("cargo"))
	_, err := c.execQuery(ctx, msg, match, SleepMedium)
	// State.Ship.Cargo is already populated by parseGetCargoData which runs
	// in handleResponse before dispatch; on nil error callers can read it.
	return err
}
```

- [ ] **Step 2: Remove the workaround sleep in DepositAllItems**

Find the block in `pkg/game/client.go` near where `DepositAllItems` calls `c.GetCargo(ctx)` (added in a previous fix). Replace:

```go
	if err := c.GetCargo(ctx); err != nil {
		c.debugLogger.Printf("DepositAllItems: get_cargo refresh failed: %v", err)
		// Fall through and try with whatever state we have.
	} else {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(SleepQuick):
		}
		state = c.GetState()
	}
```

with:

```go
	if err := c.GetCargo(ctx); err != nil {
		c.debugLogger.Printf("DepositAllItems: get_cargo refresh failed: %v", err)
		// Fall through and try with whatever state we have.
	} else {
		// GetCargo now blocks via execQuery until the reply has landed and
		// parseGetCargoData has run, so State is guaranteed fresh here.
		state = c.GetState()
	}
```

- [ ] **Step 3: Build and run existing tests**

```
go build ./... && go test ./pkg/game/... -short
```
Expected: PASS.

- [ ] **Step 4: Live smoke in REPL (one-time manual check)**

Run `play_as` and issue `deposit_all` at a docked station. Expected: no "requested quantity X exceeds available Y" error for the first item; every item deposits the quantity the server currently holds. Note the outcome in the commit body.

```
go run ./cmd/tools/play_as
```

- [ ] **Step 5: Commit**

```bash
git add pkg/game/client_commands.go pkg/game/client.go
git commit -m "feat(game): migrate GetCargo to execQuery; drop deposit_all workaround"
```

---

## Section 7 — Migrate GetChatHistory (batch 0, part 2)

### Task 10: GetChatHistory uses execQuery, drop mbox and play_as workarounds

**Files:**
- Modify: `pkg/game/client_commands.go:929` — `GetChatHistory`
- Modify: `pkg/mbox/ingest.go` — remove `waitForChatHistoryResponse`
- Modify: `cmd/tools/play_as/main.go` — remove `SleepQuick` in `fetchMessages`

- [ ] **Step 1: Rewrite GetChatHistory**

Find `GetChatHistory` in `pkg/game/client_commands.go:929`. Replace:

```go
func (c *Client) GetChatHistory(ctx context.Context, channel string, payload map[string]any) error {
	if payload == nil {
		payload = map[string]any{}
	}
	payload["channel"] = channel
	return c.Send(ctx, protocol.Message{
		Type:      "get_chat_history",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	})
}
```

with:

```go
func (c *Client) GetChatHistory(ctx context.Context, channel string, payload map[string]any) error {
	if payload == nil {
		payload = map[string]any{}
	}
	payload["channel"] = channel
	msg := protocol.Message{
		Type:      "get_chat_history",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	// Server response carries {channel, messages[], has_more, total_count}
	// with no "action" field; match on type+channel+shape.
	match := matchAll(
		matchType(protocol.TypeOK),
		matchChannel(channel),
		matchPayloadKey("messages"),
	)
	_, err := c.execQuery(ctx, msg, match, SleepMedium)
	return err
}
```

- [ ] **Step 2: Remove the mbox workaround**

Edit `pkg/mbox/ingest.go`. Delete the `waitForChatHistoryResponse` helper and its call site. Inside `backfillChannel`, replace:

```go
		if err := client.GetChatHistory(ctx, channel, payload); err != nil {
			return cr, fmt.Errorf("get_chat_history(%s): %w", channel, err)
		}

		// GetChatHistory is fire-and-forget over the WebSocket. Wait for the
		// response to populate the raw-JSON slot before reading — otherwise
		// we race with the server's reply and see stale or empty data, which
		// silently aborts the backfill. The wait doubles as request pacing,
		// so we skip the trailing sleep at the bottom of the loop.
		if !waitForChatHistoryResponse(ctx, client, channel, opts.RequestInterval) {
			break
		}

		raw := client.GetRawJSON("_last")
```

with:

```go
		if err := client.GetChatHistory(ctx, channel, payload); err != nil {
			return cr, fmt.Errorf("get_chat_history(%s): %w", channel, err)
		}

		// GetChatHistory now blocks via execQuery until the server reply
		// lands in _last; read directly.
		raw := client.GetRawJSON("_last")
```

Also delete the entire `waitForChatHistoryResponse` function and the `strings` import if it becomes unused.

- [ ] **Step 3: Remove the play_as workaround**

Edit `cmd/tools/play_as/main.go`. In `chatPoller.fetchMessages`, replace:

```go
	if err := cp.client.GetChatHistory(cp.ctx, channel, map[string]any{"limit": 20}); err != nil {
		return nil, false
	}
	// GetChatHistory is fire-and-forget over WS; wait for the reply to
	// populate _last before reading, otherwise we see stale/empty data and
	// the poll silently returns no messages.
	select {
	case <-cp.ctx.Done():
		return nil, false
	case <-time.After(game.SleepQuick):
	}
	raw := cp.client.GetRawJSON("_last")
```

with:

```go
	if err := cp.client.GetChatHistory(cp.ctx, channel, map[string]any{"limit": 20}); err != nil {
		return nil, false
	}
	raw := cp.client.GetRawJSON("_last")
```

- [ ] **Step 4: Build and test**

```
go build ./... && go test ./pkg/game/... ./pkg/mbox/... ./cmd/tools/play_as/... -short
```
Expected: PASS.

The `pkg/mbox` tests that were tolerant of an instant in-memory fake still pass because `execQuery` returns immediately when the router sees a matching response, and the fake's `fakeGameClient.GetChatHistory` still sets the raw-JSON slot before returning — dispatch will find a subscriber and deliver.

- [ ] **Step 5: Live smoke in REPL (manual check)**

Run `play_as`, issue `mbox backfill`. Expected: numbers are no longer 0-all-around when fresh server data is available; reports reflect actual new-vs-known counts.

- [ ] **Step 6: Commit**

```bash
git add pkg/game/client_commands.go pkg/mbox/ingest.go cmd/tools/play_as/main.go
git commit -m "feat(game): migrate GetChatHistory to execQuery; drop mbox/play_as workarounds"
```

---

## Section 8 — Enforcement: deprecation, CI gate, tracking doc

### Task 11: Deprecate legacy entry points

**Files:**
- Modify: `pkg/game/client.go` (`Send`, `waitForResponse`, `waitForActionResponse`)
- Modify: `pkg/game/client_queue.go` (`Enqueue`)

- [ ] **Step 1: Add godoc Deprecated lines**

On `func (c *Client) Send(ctx context.Context, msg protocol.Message) error`, insert above the func:

```go
// Deprecated: prefer execQuery / execMutation / subscribePush. Send is the
// low-level fire-and-forget wire primitive; direct callers lose response
// correlation. New code must use the response-router primitives.
```

On `func (c *Client) waitForResponse(ctx context.Context, messageType string, timeout time.Duration) (protocol.Response, error)`:

```go
// Deprecated: use execQuery with an appropriate Classifier. Type-keyed
// single-slot waiter; multiple callers collide.
```

On `func (c *Client) waitForActionResponse(ctx context.Context, timeout time.Duration) error`:

```go
// Deprecated: use execMutation with matchCommand + terminateOnAction.
```

On `func (q *CommandQueue) Enqueue(...)`:

```go
// Deprecated: use Client.execMutation. CommandQueue will be removed once
// the last caller migrates.
```

- [ ] **Step 2: Build to confirm no stray errors**

```
go vet ./pkg/game/...
```
Expected: no errors (warnings about Deprecated on own methods are not reported inside the defining package; downstream callers will surface on `go vet`).

- [ ] **Step 3: Commit**

```bash
git add pkg/game/client.go pkg/game/client_queue.go
git commit -m "chore(game): deprecate legacy Send/waitForResponse/CommandQueue entry points"
```

---

### Task 12: CI gate script + allowlist

**Files:**
- Create: `scripts/check_legacy_response_api.sh`
- Create: `docs/migration/response-router.md`

- [ ] **Step 1: Write the gate script**

Create `scripts/check_legacy_response_api.sh`:

```bash
#!/usr/bin/env bash
#
# Fails if new call sites to the legacy response API appear outside the
# migration allowlist. Shrink the allowlist as methods migrate; when it's
# empty, delete this script along with Client.Send, waitForResponse, and
# client_queue.go.
#
# Legacy entry points (any reference outside allowlist = failure):
#   - c.Send(  / client.Send(   (direct fire-and-forget send)
#   - waitForResponse(           (single-slot type-keyed waiter)
#   - waitForActionResponse(     (multi-type action waiter)
#   - CommandQueue.Enqueue       (serialized legacy queue)

set -euo pipefail

ROOT="$(git rev-parse --show-toplevel)"
cd "$ROOT"

# Files allowed to retain legacy calls until they migrate. Delete entries as
# you migrate methods. Comments after '#' are documentation only.
ALLOWLIST=(
    # Router internals implement these — keep.
    pkg/game/client.go
    pkg/game/client_queue.go
    pkg/game/response_exec.go
    # Large surface still using legacy Send — shrink batch by batch.
    pkg/game/client_commands.go
    pkg/game/mcp_game_client_commands.go
    pkg/game/mining.go
    pkg/game/crafting.go
    pkg/game/crafting_loop.go
    pkg/game/bulk_sell.go
    pkg/game/agent.go
    # Tests legitimately exercise legacy paths.
    pkg/game/client_queue_test.go
    pkg/game/client_integration_test.go
    pkg/game/client_race_test.go
    pkg/game/client_retry_test.go
    pkg/game/client_helpers_test.go
)

PATTERN='(\b\w*\.Send\(|waitForResponse\(|waitForActionResponse\(|CommandQueue.*\.Enqueue\()'

# Build a -name filter excluding the allowlist.
EXCLUDES=""
for f in "${ALLOWLIST[@]}"; do
    EXCLUDES="$EXCLUDES :(exclude)$f"
done

# shellcheck disable=SC2086
HITS=$(git grep -nE "$PATTERN" -- 'pkg/game/**.go' $EXCLUDES || true)

if [[ -n "$HITS" ]]; then
    echo "✗ New legacy response-API call detected outside allowlist:" >&2
    echo "$HITS" >&2
    echo >&2
    echo "Use c.execQuery / c.execMutation / c.subscribePush instead. See" >&2
    echo "docs/superpowers/specs/2026-04-24-response-router-design.md." >&2
    echo "If this file is being actively migrated, add it to the ALLOWLIST" >&2
    echo "in this script — temporarily." >&2
    exit 1
fi

echo "✓ No new legacy response-API calls."
```

- [ ] **Step 2: Make executable and run**

```bash
chmod +x scripts/check_legacy_response_api.sh
./scripts/check_legacy_response_api.sh
```
Expected: `✓ No new legacy response-API calls.` (all current usages are in allowlisted files).

- [ ] **Step 3: Hook into the pre-commit / CI pipeline**

Check the existing pre-commit hook (referenced by `git commit` output earlier, which runs `Build + Tests + Lint`). Find the script responsible — likely `.git/hooks/pre-commit` or a hook module:

```bash
find . -path ./node_modules -prune -o -name 'pre-commit*' -print 2>/dev/null
grep -rln "Running pre-commit checks" .github scripts .husky 2>/dev/null | head
```

Add a fourth step to that script invoking `./scripts/check_legacy_response_api.sh` after lint. If the hook is external or you cannot locate it, add an explicit invocation in `Makefile` (create/edit a `check` target) and document in the migration doc that CI should call it.

- [ ] **Step 4: Commit**

```bash
git add scripts/check_legacy_response_api.sh
git commit -m "ci(game): enforce no-new-legacy-response-API gate with allowlist"
```

---

### Task 13: Migration tracking doc

**Files:**
- Create: `docs/migration/response-router.md`

- [ ] **Step 1: Write the tracking doc**

Create `docs/migration/response-router.md`:

```markdown
# Response Router Migration Tracking

**Spec:** [2026-04-24-response-router-design.md](../superpowers/specs/2026-04-24-response-router-design.md)
**Phase 0 plan:** [2026-04-24-response-router-phase0.md](../superpowers/plans/2026-04-24-response-router-phase0.md)

Legend: ✅ migrated · 🚧 in progress · ⬜ not started

## Batch 0 — Validation

| Method            | Status | Notes                          |
| ----------------- | ------ | ------------------------------ |
| `GetCargo`        | ✅     | Phase 0 plan, task 9           |
| `GetChatHistory`  | ✅     | Phase 0 plan, task 10          |

## Batch 1 — Queries

| Method              | Status |
| ------------------- | ------ |
| `GetStatus`         | ⬜     |
| `GetShip`           | ⬜     |
| `GetSystem`         | ⬜     |
| `GetPOI`            | ⬜     |
| `GetMap`            | ⬜     |
| `GetSkills`         | ⬜     |
| `ViewStorage`       | ⬜     |
| `ViewMarket`        | ⬜     |
| `GetNearby`         | ⬜     |
| `GetNotifications`  | ⬜     |

## Batch 2 — Simple mutations

| Method              | Status |
| ------------------- | ------ |
| `DepositItems`      | ⬜     |
| `WithdrawItems`     | ⬜     |
| `Buy`               | ⬜     |
| `Sell`              | ⬜     |
| `Jettison`          | ⬜     |
| `Refuel`            | ⬜     |
| `Repair`            | ⬜     |
| `SellAll`           | ⬜     |
| `Craft`             | ⬜     |
| `CreateBuyOrder`    | ⬜     |
| `CreateSellOrder`   | ⬜     |

## Batch 3 — Complex mutations

| Method              | Status |
| ------------------- | ------ |
| `Travel`            | ⬜     |
| `Jump`              | ⬜     |
| `Dock`              | ⬜     |
| `Undock`            | ⬜     |
| `Mine`              | ⬜     |
| `Salvage`           | ⬜     |
| `Battle`            | ⬜     |
| `Scan`              | ⬜     |

## Batch 4 — Long tail

Factions (30+), missions, forum, chat send, social, drones, bases,
insurance, commission, captain's log. Enumerate when batch 3 lands.

## Completion criteria

When every row is ✅:
1. Delete `scripts/check_legacy_response_api.sh`.
2. Rename `Client.Send` → `client.send` (unexported).
3. Delete `pkg/game/client_queue.go` and its test file.
4. Remove `waitForResponse` / `waitForActionResponse`.
5. Delete this tracking doc.
```

- [ ] **Step 2: Commit**

```bash
git add docs/migration/response-router.md
git commit -m "docs(migration): response router tracking checklist"
```

---

## Self-review notes

Inline checks performed against the spec:

**Spec coverage:**
- §Architecture → Tasks 3–4, 8 ✅
- §Classifier library → Task 1 ✅
- §Terminator library → Task 2 ✅
- §Primitives (execQuery/Mutation/Push) → Tasks 5–7 ✅
- §Migration batch 0 → Tasks 9–10 ✅
- §No-new-legacy enforcement → Tasks 11–12 ✅
- §Migration tracking doc → Task 13 ✅
- Batches 1–4 → follow-up plans (explicitly scoped out of this plan) ✅
- §Non-goals honored: no wire changes, no reconnect replay, no push callback signature changes, no agent runner rewrite ✅

**Type consistency:**
- `Classifier` type signature identical across classifier.go / response_router.go / response_exec.go ✅
- `Terminator` signature `(resp) (bool, error)` consistent ✅
- `subscription` fields referenced the same way in register/dispatch ✅
- `matchAll`, `matchType`, `matchAction`, `matchCommand`, `matchChannel`, `matchPayloadKey` all match their definitions and test call-sites ✅

**No placeholders:** every step has runnable code or exact commands; no TBDs.

---

## Plan complete — saved to `docs/superpowers/plans/2026-04-24-response-router-phase0.md`.

Two execution options:

1. **Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration.
2. **Inline Execution** — execute tasks in this session using `superpowers:executing-plans`, batch execution with checkpoints for review.

Which approach?
