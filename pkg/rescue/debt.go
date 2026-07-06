package rescue

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Debt is one owed rescue-fee reimbursement: the strandee gifts Credits to the
// rescuer's in-game username Recipient once it is next docked.
type Debt struct {
	Recipient string `json:"recipient"`
	Credits   int    `json:"credits"`
}

// debtPath is the strandee's outstanding-debt file.
func debtPath(agentsDir, strandeeID string) string {
	return filepath.Join(agentsDir, strandeeID, "rescue-debts.json")
}

// LoadDebts reads a strandee's outstanding rescue debts. A missing file is not
// an error — it means no debts.
func LoadDebts(agentsDir, strandeeID string) ([]Debt, error) {
	b, err := os.ReadFile(debtPath(agentsDir, strandeeID))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("rescue: read debts %s: %w", strandeeID, err)
	}
	var debts []Debt
	if err := json.Unmarshal(b, &debts); err != nil {
		return nil, fmt.Errorf("rescue: parse debts %s: %w", strandeeID, err)
	}
	return debts, nil
}

// writeDebts writes the list, or removes the file when the list is empty.
func writeDebts(agentsDir, strandeeID string, debts []Debt) error {
	p := debtPath(agentsDir, strandeeID)
	if len(debts) == 0 {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("rescue: clear debts %s: %w", strandeeID, err)
		}
		return nil
	}
	b, err := json.Marshal(debts)
	if err != nil {
		return fmt.Errorf("rescue: marshal debts %s: %w", strandeeID, err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("rescue: mkdir debts %s: %w", strandeeID, err)
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		return fmt.Errorf("rescue: write debts %s: %w", strandeeID, err)
	}
	return nil
}

// AppendDebt adds one debt to the strandee's list, creating the file if absent.
func AppendDebt(agentsDir, strandeeID string, d Debt) error {
	debts, err := LoadDebts(agentsDir, strandeeID)
	if err != nil {
		return err
	}
	return writeDebts(agentsDir, strandeeID, append(debts, d))
}

// RemoveHead drops the first debt and rewrites the file (removing it when the
// list empties). A missing/empty file is a no-op.
func RemoveHead(agentsDir, strandeeID string) error {
	debts, err := LoadDebts(agentsDir, strandeeID)
	if err != nil {
		return err
	}
	if len(debts) == 0 {
		return nil
	}
	return writeDebts(agentsDir, strandeeID, debts[1:])
}
