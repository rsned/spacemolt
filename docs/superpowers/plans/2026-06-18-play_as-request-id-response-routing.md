# play_as request_id Response Routing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `play_as` display the exact request_id-correlated server response for the command the user issued, instead of reading a command-keyed slot that concurrent background workers can clobber — and convert the remaining fire-and-forget commands onto the correlated path so every command yields a race-free structured frame.

**Architecture:** The WS client (`*game.Client`) already receives the correctly-correlated terminal response from `(*RequestHandle).Result(ctx)` but discards it at all 160 call sites. We add a context-scoped "result sink": a caller (the REPL) puts a `*protocol.Response` into its `context.Context` via `game.WithResultSink`; a new `(*Client).await(ctx, h)` chokepoint — which replaces every inline `h.Result(ctx)` — writes the terminal response into that sink before returning. Because the sink lives in the caller's own context, the REPL goroutine captures exactly its command's frame while background workers (chat poller, faction backfiller) using their own contexts never touch it. The REPL then prints the captured payload, falling back to the old command-keyed lookup only when the sink is empty (MCP transport, or commands not yet on Submit).

**Tech Stack:** Go 1.24+, `github.com/rsned/spacemolt/internal/protocol`, existing `pkg/game` Submit/router infrastructure (`submit.go`, `response_router.go`, `terminator.go`).

## Global Constraints

- Target Go 1.24+; use modern idioms (range-over-int, `b.Loop()` in benchmarks).
- All new code must pass `golangci-lint` with no new findings.
- Run `go build ./...` and `go test ./...` before every commit.
- Any sleeps/pauses must use the `pkg/game/constants.go` Sleep constants.
- Do not change any signature in the `game.GameClient` interface (`pkg/game/interface.go`) — doing so breaks every mock in `pkg/agent` and `pkg/skills`. All new behavior is additive (a new package-level function + a new unexported method).

---

### Task 1: Result-sink context + `await` chokepoint

**Files:**
- Modify: `pkg/game/submit.go` (add `WithResultSink`, `resultSinkFrom`, `(*Client).await`)
- Test: `pkg/game/submit_test.go` (add `TestAwait_FillsResultSink`, `TestAwait_NoSinkIsNoop`)

**Interfaces:**
- Consumes: existing `(*Client).Submit`, `(*RequestHandle).Result`, `protocol.Response`, the `newSubmitTestClient(t)` test harness already in `submit_test.go`.
- Produces:
  - `func WithResultSink(ctx context.Context, sink *protocol.Response) context.Context` — exported; callers attach a sink.
  - `func resultSinkFrom(ctx context.Context) *protocol.Response` — unexported helper.
  - `func (c *Client) await(ctx context.Context, h *RequestHandle) (protocol.Response, error)` — waits for the terminal, writes it into the ctx sink if present, returns `(resp, err)`. Drop-in replacement for `h.Result(ctx)`.

- [ ] **Step 1: Write the failing test**

Add to `pkg/game/submit_test.go`:

```go
func TestAwait_FillsResultSink(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	var sink protocol.Response
	ctx := WithResultSink(context.Background(), &sink)

	h, err := c.Submit(ctx, protocol.Message{Type: "get_status"}, WithAckOnly())
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	sent := <-sendCh
	go c.router.dispatch(protocol.Response{
		Type: protocol.TypeOK, RequestID: sent.RequestID,
		Payload: map[string]any{"action": "get_status", "credits": 42.0},
	})

	resp, err := c.await(ctx, h)
	if err != nil {
		t.Fatalf("await: %v", err)
	}
	if resp.RequestID != sent.RequestID {
		t.Errorf("returned resp.RequestID = %q, want %q", resp.RequestID, sent.RequestID)
	}
	if sink.RequestID != sent.RequestID {
		t.Errorf("sink.RequestID = %q, want %q", sink.RequestID, sent.RequestID)
	}
	if got, _ := sink.Payload["credits"].(float64); got != 42.0 {
		t.Errorf("sink.Payload[credits] = %v, want 42", sink.Payload["credits"])
	}
}

func TestAwait_NoSinkIsNoop(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	ctx := context.Background() // no sink attached

	h, err := c.Submit(ctx, protocol.Message{Type: "get_status"}, WithAckOnly())
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	sent := <-sendCh
	go c.router.dispatch(protocol.Response{
		Type: protocol.TypeOK, RequestID: sent.RequestID,
		Payload: map[string]any{"action": "get_status"},
	})

	if _, err := c.await(ctx, h); err != nil {
		t.Fatalf("await with no sink must not error: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/game/ -run 'TestAwait_' -v`
