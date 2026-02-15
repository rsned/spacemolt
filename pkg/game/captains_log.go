package game

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// AgentLog represents what an agent is currently doing and has learned.
// This is stored locally in data/agents/{AGENT_ID}/captains_log/ for agent memory across restarts.
// Not to be confused with CaptainsLogEntry which is the server's captain's log API response.
type AgentLog struct {
	Timestamp   time.Time `json:"timestamp"`
	AgentID     string    `json:"agent_id"`
	AgentName   string    `json:"agent_name"`
	CurrentGoal string    `json:"current_goal"` // What they're doing now
	Location    string    `json:"location"`     // Current system/POI
	Notes       []string  `json:"notes"`        // Things learned/observed
}

// GetCaptainsLogDir returns the path to the captain's log directory for an agent
func GetCaptainsLogDir(agentID string) string {
	return filepath.Join("data", "agents", agentID, "captains_log")
}

// ensureCaptainsLogDir creates the captain's log directory if it doesn't exist
func ensureCaptainsLogDir(agentID string) error {
	dir := GetCaptainsLogDir(agentID)
	return os.MkdirAll(dir, 0755)
}

// WriteCaptainsLog writes a captain's log entry if:
// 1. More than 1 minute has passed since last entry
// 2. The content has changed from the last entry
//
// Example usage:
//
//	entry := &game.AgentLog{
//	    AgentName:   "Ironbeard",
//	    CurrentGoal: "Exploring the galaxy, scanning every POI and docking at every station",
//	    Location:    "System: sol, POI: sol_station",
//	    Notes:       []string{"Found rare minerals at sol_belt", "Market prices favorable for ore"},
//	}
//	if err := game.WriteCaptainsLog("explorer-1", entry); err != nil {
//	    log.Printf("Failed to write captain's log: %v", err)
//	}
func WriteCaptainsLog(agentID string, entry *AgentLog) error {
	if err := ensureCaptainsLogDir(agentID); err != nil {
		return fmt.Errorf("failed to create captains_log directory: %w", err)
	}

	// Read latest entry to check if content changed
	latest, err := ReadLatestCaptainsLog(agentID)
	if err == nil && latest != nil {
		// Check if less than 1 minute has passed
		if time.Since(latest.Timestamp) < 1*time.Minute {
			return nil // Skip writing - too soon
		}

		// Check if content is the same
		if latest.CurrentGoal == entry.CurrentGoal &&
			latest.Location == entry.Location &&
			strings.Join(latest.Notes, "|") == strings.Join(entry.Notes, "|") {
			return nil // Skip writing - nothing changed
		}
	}

	// Set timestamp and agent ID
	entry.Timestamp = time.Now()
	entry.AgentID = agentID

	// Generate filename: captains_log.YYYYMMDDHHMM.log
	filename := fmt.Sprintf("captains_log.%s.log", entry.Timestamp.Format("200601021504"))
	filePath := filepath.Join(GetCaptainsLogDir(agentID), filename)

	// Marshal to JSON
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal log entry: %w", err)
	}

	// Write to file
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write log file: %w", err)
	}

	return nil
}

// ReadLatestCaptainsLog reads the most recent captain's log entry for an agent.
// Returns nil if no logs exist yet.
//
// Example usage:
//
//	log, err := game.ReadLatestCaptainsLog("explorer-1")
//	if err != nil {
//	    return err
//	}
//	if log != nil {
//	    logger.Printf("Resuming previous mission: %s", log.CurrentGoal)
//	}
func ReadLatestCaptainsLog(agentID string) (*AgentLog, error) {
	dir := GetCaptainsLogDir(agentID)

	// Read directory
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No logs yet
		}
		return nil, fmt.Errorf("failed to read captains_log directory: %w", err)
	}

	// Filter for .log files and sort by name (timestamp is in filename)
	var logFiles []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".log") {
			logFiles = append(logFiles, entry.Name())
		}
	}

	if len(logFiles) == 0 {
		return nil, nil // No logs yet
	}

	// Sort descending (latest first)
	sort.Sort(sort.Reverse(sort.StringSlice(logFiles)))

	// Read the latest file
	latestFile := filepath.Join(dir, logFiles[0])
	data, err := os.ReadFile(latestFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read log file: %w", err)
	}

	var entry AgentLog
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, fmt.Errorf("failed to parse log file: %w", err)
	}

	return &entry, nil
}

// ReadAllCaptainsLogs reads all captain's log entries for an agent, newest first.
// Returns nil if no logs exist yet.
//
// Example usage:
//
//	logs, err := game.ReadAllCaptainsLogs("explorer-1")
//	if err != nil {
//	    return err
//	}
//	for _, log := range logs {
//	    fmt.Printf("[%s] %s: %s\n", log.Timestamp.Format(time.RFC3339), log.Location, log.CurrentGoal)
//	}
func ReadAllCaptainsLogs(agentID string) ([]*AgentLog, error) {
	dir := GetCaptainsLogDir(agentID)

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read captains_log directory: %w", err)
	}

	var logFiles []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".log") {
			logFiles = append(logFiles, entry.Name())
		}
	}

	if len(logFiles) == 0 {
		return nil, nil
	}

	// Sort descending (newest first)
	sort.Sort(sort.Reverse(sort.StringSlice(logFiles)))

	var logs []*AgentLog
	for _, filename := range logFiles {
		filePath := filepath.Join(dir, filename)
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue // Skip files we can't read
		}

		var entry AgentLog
		if err := json.Unmarshal(data, &entry); err != nil {
			continue // Skip invalid files
		}

		logs = append(logs, &entry)
	}

	return logs, nil
}

// FormatCaptainsLogForDisplay formats a captain's log entry for human-readable display
func FormatCaptainsLogForDisplay(entry *AgentLog) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("=== Captain's Log - %s ===\n", entry.Timestamp.Format("2006-01-02 15:04")))
	sb.WriteString(fmt.Sprintf("Captain: %s\n", entry.AgentName))
	sb.WriteString(fmt.Sprintf("Location: %s\n", entry.Location))
	sb.WriteString(fmt.Sprintf("Mission: %s\n", entry.CurrentGoal))
	if len(entry.Notes) > 0 {
		sb.WriteString("Notes:\n")
		for _, note := range entry.Notes {
			sb.WriteString(fmt.Sprintf("  - %s\n", note))
		}
	}
	return sb.String()
}
