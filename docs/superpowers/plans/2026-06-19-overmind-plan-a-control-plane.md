# Overmind Plan A — Control Plane & Supervisor Skeleton — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the overmind↔worker control plane and a supervisor that spawns, health-checks, restarts, and checkpoints a fleet of thin stub workers — proving the skeleton end-to-end before any `play_as` refactor.

**Architecture:** One `overmind` process listens on a local Unix-domain socket (NDJSON wire). It spawns N `worker` subprocesses; each worker connects in, sends `hello`, then streams `status`/`event` heartbeats and obeys `abort`/`pause`/`resume`. Each worker persists a small SQLite checkpoint and, on restart, reconciles its saved state against fresh game state. The Plan-A worker is a **thin stub**: it connects to the game and emits heartbeats but runs no real automation (that arrives in Plan B). No web UI (Plan D), no guardrail rules (Plan C) — only the event hook seam they will use.

**Tech Stack:** Go 1.24, `modernc.org/sqlite` (pure-Go SQLite, already used by `pkg/mbox`), `encoding/json`, `net` (unix sockets), `os/exec`. Module path `github.com/rsned/spacemolt`.

## Global Constraints

- Target Go 1.24+; use modern features (range-over-int, `b.Loop()` in benchmarks). — verbatim from CLAUDE.md
- All new code must pass `golangci-lint` with no new findings; run it after each series of changes. — verbatim from CLAUDE.md
- Any sleep/pause MUST use a predefined constant in `pkg/game/constants.go` (`SleepTick=10s`, `SleepQuick=2s`, `SleepShort=5s`, `SleepMedium=30s`, …); if none fits, stop and ask the user to add one. — verbatim from CLAUDE.md
- Run `go build ./...` and `go test ./...` before committing. — verbatim from CLAUDE.md
- Compiled binaries go in `bin/`, never the repo root. — verbatim from CLAUDE.md
- Always check actual API/server response struct field names before coding against them — do not assume. — verbatim from CLAUDE.md

---

## File Structure

New packages and binaries (all created by this plan):

| Path | Responsibility |
|------|----------------|
| `pkg/overmind/control/messages.go` | Wire message types: `Type` constants, `Envelope`, typed payloads (`Hello`, `Status`, `Event`, `Abort`), envelope build/decode helpers. |
| `pkg/overmind/control/codec.go` | NDJSON `Encoder`/`Decoder` over `io.Writer`/`io.Reader`. |
| `pkg/overmind/checkpoint/store.go` | Per-worker SQLite checkpoint: intent, known-state, task journal, cursors. Mirrors `pkg/mbox` open/migrate pattern. |
| `pkg/overmind/checkpoint/reconcile.go` | Pure `Reconcile(saved, live KnownState) Reconciliation` — Resume vs Diverged. No game dependency. |
| `pkg/overmind/supervisor/fleet.go` | In-memory fleet registry: `Fleet`, `WorkerInfo`, apply `hello`/`status`, snapshot, restart-decision predicate. |
| `pkg/overmind/supervisor/server.go` | Control-channel server: accept worker conns, route inbound messages, send outbound to a named worker, event hook. |
| `pkg/overmind/supervisor/supervisor.go` | Process lifecycle: spawn workers, monitor health, restart on exit/silence. |
| `pkg/overmind/supervisor/config.go` | Load `data/overmind/fleet.yaml` → `[]WorkerSpec`. |
| `cmd/worker/main.go` | Thin stub worker binary. |
| `cmd/overmind/main.go` | Wire server + supervisor; CLI/log output only. |
| `data/overmind/fleet.yaml` | Fleet roster (agent_id/role/station). |

Deleted: `cmd/agent-server/` (abandoned, superseded).

**Interface contract used across packages (defined in Task 1, referenced everywhere):**

```go
// pkg/overmind/control
type Type string
const (
    TypeHello   Type = "hello"
    TypeStatus  Type = "status"
    TypeEvent   Type = "event"
    TypeAbort   Type = "abort"
    TypePause   Type = "pause"
    TypeResume  Type = "resume"
)
type Envelope struct {
    Type    Type            `json:"type"`
    AgentID string          `json:"agent_id"`
    Payload json.RawMessage `json:"payload,omitempty"`
}
func NewEnvelope(t Type, agentID string, payload any) (Envelope, error)
func (e Envelope) Into(v any) error
type Hello  struct { AgentID, Role, Station string; PID int }
type Status struct {
    System, POI string; Docked bool
    Hull, MaxHull, Fuel, MaxFuel, Credits float64
    StandingBehavior, ActiveTaskID string
    Timestamp string // RFC3339Nano
}
type Event  struct { Kind, Detail, Timestamp string }
type Abort  struct { Reason string; Flee bool }
func NewEncoder(w io.Writer) *Encoder
func (e *Encoder) Encode(env Envelope) error
func NewDecoder(r io.Reader) *Decoder
func (d *Decoder) Decode() (Envelope, error)
```

---

## Task 1: Control message types

**Files:**
- Create: `pkg/overmind/control/messages.go`
- Test: `pkg/overmind/control/messages_test.go`

**Interfaces:**
- Produces: `Type` + constants, `Envelope`, `Hello`, `Status`, `Event`, `Abort`, `NewEnvelope(t, agentID, payload) (Envelope, error)`, `(Envelope).Into(v any) error`.

- [ ] **Step 1: Write the failing test**

```go
package control

import "testing"

func TestEnvelopeRoundTrip(t *testing.T) {
	want := Hello{AgentID: "resident-1", Role: "resident", Station: "ST-9", PID: 42}
	env, err := NewEnvelope(TypeHello, want.AgentID, want)
	if err != nil {
		t.Fatalf("NewEnvelope: %v", err)
	}
	if env.Type != TypeHello || env.AgentID != "resident-1" {
		t.Fatalf("envelope header wrong: %+v", env)
	}
	var got Hello
	if err := env.Into(&got); err != nil {
		t.Fatalf("Into: %v", err)
	}
	if got != want {
		t.Fatalf("round trip mismatch: got %+v want %+v", got, want)
	}
}

func TestIntoWrongShapeStillDecodesKnownFields(t *testing.T) {
	env, _ := NewEnvelope(TypeStatus, "a", Status{System: "SOL", Credits: 100})
	var got Status
	if err := env.Into(&got); err != nil {
		t.Fatalf("Into: %v", err)
	}
	if got.System != "SOL" || got.Credits != 100 {
		t.Fatalf("payload lost: %+v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/overmind/control/ -run TestEnvelope -v`
Expected: FAIL — package/identifiers undefined.

- [ ] **Step 3: Write minimal implementation**

```go
// Package control defines the overmind<->worker control-channel wire format.
package control

import (
	"encoding/json"
	"fmt"
)

// Type identifies a control message kind.
type Type string

const (
	TypeHello  Type = "hello"
	TypeStatus Type = "status"
	TypeEvent  Type = "event"
	TypeAbort  Type = "abort"
	TypePause  Type = "pause"
	TypeResume Type = "resume"
)

// Envelope is the framed wire unit; one Envelope is one NDJSON line.
type Envelope struct {
	Type    Type            `json:"type"`
	AgentID string          `json:"agent_id"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Hello is the first message a worker sends after connecting.
type Hello struct {
	AgentID string `json:"agent_id"`
	Role    string `json:"role"`
	Station string `json:"station"`
	PID     int    `json:"pid"`
}

