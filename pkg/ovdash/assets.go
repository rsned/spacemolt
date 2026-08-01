package ovdash

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/rsned/spacemolt/pkg/assets"
)

// LoadAssetCoverage reads asset-ledger freshness from the assets DB
// (read-only). A missing or unreadable DB yields no rows and no error: the
// dashboard must render whether or not asset capture is deployed.
func LoadAssetCoverage(ctx context.Context, dbPath string, now time.Time, staleAfter time.Duration) ([]assets.CoverageRow, error) {
	if dbPath == "" {
		return nil, nil
	}
	db, err := sql.Open(sqliteDriver, "file:"+dbPath+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open assets db: %w", err)
	}
	defer db.Close() //nolint:errcheck

	rows, err := assets.Coverage(ctx, db, now, staleAfter)
	if err != nil {
		// An absent ledger is the normal pre-deploy state, not a dashboard
		// failure. Report empty and let the panel show "not deployed".
		return nil, nil
	}

	return rows, nil
}