Expected: FAIL — `c.await` and `WithResultSink` undefined (compile error).

- [ ] **Step 3: Implement the helper and chokepoint**

Add to `pkg/game/submit.go` (e.g. just below the `SubmitOption` block):

```go
// resultSinkKey is the context key under which a caller stores a
// *protocol.Response sink. Unexported zero-size struct type keeps the key
// collision-free.
type resultSinkKey struct{}

// WithResultSink returns a context that captures the terminal protocol.Response
// of any Submit awaited via (*Client).await into *sink. Interactive callers
// (the play_as REPL) use this to obtain the exact request_id-correlated frame
// for the command they issued, rather than reading the racy command-keyed
// latestRawJSON slot that a concurrent background command can clobber. The sink
// belongs to the caller's own context, so unrelated goroutines (background
// pollers) using their own contexts never write to it.
func WithResultSink(ctx context.Context, sink *protocol.Response) context.Context {
	return context.WithValue(ctx, resultSinkKey{}, sink)
}

// resultSinkFrom returns the sink attached by WithResultSink, or nil.
func resultSinkFrom(ctx context.Context) *protocol.Response {
	s, _ := ctx.Value(resultSinkKey{}).(*protocol.Response)
	return s
}

// await waits for h's terminal response and, when ctx carries a sink (see
// WithResultSink), records the response into it before returning. It is the
// single chokepoint that replaces inline `h.Result(ctx)` across the command
// methods, so any caller can capture the correlated frame without changing
// method signatures. The response is captured even on a terminal *ServerError
// (the error frame is carried in resp), which is what raw/json error display
// needs; it is left untouched on ctx-cancel/timeout (resp is the zero value).
func (c *Client) await(ctx context.Context, h *RequestHandle) (protocol.Response, error) {
	resp, err := h.Result(ctx)
	if sink := resultSinkFrom(ctx); sink != nil {
		*sink = resp
	}
	return resp, err
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/game/ -run 'TestAwait_' -v`
Expected: PASS (both tests).

- [ ] **Step 5: Commit**

```bash
git add pkg/game/submit.go pkg/game/submit_test.go
git commit -m "feat(game): add result-sink context + await chokepoint for response capture"
```

---

### Task 2: Route command-method terminal reads through `await`

**Files:**
- Modify: `pkg/game/client_commands.go`, `pkg/game/client.go` (every `h.Result(ctx)` call site in command methods)
- Test: `pkg/game/submit_test.go` (add `TestAwait_CommandMethodFillsSink`)

**Interfaces:**
- Consumes: `(*Client).await` from Task 1.
- Produces: no new symbols — this is a mechanical replacement. After it, calling any converted command method with a sink-bearing ctx fills the sink.

- [ ] **Step 1: Write the failing test**

Add to `pkg/game/submit_test.go`:

```go
// TestAwait_CommandMethodFillsSink proves the mechanical sweep wired await into
// a real command method end to end: GetDrones, driven through the Submit test
// harness, deposits its correlated terminal into the ctx sink.
func TestAwait_CommandMethodFillsSink(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	var sink protocol.Response
	ctx := WithResultSink(context.Background(), &sink)

	errCh := make(chan error, 1)
	go func() { errCh <- c.GetDrones(ctx) }()

	sent := <-sendCh
	if sent.Type != "get_drones" {
		t.Fatalf("sent.Type = %q, want get_drones", sent.Type)
	}
	c.router.dispatch(protocol.Response{
		Type: protocol.TypeOK, RequestID: sent.RequestID,
		Payload: map[string]any{"action": "get_drones", "drones": []any{}},
	})

	if err := <-errCh; err != nil {
		t.Fatalf("GetDrones: %v", err)
	}
	if sink.RequestID != sent.RequestID {
		t.Errorf("sink.RequestID = %q, want %q (sweep did not wire await into GetDrones)",
			sink.RequestID, sent.RequestID)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/game/ -run TestAwait_CommandMethodFillsSink -v`
Expected: FAIL — `sink.RequestID` is empty because `GetDrones` still calls `h.Result(ctx)` (which does not touch the sink).

- [ ] **Step 3: Perform the mechanical sweep**

Find every terminal-result read in the command methods:

