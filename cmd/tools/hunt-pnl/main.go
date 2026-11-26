// hunt-pnl reports each hunter's realized profit and loss from the server-side
// action log that capture_action_log stores in assets.db: mission payouts in,
// fuel and market buys out, per hunter and per Station Need.
//
// It only reads assets.db, so it makes no network calls and never collides
// with a live worker. The log is captured twice a day by each worker, so the
// report runs up to each hunter's last capture, which is printed per row: a
// stranded or stopped hunter's log goes stale.
//
// A job's cost is every refuel and market buy the hunter made between
// accepting the need and completing it. That is the whole of what it spent in
// that time, not a metered share: a tank filled just before the accept is
// charged to the job before.
//
// Usage:
//
//	hunt-pnl                          # every hunter with a need in the last 24h
//	hunt-pnl --since=2026-11-25T00:27:00Z
//	hunt-pnl --since=12h --jobs       # plus one row per completed need
//	hunt-pnl --agents=pirate-5,pirate-9 --json
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// event is one stored action-log row. Every data value is a string in
// assets.db, numbers included.
type event struct {
	Type      string
	CreatedAt string
	Data      map[string]string
}

// Job is one completed mission with what the hunter spent while holding it.
type Job struct {
	Title      string  `json:"title"`
	MissionID  string  `json:"mission_id"`
	AcceptedAt string  `json:"accepted_at,omitempty"`
	DoneAt     string  `json:"completed_at"`
	Hours      float64 `json:"hours,omitempty"`
	Reward     int     `json:"reward"`
	Spend      int     `json:"spend"`
	// Paired is false when the accept fell outside the window, so Spend is
	// unknown and the job is left out of the losing-job count.
	Paired bool `json:"paired"`
}

// Net is the reward less what was spent while the job was held.
func (j Job) Net() int { return j.Reward - j.Spend }

// AgentPnL is one hunter's money in and out over the window.
type AgentPnL struct {
	AgentID   string `json:"agent_id"`
	LastEvent string `json:"last_event"`
	Completed int    `json:"completed"`
	Abandoned int    `json:"abandoned"`
	Expired   int    `json:"expired"`
	Income    int    `json:"income"`
	Fuel      int    `json:"fuel_cost"`
	FuelUnits int    `json:"fuel_units"`
	Refuels   int    `json:"refuels"`
	Buys      int    `json:"buys"`
	Sales     int    `json:"sales"`
	Fees      int    `json:"fees"`
	Jobs      []Job  `json:"jobs,omitempty"`
}

// Net is income and sales less fuel, buys and fees.
func (p AgentPnL) Net() int { return p.Income + p.Sales - p.Fuel - p.Buys - p.Fees }

// Losers counts paired jobs that cost more than they paid.
func (p AgentPnL) Losers() int {
	n := 0
	for _, j := range p.Jobs {
		if j.Paired && j.Net() < 0 {
			n++
		}
	}
	return n
}

func num(d map[string]string, key string) int {
	f, err := strconv.ParseFloat(d[key], 64)
	if err != nil {
		return 0
	}
	return int(f)
}

