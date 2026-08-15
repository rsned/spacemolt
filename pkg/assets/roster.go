package assets

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
)

// RosterShip is the active hull as the roster shows it. CargoCapacity comes
// from the ship catalog, not this database, so the caller fills it in;
// zero means "catalog didn't know this class", not an empty hold.
type RosterShip struct {
	ShipID        string `json:"ship_id"`
	ClassID       string `json:"class_id"`
	ClassName     string `json:"class_name"`
	HullCurrent   int    `json:"hull_current"`
	HullMax       int    `json:"hull_max"`
	FuelCurrent   int    `json:"fuel_current"`
	FuelMax       int    `json:"fuel_max"`
	CargoUsed     int    `json:"cargo_used"`
	CargoCapacity int    `json:"cargo_capacity"`
}

// RosterCapability is one unlock as stored by the capability evaluator.
type RosterCapability struct {
	Eligible bool   `json:"eligible"`
	Reason   string `json:"reason,omitempty"`
}

// RosterRow is one ledger agent for the dashboard roster. Fleet, Role and
// Stale are the caller's to fill: fleet membership lives in the live status
// snapshot and staleness policy belongs to the dashboard, not this package.
type RosterRow struct {
	PlayerID      string                      `json:"player_id"`
	AgentID       string                      `json:"agent_id"`
	Username      string                      `json:"username"`
	Empire        string                      `json:"empire"`
	Credits       float64                     `json:"credits"`
	FactionID     string                      `json:"faction_id"`
	FactionRank   string                      `json:"faction_rank"`
	CurrentSystem string                      `json:"current_system"`
	CurrentPOI    string                      `json:"current_poi"`
	DockedAtBase  string                      `json:"docked_at_base"`
	Experience    int                         `json:"experience"`
	CapturedAt    string                      `json:"captured_at"`
	Ship          *RosterShip                 `json:"ship,omitempty"`
	Capabilities  map[string]RosterCapability `json:"capabilities"`
	Fleet         string                      `json:"fleet,omitempty"`
	Role          string                      `json:"role,omitempty"`
	Stale         bool                        `json:"stale"`
}

