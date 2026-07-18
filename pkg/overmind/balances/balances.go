// Package balances records fleet balance snapshots so the overmind can track
// each agent's starting balance and day-over-day credit deltas.
//
// Two artifacts are produced, both under data/overmind/:
//   - a live status file (fleet-status.json), overwritten every heartbeat tick
//     with the current view of every worker, for on-demand reporting; and
//   - an append-only daily history (fleet-history.jsonl), one record per agent
//     per UTC day, captured at the first tick after the date rolls over. The
//     first record for an agent is its starting balance; each later record is a
//     midnight snapshot the report tool diffs into daily deltas.
package balances

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// dateLayout is the UTC calendar-day key for a daily snapshot.
const dateLayout = "2006-01-02"

// LiveRecord is the current view of one worker, written to the status file.
type LiveRecord struct {
	AgentID          string  `json:"agent_id"`
	Role             string  `json:"role"`
	System           string  `json:"system"`
	POI              string  `json:"poi"`
	Docked           bool    `json:"docked"`
	Credits          float64 `json:"credits"`
	Hull             float64 `json:"hull"`
	MaxHull          float64 `json:"max_hull"`
	Fuel             float64 `json:"fuel"`
	MaxFuel          float64 `json:"max_fuel"`
	CargoUsed        float64 `json:"cargo_used"`
	CargoCapacity    float64 `json:"cargo_capacity"`
	StandingBehavior string  `json:"standing_behavior"`
	ActiveTaskID     string  `json:"active_task_id"`
	// Activity is a short human-readable description of the worker's current
	// unit of work (e.g. "Opportunity #100042 24 power_cell from A to B"),
	// surfaced as a sub-line on the status page. Empty when idle.
	Activity   string `json:"activity,omitempty"`
	FactionID  string `json:"faction_id,omitempty"`
	FactionTag string `json:"faction_tag,omitempty"`
	Healthy    bool   `json:"healthy"`
	Restarts   int    `json:"restarts"`
	// Quarantined mirrors the supervisor's pulled-from-fleet state; the reason
	// says why (e.g. "fuel-dead: ..."). Omitted for healthy workers.
	Quarantined      bool   `json:"quarantined,omitempty"`
	QuarantineReason string `json:"quarantine_reason,omitempty"`
	// Seen is true once the worker has sent at least one heartbeat, so its
	// Credits reflect a real balance rather than the zero value. Only seen
	// workers are eligible for a daily snapshot.
	Seen     bool   `json:"seen"`
	LastSeen string `json:"last_seen"`
}

// StatusFile is the on-disk shape of the live status file.
type StatusFile struct {
	CapturedAt string       `json:"captured_at"`
	Workers    []LiveRecord `json:"workers"`
}

// DailyRecord is one agent's balance on one UTC day.
type DailyRecord struct {
	Date       string  `json:"date"`
	AgentID    string  `json:"agent_id"`
	Credits    float64 `json:"credits"`
	System     string  `json:"system"`
	CapturedAt string  `json:"captured_at"`
}

// Recorder writes the live status file and the daily history. It is safe for
// concurrent use, though in practice a single ticker goroutine drives it.
type Recorder struct {
	statusPath  string
	historyPath string
	// recorded[date][agentID] is true once that agent has a row for that UTC
	// day, so each agent is snapshotted at most once per day and a restart does
	// not duplicate rows.
	recorded map[string]map[string]bool
}

// NewRecorder returns a Recorder for the given paths, seeding the recorded set
// from the existing history so a restart does not duplicate rows.
func NewRecorder(statusPath, historyPath string) (*Recorder, error) {
	r := &Recorder{statusPath: statusPath, historyPath: historyPath, recorded: map[string]map[string]bool{}}
	recs, err := ReadHistory(historyPath)
	if err != nil {
		return nil, err
	}
	for _, rec := range recs {
		r.mark(rec.Date, rec.AgentID)
	}
	return r, nil
}

func (r *Recorder) mark(date, agentID string) {
	day := r.recorded[date]
	if day == nil {
		day = map[string]bool{}
		r.recorded[date] = day
	}
	day[agentID] = true
}

// WriteStatus atomically overwrites the live status file with the given records.
func (r *Recorder) WriteStatus(live []LiveRecord, now time.Time) error {
	sf := StatusFile{CapturedAt: now.UTC().Format(time.RFC3339), Workers: live}
	data, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return fmt.Errorf("balances: marshal status: %w", err)
	}
	return atomicWrite(r.statusPath, append(data, '\n'))
}