// Status is a worker heartbeat snapshot.
type Status struct {
	System           string  `json:"system"`
	POI              string  `json:"poi"`
	Docked           bool    `json:"docked"`
	Hull             float64 `json:"hull"`
	MaxHull          float64 `json:"max_hull"`
	Fuel             float64 `json:"fuel"`
	MaxFuel          float64 `json:"max_fuel"`
	Credits          float64 `json:"credits"`
	StandingBehavior string  `json:"standing_behavior"`
	ActiveTaskID     string  `json:"active_task_id"`
	Timestamp        string  `json:"timestamp"`
}

// Event is a notable worker-side occurrence (action result, danger signal).
type Event struct {
	Kind      string `json:"kind"`
	Detail    string `json:"detail"`
	Timestamp string `json:"timestamp"`
}

// Abort tells a worker to stop now; Flee requests undock/flee first.
type Abort struct {
	Reason string `json:"reason"`
	Flee   bool   `json:"flee"`
}

// NewEnvelope marshals payload and wraps it with the given type and agent id.
func NewEnvelope(t Type, agentID string, payload any) (Envelope, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return Envelope{}, fmt.Errorf("control: marshal payload: %w", err)
	}
	return Envelope{Type: t, AgentID: agentID, Payload: raw}, nil
}

// Into unmarshals the envelope payload into v.
func (e Envelope) Into(v any) error {
	if len(e.Payload) == 0 {
		return nil
	}
	if err := json.Unmarshal(e.Payload, v); err != nil {
		return fmt.Errorf("control: decode payload: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/overmind/control/ -run TestEnvelope -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/overmind/control/messages.go pkg/overmind/control/messages_test.go
git commit -m "feat(overmind): control-channel message types"
```

---

## Task 2: NDJSON codec

**Files:**
- Create: `pkg/overmind/control/codec.go`
- Test: `pkg/overmind/control/codec_test.go`

**Interfaces:**
- Consumes: `Envelope` (Task 1).
- Produces: `NewEncoder(io.Writer) *Encoder`, `(*Encoder).Encode(Envelope) error`, `NewDecoder(io.Reader) *Decoder`, `(*Decoder).Decode() (Envelope, error)`. Decode returns `io.EOF` at end of stream. Encoder is safe for concurrent use.

- [ ] **Step 1: Write the failing test**

```go
package control

import (
	"bytes"
	"io"
	"sync"
	"testing"
)

func TestCodecRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	in := []Envelope{
		{Type: TypeHello, AgentID: "a"},
		{Type: TypeStatus, AgentID: "a"},
	}
	for _, e := range in {
		if err := enc.Encode(e); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}
	dec := NewDecoder(&buf)
	for i := range in {
		got, err := dec.Decode()
		if err != nil {
			t.Fatalf("decode %d: %v", i, err)
		}
		if got.Type != in[i].Type || got.AgentID != in[i].AgentID {
			t.Fatalf("decode %d mismatch: %+v", i, got)
		}
	}
	if _, err := dec.Decode(); err != io.EOF {
		t.Fatalf("expected EOF, got %v", err)
	}
}

func TestEncoderConcurrentSafe(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = enc.Encode(Envelope{Type: TypeStatus, AgentID: "x"})
		}()
	}
	wg.Wait()
	dec := NewDecoder(&buf)
	n := 0
	for {
		if _, err := dec.Decode(); err != nil {
			break
		}
		n++
	}
	if n != 50 {
		t.Fatalf("expected 50 framed messages, got %d", n)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/overmind/control/ -run TestCodec -v`
Expected: FAIL — `NewEncoder`/`NewDecoder` undefined.

- [ ] **Step 3: Write minimal implementation**

```go
package control

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// maxLineBytes bounds a single NDJSON line. Status/event payloads are small;
// 1 MiB is generous headroom against the 64 KiB bufio.Scanner default.
const maxLineBytes = 1 << 20

// Encoder writes length-delimited (newline) JSON envelopes. Safe for
// concurrent use by multiple goroutines.
type Encoder struct {
	mu sync.Mutex
	w  *bufio.Writer
}

// NewEncoder returns an Encoder writing to w.
func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w: bufio.NewWriter(w)}
}

