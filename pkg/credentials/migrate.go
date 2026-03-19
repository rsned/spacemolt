package credentials

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
)

// MigrateCredentials migrates per-agent credential files from an old directory
// structure to a new one.
//
// Old layout: oldDir/AGENT_ID/credentials.json
// New layout: newDir/AGENT_ID/credentials.json
//
// Existing credentials at the new location are not overwritten.
// Returns the number of credentials migrated.
func MigrateCredentials(oldDir, newDir string) (int, error) {
	if oldDir == newDir {
		return 0, nil
	}

	if _, err := os.Stat(oldDir); os.IsNotExist(err) {
		return 0, nil
	}

	entries, err := os.ReadDir(oldDir)
	if err != nil {
		return 0, fmt.Errorf("reading old credentials directory: %w", err)
	}

	migratedCount := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		agentID := entry.Name()
		oldCredPath := filepath.Join(oldDir, agentID, "credentials.json")
		newCredPath := filepath.Join(newDir, agentID, "credentials.json")

		if _, err := os.Stat(oldCredPath); os.IsNotExist(err) {
			continue
		}

		if _, err := os.Stat(newCredPath); err == nil {
			log.Printf("  [%s] credentials already exist at new location, skipping", agentID)
			continue
		}

		newAgentDir := filepath.Join(newDir, agentID)
		if err := os.MkdirAll(newAgentDir, 0755); err != nil {
			log.Printf("  [%s] failed to create directory: %v", agentID, err)
			continue
		}

		data, err := os.ReadFile(oldCredPath)
		if err != nil {
			log.Printf("  [%s] failed to read old credentials: %v", agentID, err)
			continue
		}

		if err := os.WriteFile(newCredPath, data, 0600); err != nil {
			log.Printf("  [%s] failed to write new credentials: %v", agentID, err)
			continue
		}

		log.Printf("  [%s] migrated credentials to %s", agentID, newCredPath)
		migratedCount++
	}

	return migratedCount, nil
}
