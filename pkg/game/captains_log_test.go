package game

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteCaptainsLog(t *testing.T) {
	// Create temporary test directory
	testAgentID := "test-captain-1"
	testDir := GetCaptainsLogDir(testAgentID)
	defer os.RemoveAll(filepath.Join("data", "agents", testAgentID))

	entry := &AgentLog{
		AgentName:   "Test Captain",
		CurrentGoal: "Testing captain's log functionality",
		Location:    "Test System",
		Notes:       []string{"Note 1", "Note 2"},
	}

	// Write first entry
	err := WriteCaptainsLog(testAgentID, entry)
	if err != nil {
		t.Fatalf("Failed to write captain's log: %v", err)
	}

	// Verify directory was created
	if _, err := os.Stat(testDir); os.IsNotExist(err) {
		t.Errorf("Captain's log directory was not created")
	}

	// Verify file was created
	entries, err := os.ReadDir(testDir)
	if err != nil {
		t.Fatalf("Failed to read test directory: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("Expected 1 log file, got %d", len(entries))
	}

	// Verify filename format
	if !strings.HasPrefix(entries[0].Name(), "captains_log.") ||
		!strings.HasSuffix(entries[0].Name(), ".log") {
		t.Errorf("Log file has incorrect format: %s", entries[0].Name())
	}
}

func TestWriteCaptainsLogRateLimit(t *testing.T) {
	testAgentID := "test-captain-2"
	defer os.RemoveAll(filepath.Join("data", "agents", testAgentID))

	entry := &AgentLog{
		AgentName:   "Test Captain",
		CurrentGoal: "Testing rate limiting",
		Location:    "Test System",
	}

	// Write first entry
	err := WriteCaptainsLog(testAgentID, entry)
	if err != nil {
		t.Fatalf("Failed to write first entry: %v", err)
	}

	// Try to write second entry immediately (should be skipped)
	entry.CurrentGoal = "Different goal"
	err = WriteCaptainsLog(testAgentID, entry)
	if err != nil {
		t.Fatalf("WriteCaptainsLog returned error: %v", err)
	}

	// Verify only one file exists (second write was skipped)
	testDir := GetCaptainsLogDir(testAgentID)
	entries, err := os.ReadDir(testDir)
	if err != nil {
		t.Fatalf("Failed to read test directory: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("Expected 1 log file (rate limited), got %d", len(entries))
	}
}

func TestWriteCaptainsLogNoChangeSkipped(t *testing.T) {
	testAgentID := "test-captain-3"
	defer os.RemoveAll(filepath.Join("data", "agents", testAgentID))

	entry := &AgentLog{
		AgentName:   "Test Captain",
		CurrentGoal: "Same goal",
		Location:    "Same location",
		Notes:       []string{"Same note"},
	}

	// Write first entry
	err := WriteCaptainsLog(testAgentID, entry)
	if err != nil {
		t.Fatalf("Failed to write first entry: %v", err)
	}

	// Manually adjust timestamp on first file to be old enough
	testDir := GetCaptainsLogDir(testAgentID)
	entries, _ := os.ReadDir(testDir)
	if len(entries) > 0 {
		oldFile := filepath.Join(testDir, entries[0].Name())
		oldTime := time.Now().Add(-2 * time.Minute)
		os.Chtimes(oldFile, oldTime, oldTime)
	}

	// Try to write same entry again (should be skipped due to no change)
	err = WriteCaptainsLog(testAgentID, entry)
	if err != nil {
		t.Fatalf("WriteCaptainsLog returned error: %v", err)
	}

	// Verify only one file exists (second write was skipped)
	entries, err = os.ReadDir(testDir)
	if err != nil {
		t.Fatalf("Failed to read test directory: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("Expected 1 log file (no change), got %d", len(entries))
	}
}

func TestReadLatestCaptainsLog(t *testing.T) {
	testAgentID := "test-captain-4"
	defer os.RemoveAll(filepath.Join("data", "agents", testAgentID))

	// Test reading when no logs exist
	log, err := ReadLatestCaptainsLog(testAgentID)
	if err != nil {
		t.Fatalf("ReadLatestCaptainsLog returned error for non-existent logs: %v", err)
	}
	if log != nil {
		t.Errorf("Expected nil log for non-existent agent, got: %v", log)
	}

	// Write a log entry
	entry := &AgentLog{
		AgentName:   "Test Captain",
		CurrentGoal: "First mission",
		Location:    "System Alpha",
		Notes:       []string{"Starting exploration"},
	}
	err = WriteCaptainsLog(testAgentID, entry)
	if err != nil {
		t.Fatalf("Failed to write log: %v", err)
	}

	// Read it back
	log, err = ReadLatestCaptainsLog(testAgentID)
	if err != nil {
		t.Fatalf("Failed to read latest log: %v", err)
	}
	if log == nil {
		t.Fatal("Expected log entry, got nil")
	}

	// Verify content
	if log.AgentName != entry.AgentName {
		t.Errorf("Expected AgentName %s, got %s", entry.AgentName, log.AgentName)
	}
	if log.CurrentGoal != entry.CurrentGoal {
		t.Errorf("Expected CurrentGoal %s, got %s", entry.CurrentGoal, log.CurrentGoal)
	}
	if log.Location != entry.Location {
		t.Errorf("Expected Location %s, got %s", entry.Location, log.Location)
	}
	if len(log.Notes) != len(entry.Notes) {
		t.Errorf("Expected %d notes, got %d", len(entry.Notes), len(log.Notes))
	}
}

func TestReadAllCaptainsLogs(t *testing.T) {
	testAgentID := "test-captain-5"
	defer os.RemoveAll(filepath.Join("data", "agents", testAgentID))

	// Test reading when no logs exist
	logs, err := ReadAllCaptainsLogs(testAgentID)
	if err != nil {
		t.Fatalf("ReadAllCaptainsLogs returned error for non-existent logs: %v", err)
	}
	if logs != nil {
		t.Errorf("Expected nil logs for non-existent agent, got: %v", logs)
	}

	// Write multiple log entries (manually create them with different timestamps)
	testDir := GetCaptainsLogDir(testAgentID)
	os.MkdirAll(testDir, 0755)

	entries := []*AgentLog{
		{
			Timestamp:   time.Now().Add(-3 * time.Minute),
			AgentID:     testAgentID,
			AgentName:   "Test Captain",
			CurrentGoal: "First mission",
			Location:    "System Alpha",
		},
		{
			Timestamp:   time.Now().Add(-2 * time.Minute),
			AgentID:     testAgentID,
			AgentName:   "Test Captain",
			CurrentGoal: "Second mission",
			Location:    "System Beta",
		},
		{
			Timestamp:   time.Now().Add(-1 * time.Minute),
			AgentID:     testAgentID,
			AgentName:   "Test Captain",
			CurrentGoal: "Third mission",
			Location:    "System Gamma",
		},
	}

	// Manually write log files
	for _, e := range entries {
		filename := filepath.Join(testDir, "captains_log."+e.Timestamp.Format("200601021504")+".log")
		data, _ := e.Timestamp.MarshalJSON()
		// Write a proper JSON entry
		content := `{
  "timestamp": ` + string(data) + `,
  "agent_id": "` + e.AgentID + `",
  "agent_name": "` + e.AgentName + `",
  "current_goal": "` + e.CurrentGoal + `",
  "location": "` + e.Location + `",
  "notes": []
}`
		os.WriteFile(filename, []byte(content), 0644)
	}

	// Read all logs
	logs, err = ReadAllCaptainsLogs(testAgentID)
	if err != nil {
		t.Fatalf("Failed to read all logs: %v", err)
	}
	if len(logs) != 3 {
		t.Errorf("Expected 3 logs, got %d", len(logs))
	}

	// Verify they're sorted newest first
	if logs[0].CurrentGoal != "Third mission" {
		t.Errorf("Expected newest log first, got: %s", logs[0].CurrentGoal)
	}
	if logs[2].CurrentGoal != "First mission" {
		t.Errorf("Expected oldest log last, got: %s", logs[2].CurrentGoal)
	}
}

func TestFormatCaptainsLogForDisplay(t *testing.T) {
	entry := &AgentLog{
		Timestamp:   time.Date(2026, 2, 14, 10, 30, 0, 0, time.UTC),
		AgentName:   "Captain Ironbeard",
		CurrentGoal: "Exploring uncharted systems",
		Location:    "System: frontier-17, POI: ancient_ruins",
		Notes:       []string{"Discovered rare artifacts", "Encountered hostile aliens"},
	}

	output := FormatCaptainsLogForDisplay(entry)

	// Verify output contains key information
	if !strings.Contains(output, "Captain Ironbeard") {
		t.Errorf("Output missing agent name")
	}
	if !strings.Contains(output, "Exploring uncharted systems") {
		t.Errorf("Output missing current goal")
	}
	if !strings.Contains(output, "frontier-17") {
		t.Errorf("Output missing location")
	}
	if !strings.Contains(output, "Discovered rare artifacts") {
		t.Errorf("Output missing notes")
	}
}

func TestGetCaptainsLogDir(t *testing.T) {
	agentID := "test-agent"
	expected := filepath.Join("data", "agents", agentID, "captains_log")
	result := GetCaptainsLogDir(agentID)
	if result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}
}