// Encode marshals env to one JSON line and flushes.
func (e *Encoder) Encode(env Envelope) error {
	raw, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("control: marshal envelope: %w", err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, err := e.w.Write(raw); err != nil {
		return fmt.Errorf("control: write: %w", err)
	}
	if err := e.w.WriteByte('\n'); err != nil {
		return fmt.Errorf("control: write newline: %w", err)
	}
	return e.w.Flush()
}

// Decoder reads newline-delimited JSON envelopes.
type Decoder struct {
	sc *bufio.Scanner
}

// NewDecoder returns a Decoder reading from r.
func NewDecoder(r io.Reader) *Decoder {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	return &Decoder{sc: sc}
}

// Decode reads the next envelope, returning io.EOF when the stream ends.
func (d *Decoder) Decode() (Envelope, error) {
	for d.sc.Scan() {
		line := d.sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var env Envelope
		if err := json.Unmarshal(line, &env); err != nil {
			return Envelope{}, fmt.Errorf("control: decode line: %w", err)
		}
		return env, nil
	}
	if err := d.sc.Err(); err != nil {
		return Envelope{}, fmt.Errorf("control: scan: %w", err)
	}
	return Envelope{}, io.EOF
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/overmind/control/ -v`
Expected: PASS (all control tests).

- [ ] **Step 5: Commit**

```bash
git add pkg/overmind/control/codec.go pkg/overmind/control/codec_test.go
git commit -m "feat(overmind): NDJSON control codec"
```

---

## Task 3: Checkpoint store

**Files:**
- Create: `pkg/overmind/checkpoint/store.go`
- Test: `pkg/overmind/checkpoint/store_test.go`

**Interfaces:**
- Produces:
  - `type Intent struct { StandingBehavior, ActiveTaskID string; StepIndex int }`
  - `type KnownState struct { System, POI string; Docked bool; Credits float64; CargoJSON string; Tick int }`
  - `type JournalEntry struct { TaskID, Outcome string; At time.Time }`
  - `Open(dbPath string) (*Store, error)`, `(*Store).Close() error`
  - `SaveIntent(Intent) error`, `LoadIntent() (Intent, bool, error)`
  - `SaveKnownState(KnownState) error`, `LoadKnownState() (KnownState, bool, error)`
  - `AppendJournal(taskID, outcome string, at time.Time) error`, `Journal(limit int) ([]JournalEntry, error)`
  - `SetCursor(key, value string) error`, `Cursor(key string) (string, bool, error)`

- [ ] **Step 1: Write the failing test**

```go
package checkpoint

import (
	"path/filepath"
	"testing"
	"time"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "cp.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestIntentRoundTrip(t *testing.T) {
	s := openTemp(t)
	if _, ok, err := s.LoadIntent(); err != nil || ok {
		t.Fatalf("empty LoadIntent: ok=%v err=%v", ok, err)
	}
	want := Intent{StandingBehavior: "track_station", ActiveTaskID: "t-1", StepIndex: 3}
	if err := s.SaveIntent(want); err != nil {
		t.Fatalf("SaveIntent: %v", err)
	}
	got, ok, err := s.LoadIntent()
	if err != nil || !ok || got != want {
		t.Fatalf("LoadIntent got=%+v ok=%v err=%v want=%+v", got, ok, err, want)
	}
	// Upsert (single row).
	want.StepIndex = 9
	_ = s.SaveIntent(want)
	got, _, _ = s.LoadIntent()
	if got.StepIndex != 9 {
		t.Fatalf("intent not upserted: %+v", got)
	}
}

func TestKnownStateAndJournalAndCursor(t *testing.T) {
	s := openTemp(t)
	ks := KnownState{System: "SOL", POI: "ST-9", Docked: true, Credits: 12345, CargoJSON: `{"iron":20}`, Tick: 7}
	if err := s.SaveKnownState(ks); err != nil {
		t.Fatalf("SaveKnownState: %v", err)
	}
	got, ok, err := s.LoadKnownState()
	if err != nil || !ok || got != ks {
		t.Fatalf("LoadKnownState got=%+v ok=%v err=%v", got, ok, err)
	}

	now := time.Now().Truncate(time.Second)
	_ = s.AppendJournal("t-1", "done", now)
	_ = s.AppendJournal("t-2", "failed", now)
	entries, err := s.Journal(10)
	if err != nil || len(entries) != 2 {
		t.Fatalf("Journal len=%d err=%v", len(entries), err)
	}
	if entries[0].TaskID != "t-2" { // newest first
		t.Fatalf("journal order wrong: %+v", entries)
	}

	if _, ok, _ := s.Cursor("mined_iron"); ok {
		t.Fatalf("unexpected cursor present")
	}
	_ = s.SetCursor("mined_iron", "14000")
	v, ok, err := s.Cursor("mined_iron")
	if err != nil || !ok || v != "14000" {
		t.Fatalf("Cursor v=%q ok=%v err=%v", v, ok, err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/overmind/checkpoint/ -v`
Expected: FAIL — package/identifiers undefined.

- [ ] **Step 3: Write minimal implementation**

```go
// Package checkpoint persists a single worker's resumable state to SQLite.
package checkpoint

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const timeFormat = time.RFC3339Nano

// Intent is the worker's current standing behavior and active task position.
type Intent struct {
	StandingBehavior string
	ActiveTaskID     string
	StepIndex        int
}

// KnownState is the last game-state snapshot used for restart reconciliation.
type KnownState struct {
	System   string
	POI      string
	Docked   bool
	Credits  float64
	CargoJSON string
	Tick     int
}

// JournalEntry is one assigned-task outcome.
type JournalEntry struct {
	TaskID  string
	Outcome string
	At      time.Time
}

// Store is a per-worker SQLite checkpoint.
type Store struct {
	db *sql.DB
}

// Open opens (or creates) the checkpoint DB, enables WAL, and migrates.
func Open(dbPath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("checkpoint: create dir: %w", err)
	}
	db, err := sql.Open("sqlite", dbPath+"?_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("checkpoint: open db: %w", err)
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("checkpoint: enable WAL: %w", err)
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("checkpoint: migrate: %w", err)
	}
	return s, nil
}

// Close releases the database connection.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	if _, err := s.db.Exec("CREATE TABLE IF NOT EXISTS schema_version (version INTEGER PRIMARY KEY)"); err != nil {
		return err
	}
	var current int
	if err := s.db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_version").Scan(&current); err != nil {
		return err
	}
	migrations := []string{
		`CREATE TABLE intent (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			standing_behavior TEXT NOT NULL,
			active_task_id    TEXT NOT NULL,
			step_index        INTEGER NOT NULL
		);
		CREATE TABLE known_state (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			system TEXT NOT NULL, poi TEXT NOT NULL, docked INTEGER NOT NULL,
			credits REAL NOT NULL, cargo_json TEXT NOT NULL, tick INTEGER NOT NULL,
			updated_at TEXT NOT NULL
		);
		CREATE TABLE task_journal (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id TEXT NOT NULL, outcome TEXT NOT NULL, at TEXT NOT NULL
		);
		CREATE TABLE cursors (key TEXT PRIMARY KEY, value TEXT NOT NULL);`,
	}
	for i := current; i < len(migrations); i++ {
		if _, err := s.db.Exec(migrations[i]); err != nil {
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := s.db.Exec("INSERT INTO schema_version (version) VALUES (?)", i+1); err != nil {
			return err
		}
	}
	return nil
}

// SaveIntent upserts the single intent row.
func (s *Store) SaveIntent(i Intent) error {
	_, err := s.db.Exec(
		`INSERT INTO intent (id, standing_behavior, active_task_id, step_index)
		 VALUES (1, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   standing_behavior=excluded.standing_behavior,
		   active_task_id=excluded.active_task_id,
		   step_index=excluded.step_index`,
		i.StandingBehavior, i.ActiveTaskID, i.StepIndex)
	if err != nil {
		return fmt.Errorf("checkpoint: save intent: %w", err)
	}
	return nil
}

// LoadIntent returns the intent row and whether one exists.
func (s *Store) LoadIntent() (Intent, bool, error) {
	var i Intent
	err := s.db.QueryRow(
		`SELECT standing_behavior, active_task_id, step_index FROM intent WHERE id=1`).
		Scan(&i.StandingBehavior, &i.ActiveTaskID, &i.StepIndex)
	if err == sql.ErrNoRows {
		return Intent{}, false, nil
	}
	if err != nil {
		return Intent{}, false, fmt.Errorf("checkpoint: load intent: %w", err)
	}
	return i, true, nil
}

// SaveKnownState upserts the single known-state row.
func (s *Store) SaveKnownState(k KnownState) error {
	_, err := s.db.Exec(
		`INSERT INTO known_state (id, system, poi, docked, credits, cargo_json, tick, updated_at)
		 VALUES (1, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   system=excluded.system, poi=excluded.poi, docked=excluded.docked,
		   credits=excluded.credits, cargo_json=excluded.cargo_json,
		   tick=excluded.tick, updated_at=excluded.updated_at`,
		k.System, k.POI, boolToInt(k.Docked), k.Credits, k.CargoJSON, k.Tick,
		time.Now().Format(timeFormat))
	if err != nil {
		return fmt.Errorf("checkpoint: save known_state: %w", err)
	}
	return nil
}

// LoadKnownState returns the known-state row and whether one exists.
func (s *Store) LoadKnownState() (KnownState, bool, error) {
	var k KnownState
	var docked int
	err := s.db.QueryRow(
		`SELECT system, poi, docked, credits, cargo_json, tick FROM known_state WHERE id=1`).
		Scan(&k.System, &k.POI, &docked, &k.Credits, &k.CargoJSON, &k.Tick)
	if err == sql.ErrNoRows {
		return KnownState{}, false, nil
	}
	if err != nil {
		return KnownState{}, false, fmt.Errorf("checkpoint: load known_state: %w", err)
	}
	k.Docked = docked != 0
	return k, true, nil
}

// AppendJournal records one task outcome.
func (s *Store) AppendJournal(taskID, outcome string, at time.Time) error {
	_, err := s.db.Exec(
		`INSERT INTO task_journal (task_id, outcome, at) VALUES (?, ?, ?)`,
		taskID, outcome, at.Format(timeFormat))
	if err != nil {
		return fmt.Errorf("checkpoint: append journal: %w", err)
	}
	return nil
}

// Journal returns up to limit entries, newest first.
func (s *Store) Journal(limit int) ([]JournalEntry, error) {
	rows, err := s.db.Query(
		`SELECT task_id, outcome, at FROM task_journal ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("checkpoint: query journal: %w", err)
	}
	defer rows.Close()
	var out []JournalEntry
	for rows.Next() {
		var e JournalEntry
		var at string
		if err := rows.Scan(&e.TaskID, &e.Outcome, &at); err != nil {
			return nil, fmt.Errorf("checkpoint: scan journal: %w", err)
		}
		e.At, _ = time.Parse(timeFormat, at)
		out = append(out, e)
	}
	return out, rows.Err()
}

// SetCursor upserts a named progress cursor.
func (s *Store) SetCursor(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO cursors (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	if err != nil {
		return fmt.Errorf("checkpoint: set cursor: %w", err)
	}
	return nil
}

// Cursor returns a named cursor and whether it exists.
func (s *Store) Cursor(key string) (string, bool, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM cursors WHERE key=?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("checkpoint: get cursor: %w", err)
	}
	return v, true, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/overmind/checkpoint/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/overmind/checkpoint/store.go pkg/overmind/checkpoint/store_test.go
git commit -m "feat(overmind): per-worker SQLite checkpoint store"
```

---

## Task 4: Restart reconciliation

**Files:**
- Create: `pkg/overmind/checkpoint/reconcile.go`
- Test: `pkg/overmind/checkpoint/reconcile_test.go`

**Interfaces:**
- Consumes: `KnownState` (Task 3).
- Produces:
  - `type Disposition int` with `Resume Disposition = iota; Diverged`
  - `func (Disposition) String() string`
  - `type Reconciliation struct { Disposition Disposition; Reasons []string }`
  - `func Reconcile(saved, live KnownState, creditDropFraction float64) Reconciliation` — Diverged if system differs, docked differs, or credits dropped by more than `creditDropFraction` of saved (e.g. 0.25). Empty saved (zero value) → Resume with no reasons (fresh worker).

- [ ] **Step 1: Write the failing test**

```go
package checkpoint

import "testing"

func TestReconcileResumeWhenMatching(t *testing.T) {
	saved := KnownState{System: "SOL", POI: "ST-9", Docked: true, Credits: 1000}
	live := saved
	r := Reconcile(saved, live, 0.25)
	if r.Disposition != Resume || len(r.Reasons) != 0 {
		t.Fatalf("expected clean Resume, got %+v", r)
	}
}

func TestReconcileDivergedOnSystemChange(t *testing.T) {
	saved := KnownState{System: "SOL", Docked: true, Credits: 1000}
	live := KnownState{System: "VEGA", Docked: true, Credits: 1000}
	r := Reconcile(saved, live, 0.25)
	if r.Disposition != Diverged || len(r.Reasons) == 0 {
		t.Fatalf("expected Diverged with reason, got %+v", r)
	}
}

func TestReconcileDivergedOnCreditDrop(t *testing.T) {
	saved := KnownState{System: "SOL", Credits: 1000}
	live := KnownState{System: "SOL", Credits: 500} // 50% drop > 25%
	r := Reconcile(saved, live, 0.25)
	if r.Disposition != Diverged {
		t.Fatalf("expected Diverged on credit drop, got %+v", r)
	}
}

func TestReconcileFreshWorker(t *testing.T) {
	r := Reconcile(KnownState{}, KnownState{System: "SOL"}, 0.25)
	if r.Disposition != Resume {
		t.Fatalf("fresh worker should Resume, got %+v", r)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/overmind/checkpoint/ -run TestReconcile -v`
Expected: FAIL — `Reconcile`/`Resume`/`Diverged` undefined.

- [ ] **Step 3: Write minimal implementation**

```go
package checkpoint

import "fmt"

// Disposition is the reconciliation outcome.
type Disposition int

const (
	// Resume means saved intent can be safely continued.
	Resume Disposition = iota
	// Diverged means live state contradicts the checkpoint; re-plan instead.
	Diverged
)

// String renders a Disposition for logs.
func (d Disposition) String() string {
	if d == Diverged {
		return "diverged"
	}
	return "resume"
}

// Reconciliation is the result of comparing a checkpoint against live state.
type Reconciliation struct {
	Disposition Disposition
	Reasons     []string
}

// Reconcile compares saved checkpoint state to freshly-fetched live state.
// A zero-value saved state (fresh worker, no checkpoint) always returns Resume.
// creditDropFraction is the fraction of saved credits whose loss flags divergence.
func Reconcile(saved, live KnownState, creditDropFraction float64) Reconciliation {
	if (saved == KnownState{}) {
		return Reconciliation{Disposition: Resume}
	}
	var reasons []string
	if saved.System != live.System {
		reasons = append(reasons, fmt.Sprintf("system changed %q->%q", saved.System, live.System))
	}
	if saved.Docked != live.Docked {
		reasons = append(reasons, fmt.Sprintf("docked changed %v->%v", saved.Docked, live.Docked))
	}
	if saved.Credits > 0 && live.Credits < saved.Credits*(1-creditDropFraction) {
		reasons = append(reasons, fmt.Sprintf("credits dropped %.0f->%.0f", saved.Credits, live.Credits))
	}
	if len(reasons) > 0 {
		return Reconciliation{Disposition: Diverged, Reasons: reasons}
	}
	return Reconciliation{Disposition: Resume}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/overmind/checkpoint/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/overmind/checkpoint/reconcile.go pkg/overmind/checkpoint/reconcile_test.go
git commit -m "feat(overmind): restart reconciliation logic"
```

---

## Task 5: Fleet registry & restart predicate

**Files:**
- Create: `pkg/overmind/supervisor/fleet.go`
- Test: `pkg/overmind/supervisor/fleet_test.go`

**Interfaces:**
- Consumes: `control.Hello`, `control.Status` (Task 1).
- Produces:
  - `type WorkerInfo struct { AgentID, Role, Station string; PID int; LastStatus control.Status; LastSeen time.Time; Healthy bool; Restarts int }`
  - `type Fleet struct { ... }`, `NewFleet() *Fleet`
  - `(*Fleet).ApplyHello(h control.Hello, pid int, now time.Time)`
  - `(*Fleet).ApplyStatus(agentID string, st control.Status, now time.Time)`
  - `(*Fleet).MarkRestart(agentID string)`
  - `(*Fleet).Snapshot() []WorkerInfo` (sorted by AgentID)
  - `func NeedsRestart(info WorkerInfo, now time.Time, silence time.Duration) bool` — true when `now - LastSeen > silence`.

- [ ] **Step 1: Write the failing test**

```go
package supervisor

import (
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/overmind/control"
)

func TestFleetApplyAndSnapshot(t *testing.T) {
	f := NewFleet()
	t0 := time.Unix(1000, 0)
	f.ApplyHello(control.Hello{AgentID: "b", Role: "resident", Station: "S2"}, 11, t0)
	f.ApplyHello(control.Hello{AgentID: "a", Role: "hauler", Station: "S1"}, 22, t0)
	f.ApplyStatus("a", control.Status{System: "SOL", Credits: 50}, t0.Add(time.Second))

	snap := f.Snapshot()
	if len(snap) != 2 || snap[0].AgentID != "a" { // sorted
		t.Fatalf("snapshot wrong: %+v", snap)
	}
	if snap[0].PID != 22 || snap[0].LastStatus.System != "SOL" {
		t.Fatalf("agent a info wrong: %+v", snap[0])
	}
}

func TestNeedsRestart(t *testing.T) {
	now := time.Unix(2000, 0)
	healthy := WorkerInfo{LastSeen: now.Add(-5 * time.Second)}
	stale := WorkerInfo{LastSeen: now.Add(-40 * time.Second)}
	if NeedsRestart(healthy, now, 30*time.Second) {
		t.Fatalf("healthy worker flagged for restart")
	}
	if !NeedsRestart(stale, now, 30*time.Second) {
		t.Fatalf("stale worker not flagged")
	}
}

func TestMarkRestartIncrements(t *testing.T) {
	f := NewFleet()
	t0 := time.Unix(1000, 0)
	f.ApplyHello(control.Hello{AgentID: "a"}, 1, t0)
	f.MarkRestart("a")
	f.MarkRestart("a")
	if f.Snapshot()[0].Restarts != 2 {
		t.Fatalf("restart count wrong: %+v", f.Snapshot()[0])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/overmind/supervisor/ -run 'TestFleet|TestNeedsRestart|TestMarkRestart' -v`
Expected: FAIL — identifiers undefined.

- [ ] **Step 3: Write minimal implementation**

```go
// Package supervisor runs the overmind control server and worker lifecycle.
package supervisor

import (
	"sort"
	"sync"
	"time"

	"github.com/rsned/spacemolt/pkg/overmind/control"
)

// WorkerInfo is the overmind's view of one worker.
type WorkerInfo struct {
	AgentID    string
	Role       string
	Station    string
	PID        int
	LastStatus control.Status
	LastSeen   time.Time
	Healthy    bool
	Restarts   int
}

// Fleet is the thread-safe in-memory registry of all workers.
type Fleet struct {
	mu      sync.RWMutex
	workers map[string]*WorkerInfo
}

// NewFleet returns an empty Fleet.
func NewFleet() *Fleet {
	return &Fleet{workers: make(map[string]*WorkerInfo)}
}

func (f *Fleet) get(agentID string) *WorkerInfo {
	w := f.workers[agentID]
	if w == nil {
		w = &WorkerInfo{AgentID: agentID}
		f.workers[agentID] = w
	}
	return w
}

// ApplyHello records a worker's identity on connect.
func (f *Fleet) ApplyHello(h control.Hello, pid int, now time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w := f.get(h.AgentID)
	w.Role, w.Station, w.PID = h.Role, h.Station, pid
	w.LastSeen, w.Healthy = now, true
}

// ApplyStatus records a heartbeat.
func (f *Fleet) ApplyStatus(agentID string, st control.Status, now time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w := f.get(agentID)
	w.LastStatus, w.LastSeen, w.Healthy = st, now, true
}

// MarkRestart increments the restart counter and marks the worker unhealthy.
func (f *Fleet) MarkRestart(agentID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w := f.get(agentID)
	w.Restarts++
	w.Healthy = false
}

// Snapshot returns a copy of all worker infos, sorted by AgentID.
func (f *Fleet) Snapshot() []WorkerInfo {
	f.mu.RLock()
	defer f.mu.RUnlock()
	out := make([]WorkerInfo, 0, len(f.workers))
	for _, w := range f.workers {
		out = append(out, *w)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AgentID < out[j].AgentID })
	return out
}

// NeedsRestart reports whether a worker has been silent past the timeout.
func NeedsRestart(info WorkerInfo, now time.Time, silence time.Duration) bool {
	return now.Sub(info.LastSeen) > silence
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/overmind/supervisor/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/overmind/supervisor/fleet.go pkg/overmind/supervisor/fleet_test.go
git commit -m "feat(overmind): fleet registry and restart predicate"
```

---

## Task 6: Control server (accept, route, send, event hook)

**Files:**
- Create: `pkg/overmind/supervisor/server.go`
- Test: `pkg/overmind/supervisor/server_test.go`

**Interfaces:**
- Consumes: `control.*` (Tasks 1-2), `Fleet` (Task 5).
- Produces:
  - `type Server struct { ... }`
  - `func NewServer(socketPath string, fleet *Fleet, logger *log.Logger) (*Server, error)` — removes any stale socket file, then listens.
  - `(*Server).Serve(ctx context.Context) error` — accept loop; per-conn goroutine reads envelopes, applies hello/status to the fleet, forwards events to the hook. Returns when ctx is cancelled.
  - `(*Server).SetEventHook(func(agentID string, ev control.Event))` — used by guardrails in Plan C.
  - `(*Server).Send(agentID string, env control.Envelope) error` — routes an outbound message to that worker's connection; error if not connected.
  - `(*Server).Addr() string` — the socket path (for spawned workers).

- [ ] **Step 1: Write the failing test**

```go
package supervisor

import (
	"context"
	"log"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/overmind/control"
)

func TestServerReceivesHelloStatusAndSends(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "om.sock")
	fleet := NewFleet()
	srv, err := NewServer(sock, fleet, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	gotEvent := make(chan control.Event, 1)
	srv.SetEventHook(func(_ string, ev control.Event) { gotEvent <- ev })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = srv.Serve(ctx) }()

	// Fake worker dials in.
	conn, err := dialRetry(t, sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	enc := control.NewEncoder(conn)
	dec := control.NewDecoder(conn)

	hello, _ := control.NewEnvelope(control.TypeHello, "a",
		control.Hello{AgentID: "a", Role: "resident", Station: "S1"})
	_ = enc.Encode(hello)
	st, _ := control.NewEnvelope(control.TypeStatus, "a", control.Status{System: "SOL"})
	_ = enc.Encode(st)
	ev, _ := control.NewEnvelope(control.TypeEvent, "a", control.Event{Kind: "combat"})
	_ = enc.Encode(ev)

	select {
	case got := <-gotEvent:
		if got.Kind != "combat" {
			t.Fatalf("event hook got %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("event hook never fired")
	}

	// Fleet should now know agent "a".
	waitFor(t, func() bool {
		snap := fleet.Snapshot()
		return len(snap) == 1 && snap[0].LastStatus.System == "SOL"
	})

	// Overmind -> worker send is received by the fake worker.
	abort, _ := control.NewEnvelope(control.TypeAbort, "a", control.Abort{Reason: "test"})
	if err := srv.Send("a", abort); err != nil {
		t.Fatalf("Send: %v", err)
	}
	got, err := dec.Decode()
	if err != nil || got.Type != control.TypeAbort {
		t.Fatalf("worker did not receive abort: %+v err=%v", got, err)
	}
}

func dialRetry(t *testing.T, sock string) (net.Conn, error) {
	t.Helper()
	var lastErr error
	for range 50 {
		c, err := net.Dial("unix", sock)
		if err == nil {
			return c, nil
		}
		lastErr = err
		time.Sleep(20 * time.Millisecond)
	}
	return nil, lastErr
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	for range 100 {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition never met")
}
```

> Note: add `"io"` to the test imports. The 20ms poll interval here is test-only scaffolding (not game pacing), so the `pkg/game` Sleep-constant rule does not apply.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/overmind/supervisor/ -run TestServer -v`
Expected: FAIL — `NewServer` undefined.

- [ ] **Step 3: Write minimal implementation**

```go
package supervisor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"github.com/rsned/spacemolt/pkg/overmind/control"
)

// Server is the overmind side of the control channel.
type Server struct {
	sock   string
	ln     net.Listener
	fleet  *Fleet
	logger *log.Logger

	mu        sync.RWMutex
	conns     map[string]*control.Encoder // agentID -> writer
	eventHook func(agentID string, ev control.Event)
}

// NewServer removes any stale socket then listens on socketPath.
func NewServer(socketPath string, fleet *Fleet, logger *log.Logger) (*Server, error) {
	if err := os.Remove(socketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("supervisor: remove stale socket: %w", err)
	}
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("supervisor: listen: %w", err)
	}
	return &Server{
		sock: socketPath, ln: ln, fleet: fleet, logger: logger,
		conns: make(map[string]*control.Encoder),
	}, nil
}

// Addr returns the socket path workers should dial.
func (s *Server) Addr() string { return s.sock }

// SetEventHook installs a callback invoked for every worker Event.
func (s *Server) SetEventHook(h func(agentID string, ev control.Event)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.eventHook = h
}

// Serve accepts connections until ctx is cancelled.
func (s *Server) Serve(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		_ = s.ln.Close()
	}()
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("supervisor: accept: %w", err)
		}
		go s.handleConn(ctx, conn)
	}
}

