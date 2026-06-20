package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/rsned/spacemolt/pkg/game"
)

// loadCraftJobs reads a JSON file holding a bare array of craft/recycle job
// objects and returns them for a bulk craft request. Each entry has the shape
// the server documents for craft's "jobs" param:
// {recipe_id, quantity, facility_id?, preset?, deliver_to?}.
func loadCraftJobs(path string) ([]map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read craft jobs file %q: %w", path, err)
	}
	var jobs []map[string]any
	if err := json.Unmarshal(data, &jobs); err != nil {
		return nil, fmt.Errorf("parse craft jobs file %q (expected a JSON array of {recipe_id, quantity, ...} objects): %w", path, err)
	}
	if len(jobs) == 0 {
		return nil, fmt.Errorf("craft jobs file %q contains no jobs", path)
	}
	if len(jobs) > game.MaxCraftBulkJobs {
		return nil, fmt.Errorf("craft jobs file %q has %d jobs (max %d per request)", path, len(jobs), game.MaxCraftBulkJobs)
	}
	return jobs, nil
}
