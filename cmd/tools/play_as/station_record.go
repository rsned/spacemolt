package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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

// rawForRecord picks the reply bytes for cmd: the error body on failure,
// otherwise the command's own raw slot, falling back to "_last". Non-JSON
// bytes are kept as a JSON string so events.jsonl stays parseable.
func rawForRecord(client game.GameClient, cmd string, cmdErr error) json.RawMessage {
	var data []byte
	if cmdErr != nil {
		data = client.GetRawJSON("_last_error")
	} else {
		key := cmd
		if k, ok := rawJSONKeyForCommand[cmd]; ok {
			key = k
		}
		data = client.GetRawJSON(key)
		if len(data) == 0 {
			data = client.GetRawJSON("_last")
		}
	}
	if len(data) == 0 {
		return nil
	}
	if json.Valid(data) {
		return json.RawMessage(data)
	}
	quoted, _ := json.Marshal(string(data))
	return quoted
}

// recordExecution writes the event for one executed command and returns
// cmdErr unchanged: a recording failure is reported, never propagated.
func recordExecution(client game.GameClient, parts []string, cmdErr error) error {
	if globalRecordDir == "" || len(parts) == 0 {
		return cmdErr
	}
	ev := stationEvent{
		AgentID:    globalAgentID,
		Command:    strings.ToLower(parts[0]),
		Args:       parts[1:],
		SnapshotID: globalSnapshotID,
		Raw:        rawForRecord(client, strings.ToLower(parts[0]), cmdErr),
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
