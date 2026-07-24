package worker

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// freightHeldFile is the per-agent file that remembers in-flight freight contracts
// across worker restarts. A carrier's own in_transit contracts never list on the
// shipping board, so this file is the only way a restarted worker can rediscover
// and resume (or settle) them.
const freightHeldFile = "freight-held.json"

// freightHeldPath is <agentsDir>/<agentID>/freight-held.json.
func freightHeldPath(agentsDir, agentID string) string {
	return filepath.Join(agentsDir, agentID, freightHeldFile)
}

// loadHeldFreight reads the persisted held set. A missing file is not an error
// (a fresh or non-freight worker): it returns (nil, nil).
func loadHeldFreight(path string) ([]*serverapi.ShipmentContract, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("freight-held read: %w", err)
	}
	var contracts []*serverapi.ShipmentContract
	if err := json.Unmarshal(raw, &contracts); err != nil {
		return nil, fmt.Errorf("freight-held decode: %w", err)
	}
	return contracts, nil
}

// saveHeldFreight writes the held set atomically (tmp + rename), creating the agent
// directory if needed. An empty set writes "[]" so the file always reflects truth.
func saveHeldFreight(path string, contracts []*serverapi.ShipmentContract) error {
	if contracts == nil {
		contracts = []*serverapi.ShipmentContract{}
	}
	data, err := json.MarshalIndent(contracts, "", "  ")
	if err != nil {
		return fmt.Errorf("freight-held marshal: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("freight-held mkdir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("freight-held write: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("freight-held replace: %w", err)
	}
	return nil
}
