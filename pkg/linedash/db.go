package linedash

import (
	"database/sql"
	"fmt"
	"os"
)

// openReadOnly opens assets.db read-only. A missing file is an error.
func openReadOnly(path string) (*sql.DB, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("stat assets db: %w", err)
	}
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open assets db: %w", err)
	}

	return db, nil
}