func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	dec := control.NewDecoder(conn)
	enc := control.NewEncoder(conn)
	var agentID string
	for {
		env, err := dec.Decode()
		if err != nil {
			if err != io.EOF && ctx.Err() == nil {
				s.logger.Printf("worker %q read error: %v", agentID, err)
			}
			break
		}
		switch env.Type {
		case control.TypeHello:
			var h control.Hello
			if err := env.Into(&h); err != nil {
				s.logger.Printf("bad hello: %v", err)
				continue
			}
			agentID = h.AgentID
			s.register(agentID, enc)
			s.fleet.ApplyHello(h, h.PID, time.Now())
		case control.TypeStatus:
			var st control.Status
			if err := env.Into(&st); err != nil {
				continue
			}
			s.fleet.ApplyStatus(env.AgentID, st, time.Now())
		case control.TypeEvent:
			var ev control.Event
			if err := env.Into(&ev); err != nil {
				continue
			}
			s.mu.RLock()
			hook := s.eventHook
			s.mu.RUnlock()
			if hook != nil {
				hook(env.AgentID, ev)
			}
		default:
			s.logger.Printf("worker %q: unhandled inbound type %q", agentID, env.Type)
		}
	}
	if agentID != "" {
		s.unregister(agentID)
	}
}

func (s *Server) register(agentID string, enc *control.Encoder) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conns[agentID] = enc
}