// computePnL folds a hunter's events, oldest first, into its P&L.
func computePnL(agentID string, events []event) AgentPnL {
	type spend struct {
		at     string
		amount int
	}
	p := AgentPnL{AgentID: agentID}
	accepted := map[string]string{}
	var spends []spend
	for _, e := range events {
		p.LastEvent = e.CreatedAt
		switch e.Type {
		case "mission.accepted":
			accepted[e.Data["mission_id"]] = e.CreatedAt
		case "mission.abandoned":
			p.Abandoned++
			delete(accepted, e.Data["mission_id"])
		case "mission.expired":
			p.Expired++
			delete(accepted, e.Data["mission_id"])
		case "mission.completed":
			j := Job{Title: e.Data["title"], MissionID: e.Data["mission_id"], DoneAt: e.CreatedAt, Reward: num(e.Data, "credits")}
			if at, ok := accepted[j.MissionID]; ok {
				j.AcceptedAt, j.Paired = at, true
				for _, s := range spends {
					if s.at >= at {
						j.Spend += s.amount
					}
				}
				if a, aerr := time.Parse(time.RFC3339, at); aerr == nil {
					if d, derr := time.Parse(time.RFC3339, e.CreatedAt); derr == nil {
						j.Hours = d.Sub(a).Hours()
					}
				}
				delete(accepted, j.MissionID)
			}
			p.Completed++
			p.Income += j.Reward
			p.Jobs = append(p.Jobs, j)
		case "ship.refuel":
			cost := num(e.Data, "total_cost")
			p.Fuel += cost
			p.FuelUnits += num(e.Data, "fuel")
			p.Refuels++
			spends = append(spends, spend{e.CreatedAt, cost})
		case "trading.exchange_fill":
			if e.Data["role"] == "buyer" {
				cost := num(e.Data, "total") + num(e.Data, "sales_tax")
				p.Buys += cost
				spends = append(spends, spend{e.CreatedAt, cost})
			} else {
				p.Sales += num(e.Data, "total")
			}
		case "credits.service_fee", "tax.property_paid", "tax.income_paid":
			p.Fees += num(e.Data, "amount")
		case "ship.repair":
			p.Fees += max(num(e.Data, "cost"), num(e.Data, "total_cost"))
		}
	}
	return p
}

// parseSince reads --since as an RFC3339 instant or as a duration back from now.
func parseSince(s string, now time.Time) (time.Time, error) {
	if d, err := time.ParseDuration(s); err == nil {
		return now.Add(-d), nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("--since %q is neither a duration (24h) nor RFC3339", s)
	}
	return t, nil
}