// MaybeSnapshotDaily ensures every seen worker has exactly one balance record
// for the current UTC day, appending rows for any that are missing one. It
// returns the number of rows written. The first appearance of an agent (its
// first-ever row) is its starting balance; the first tick past each UTC
// midnight captures that day's balances, and a worker that connects late still
// gets a starting row the moment it is first seen. Unseen workers (no heartbeat
// yet, so credits unknown) are skipped until they report in.
func (r *Recorder) MaybeSnapshotDaily(live []LiveRecord, now time.Time) (int, error) {
	date := now.UTC().Format(dateLayout)
	ts := now.UTC().Format(time.RFC3339)
	day := r.recorded[date]
	var rows []DailyRecord
	for _, w := range live {
		if !w.Seen || (day != nil && day[w.AgentID]) {
			continue
		}
		rows = append(rows, DailyRecord{
			Date: date, AgentID: w.AgentID, Credits: w.Credits, System: w.System, CapturedAt: ts,
		})
	}
	if len(rows) == 0 {
		return 0, nil
	}
	if err := appendHistory(r.historyPath, rows); err != nil {
		return 0, err
	}
	for _, row := range rows {
		r.mark(date, row.AgentID)
	}
	return len(rows), nil
}

// ReadHistory loads all daily records from path. A missing file is not an error.
func ReadHistory(path string) ([]DailyRecord, error) {
	f, err := os.Open(path) //nolint:gosec // path is operator-controlled
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("balances: open history: %w", err)
	}
	defer func() { _ = f.Close() }()
	var out []DailyRecord
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec DailyRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			return nil, fmt.Errorf("balances: decode history line: %w", err)
		}
		out = append(out, rec)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("balances: scan history: %w", err)
	}
	return out, nil
}

// ReadStatus loads the live status file. A missing file is not an error.
func ReadStatus(path string) (StatusFile, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is operator-controlled
	if err != nil {
		if os.IsNotExist(err) {
			return StatusFile{}, nil
		}
		return StatusFile{}, fmt.Errorf("balances: read status: %w", err)
	}
	var sf StatusFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return StatusFile{}, fmt.Errorf("balances: decode status: %w", err)
	}
	return sf, nil
}

func appendHistory(path string, rows []DailyRecord) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("balances: mkdir history: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644) //nolint:gosec // operator-controlled path
	if err != nil {
		return fmt.Errorf("balances: open history for append: %w", err)
	}
	defer func() { _ = f.Close() }()
	w := bufio.NewWriter(f)
	for _, rec := range rows {
		line, err := json.Marshal(rec)
		if err != nil {
			return fmt.Errorf("balances: marshal history row: %w", err)
		}
		if _, err := w.Write(append(line, '\n')); err != nil {
			return fmt.Errorf("balances: write history row: %w", err)
		}
	}
	return w.Flush()
}

func atomicWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("balances: mkdir status: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil { //nolint:gosec // operator-controlled path
		return fmt.Errorf("balances: write status tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("balances: rename status: %w", err)
	}
	return nil
}

// ProfitAgg is an agent's realized arbitrage performance, aggregated from the
// market database's completed opportunities.
type ProfitAgg struct {
	Hauls int
	Gross float64
}

// AgentReport is the computed per-agent reporting line.
type AgentReport struct {
	AgentID         string
	Role            string
	System          string
	POI             string
	Healthy         bool
	Seen            bool
	Credits         float64
	StartingBalance float64
	StartDate       string
	TotalDelta      float64 // Credits - StartingBalance
	SinceSnapshot   float64 // Credits - most-recent daily snapshot
	LastSnapshot    string  // date of the most-recent daily snapshot
	DaysTracked     int
	Hauls           int
	RealizedProfit  float64
}

// BuildReport joins the live status, daily history, and per-agent realized
// profit into one report line per live worker, sorted by agent id. Agents are
// keyed off the live status; history/profit are matched in by agent id.
func BuildReport(live []LiveRecord, history []DailyRecord, profit map[string]ProfitAgg) []AgentReport {
	type hist struct {
		startCredits float64
		startDate    string
		lastCredits  float64
		lastDate     string
		days         int
	}
	byAgent := make(map[string]*hist)
	for _, rec := range history {
		h := byAgent[rec.AgentID]
		if h == nil {
			h = &hist{startCredits: rec.Credits, startDate: rec.Date}
			byAgent[rec.AgentID] = h
		}
		// History is appended in chronological order; track first and last.
		if rec.Date < h.startDate {
			h.startCredits, h.startDate = rec.Credits, rec.Date
		}
		if rec.Date >= h.lastDate {
			h.lastCredits, h.lastDate = rec.Credits, rec.Date
		}
		h.days++
	}
	out := make([]AgentReport, 0, len(live))
	for _, w := range live {
		ar := AgentReport{
			AgentID: w.AgentID, Role: w.Role, System: w.System, POI: w.POI,
			Healthy: w.Healthy, Seen: w.Seen, Credits: w.Credits,
		}
		if p, ok := profit[w.AgentID]; ok {
			ar.Hauls, ar.RealizedProfit = p.Hauls, p.Gross
		}
		if h := byAgent[w.AgentID]; h != nil {
			ar.StartingBalance = h.startCredits
			ar.StartDate = h.startDate
			ar.TotalDelta = w.Credits - h.startCredits
			ar.SinceSnapshot = w.Credits - h.lastCredits
			ar.LastSnapshot = h.lastDate
			ar.DaysTracked = h.days
		}
		out = append(out, ar)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AgentID < out[j].AgentID })
	return out
}
