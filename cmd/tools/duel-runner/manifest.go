package main

import (
	"bufio"
	"encoding/json"
	"os"
	"strconv"
	"time"
)

// Record is one completed (or voided) duel run. The manifest is append-only
// JSONL: a killed session loses at most the in-flight duel.
type Record struct {
	ScenarioID string    `json:"scenario_id"`
	Repeat     int       `json:"repeat"`
	BattleID   string    `json:"battle_id"`
	Started    time.Time `json:"started"`
	Ended      time.Time `json:"ended"`
	Outcome    string    `json:"outcome"`
	Void       bool      `json:"void"`
	// Mode is "arena" or "lawless" -- which fighting rules produced this
	// run. Analysis must not pool the two blindly: an arena run's ship
	// state is restored afterwards, and its terminal flee is a forfeit
	// rather than an escape.
	Mode string `json:"mode,omitempty"`
	// XPUsedToday is the challenger's arena XP ledger at the moment this
	// duel started (skill id -> XP earned from arena fights today), read
	// from `arena status`. Arena fights grant combat XP normally up to a
	// daily per-skill cap, so a long campaign can level the duelling bots
	// underneath the measurements -- crit alone moves 1% per level. This
	// snapshot is what makes that drift auditable after the fact.
	XPUsedToday map[string]int `json:"xp_used_today,omitempty"`
}

// DoneKey identifies one (scenario, repeat) run.
func DoneKey(id string, repeat int) string { return id + "#" + strconv.Itoa(repeat) }

// AppendRecord appends one JSON line, fsyncing so a crash cannot lose it.
func AppendRecord(path string, r Record) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck
	line, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		return err
	}
	return f.Sync()
}

// LoadDone returns the set of completed non-void (scenario, repeat) keys.
// A missing manifest is an empty campaign, not an error.
func LoadDone(path string) (map[string]bool, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck
	done := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var r Record
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			return nil, err
		}
		if !r.Void {
			done[DoneKey(r.ScenarioID, r.Repeat)] = true
		}
	}
	return done, sc.Err()
}
