package agent

import (
	"encoding/json"
	"fmt"
	"os"
)

// LoadPersonalityJSON loads a personality from a JSON file
func LoadPersonalityJSON(path string) (Personality, error) {
	var p Personality

	data, err := os.ReadFile(path)
	if err != nil {
		return p, fmt.Errorf("failed to read personality file: %w", err)
	}

	if err := json.Unmarshal(data, &p); err != nil {
		return p, fmt.Errorf("failed to parse personality JSON: %w", err)
	}

	return p, nil
}

// SavePersonalityJSON saves a personality to a JSON file
func SavePersonalityJSON(p Personality, path string) error {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal personality: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write personality file: %w", err)
	}

	return nil
}
