package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
	"github.com/rsned/spacemolt/pkg/game"
)

// Player stations are thinly documented: maintenance items and rates, what a
// fresh station ships with, and even the founding command name are only
// learned by doing it. --record-dir keeps every command and its raw reply
// (failures included -- they are the most informative) in a per-station
// record set, so the guide and the station console are built from what the
// server actually said rather than reconstructed from scrollback.
// See docs/superpowers/specs/2026-10-25-cloverfield-home-station-design.md.

// globalRecordDir is the station record set directory; empty disables recording.
var globalRecordDir string

// globalSnapshotID groups the reads of one station_snapshot; empty otherwise.
var globalSnapshotID string

// stationEvent is one line of <record-dir>/events.jsonl.
type stationEvent struct {
	ObservedUTC string          `json:"observed_utc"`
	Tick        int64           `json:"tick,omitempty"`
	AgentID     string          `json:"agent_id"`
	Command     string          `json:"command"`
	Args        []string        `json:"args,omitempty"`
	SnapshotID  string          `json:"snapshot_id,omitempty"`
	Error       string          `json:"error,omitempty"`
	Raw         json.RawMessage `json:"raw,omitempty"`
}

// unrecordedCommands are local or bookkeeping commands with no server reply
// worth keeping; station_snapshot's reads are recorded individually.
var unrecordedCommands = map[string]bool{
	"sleep": true, "wait": true, "raw": true, "ledger": true, "station_snapshot": true,
}

// appendJSONLine appends v as one JSON line to dir/name, creating dir.
func appendJSONLine(dir, name string, v any) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	line, err := json.Marshal(v)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// appendStationEvent appends ev to dir/events.jsonl; a no-op when dir is empty.
func appendStationEvent(dir string, ev stationEvent) error {
	if dir == "" {
		return nil
	}
	if ev.ObservedUTC == "" {
		ev.ObservedUTC = time.Now().UTC().Format(time.RFC3339)
	}
	return appendJSONLine(dir, "events.jsonl", ev)
}

// currentReply is the request-correlated server frame for the command being
// executed, as chosen by simpleCommand. executeCommand resets it per command.
// The recorder never reads the shared _last / _last_error / command-keyed raw
// slots: they are not cleared between commands and the chat poller writes
// them, so they can hold a previous command's reply.
var currentReply protocol.Response

// noteReply keeps r as the current command's reply when it is a real frame.
func noteReply(r protocol.Response) {
	if r.Type != "" && len(r.Payload) > 0 {
		currentReply = r
	}
}

// replyForRecord marshals a correlated frame's payload; nil when there is none.
func replyForRecord(r protocol.Response) json.RawMessage {
	if r.Type == "" || len(r.Payload) == 0 {
		return nil
	}
	raw, err := json.Marshal(r.Payload)
	if err != nil {
		quoted, _ := json.Marshal(fmt.Sprint(r.Payload))
		return quoted
	}
	return raw
}

// recordExecution writes the event for one executed command and returns
// cmdErr unchanged: a recording failure is reported, never propagated.
func recordExecution(client game.GameClient, parts []string, cmdErr error, raw json.RawMessage) error {
	if globalRecordDir == "" || len(parts) == 0 {
		return cmdErr
	}
	ev := stationEvent{
		AgentID:    globalAgentID,
		Command:    strings.ToLower(parts[0]),
		Args:       parts[1:],
		SnapshotID: globalSnapshotID,
		Raw:        raw,
	}
	if st := client.GetState(); st != nil {
		ev.Tick = st.CurrentTick
	}
	if cmdErr != nil {
		ev.Error = cmdErr.Error()
	}
	if err := appendStationEvent(globalRecordDir, ev); err != nil {
		fmt.Printf("(station record: %v)\n", err)
	}
	return cmdErr
}

// snapshotCommands are the reads one station_snapshot runs, in order.
func snapshotCommands() [][]string {
	return [][]string{
		{"get_status"},
		{"faction_info"},
		{"view_faction_storage"},
		{"station", "--action", "info"},
		{"facility", "list"},
		{"facility", "faction_list"},
	}
}

// runSnapshot runs every snapshot read under one shared snapshot id. A failing
// read (e.g. station info before founding) does not stop the rest; all
// failures come back joined.
func runSnapshot(exec func([]string) error) error {
	globalSnapshotID = time.Now().UTC().Format("20060102T150405Z")
	defer func() { globalSnapshotID = "" }()
	var errs []error
	for _, p := range snapshotCommands() {
		if err := exec(p); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", strings.Join(p, " "), err))
		}
	}
	return errors.Join(errs...)
}

// ledgerCategories are the accepted ledger entry categories.
var ledgerCategories = []string{
	"founding", "build", "maintenance-items", "maintenance-labor", "ammo",
	"haul-in", "purchase", "revenue", "transfer",
}

const ledgerUsage = "usage: ledger <category> <item|credits> <qty> [note...]  (negative qty = out; categories: %s)"

// ledgerEntry is one line of <record-dir>/ledger.jsonl: a categorised credit
// or item movement, the raw material for the station's finance books.
type ledgerEntry struct {
	ObservedUTC string `json:"observed_utc"`
	AgentID     string `json:"agent_id"`
	Category    string `json:"category"`
	Item        string `json:"item"`
	Qty         int    `json:"qty"`
	Credits     int    `json:"credits,omitempty"`
	Note        string `json:"note,omitempty"`
}

// parseLedgerArgs parses `<category> <item|credits> <qty> [note...]`.
func parseLedgerArgs(args []string) (ledgerEntry, error) {
	usage := fmt.Errorf(ledgerUsage, strings.Join(ledgerCategories, ", "))
	if len(args) < 3 || !slices.Contains(ledgerCategories, args[0]) {
		return ledgerEntry{}, usage
	}
	qty, err := strconv.Atoi(args[2])
	if err != nil {
		return ledgerEntry{}, usage
	}
	e := ledgerEntry{
		Category: args[0],
		Item:     args[1],
		Qty:      qty,
		Note:     strings.Join(args[3:], " "),
	}
	if e.Item == "credits" {
		e.Credits = qty
	}
	return e, nil
}

// runLedger parses args and appends the entry to dir/ledger.jsonl.
func runLedger(dir string, args []string) error {
	if dir == "" {
		return errors.New("ledger needs --record-dir")
	}
	e, err := parseLedgerArgs(args)
	if err != nil {
		return err
	}
	e.ObservedUTC = time.Now().UTC().Format(time.RFC3339)
	e.AgentID = globalAgentID
	if err := appendJSONLine(dir, "ledger.jsonl", e); err != nil {
		return err
	}
	fmt.Printf("📒 ledger: %s %s %d\n", e.Category, e.Item, e.Qty)
	return nil
}