// hunters lists the pirate-* agents that held a wildlife Station Need
// (mission id "creature_...") in the window.
func hunters(ctx context.Context, db *sql.DB, since string) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT DISTINCT a.agent_id FROM action_log_events e
		JOIN agents a ON a.player_id = e.player_id
		WHERE a.agent_id LIKE 'pirate-%' AND e.created_at >= ?
		  AND e.event_type IN ('mission.accepted','mission.completed','mission.abandoned','mission.expired')
		  AND json_extract(e.data_json,'$.mission_id') LIKE 'creature_%'`, since)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func loadEvents(ctx context.Context, db *sql.DB, agentID, since string) ([]event, error) {
	rows, err := db.QueryContext(ctx, `SELECT e.event_type, e.created_at, e.data_json FROM action_log_events e
		JOIN agents a ON a.player_id = e.player_id
		WHERE a.agent_id = ? AND e.created_at >= ? ORDER BY e.event_id`, agentID, since)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []event
	for rows.Next() {
		var (
			e   event
			raw string
		)
		if err := rows.Scan(&e.Type, &e.CreatedAt, &raw); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(raw), &e.Data)
		out = append(out, e)
	}
	return out, rows.Err()
}

// agentOrder sorts pirate-5 before pirate-10.
func agentOrder(a, b string) bool {
	na, _ := strconv.Atoi(a[strings.LastIndex(a, "-")+1:])
	nb, _ := strconv.Atoi(b[strings.LastIndex(b, "-")+1:])
	if na != nb {
		return na < nb
	}
	return a < b
}

func report(w io.Writer, pnls []AgentPnL, jobs bool) {
	fmt.Fprintf(w, "%-10s %-20s %4s %4s %4s %8s %8s %6s %6s %6s %8s %6s\n", //nolint:errcheck
		"hunter", "log ends (UTC)", "done", "aban", "exp", "income", "fuel", "cr/u", "buys", "fees", "net", "losers")
	var t AgentPnL
	for _, p := range pnls {
		perUnit := 0.0
		if p.FuelUnits > 0 {
			perUnit = float64(p.Fuel) / float64(p.FuelUnits)
		}
		fmt.Fprintf(w, "%-10s %-20s %4d %4d %4d %8d %8d %6.1f %6d %6d %8d %6d\n", //nolint:errcheck
			p.AgentID, p.LastEvent, p.Completed, p.Abandoned, p.Expired, p.Income, p.Fuel, perUnit, p.Buys, p.Fees, p.Net(), p.Losers())
		t.Completed += p.Completed
		t.Abandoned += p.Abandoned
		t.Expired += p.Expired
		t.Income += p.Income
		t.Fuel += p.Fuel
		t.FuelUnits += p.FuelUnits
		t.Buys += p.Buys
		t.Sales += p.Sales
		t.Fees += p.Fees
		t.Jobs = append(t.Jobs, p.Jobs...)
	}
	perUnit := 0.0
	if t.FuelUnits > 0 {
		perUnit = float64(t.Fuel) / float64(t.FuelUnits)
	}
	fmt.Fprintf(w, "%-10s %-20s %4d %4d %4d %8d %8d %6.1f %6d %6d %8d %6d\n", //nolint:errcheck
		"FLEET", "", t.Completed, t.Abandoned, t.Expired, t.Income, t.Fuel, perUnit, t.Buys, t.Fees, t.Net(), t.Losers())
	if !jobs {
		return
	}
	fmt.Fprintf(w, "\n%-10s %-20s %-36s %6s %7s %7s %8s\n", "hunter", "completed (UTC)", "need", "hours", "reward", "spend", "net") //nolint:errcheck
	for _, p := range pnls {
		for _, j := range p.Jobs {
			title := strings.TrimPrefix(j.Title, "Station Need: ")
			if !j.Paired {
				fmt.Fprintf(w, "%-10s %-20s %-36.36s %6s %7d %7s %8s\n", p.AgentID, j.DoneAt, title, "?", j.Reward, "?", "?") //nolint:errcheck
				continue
			}
			fmt.Fprintf(w, "%-10s %-20s %-36.36s %6.1f %7d %7d %8d\n", p.AgentID, j.DoneAt, title, j.Hours, j.Reward, j.Spend, j.Net()) //nolint:errcheck
		}
	}
}

func main() {
	assetsDB := flag.String("assets-db", "data/assets.db", "Path to assets.db")
	sinceFlag := flag.String("since", "24h", "Window start: a duration back from now (24h) or an RFC3339 instant")
	agentsFlag := flag.String("agents", "", "Comma-separated agent ids (default: every pirate-* that held a wildlife need in the window)")
	jobs := flag.Bool("jobs", false, "Also print one row per completed need")
	asJSON := flag.Bool("json", false, "Emit JSON")
	flag.Parse()

	since, err := parseSince(*sinceFlag, time.Now())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	sinceStr := since.UTC().Format(time.RFC3339)

	db, err := sql.Open("sqlite", "file:"+*assetsDB+"?mode=ro&_pragma=busy_timeout(20000)")
	if err != nil {
		fmt.Fprintln(os.Stderr, "open assets db:", err)
		os.Exit(1)
	}
	defer func() { _ = db.Close() }()
	ctx := context.Background()

	var agents []string
	if *agentsFlag != "" {
		agents = strings.Split(*agentsFlag, ",")
	} else if agents, err = hunters(ctx, db, sinceStr); err != nil {
		fmt.Fprintln(os.Stderr, "list hunters:", err)
		os.Exit(1) //nolint:gocritic // exiting; the deferred close does not matter
	}
	sort.Slice(agents, func(i, j int) bool { return agentOrder(agents[i], agents[j]) })

	pnls := make([]AgentPnL, 0, len(agents))
	for _, a := range agents {
		evs, lerr := loadEvents(ctx, db, strings.TrimSpace(a), sinceStr)
		if lerr != nil {
			fmt.Fprintf(os.Stderr, "load %s: %v\n", a, lerr)
			os.Exit(1)
		}
		pnls = append(pnls, computePnL(strings.TrimSpace(a), evs))
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(pnls)
		return
	}
	fmt.Printf("hunt P&L since %s\n\n", sinceStr)
	report(os.Stdout, pnls, *jobs)
}