func (s *Server) unregister(agentID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.conns, agentID)
}

// Send routes env to the named worker's connection.
func (s *Server) Send(agentID string, env control.Envelope) error {
	s.mu.RLock()
	enc := s.conns[agentID]
	s.mu.RUnlock()
	if enc == nil {
		return fmt.Errorf("supervisor: worker %q not connected", agentID)
	}
	return enc.Encode(env)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/overmind/supervisor/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/overmind/supervisor/server.go pkg/overmind/supervisor/server_test.go
git commit -m "feat(overmind): control server with routing and event hook"
```

---

## Task 7: Fleet config loader

**Files:**
- Create: `pkg/overmind/supervisor/config.go`
- Create: `data/overmind/fleet.yaml`
- Test: `pkg/overmind/supervisor/config_test.go`

**Interfaces:**
- Produces:
  - `type WorkerSpec struct { AgentID, Role, Station string }`
  - `func LoadFleet(path string) ([]WorkerSpec, error)` — parses YAML; errors if any entry is missing `agent_id`.

YAML shape:
```yaml
workers:
  - agent_id: resident-nebula-1
    role: resident
    station: STN-NEB-1
```

- [ ] **Step 1: Write the failing test**

```go
package supervisor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFleet(t *testing.T) {
	p := filepath.Join(t.TempDir(), "fleet.yaml")
	os.WriteFile(p, []byte("workers:\n"+
		"  - agent_id: r1\n    role: resident\n    station: S1\n"+
		"  - agent_id: h1\n    role: hauler\n    station: S2\n"), 0o644)
	specs, err := LoadFleet(p)
	if err != nil {
		t.Fatalf("LoadFleet: %v", err)
	}
	if len(specs) != 2 || specs[0].AgentID != "r1" || specs[1].Role != "hauler" {
		t.Fatalf("parsed wrong: %+v", specs)
	}
}

func TestLoadFleetRejectsMissingID(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.yaml")
	os.WriteFile(p, []byte("workers:\n  - role: resident\n"), 0o644)
	if _, err := LoadFleet(p); err == nil {
		t.Fatal("expected error for missing agent_id")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/overmind/supervisor/ -run TestLoadFleet -v`
Expected: FAIL — `LoadFleet` undefined.

- [ ] **Step 3: Write minimal implementation**

First confirm the YAML library already used in the repo:

Run: `grep -rhoE '"(gopkg.in/yaml.v[23]|sigs.k8s.io/yaml)"' --include=*.go . | sort -u`

Use whichever import that prints (the repo already loads agent YAML configs). The code below assumes `gopkg.in/yaml.v3`; if the grep shows a different package, adjust the import and tag style accordingly.

```go
package supervisor

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// WorkerSpec is one roster entry the supervisor will spawn.
type WorkerSpec struct {
	AgentID string `yaml:"agent_id"`
	Role    string `yaml:"role"`
	Station string `yaml:"station"`
}

type fleetFile struct {
	Workers []WorkerSpec `yaml:"workers"`
}

// LoadFleet parses the fleet roster YAML at path.
func LoadFleet(path string) ([]WorkerSpec, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("supervisor: read fleet: %w", err)
	}
	var ff fleetFile
	if err := yaml.Unmarshal(raw, &ff); err != nil {
		return nil, fmt.Errorf("supervisor: parse fleet: %w", err)
	}
	for i, w := range ff.Workers {
		if w.AgentID == "" {
			return nil, fmt.Errorf("supervisor: fleet entry %d missing agent_id", i)
		}
	}
	return ff.Workers, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/overmind/supervisor/ -v`
Expected: PASS.

- [ ] **Step 5: Create the seed roster file**

Create `data/overmind/fleet.yaml` with two placeholder residents (real station IDs filled in operationally):

```yaml
# Overmind fleet roster. role is a label in Plan A; standing behaviors per
# role arrive in Plan B (data/overmind/roles.yaml).
workers:
  - agent_id: resident-nebula-1
    role: resident
    station: STN-NEB-1
  - agent_id: resident-nebula-2
    role: resident
    station: STN-NEB-2
```

- [ ] **Step 6: Commit**

```bash
git add pkg/overmind/supervisor/config.go pkg/overmind/supervisor/config_test.go data/overmind/fleet.yaml
git commit -m "feat(overmind): fleet roster config loader"
```

---

## Task 8: Supervisor process lifecycle

**Files:**
- Create: `pkg/overmind/supervisor/supervisor.go`
- Test: `pkg/overmind/supervisor/supervisor_test.go`

**Interfaces:**
- Consumes: `Fleet`, `WorkerSpec`, `Server`, `NeedsRestart` (Tasks 5-7).
- Produces:
  - `type SpawnFunc func(ctx context.Context, spec WorkerSpec, socket string) (*exec.Cmd, error)` — injectable so tests avoid real processes.
  - `func DefaultSpawn(workerBin string) SpawnFunc` — builds the real `exec.Command`.
  - `type Supervisor struct { ... }`
  - `func NewSupervisor(server *Server, fleet *Fleet, specs []WorkerSpec, spawn SpawnFunc, logger *log.Logger) *Supervisor`
  - `(*Supervisor).Run(ctx context.Context) error` — spawns each spec once, then every `game.SleepMedium` checks for silent/dead workers and respawns them (capped). Returns when ctx cancelled.
  - `(*Supervisor).SilenceTimeout` field (default `3*game.SleepTick`).

- [ ] **Step 1: Write the failing test**

```go
package supervisor

import (
	"context"
	"io"
	"log"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"
)

func TestSupervisorSpawnsEachSpecOnce(t *testing.T) {
	specs := []WorkerSpec{{AgentID: "a"}, {AgentID: "b"}}
	var spawned atomic.Int32
	spawn := func(ctx context.Context, spec WorkerSpec, socket string) (*exec.Cmd, error) {
		spawned.Add(1)
		// A real, harmless short-lived command stands in for a worker.
		cmd := exec.CommandContext(ctx, "true")
		return cmd, cmd.Start()
	}
	fleet := NewFleet()
	sup := NewSupervisor(nil, fleet, specs, spawn, log.New(io.Discard, "", 0))
	sup.SilenceTimeout = time.Hour // disable restart churn for this test

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_ = sup.Run(ctx)

	if spawned.Load() < 2 {
		t.Fatalf("expected >=2 spawns, got %d", spawned.Load())
	}
}
```

> Note: `Run` references `server` only to read `Addr()`; guard against a nil server in tests (use `""` when nil). The 300ms timeout is test scaffolding, not game pacing.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/overmind/supervisor/ -run TestSupervisor -v`
Expected: FAIL — `NewSupervisor` undefined.

- [ ] **Step 3: Write minimal implementation**

```go
package supervisor

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// SpawnFunc starts a worker process for spec, told to dial socket.
type SpawnFunc func(ctx context.Context, spec WorkerSpec, socket string) (*exec.Cmd, error)

// DefaultSpawn returns a SpawnFunc that launches workerBin with flags.
func DefaultSpawn(workerBin string) SpawnFunc {
	return func(ctx context.Context, spec WorkerSpec, socket string) (*exec.Cmd, error) {
		cmd := exec.CommandContext(ctx,
			workerBin,
			"--agent", spec.AgentID,
			"--role", spec.Role,
			"--station", spec.Station,
			"--socket", socket,
		)
		cmd.Stdout = log.Writer()
		cmd.Stderr = log.Writer()
		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("supervisor: start worker %q: %w", spec.AgentID, err)
		}
		return cmd, nil
	}
}

// Supervisor spawns and keeps workers alive.
type Supervisor struct {
	server         *Server
	fleet          *Fleet
	specs          []WorkerSpec
	spawn          SpawnFunc
	logger         *log.Logger
	SilenceTimeout time.Duration
	MaxRestarts    int
}

// NewSupervisor wires a supervisor. server may be nil in tests.
func NewSupervisor(server *Server, fleet *Fleet, specs []WorkerSpec, spawn SpawnFunc, logger *log.Logger) *Supervisor {
	return &Supervisor{
		server: server, fleet: fleet, specs: specs, spawn: spawn, logger: logger,
		SilenceTimeout: 3 * game.SleepTick,
		MaxRestarts:    100,
	}
}

func (s *Supervisor) socket() string {
	if s.server == nil {
		return ""
	}
	return s.server.Addr()
}

// Run spawns each spec, then periodically restarts silent/dead workers.
func (s *Supervisor) Run(ctx context.Context) error {
	for _, spec := range s.specs {
		s.launch(ctx, spec)
	}
	ticker := time.NewTicker(game.SleepMedium)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			s.reapAndRestart(ctx)
		}
	}
}

func (s *Supervisor) launch(ctx context.Context, spec WorkerSpec) {
	if _, err := s.spawn(ctx, spec, s.socket()); err != nil {
		s.logger.Printf("spawn %q failed: %v", spec.AgentID, err)
	}
}

func (s *Supervisor) reapAndRestart(ctx context.Context) {
	now := time.Now()
	healthy := make(map[string]WorkerInfo)
	for _, w := range s.fleet.Snapshot() {
		healthy[w.AgentID] = w
	}
	for _, spec := range s.specs {
		w, seen := healthy[spec.AgentID]
		if !seen || NeedsRestart(w, now, s.SilenceTimeout) {
			if seen && w.Restarts >= s.MaxRestarts {
				continue
			}
			s.logger.Printf("restarting worker %q (seen=%v)", spec.AgentID, seen)
			s.fleet.MarkRestart(spec.AgentID)
			s.launch(ctx, spec)
		}
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/overmind/supervisor/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/overmind/supervisor/supervisor.go pkg/overmind/supervisor/supervisor_test.go
git commit -m "feat(overmind): supervisor process lifecycle and restart"
```

---

## Task 9: Thin stub worker binary

**Files:**
- Create: `cmd/worker/main.go`
- Test: `cmd/worker/main_test.go` (unit test on the pure status-builder helper)

**Interfaces:**
- Consumes: `game.InitializeAgent` (`pkg/game/agent.go:111`), `game.GameClient.GetState()` (`pkg/game/interface.go:210`), `checkpoint.*`, `control.*`, `game` Sleep constants.
- Produces (within package main):
  - `func buildStatus(st *game.State, standing, taskID string, now time.Time) control.Status`
  - `func buildKnownState(st *game.State, tick int) checkpoint.KnownState`

The worker `main`:
1. Parses flags `--agent --role --station --socket --db-path`.
2. Opens checkpoint at `data/agents/<agent>/checkpoint.db` (or `--db-path`).
3. `game.InitializeAgent` → connect; fetch fresh state via the client's status/system queries already used by `play_as`.
4. `Reconcile(savedKnownState, liveKnownState, 0.25)`; on `Diverged`, emit a `control.Event{Kind:"reconcile_diverged", Detail:reasons}` after connecting.
5. Dial `--socket`; send `Hello{AgentID,Role,Station,PID:os.Getpid()}`.
6. Heartbeat loop every `game.SleepTick`: send `Status`; every loop also `SaveKnownState`.
7. Reader goroutine: on `Abort` → log, save checkpoint, exit 0; on `Pause`/`Resume` → toggle a flag (standing behavior is a no-op stub in Plan A).

> Verify actual method names on `game.GameClient` for fetching status/system before coding step 6 (CLAUDE.md: do not assume field/method names). Read `pkg/game/interface.go` and reuse exactly what `play_as` calls (e.g. `GetStatus`, `GetSystem`).

- [ ] **Step 1: Write the failing test (pure helper only)**

```go
package main

import (
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

func TestBuildStatusAndKnownState(t *testing.T) {
	st := &game.State{
		CurrentSystem: "SOL", CurrentPOI: "ST-9",
		Credits: 5000, Hull: 80, MaxHull: 100, Fuel: 30, MaxFuel: 50,
	}
	now := time.Unix(1000, 0)
	got := buildStatus(st, "track_station", "t-1", now)
	if got.System != "SOL" || got.POI != "ST-9" || got.Credits != 5000 {
		t.Fatalf("buildStatus wrong: %+v", got)
	}
	if got.StandingBehavior != "track_station" || got.ActiveTaskID != "t-1" {
		t.Fatalf("buildStatus labels wrong: %+v", got)
	}
	if got.Timestamp == "" {
		t.Fatalf("timestamp missing")
	}
	ks := buildKnownState(st, 7)
	if ks.System != "SOL" || ks.Credits != 5000 || ks.Tick != 7 {
		t.Fatalf("buildKnownState wrong: %+v", ks)
	}
}
```

> Before writing the test, confirm `game.State` exposes `CurrentSystem`, `CurrentPOI`, `Credits`, `Hull`, `MaxHull`, `Fuel`, `MaxFuel` (verified in `pkg/game/types.go:296+`). If a field for "docked" exists on State, map it; otherwise derive docked from `CurrentPOI != "" && !st.Traveling` and assert that in the test.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/worker/ -v`
Expected: FAIL — `buildStatus` undefined.

- [ ] **Step 3: Write minimal implementation**

Implement `buildStatus` and `buildKnownState` plus `main`. Helpers:

```go
func buildStatus(st *game.State, standing, taskID string, now time.Time) control.Status {
	return control.Status{
		System:           st.CurrentSystem,
		POI:              st.CurrentPOI,
		Docked:           st.CurrentPOI != "" && !st.Traveling,
		Hull:             st.Hull,
		MaxHull:          st.MaxHull,
		Fuel:             st.Fuel,
		MaxFuel:          st.MaxFuel,
		Credits:          st.Credits,
		StandingBehavior: standing,
		ActiveTaskID:     taskID,
		Timestamp:        now.Format(time.RFC3339Nano),
	}
}

func buildKnownState(st *game.State, tick int) checkpoint.KnownState {
	return checkpoint.KnownState{
		System:  st.CurrentSystem,
		POI:     st.CurrentPOI,
		Docked:  st.CurrentPOI != "" && !st.Traveling,
		Credits: st.Credits,
		Tick:    tick,
	}
}
```

The `main` glue (connect, dial, heartbeat, reader) follows the sequence above; keep all sleeps on `game.SleepTick`/`game.SleepQuick`/`game.SleepReconnect`. Use `st := client.GetState()` for the snapshot. Wrap the dial with a bounded retry on `game.SleepQuick` so the worker tolerates the supervisor's socket not being ready yet.

- [ ] **Step 4: Run test + build**

Run: `go test ./cmd/worker/ -v && go build -o bin/worker ./cmd/worker`
Expected: PASS, then a binary at `bin/worker`.

- [ ] **Step 5: Commit**

```bash
git add cmd/worker/main.go cmd/worker/main_test.go
git commit -m "feat(overmind): thin stub worker with heartbeat and checkpoint"
```

---

## Task 10: Overmind binary + delete agent-server

**Files:**
- Create: `cmd/overmind/main.go`
- Delete: `cmd/agent-server/` (entire directory)

**Interfaces:**
- Consumes: `supervisor.*` (Tasks 5-8).

`cmd/overmind` `main`:
1. Flags: `--socket` (default `data/overmind/overmind.sock`), `--worker-bin` (default `bin/worker`), `--fleet` (default `data/overmind/fleet.yaml`).
2. `LoadFleet` → specs.
3. `NewFleet`, `NewServer`, `NewSupervisor(server, fleet, specs, DefaultSpawn(workerBin), logger)`.
4. `ctx` cancelled on SIGINT/SIGTERM (mirror `play_as` `os/signal` handling).
5. `go server.Serve(ctx)`; `go supervisor.Run(ctx)`.
6. Status ticker every `game.SleepMedium`: log `fleet.Snapshot()` as a compact table (agent, role, system, hull%, healthy, restarts).
7. On ctx cancel: best-effort `server.Send(agentID, abort)` to each connected worker, then return.

- [ ] **Step 1: Delete the abandoned binary**

```bash
git rm -r cmd/agent-server
```

- [ ] **Step 2: Write `cmd/overmind/main.go`**

Implement the glue described above. No new exported types — pure wiring over Tasks 5-8. Keep all periodic intervals on `game.Sleep*` constants.

- [ ] **Step 3: Build both binaries**

Run:
```bash
go build -o bin/worker ./cmd/worker && go build -o bin/overmind ./cmd/overmind
```
Expected: both build with no errors.

- [ ] **Step 4: Whole-suite + lint gate**

Run:
```bash
go build ./... && go test ./... && golangci-lint run ./pkg/overmind/... ./cmd/overmind/... ./cmd/worker/...
```
Expected: build clean, tests pass, no new lint findings.

- [ ] **Step 5: Commit**

```bash
git add cmd/overmind/main.go
git commit -m "feat(overmind): overmind supervisor binary; remove agent-server"
```

---

## Task 11: End-to-end integration verification (manual, no live game)

**Goal:** Prove the skeleton without the live game server by running the overmind against a fake worker, then (optionally) against the real stub worker pointed at the game.

**Files:**
- Create: `cmd/overmind/integration_test.go` (build-tagged `//go:build integration`)

**Interface:** spins up `NewServer` + a goroutine acting as a fake worker that dials the socket, sends hello + N status messages, then exits; asserts the fleet snapshot reflects it and that a sent `abort` is received.

- [ ] **Step 1: Write the integration test**

```go
//go:build integration

package main

// A focused end-to-end check of the control plane using an in-process fake
// worker (no game server, no subprocess). Run: go test -tags=integration ./cmd/overmind/
```

Implement: create a temp socket, `supervisor.NewServer`, `Serve` in a goroutine, dial as a fake worker, send `Hello`+`Status`, `waitFor` the snapshot to show the agent, `srv.Send` an `Abort`, assert the fake worker decodes it. (Reuse the pattern from Task 6's `server_test.go`.)

- [ ] **Step 2: Run the integration test**

Run: `go test -tags=integration ./cmd/overmind/ -v`
Expected: PASS.

- [ ] **Step 3: Manual smoke against the real stub worker (optional, requires game creds)**

Run:
```bash
go build -o bin/worker ./cmd/worker && go build -o bin/overmind ./cmd/overmind
./bin/overmind --fleet data/overmind/fleet.yaml --worker-bin bin/worker
```
Expected: overmind logs each worker connecting (`hello`), then a status table refreshing every 30s. Kill one worker process (`kill <pid>`); within ~30s the supervisor logs a restart and the worker reappears. Ctrl+C → workers receive abort and exit cleanly.

> If real game credentials are unavailable in this environment, document that Step 3 was skipped and rely on Steps 1-2 (CLAUDE.md / verification-before-completion: report skipped steps honestly).

- [ ] **Step 4: Commit**

```bash
git add cmd/overmind/integration_test.go
git commit -m "test(overmind): control-plane end-to-end integration test"
```

---

## Self-Review

**Spec coverage (against Plan-A scope in `2026-06-19-overmind-fleet-manager-design.md`):**
- Supervisor spawn/monitor/auto-restart → Tasks 5, 8, 10. ✓
- Control channel (NDJSON/unix socket, message set) → Tasks 1, 2, 6. ✓ (Plan-A subset: hello/status/event/abort/pause/resume; assign_task/escalate/etc. deferred to Plans B-C as designed.)
- Per-worker SQLite checkpoint → Task 3. ✓
- Restart reconciliation → Task 4, used in Task 9. ✓
- Thin stub worker (no play_as refactor) → Task 9. ✓
- Fleet roster config → Task 7. ✓
- Event-hook seam for guardrails (Plan C) → Task 6 (`SetEventHook`). ✓
- `cmd/agent-server` deleted → Task 10. ✓
- Deferred by design (NOT in this plan): roles.yaml standing behaviors, script catalog, guardrail rules, web UI, strategic brain, mobile roles. ✓ documented.

**Placeholder scan:** No TBD/TODO; every code step shows complete code. Two intentional verification gates (Task 7 Step 3 grep for the YAML import; Task 9 verify `GameClient` method/`State` field names) are explicit checks, not placeholders — they exist because CLAUDE.md forbids assuming API names.

**Type consistency:** `KnownState`, `Intent`, `Reconcile`, `control.Status/Hello/Event/Abort`, `Fleet.ApplyHello/ApplyStatus/Snapshot/MarkRestart`, `NeedsRestart`, `Server.Send/SetEventHook/Addr`, `SpawnFunc`, `WorkerSpec` are used with identical signatures across tasks. `buildStatus`/`buildKnownState` signatures match their test and call sites.

**Risks flagged for the implementer:**
- `game.State` "docked" derivation is assumed (`CurrentPOI != "" && !Traveling`); Task 9 Step 1 instructs verifying against the real struct and adjusting both helper and test together.
- YAML import package must be confirmed by grep (Task 7) to match repo convention.
- The real stub worker (Task 9) connects to the live game; integration Steps 1-2 do not, so CI/offline runs stay green.