```bash
grep -rn '\.Result(ctx)' pkg/game/client_commands.go pkg/game/client.go
```

For **each** match, replace `h.Result(ctx)` with `c.await(ctx, h)`. The receiver `c` and the handle name `h` are in scope at every site (all are methods on `*Client` that named the handle `h`). The return shape is identical (`(protocol.Response, error)`), so assignment forms are unchanged. Concretely:

- `_, err = h.Result(ctx)` → `_, err = c.await(ctx, h)`
- `resp, err := h.Result(ctx)` → `resp, err := c.await(ctx, h)`
- `if err == nil { _, err = h.Result(ctx) }` → `if err == nil { _, err = c.await(ctx, h) }`

Do **not** touch:
- `h.Ack(ctx)` calls (acks, not terminals).
- `pkg/game/submit.go` (the `Result`/`await` definitions and any internal replay logic).
- `*_test.go` files (tests drive `h.Result` deliberately).

After editing, confirm none remain in the command files:

```bash
grep -rn '\.Result(ctx)' pkg/game/client_commands.go pkg/game/client.go
```
Expected: no output.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go build ./... && go test ./pkg/game/ -run 'TestAwait_|TestSubmit_' -v`
Expected: PASS, including `TestAwait_CommandMethodFillsSink`. Then full regression:
Run: `go test ./pkg/game/`
Expected: ok.

- [ ] **Step 5: Commit**

```bash
git add pkg/game/client_commands.go pkg/game/client.go pkg/game/submit_test.go
git commit -m "refactor(game): route command-method terminal reads through await"
```

---

### Task 3: REPL prints the correlated response

**Files:**
- Modify: `cmd/tools/play_as/main.go` (`simpleCommand` at `:7535`; add `chooseResponseJSON`)
- Test: `cmd/tools/play_as/simple_command_test.go` (add `TestChooseResponseJSON_*`)

**Interfaces:**
- Consumes: `game.WithResultSink` (Task 1), `protocol.Response`, existing `lookupRawJSON`, `printResponse`.
- Produces: `func chooseResponseJSON(sink protocol.Response, client game.GameClient, command string) []byte` — returns the JSON bytes to display: the marshaled sink payload when the sink was filled, otherwise the command-keyed fallback.

- [ ] **Step 1: Write the failing test**

Add to `cmd/tools/play_as/simple_command_test.go` (the file already imports `context`, `errors`, `testing`, `game`; add `encoding/json` and `internal/protocol` to its imports):

```go
func TestChooseResponseJSON_PrefersSink(t *testing.T) {
	sink := protocol.Response{
		Type:      "action_result",
		RequestID: "req-1",
		Payload:   map[string]any{"command": "dock", "result": map[string]any{"docked": true}},
	}
	got := chooseResponseJSON(sink, stubGameClientForSimple{}, "dock")

	var decoded map[string]any
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatalf("result is not valid JSON: %v (%s)", err, got)
	}
	if decoded["command"] != "dock" {
		t.Errorf("expected sink payload (command=dock), got %s", got)
	}
}

func TestChooseResponseJSON_FallsBackWhenSinkEmpty(t *testing.T) {
	// Zero-value sink (Type == "") → fall back to the command-keyed lookup.
	client := rawStub{raw: []byte(`{"from":"fallback"}`)}
	got := chooseResponseJSON(protocol.Response{}, client, "dock")
	if string(got) != `{"from":"fallback"}` {
		t.Errorf("expected fallback bytes, got %s", got)
	}
}

// rawStub returns canned bytes from GetRawJSON for the fallback path.
type rawStub struct {
	game.GameClient
	raw []byte
}

