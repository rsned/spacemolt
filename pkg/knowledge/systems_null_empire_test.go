package knowledge

import (
	"context"
	"testing"
)

// A systems row with a NULL empire broke GetSystems outright on 2026-10-12
// ("scan system: converting NULL to string is unsupported"), and because the
// haul worker's stronghold guard fails CLOSED on a read error, one NULL in one
// column of one row silently blocked stronghold routing for all 22 unlocked
// haulers — the exact markets the pirate-unlock campaign existed to open.
//
// description and security_status were already COALESCE'd in the query; empire
// was not. Every nullable column must survive a NULL.
func TestGetSystemsToleratesNullTextColumns(t *testing.T) {
	kb, err := NewSQLiteKB(Config{DBPath: testDBPath(t)})
	if err != nil {
		t.Fatalf("NewSQLiteKB: %v", err)
	}
	defer func() { _ = kb.Close() }()

	// A row as a partial map import leaves it: name and position only.
	if _, err := kb.db.ExecContext(context.Background(),
		`INSERT INTO systems (id, name, position_x, position_y) VALUES ('bnki','BNKI',601.2,3947.6)`,
	); err != nil {
		t.Fatalf("insert sparse system: %v", err)
	}

	systems, err := kb.GetSystems(context.Background())
	if err != nil {
		t.Fatalf("GetSystems with a NULL empire/description/security_status: %v", err)
	}

	var found bool
	for _, s := range systems {
		if s.ID == "bnki" {
			found = true
			if s.Empire != "" || s.Description != "" || s.SecurityStatus != "" {
				t.Errorf("sparse row = %+v, want empty strings for the absent text columns", s)
			}
		}
	}
	if !found {
		t.Errorf("GetSystems returned %d systems, none of them bnki", len(systems))
	}
}