// Roster returns every identity in the ledger, profile and active hull
// attached where captured. An agent with an identity row but no profile yet
// still appears — hiding it would make a capture gap invisible, and the
// roster doubles as the coverage audit.
func Roster(ctx context.Context, db *sql.DB) ([]RosterRow, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT a.player_id, a.agent_id, a.username,
		       COALESCE(p.empire,''), COALESCE(p.credits,0),
		       COALESCE(p.faction_id,''), COALESCE(p.faction_rank,''),
		       COALESCE(p.current_system,''), COALESCE(p.current_poi,''),
		       COALESCE(p.docked_at_base,''), COALESCE(p.experience,0),
		       COALESCE(p.captured_at,'')
		  FROM agents a LEFT JOIN agent_profile p ON p.player_id = a.player_id`)
	if err != nil {
		return nil, fmt.Errorf("assets: roster query: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	byPlayer := map[string]*RosterRow{}
	var out []*RosterRow
	for rows.Next() {
		r := &RosterRow{Capabilities: map[string]RosterCapability{}}
		if err := rows.Scan(&r.PlayerID, &r.AgentID, &r.Username, &r.Empire, &r.Credits,
			&r.FactionID, &r.FactionRank, &r.CurrentSystem, &r.CurrentPOI,
			&r.DockedAtBase, &r.Experience, &r.CapturedAt); err != nil {
			return nil, fmt.Errorf("assets: roster scan: %w", err)
		}
		byPlayer[r.PlayerID] = r
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("assets: roster rows: %w", err)
	}

	if err := attachActiveHulls(ctx, db, byPlayer); err != nil {
		return nil, err
	}
	if err := attachCapabilities(ctx, db, byPlayer); err != nil {
		return nil, err
	}

	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.AgentID != b.AgentID {
			return a.AgentID < b.AgentID
		}
		return a.PlayerID < b.PlayerID
	})
	res := make([]RosterRow, len(out))
	for i, r := range out {
		res[i] = *r
	}

	return res, nil
}

func attachActiveHulls(ctx context.Context, db *sql.DB, byPlayer map[string]*RosterRow) error {
	rows, err := db.QueryContext(ctx, `
		SELECT player_id, ship_id, class_id, class_name,
		       hull_current, hull_max, fuel_current, fuel_max, cargo_used
		  FROM agent_hulls WHERE is_active = 1`)
	if err != nil {
		return fmt.Errorf("assets: roster hulls: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	for rows.Next() {
		var (
			pid string
			s   RosterShip
		)
		if err := rows.Scan(&pid, &s.ShipID, &s.ClassID, &s.ClassName,
			&s.HullCurrent, &s.HullMax, &s.FuelCurrent, &s.FuelMax, &s.CargoUsed); err != nil {
			return fmt.Errorf("assets: roster hull scan: %w", err)
		}
		if r, ok := byPlayer[pid]; ok {
			ship := s
			r.Ship = &ship
		}
	}

	return rows.Err()
}

func attachCapabilities(ctx context.Context, db *sql.DB, byPlayer map[string]*RosterRow) error {
	rows, err := db.QueryContext(ctx, `
		SELECT player_id, capability, eligible, blocking_reason FROM agent_capability`)
	if err != nil {
		return fmt.Errorf("assets: roster capabilities: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	for rows.Next() {
		var (
			pid, cap, reason string
			eligible         int
		)
		if err := rows.Scan(&pid, &cap, &eligible, &reason); err != nil {
			return fmt.Errorf("assets: roster capability scan: %w", err)
		}
		if r, ok := byPlayer[pid]; ok {
			r.Capabilities[cap] = RosterCapability{Eligible: eligible != 0, Reason: reason}
		}
	}

	return rows.Err()
}

// SheetSkill, SheetStanding, SheetHull and SheetStorage are the sheet's
// JSON shapes for the corresponding ledger tables.
type SheetSkill struct {
	Skill string  `json:"skill"`
	Level int     `json:"level"`
	XP    float64 `json:"xp"`
}

type SheetStanding struct {
	Faction           string `json:"faction"`
	Reputation        int    `json:"reputation"`
	Baseline          int    `json:"baseline"`
	OutstandingBounty int    `json:"outstanding_bounty"`
	JailedUntil       string `json:"jailed_until,omitempty"`
}

type SheetHull struct {
	RosterShip
	IsActive     bool   `json:"is_active"`
	Location     string `json:"location"`
	Modules      int    `json:"modules"`
	ListingPrice int64  `json:"listing_price,omitempty"`
}

type SheetStorage struct {
	BaseID  string  `json:"base_id"`
	Credits int64   `json:"credits"`
	Items   int     `json:"items"`
	Units   float64 `json:"units"`
}

// Sheet is the full drill-down view for one agent.
type Sheet struct {
	RosterRow
	Skills    []SheetSkill    `json:"skills"`
	Standings []SheetStanding `json:"standings"`
	Hulls     []SheetHull     `json:"hulls"`
	Storage   []SheetStorage  `json:"storage"`
}

// SheetFor loads the sheet for one agent by agent id (falling back to player
// id, so ledger-only identities without an agent mapping stay reachable).
// ok=false means no such identity.
func SheetFor(ctx context.Context, db *sql.DB, agentID string) (Sheet, bool, error) {
	var pid string
	err := db.QueryRowContext(ctx,
		`SELECT player_id FROM agents WHERE agent_id = ? OR player_id = ? LIMIT 1`,
		agentID, agentID).Scan(&pid)
	if err == sql.ErrNoRows {
		return Sheet{}, false, nil
	}
	if err != nil {
		return Sheet{}, false, fmt.Errorf("assets: sheet identity %s: %w", agentID, err)
	}

	all, err := Roster(ctx, db)
	if err != nil {
		return Sheet{}, false, err
	}
	var sheet Sheet
	for _, r := range all {
		if r.PlayerID == pid {
			sheet.RosterRow = r

			break
		}
	}
	if sheet.PlayerID == "" {
		return Sheet{}, false, nil
	}

	if err := sheetDetails(ctx, db, pid, &sheet); err != nil {
		return Sheet{}, false, err
	}

	return sheet, true, nil
}

func sheetDetails(ctx context.Context, db *sql.DB, pid string, sheet *Sheet) error {
	skillRows, err := db.QueryContext(ctx,
		`SELECT skill, level, xp FROM agent_skills WHERE player_id = ? ORDER BY level DESC, skill`, pid)
	if err != nil {
		return fmt.Errorf("assets: sheet skills: %w", err)
	}
	defer skillRows.Close() //nolint:errcheck
	for skillRows.Next() {
		var s SheetSkill
		if err := skillRows.Scan(&s.Skill, &s.Level, &s.XP); err != nil {
			return fmt.Errorf("assets: sheet skill scan: %w", err)
		}
		sheet.Skills = append(sheet.Skills, s)
	}
	if err := skillRows.Err(); err != nil {
		return err
	}

	standingRows, err := db.QueryContext(ctx, `
		SELECT faction, reputation, baseline, outstanding_bounty, jailed_until
		  FROM agent_standings WHERE player_id = ? ORDER BY baseline DESC, faction`, pid)
	if err != nil {
		return fmt.Errorf("assets: sheet standings: %w", err)
	}
	defer standingRows.Close() //nolint:errcheck
	for standingRows.Next() {
		var s SheetStanding
		if err := standingRows.Scan(&s.Faction, &s.Reputation, &s.Baseline,
			&s.OutstandingBounty, &s.JailedUntil); err != nil {
			return fmt.Errorf("assets: sheet standing scan: %w", err)
		}
		sheet.Standings = append(sheet.Standings, s)
	}
	if err := standingRows.Err(); err != nil {
		return err
	}

	hullRows, err := db.QueryContext(ctx, `
		SELECT ship_id, class_id, class_name, hull_current, hull_max,
		       fuel_current, fuel_max, cargo_used, is_active, location, modules, listing_price
		  FROM agent_hulls WHERE player_id = ? ORDER BY is_active DESC, class_name, ship_id`, pid)
	if err != nil {
		return fmt.Errorf("assets: sheet hulls: %w", err)
	}
	defer hullRows.Close() //nolint:errcheck
	for hullRows.Next() {
		var (
			h      SheetHull
			active int
		)
		if err := hullRows.Scan(&h.ShipID, &h.ClassID, &h.ClassName, &h.HullCurrent, &h.HullMax,
			&h.FuelCurrent, &h.FuelMax, &h.CargoUsed, &active, &h.Location, &h.Modules,
			&h.ListingPrice); err != nil {
			return fmt.Errorf("assets: sheet hull scan: %w", err)
		}
		h.IsActive = active != 0
		sheet.Hulls = append(sheet.Hulls, h)
	}
	if err := hullRows.Err(); err != nil {
		return err
	}

	storageRows, err := db.QueryContext(ctx, `
		SELECT s.base_id, s.credits,
		       COUNT(i.item_id), COALESCE(SUM(i.quantity),0)
		  FROM agent_storage s
		  LEFT JOIN agent_storage_items i ON i.player_id = s.player_id AND i.base_id = s.base_id
		 WHERE s.player_id = ?
		 GROUP BY s.base_id, s.credits ORDER BY s.base_id`, pid)
	if err != nil {
		return fmt.Errorf("assets: sheet storage: %w", err)
	}
	defer storageRows.Close() //nolint:errcheck
	for storageRows.Next() {
		var s SheetStorage
		if err := storageRows.Scan(&s.BaseID, &s.Credits, &s.Items, &s.Units); err != nil {
			return fmt.Errorf("assets: sheet storage scan: %w", err)
		}
		sheet.Storage = append(sheet.Storage, s)
	}

	return storageRows.Err()
}