func (s rawStub) GetRawJSON(string) []byte { return s.raw }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/tools/play_as/ -run TestChooseResponseJSON_ -v`
Expected: FAIL — `chooseResponseJSON` undefined.

- [ ] **Step 3: Implement `chooseResponseJSON` and wire `simpleCommand`**

Add `chooseResponseJSON` near `showLastResponse` in `cmd/tools/play_as/main.go`:

```go
// chooseResponseJSON returns the JSON payload bytes to display for a command.
// When sink was populated by the request_id-correlated await path (Type set),
// it marshals the sink's payload — the exact frame for THIS command, immune to
// the command-keyed-slot clobber race. Otherwise (MCP transport, or commands
// not yet on Submit) it falls back to the legacy command-keyed lookup.
func chooseResponseJSON(sink protocol.Response, client game.GameClient, command string) []byte {
	if sink.Type != "" {
		if raw, err := json.Marshal(sink.Payload); err == nil {
			return raw
		}
	}
	return lookupRawJSON(client, command)
}
```

Then rewrite `simpleCommand` (`:7535`) to attach a sink and display via the helper. Replace the body so it reads:

```go
func simpleCommand(client game.GameClient, fn func(context.Context) error, ctx context.Context, wait time.Duration, command string, format outputFormat) error {
	var sink protocol.Response
	cctx := game.WithResultSink(ctx, &sink)
	if err := fn(cctx); err != nil {
		// Propagate the goal-reached sentinel unchanged for the loop
		// executor / REPL dispatcher to display.
		var goal *game.GoalReachedError
		if errors.As(err, &goal) {
			return err
		}
		// In raw/JSON modes, surface the server's actual error frame. Prefer the
		// correlated sink (await captures error frames too); fall back to the
		// dedicated _last_error slot when the sink is empty (e.g. send-failure
		// before any frame, or MCP transport).
		if format != formatStyled {
			if raw := chooseErrorJSON(sink, client); len(raw) > 0 {
				printResponse(raw, format, command)
			}
		}
		return err
	}
	if raw := chooseResponseJSON(sink, client, command); len(raw) > 0 {
		printResponse(raw, format, command)
	}
	if wait > 0 {
		time.Sleep(wait)
	}
	return nil
}

// chooseErrorJSON mirrors chooseResponseJSON for the error path: prefer the
// correlated sink (await populates resp even on a terminal *ServerError),
// otherwise the dedicated _last_error slot.
func chooseErrorJSON(sink protocol.Response, client game.GameClient) []byte {
	if sink.Type != "" {
		if raw, err := json.Marshal(sink.Payload); err == nil {
			return raw
		}
	}
	return client.GetRawJSON("_last_error")
}
```

(`encoding/json`, `errors`, `time`, `context`, and `game` are already imported by `main.go`.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go build ./... && go test ./cmd/tools/play_as/ -run 'TestChooseResponseJSON_|TestSimpleCommand_' -v`
Expected: PASS (new tests plus the existing `TestSimpleCommand_*` still green).

- [ ] **Step 5: Commit**

```bash
git add cmd/tools/play_as/main.go cmd/tools/play_as/simple_command_test.go
git commit -m "feat(play_as): display request_id-correlated response via result sink"
```

---

### Task 4: Convert remaining mutation commands to Submit

**Files:**
- Modify: `pkg/game/client_commands.go` (the 13 mutation methods listed below)
- Test: `pkg/game/submit_test.go` (add `TestConvertedMutation_Correlates`)

**Interfaces:**
- Consumes: `(*Client).Submit`, `WithTerminator`, `terminateOnActionOrOK`, `(*Client).await` (Tasks 1–2).
- Produces: each listed method now stamps a request_id and awaits its terminal. No signature changes.

**Conversion rule:** these methods currently end with `return c.send(ctx, msg)`. A mutation may return either a synchronous `type=ok` or an `action_result`, so use `WithTerminator(terminateOnActionOrOK)` (the same choice `RawCommand` makes at `client_commands.go:2183`) — `terminateOnAction` alone would hang on synchronous-ok mutations. Keep each method's existing `protocol.Message` payload verbatim.

**The 13 mutation methods (current line, in `client_commands.go`):**
`TradeCancel` (534), `TradeDecline` (543), `AbandonMission` (973), `DeclineMission` (982), `ClaimInsurance` (1009), `ForumReply` (1288), `ForumDeleteReply` (1300), `ForumDeleteThread` (1309), `ForumUpvote` (1318), `CaptainsLogAdd` (1335), `SetAnonymous` (1365), `SetColors` (1374), `SetPlayerStatus` (1386).

- [ ] **Step 1: Write the failing test**

Add to `pkg/game/submit_test.go`:

