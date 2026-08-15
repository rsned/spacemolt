package ovdash

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/rsned/spacemolt/pkg/assets"
)

// LoadRoster reads the full agent roster from the assets ledger and
// decorates each active hull with its catalog cargo capacity. Absent-DB
// handling mirrors LoadAssetCoverage: a ledger that doesn't exist yet is the
// normal pre-deploy state and yields no rows, not an error.
func LoadRoster(ctx context.Context, assetsPath, kbPath string, now time.Time, staleAfter time.Duration) ([]assets.RosterRow, error) {
	db, ok, err := openAssetsRO(assetsPath)
	if err != nil || !ok {
		return nil, err
	}
	defer db.Close() //nolint:errcheck

	rows, err := assets.Roster(ctx, db)
	if err != nil {
		return nil, err
	}
	caps, err := cargoCapacities(ctx, kbPath)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		decorateRosterRow(&rows[i], caps, now, staleAfter)
	}

	return rows, nil
}

// LoadSheet reads one agent's character sheet. nil with no error means the
// ledger is absent or the agent is unknown.
func LoadSheet(ctx context.Context, assetsPath, kbPath, agentID string, now time.Time, staleAfter time.Duration) (*assets.Sheet, error) {
	db, ok, err := openAssetsRO(assetsPath)
	if err != nil || !ok {
		return nil, err
	}
	defer db.Close() //nolint:errcheck

	sheet, found, err := assets.SheetFor(ctx, db, agentID)
	if err != nil || !found {
		return nil, err
	}
	caps, err := cargoCapacities(ctx, kbPath)
	if err != nil {
		return nil, err
	}
	decorateRosterRow(&sheet.RosterRow, caps, now, staleAfter)
	for i := range sheet.Hulls {
		if c, ok := caps[sheet.Hulls[i].ClassID]; ok {
			sheet.Hulls[i].CargoCapacity = c
		}
	}

	return &sheet, nil
}

func decorateRosterRow(r *assets.RosterRow, caps map[string]int, now time.Time, staleAfter time.Duration) {
	if r.Ship != nil {
		if c, ok := caps[r.Ship.ClassID]; ok {
			r.Ship.CargoCapacity = c
		}
	}
	// Never-captured counts as stale: an empty timestamp parses to zero time.
	at, _ := time.Parse(time.RFC3339, r.CapturedAt)
	r.Stale = now.Sub(at) > staleAfter
}

func openAssetsRO(path string) (*sql.DB, bool, error) {
	if path == "" {
		return nil, false, nil
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("stat assets db: %w", err)
	}
	db, err := sql.Open(sqliteDriver, "file:"+path+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, false, fmt.Errorf("open assets db: %w", err)
	}

	return db, true, nil
}

// cargoCapacities loads class-id -> cargo capacity from the ship catalog.
// A missing catalog is tolerated (capacities read 0) so the roster still
// renders when the knowledge DB is absent, e.g. in tests.
func cargoCapacities(ctx context.Context, kbPath string) (map[string]int, error) {
	if kbPath == "" {
		return nil, nil
	}
	if _, err := os.Stat(kbPath); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	db, err := sql.Open(sqliteDriver, "file:"+kbPath+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open kb: %w", err)
	}
	defer db.Close() //nolint:errcheck

	rows, err := db.QueryContext(ctx, `SELECT id, cargo_capacity FROM ships`)
	if err != nil {
		return nil, fmt.Errorf("kb ships: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	out := map[string]int{}
	for rows.Next() {
		var (
			id string
			c  int
		)
		if err := rows.Scan(&id, &c); err != nil {
			return nil, fmt.Errorf("kb ships scan: %w", err)
		}
		out[id] = c
	}

	return out, rows.Err()
}
