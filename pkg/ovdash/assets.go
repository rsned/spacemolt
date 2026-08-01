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

// LoadAssetCoverage reads asset-ledger freshness from the assets DB
// (read-only). A DB that does not exist on disk yields no rows and no error
// — the normal pre-deploy state, so the dashboard renders "not deployed"
// rather than logging noise. A DB that DOES exist but fails to open or query
// (corruption, a missing table, SQLITE_BUSY under the hourly write herd) is
// a real failure and must be returned as an error, not swallowed: the caller
// keeps the last-good reading on error and otherwise overwrites it, so
// conflating "absent" with "broken" here would silently discard good data.
func LoadAssetCoverage(ctx context.Context, dbPath string, now time.Time, staleAfter time.Duration) ([]assets.CoverageRow, error) {
	if dbPath == "" {
		return nil, nil
	}
	if _, err := os.Stat(dbPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat assets db: %w", err)
	}

	db, err := sql.Open(sqliteDriver, "file:"+dbPath+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open assets db: %w", err)
	}
	defer db.Close() //nolint:errcheck

	rows, err := assets.Coverage(ctx, db, now, staleAfter)
	if err != nil {
		return nil, fmt.Errorf("assets coverage: %w", err)
	}

	return rows, nil
}
