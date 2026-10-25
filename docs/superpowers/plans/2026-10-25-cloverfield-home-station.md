# Cloverfield Home Station Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give play_as a recorder that writes every command and its raw server reply into a per-station record set, then use it while founding Cloverfield by hand and getting it to the "we live here" milestone.

**Architecture:** Tasks 1–3 are a small amount of Go in `cmd/tools/play_as`: a `--record-dir` flag that wraps `executeCommand`, a `station_snapshot` command that runs a fixed set of reads under one snapshot id, and a `ledger` command for categorised money and item movements. Task 4 sets up the record set's runbook and findings files. Tasks 5–9 are hands-on game operations driven through play_as by the user and Claude together. Each has a gate, and no code is written in them.

**Tech Stack:** Go 1.24+, play_as REPL, JSONL files under `data/stations/cloverfield/`.

**Spec:** `docs/superpowers/specs/2026-10-25-cloverfield-home-station-design.md`

## Global Constraints

- play_as is run only via `go run ./cmd/tools/play_as` (never bin/); the account is **craftsman-1** (the user's manual account).
- Treasury floor **1,000,000 cr** at all times; founding needs **≥6M** in treasury first; shortfall is sent by **the_banker**.
- Consumable/stockpile purchases **≤1,000,000 cr total** without the user's OK.
- **Every facility build is planned and approved by the user first** (cost, materials and their source, expected maintenance).
- Never route craftsman-1 or any convoy ship through a pirate stronghold; energy crystals are reserved.
- Do **not** run `set_home_base` / change faction home in this plan.
- Record field names exactly as the server returns them; every finding is tagged *measured* or *doc-quoted* with its date.
- Any sleep in client code uses a Sleep constant from `pkg/game/constants.go`.
- After code changes: `go build ./...`, `go test ./cmd/tools/play_as/...`, `golangci-lint run ./cmd/tools/play_as/...` with no new findings.
- Never `git add -A`: the tree has dirty `data/agents/*` files. Stage paths explicitly.

## Review Focus

- **Recorder write failure** (dir missing, disk full): the game command's result must still be returned and printed. A recording error is printed as a note and never fails the command. → test in Task 1.
- **Command that errors server-side:** the event must still be written, with `error` set and the raw body taken from `_last_error`. Failures are the most valuable records. → test in Task 1.
- **Recorder off (`--record-dir ""`, the default):** no file is created and behaviour is unchanged. → test in Task 1.
- **Unknown ledger category or non-integer quantity:** rejected with a usage message, and nothing is appended. → test in Task 3.
- **One read inside `station_snapshot` fails** (e.g. `station info` before founding): the remaining reads still run and the failure is recorded. → test in Task 2.

---

### Task 1: `--record-dir` recorder in play_as

**Files:**
- Create: `cmd/tools/play_as/station_record.go`
- Create: `cmd/tools/play_as/station_record_test.go`
- Modify: `cmd/tools/play_as/main.go` (flag block ~line 92–116; `executeCommand` at ~6831)

**Interfaces:**
- Produces:
  - `var globalRecordDir string` (set from flag `--record-dir`, default `""`)
  - `type stationEvent struct { ObservedUTC string "json:\"observed_utc\""; Tick int64 "json:\"tick,omitempty\""; AgentID string "json:\"agent_id\""; Command string "json:\"command\""; Args []string "json:\"args,omitempty\""; SnapshotID string "json:\"snapshot_id,omitempty\""; Error string "json:\"error,omitempty\""; Raw json.RawMessage "json:\"raw,omitempty\"" }`
  - `func appendStationEvent(dir string, ev stationEvent) error`: appends one line to `<dir>/events.jsonl` and creates the dir if needed. If `dir == ""` it does nothing and returns nil.
  - `func rawForRecord(client game.GameClient, cmd string, cmdErr error) json.RawMessage`: on error, `_last_error`; otherwise `rawJSONKeyForCommand[cmd]` if present, else `cmd`, else `_last`. If the bytes aren't valid JSON, wrap them as a JSON string.
  - `var globalSnapshotID string` (Task 2 sets it; copied into each event)

- [ ] **Step 1: Write failing tests** in `station_record_test.go`:
  - `TestAppendStationEvent_WritesJSONL`: two appends to `t.TempDir()`; file has 2 lines; line 2 unmarshals with `Command=="facility"`, `Args==["list"]`, `Tick==123`, `Raw` equal to the input JSON.
  - `TestAppendStationEvent_EmptyDirNoop`: `appendStationEvent("", ev)` returns nil, and nothing is created in cwd (run inside `t.Chdir(t.TempDir())`, assert dir empty).
  - `TestRawForRecord_ErrorUsesLastError`: stub client whose `GetRawJSON("_last_error")` returns `{"error":"no_faction_storage"}`; with a non-nil err, the result equals that.
  - `TestRawForRecord_NonJSONWrapped`: `GetRawJSON` returns `not json`; the result is `"not json"` (valid JSON string).
  - `TestRecordedExecute_RecordErrorDoesNotFailCommand`: with `globalRecordDir` pointing at a path under a *file* (so MkdirAll fails), `recordExecution(...)` returns the command's own nil error.
- [ ] **Step 2: Run** `go test ./cmd/tools/play_as/ -run 'StationEvent|RawForRecord|RecordedExecute'`. Expected: FAIL (undefined).
- [ ] **Step 3: Implement** in `station_record.go`, plus `func recordExecution(client game.GameClient, parts []string, cmdErr error) error`. It builds the event (tick from `client.GetState().CurrentTick`, agent `globalAgentID`, `globalSnapshotID`), calls `appendStationEvent(globalRecordDir, …)`, prints `(station record: %v)` on failure, and returns `cmdErr` unchanged. In `main.go`: add the flag `record-dir` ("Append every command and its raw reply to <dir>/events.jsonl (station record set); empty disables"), assign `globalRecordDir`, and wrap: rename the existing `executeCommand` → `executeCommandInner`, and make a new `executeCommand` that resolves nothing itself, calls the inner function, then `return recordExecution(client, parts, err)` when `globalRecordDir != ""`. Skip recording for `sleep`/`wait`/`raw`/`ledger`/`station_snapshot` (the snapshot's reads are recorded individually).
- [ ] **Step 4: Run** the same tests → PASS; `go build ./... && go test ./cmd/tools/play_as/... && golangci-lint run ./cmd/tools/play_as/...` → clean.
- [ ] **Step 5: Commit** `git add cmd/tools/play_as/station_record.go cmd/tools/play_as/station_record_test.go cmd/tools/play_as/main.go && git commit -m "feat(play_as): --record-dir appends every command + raw reply to a station record set"`

### Task 2: `station_snapshot` command

**Files:**
- Modify: `cmd/tools/play_as/station_record.go`, `station_record_test.go`, `main.go` (add `case "station_snapshot":` in `executeCommandInner`)

**Interfaces:**
- Consumes: `globalSnapshotID`, `executeCommand` (Task 1)
- Produces: `func snapshotCommands() [][]string` returning exactly, in order:
  `{"get_status"}`, `{"faction_info"}`, `{"view_faction_storage"}`, `{"station","info"}`, `{"facility","list"}`, `{"facility","faction_list"}`

- [ ] **Step 1: Failing tests:**
  - `TestSnapshotCommands_Order`: equals the list above.
  - `TestRunSnapshot_ContinuesAfterFailure`: `runSnapshot(exec func([]string) error)` with an exec that fails on `station info`. All 6 are called, the returned error mentions `station info`, and `globalSnapshotID` is non-empty during every call and `""` afterwards.
- [ ] **Step 2: Run** → FAIL.
- [ ] **Step 3: Implement** `func runSnapshot(exec func([]string) error) error`. Set `globalSnapshotID` to `time.Now().UTC().Format("20060102T150405Z")`, defer reset, run all commands, and join the errors with `errors.Join`. Wire `case "station_snapshot":` to call it with a closure over `executeCommand(client, ctx, p, format)`. Add one line to the play_as help text.
- [ ] **Step 4: Run** tests, build, lint → clean.
- [ ] **Step 5: Commit** `feat(play_as): station_snapshot runs the station reads under one snapshot id`

### Task 3: `ledger` command

**Files:**
- Modify: `cmd/tools/play_as/station_record.go`, `station_record_test.go`, `main.go` (`case "ledger":`)

**Interfaces:**
- Produces:
  - `var ledgerCategories = []string{"founding","build","maintenance-items","maintenance-labor","ammo","haul-in","purchase","revenue","transfer"}`
  - `type ledgerEntry struct { ObservedUTC, AgentID, Category, Item string; Qty int; Credits int; Note string }` (json tags snake_case; `Item` is `"credits"` for money)
  - `func parseLedgerArgs(args []string) (ledgerEntry, error)`. Usage: `ledger <category> <item|credits> <qty> [note…]`. A negative qty means out of the station/treasury. For `credits`, `Credits=qty`.
  - `func appendLedger(dir string, e ledgerEntry) error` → `<dir>/ledger.jsonl`

- [ ] **Step 1: Failing tests:** `TestParseLedger_Valid` (`ammo ferrous_slug_case 500 convoy one` → Category `ammo`, Qty 500, Note `convoy one`); `TestParseLedger_Credits` (`founding credits -5000000` → Credits -5000000); `TestParseLedger_BadCategory` and `TestParseLedger_BadQty` both return errors containing `usage: ledger`; `TestLedgerCommand_NoAppendOnError` (bad args → `ledger.jsonl` not created).
- [ ] **Step 2: Run** → FAIL.
- [ ] **Step 3: Implement**; the command requires `globalRecordDir != ""` (error: `ledger needs --record-dir`).
- [ ] **Step 4: Run** tests, build, lint → clean.
- [ ] **Step 5: Commit** `feat(play_as): ledger records categorised credit/item movements for a station`

### Task 4: Record set and runbook

**Files:**
- Create: `data/stations/cloverfield/README.md`: what each file is (`events.jsonl`, `ledger.jsonl`, `findings.md`), the play_as invocation `go run ./cmd/tools/play_as --record-dir data/stations/cloverfield craftsman-1`, and the snapshot cadence (start and end of every session, and at least hourly during one).
- Create: `data/stations/cloverfield/findings.md`: headings `What comes with a station`, `Founding`, `Maintenance (per facility, per cycle)`, `Power & life support`, `Defense`, `Costs we paid`, `Doc contradictions`. Seed the last one with the `found_station` vs `build_base` conflict (*doc-quoted*, 2026-10-25).

- [ ] **Step 1:** Write both files. Confirm `git check-ignore data/stations/cloverfield/findings.md` prints nothing.
- [ ] **Step 2: Commit** `git add data/stations/cloverfield && git commit -m "docs(station): Cloverfield record set + runbook"`

---

Tasks 5–9 are **by hand, with the user**. Each one ends at a gate, and the next starts only after the user's go-ahead. Record everything with `--record-dir data/stations/cloverfield`. After each task, add findings to `findings.md` and commit `data/stations/cloverfield/` explicitly.

### Task 5: Phase 0 pre-flight (read-only)

- [ ] `station_snapshot`, then `get_base_cost`. Record the correct founding command, the requirements, and whether the core is consumed if stated.
- [ ] `facility types` for every category (faction, personal, production, service/infrastructure, defense). Paste the full catalogue into findings, or a `catalog-2026-10-25.json` next to it.
- [ ] Confirm faction, craftsman-1 role permissions (`manage_bases`, `manage_treasury`), and treasury ≥6M. If short, ask the user to have the_banker send the difference.
- [ ] Plan the route craftsman-1 → Cloverfield, avoiding strongholds.
- [ ] **Gate:** if anything fails (ineligible, someone has already founded there, unmeetable requirement), stop and report. Otherwise present the founding plan to the user.

### Task 6: Phase 1 stockpile and convoy

- [ ] Draft a shopping list from the Task 5 catalogue: fuel, power cells, coolant, steel, circuits, repair kits/armor plate, Ferrous Slug Case. Give quantities and reasoning, with the total ≤1M cr. **User approves.**
- [ ] Buy and load. `ledger purchase <item> <qty>` for each, and record the credits spent.
- [ ] Fly the convoy to Cloverfield; stay undocked at the chosen planet POI.

### Task 7: Phase 2 found and Phase 3 inventory

- [ ] Run the confirmed founding command. Then `ledger founding credits -<charged>` and `ledger founding station_core -1` if it was consumed. Record the station base id **and** POI id.
- [ ] Dock, `station_snapshot`, and deposit the whole stockpile into faction storage (each deposit gets `ledger haul-in`).
- [ ] Write up findings: what came with the station (services, facilities, power block, storage tier).

### Task 8: Phase 4 arm, then baseline

- [ ] Present the Point Defense Battery build plan (cost, materials, expected maintenance). **User approves.** Build it, then stock its ammo.
- [ ] Run the station with nothing else added for days, taking `station_snapshot` on the cadence. Each session, diff storage and treasury between snapshots, and record per-item consumption per cycle in findings (*measured*).
- [ ] Restock anything with **< 3 days of supply left**, staying within the purchase cap.

### Task 9: Phase 5 "we live here" milestone

- [ ] Add facilities one at a time. Each one gets a plan, user approval, the build, and a findings entry: cost, materials, build time, and **change in the maintenance bill**.
- [ ] Once the baseline shows which items maintenance consumes, write a small follow-up plan for the spec's "days of supply left" alert on the :8093 dashboard (alert below 3 days). Until it exists, check by hand daily.
- [ ] Milestone check: founded, docked, storage working, refuel and repair running, maintenance has ≥3 days of supply, PD battery armed, findings up to date. Report to the user. The guide (KB) and the HMI get their own brainstorm next.