```go
// TestConvertedMutation_Correlates proves a converted fire-and-forget mutation
// now flows through Submit (stamps a request_id) and awaits a terminal.
func TestConvertedMutation_Correlates(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	var sink protocol.Response
	ctx := WithResultSink(context.Background(), &sink)

	errCh := make(chan error, 1)
	go func() { errCh <- c.AbandonMission(ctx, "m-1") }()

	sent := <-sendCh
	if sent.Type != "abandon_mission" {
		t.Fatalf("sent.Type = %q, want abandon_mission", sent.Type)
	}
	if sent.RequestID == "" {
		t.Fatal("converted mutation did not stamp a request_id (still on c.send)")
	}
	c.router.dispatch(protocol.Response{
		Type: protocol.TypeActionResult, RequestID: sent.RequestID,
		Payload: map[string]any{"command": "abandon_mission", "result": map[string]any{"abandoned": true}},
	})

	if err := <-errCh; err != nil {
		t.Fatalf("AbandonMission: %v", err)
	}
	if sink.RequestID != sent.RequestID {
		t.Errorf("sink.RequestID = %q, want %q", sink.RequestID, sent.RequestID)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/game/ -run TestConvertedMutation_Correlates -v`
Expected: FAIL — `AbandonMission` still calls `c.send`, so `sent.RequestID == ""` triggers the fatal.

- [ ] **Step 3: Convert the 13 mutation methods**

For each method, replace the trailing `return c.send(ctx, msg)` (or the inline `return c.send(ctx, protocol.Message{...})`) with the Submit+await form. Worked example — `AbandonMission` (`:973`), before:

```go
func (c *Client) AbandonMission(ctx context.Context, missionID string) error {
	return c.send(ctx, protocol.Message{
		Type:      "abandon_mission",
		Payload:   map[string]any{"mission_id": missionID},
		Timestamp: time.Now().UnixMilli(),
	})
}
```

After:

```go
func (c *Client) AbandonMission(ctx context.Context, missionID string) error {
	h, err := c.Submit(ctx, protocol.Message{
		Type:      "abandon_mission",
		Payload:   map[string]any{"mission_id": missionID},
		Timestamp: time.Now().UnixMilli(),
	}, WithTerminator(terminateOnActionOrOK), WithTimeout(SleepMedium))
	if err != nil {
		return err
	}
	_, err = c.await(ctx, h)
	return err
}
```

Apply the identical wrapper to the other 12 (payloads unchanged): `TradeCancel`, `TradeDecline`, `DeclineMission`, `ClaimInsurance`, `ForumReply`, `ForumDeleteReply`, `ForumDeleteThread`, `ForumUpvote`, `CaptainsLogAdd`, `SetAnonymous`, `SetColors`, `SetPlayerStatus`. Each ends up as: build the same `protocol.Message`, `c.Submit(ctx, msg, WithTerminator(terminateOnActionOrOK), WithTimeout(SleepMedium))`, return early on the Submit error, then `_, err = c.await(ctx, h); return err`.

Confirm none of the 13 still call `c.send`:

```bash
grep -nA6 'func (c \*Client) \(TradeCancel\|TradeDecline\|AbandonMission\|DeclineMission\|ClaimInsurance\|ForumReply\|ForumDeleteReply\|ForumDeleteThread\|ForumUpvote\|CaptainsLogAdd\|SetAnonymous\|SetColors\|SetPlayerStatus\)(' pkg/game/client_commands.go | grep 'c.send('
```
Expected: no output.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go build ./... && go test ./pkg/game/ -run TestConvertedMutation_Correlates -v`
Expected: PASS. Then: `go test ./pkg/game/` → ok.

- [ ] **Step 5: Commit**

```bash
git add pkg/game/client_commands.go pkg/game/submit_test.go
git commit -m "feat(game): convert remaining mutation commands to request_id Submit"
```

---

### Task 5: Convert remaining query commands to Submit

**Files:**
- Modify: `pkg/game/client_commands.go` (the 13 query methods listed below)
- Test: `pkg/game/submit_test.go` (add `TestConvertedQuery_Correlates`)

**Interfaces:**
- Consumes: `(*Client).Submit`, `WithAckOnly`, `(*Client).await`.
- Produces: each listed query now stamps a request_id and treats the first response as terminal. No signature changes.

**Conversion rule:** queries make no state change and return a single `type=ok`. Use `WithAckOnly()` (first response is terminal, skips the per-action lock) — the same choice `GetDrones`/`GetDrone` make at `client_commands.go:2198,2211`. Keep each method's payload verbatim.

**The 13 query methods (current line, in `client_commands.go`):**
`AnalyzeMarket` (382), `EstimatePurchase` (482), `GetTrades` (494), `GetCommands` (1149), `GetGuide` (1157), `SearchChangelog` (1170), `SearchSystems` (1179), `GetVersion` (1188), `Help` (1196), `ForumList` (1250), `ForumGetThread` (1279), `CaptainsLogGet` (1344), `CaptainsLogList` (1353).

- [ ] **Step 1: Write the failing test**

Add to `pkg/game/submit_test.go`:

```go
// TestConvertedQuery_Correlates proves a converted query flows through Submit
// with WithAckOnly (first response terminal) and fills the sink.
func TestConvertedQuery_Correlates(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	var sink protocol.Response
	ctx := WithResultSink(context.Background(), &sink)

	errCh := make(chan error, 1)
	go func() { errCh <- c.GetVersion(ctx) }()

	sent := <-sendCh
	if sent.Type != "get_version" {
		t.Fatalf("sent.Type = %q, want get_version", sent.Type)
	}
	if sent.RequestID == "" {
		t.Fatal("converted query did not stamp a request_id (still on c.send)")
	}
	c.router.dispatch(protocol.Response{
		Type: protocol.TypeOK, RequestID: sent.RequestID,
		Payload: map[string]any{"action": "get_version", "version": "v0.294.0"},
	})

	if err := <-errCh; err != nil {
		t.Fatalf("GetVersion: %v", err)
	}
	if v, _ := sink.Payload["version"].(string); v != "v0.294.0" {
		t.Errorf("sink.Payload[version] = %v, want v0.294.0", sink.Payload["version"])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/game/ -run TestConvertedQuery_Correlates -v`
Expected: FAIL — `GetVersion` still uses `c.send`, so `sent.RequestID == ""`.

- [ ] **Step 3: Convert the 13 query methods**

Worked example — `GetVersion` (`:1188`), before:

```go
func (c *Client) GetVersion(ctx context.Context) error {
	return c.send(ctx, protocol.Message{
		Type:      "get_version",
		Timestamp: time.Now().UnixMilli(),
	})
}
```

After:

```go
func (c *Client) GetVersion(ctx context.Context) error {
	h, err := c.Submit(ctx, protocol.Message{
		Type:      "get_version",
		Timestamp: time.Now().UnixMilli(),
	}, WithAckOnly(), WithTimeout(SleepMedium))
	if err != nil {
		return err
	}
	_, err = c.await(ctx, h)
	return err
}
```

Apply the identical wrapper to the other 12 (payloads unchanged): `AnalyzeMarket`, `EstimatePurchase`, `GetTrades`, `GetCommands`, `GetGuide`, `SearchChangelog`, `SearchSystems`, `Help`, `ForumList`, `ForumGetThread`, `CaptainsLogGet`, `CaptainsLogList`.

Confirm none of the 13 still call `c.send`:

```bash
grep -nA6 'func (c \*Client) \(AnalyzeMarket\|EstimatePurchase\|GetTrades\|GetCommands\|GetGuide\|SearchChangelog\|SearchSystems\|GetVersion\|Help\|ForumList\|ForumGetThread\|CaptainsLogGet\|CaptainsLogList\)(' pkg/game/client_commands.go | grep 'c.send('
```
Expected: no output.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go build ./... && go test ./pkg/game/ -run TestConvertedQuery_Correlates -v`
Expected: PASS. Then full suite: `go test ./...` → all packages ok.

- [ ] **Step 5: Commit**

```bash
git add pkg/game/client_commands.go pkg/game/submit_test.go
git commit -m "feat(game): convert remaining query commands to request_id Submit"
```

---

## Final verification

- [ ] Run `go build ./...` — clean.
- [ ] Run `go test ./...` — all packages ok.
- [ ] Run `golangci-lint run ./pkg/game/... ./cmd/tools/play_as/...` — no new findings.
- [ ] Confirm zero remaining `c.send(` calls among the 26 converted methods:
  `grep -n 'c.send(' pkg/game/client_commands.go` — only the genuinely-unconverted lightweight commands (if any were intentionally left) should remain; none of the 26 names above.

## Notes / out of scope

- **MCP transport** (`MCPGameClient`) has no Submit path, so its REPL sessions never fill the sink and fall back to `lookupRawJSON` — unchanged behavior, acceptable.
- **Local value-add commands** (`craftable`, `demand`, `plan_route`, `autopilot`, status dashboards, `update_*`, etc.) compute client-side with no server frame; they print their own output and are unaffected by this work.
- The command-keyed `storeRawJSON` slot and `lookupRawJSON` remain as the fallback path; this plan does not remove them.
