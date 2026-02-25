// Command view-learning provides interactive access to agent learning records.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/charmbracelet/lipgloss"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// Output format constants
const (
	formatTable = "table"
	formatJSON  = "json"
)

// Sort order constants
const (
	sortTime = "time"
	sortType = "type"
)

// AgentInfo holds agent information from the database
type AgentInfo struct {
	ID      string
	Name    string
	Role    string
	Faction sql.NullString
	Status  string
}

// Styles for terminal output
var (
	// Header style - bold and with color
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("cyan"))

	// Sub-header style
	subHeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("magenta"))

	// Label style
	labelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("blue"))

	// Value style
	valueStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("white"))

	// Success style
	successStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("green"))

	// Error style
	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("red"))

	// Dim style for metadata
	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")) // Gray

	// Border style
	borderStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	// Table header style
	tableHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("cyan"))

	// Row style
	rowStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("white"))

	// Alternating row style
	altRowStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("white")).
			Background(lipgloss.Color("236"))
)

// Config holds the CLI configuration
type Config struct {
	DBPath string
	Limit  int
	Format string
	Sort   string
}

// Command represents a CLI subcommand
type Command struct {
	Name        string
	Description string
	Handler     func(ctx context.Context, db *sql.DB, cfg Config, args []string) error
}

// NOTE: Commands must be sorted alphabetically for BinarySearchFunc to work
var commands = []Command{
	{
		Name:        "agents",
		Description: "List all agents in the knowledge base",
		Handler:     cmdAgents,
	},
	{
		Name:        "experiences",
		Description: "Show experience records for an agent",
		Handler:     cmdExperiences,
	},
	{
		Name:        "history",
		Description: "Show action history for an agent",
		Handler:     cmdHistory,
	},
	{
		Name:        "summary",
		Description: "Show learning statistics for an agent",
		Handler:     cmdSummary,
	},
	{
		Name:        "systems",
		Description: "Show all discovered systems",
		Handler:     cmdSystems,
	},
}

func main() {
	// Define global flags
	cfg := Config{
		DBPath: "spacemolt-knowledge.db",
		Limit:  20,
		Format: formatTable,
		Sort:   sortTime,
	}

	flag.StringVar(&cfg.DBPath, "db-path", cfg.DBPath, "Path to SQLite database file")
	flag.IntVar(&cfg.Limit, "limit", cfg.Limit, "Limit number of records to show")
	flag.StringVar(&cfg.Format, "format", cfg.Format, "Output format: table, json")
	flag.StringVar(&cfg.Sort, "sort", cfg.Sort, "Sort order: time, type")

	// Custom usage function
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "%s\n\n", headerStyle.Render("view-learning - Interactive access to agent learning records"))
		fmt.Fprintf(os.Stderr, "Usage:\n  view-learning [global-flags] <command> [command-args]\n\n")
		fmt.Fprintf(os.Stderr, "Global Flags:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nCommands:\n")
		for _, cmd := range commands {
			fmt.Fprintf(os.Stderr, "  %-15s %s\n", cmd.Name, cmd.Description)
		}
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  view-learning agents\n")
		fmt.Fprintf(os.Stderr, "  view-learning history agent-123 --limit 50\n")
		fmt.Fprintf(os.Stderr, "  view-learning experiences agent-123 --format json\n")
		fmt.Fprintf(os.Stderr, "  view-learning systems\n")
		fmt.Fprintf(os.Stderr, "  view-learning summary agent-123\n")
	}

	flag.Parse()

	// Get remaining arguments
	args := flag.Args()

	if len(args) == 0 {
		flag.Usage()
		os.Exit(1)
	}

	// Find and execute command
	cmdName := args[0]
	cmdArgs := args[1:]

	cmd, found := slices.BinarySearchFunc(commands, cmdName, func(c Command, name string) int {
		return strings.Compare(c.Name, name)
	})

	if !found {
		fmt.Fprintf(os.Stderr, "%s Unknown command: %s\n\n", errorStyle.Render("Error:"), cmdName)
		flag.Usage()
		os.Exit(1)
	}

	// Open database connection
	db, err := openDatabase(cfg.DBPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s Failed to open database: %v\n", errorStyle.Render("Error:"), err)
		os.Exit(1)
	}
	defer func() { _ = db.Close() }()

	// Execute command
	ctx := context.Background()
	if err := commands[cmd].Handler(ctx, db, cfg, cmdArgs); err != nil {
		fmt.Fprintf(os.Stderr, "%s %v\n", errorStyle.Render("Error:"), err)
		os.Exit(1)
	}
}

// openDatabase opens the SQLite database connection
func openDatabase(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Set busy timeout
	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to set busy timeout: %w", err)
	}

	// Enable WAL mode for better concurrency
	if _, err := db.Exec("PRAGMA journal_mode = WAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
	}

	return db, nil
}

// cmdHistory shows action history for an agent
func cmdHistory(ctx context.Context, db *sql.DB, cfg Config, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: view-learning history <agent-id>")
	}
	agentID := args[0]

	// Query experiences (history)
	query := `
		SELECT id, time, type, description, outcome, location
		FROM experiences
		WHERE agent_id = ?
		ORDER BY time DESC
		LIMIT ?
	`

	rows, err := db.QueryContext(ctx, query, agentID, cfg.Limit)
	if err != nil {
		return fmt.Errorf("failed to query history: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var records []map[string]any
	for rows.Next() {
		var id int
		var timestamp, expType, description string
		var outcome, location sql.NullString

		if err := rows.Scan(&id, &timestamp, &expType, &description, &outcome, &location); err != nil {
			return fmt.Errorf("failed to scan row: %w", err)
		}

		record := map[string]any{
			"id":          id,
			"time":        timestamp,
			"type":        expType,
			"description": description,
		}
		if outcome.Valid {
			record["outcome"] = outcome.String
		}
		if location.Valid {
			record["location"] = location.String
		}

		records = append(records, record)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating rows: %w", err)
	}

	// Output based on format
	switch cfg.Format {
	case formatJSON:
		return outputJSON(records)
	default:
		outputHistoryTable(agentID, records)
		return nil
	}
}

// outputHistoryTable displays history records in table format
func outputHistoryTable(agentID string, records []map[string]any) {
	fmt.Println(headerStyle.Render(fmt.Sprintf("Action History for Agent: %s", agentID)))
	fmt.Println(borderStyle.Render(strings.Repeat("─", 80)))

	if len(records) == 0 {
		fmt.Println(dimStyle.Render("  No history records found."))
		return
	}

	for i, record := range records {
		timestamp := record["time"].(string)
		expType := record["type"].(string)
		description := record["description"].(string)

		fmt.Printf("\n%s %s %s\n",
			dimStyle.Render("["+timestamp+"]"),
			tableHeaderStyle.Render(fmt.Sprintf("[%s]", expType)),
			valueStyle.Render(description))

		if outcome, ok := record["outcome"]; ok && outcome != nil {
			fmt.Printf("  %s %s\n",
				labelStyle.Render("Outcome:"),
				successStyle.Render(outcome.(string)))
		}

		if location, ok := record["location"]; ok && location != nil {
			fmt.Printf("  %s %s\n",
				labelStyle.Render("Location:"),
				valueStyle.Render(location.(string)))
		}

		if i < len(records)-1 {
			fmt.Println(dimStyle.Render(strings.Repeat("─", 40)))
		}
	}

	fmt.Printf("\n%s %s record(s)\n",
		labelStyle.Render("Total:"),
		valueStyle.Render(fmt.Sprintf("%d", len(records))))
}

// cmdExperiences shows experience records for an agent
func cmdExperiences(ctx context.Context, db *sql.DB, cfg Config, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: view-learning experiences <agent-id>")
	}
	agentID := args[0]

	// Query experiences
	query := `
		SELECT time, type, description, outcome, location
		FROM experiences
		WHERE agent_id = ?
		ORDER BY time DESC
		LIMIT ?
	`

	// Apply sorting
	orderClause := "time DESC"
	if cfg.Sort == sortType {
		orderClause = "type ASC, time DESC"
	}

	query = strings.Replace(query, "ORDER BY time DESC", "ORDER BY "+orderClause, 1)

	rows, err := db.QueryContext(ctx, query, agentID, cfg.Limit)
	if err != nil {
		return fmt.Errorf("failed to query experiences: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var experiences []knowledge.Experience
	for rows.Next() {
		var exp knowledge.Experience
		var outcome, location sql.NullString

		if err := rows.Scan(&exp.Time, &exp.Type, &exp.Description, &outcome, &location); err != nil {
			return fmt.Errorf("failed to scan row: %w", err)
		}

		if outcome.Valid {
			exp.Outcome = outcome.String
		}
		if location.Valid {
			exp.Location = location.String
		}

		experiences = append(experiences, exp)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating rows: %w", err)
	}

	// Output based on format
	switch cfg.Format {
	case formatJSON:
		return outputJSON(experiences)
	default:
		outputExperiencesTable(agentID, experiences)
		return nil
	}
}

// outputExperiencesTable displays experiences in table format
func outputExperiencesTable(agentID string, experiences []knowledge.Experience) {
	fmt.Println(headerStyle.Render(fmt.Sprintf("Experiences for Agent: %s", agentID)))
	fmt.Println(borderStyle.Render(strings.Repeat("─", 80)))

	if len(experiences) == 0 {
		fmt.Println(dimStyle.Render("  No experience records found."))
		return
	}

	// Calculate column widths
	timeWidth := 20
	typeWidth := 15
	descWidth := 40

	// Print header
	fmt.Printf("%s  %s  %s\n",
		tableHeaderStyle.Render(padRight("Time", timeWidth)),
		tableHeaderStyle.Render(padRight("Type", typeWidth)),
		tableHeaderStyle.Render("Description"))
	fmt.Println(borderStyle.Render(strings.Repeat("─", 80)))

	// Print rows
	for i, exp := range experiences {
		style := rowStyle
		if i%2 == 1 {
			style = altRowStyle
		}

		// Truncate description if too long
		description := exp.Description
		if len(description) > descWidth {
			description = description[:descWidth-3] + "..."
		}

		fmt.Printf("%s  %s  %s\n",
			style.Render(padRight(formatTimestamp(exp.Time), timeWidth)),
			style.Render(padRight(exp.Type, typeWidth)),
			style.Render(description))

		// Print outcome and location if present
		if exp.Outcome != "" {
			fmt.Printf("  %s %s\n",
				dimStyle.Render("→"),
				successStyle.Render(exp.Outcome))
		}
		if exp.Location != "" {
			fmt.Printf("  %s %s\n",
				dimStyle.Render("→"),
				valueStyle.Render("@ "+exp.Location))
		}

		fmt.Println()
	}

	fmt.Printf("\n%s %s experience(s)\n",
		labelStyle.Render("Total:"),
		valueStyle.Render(fmt.Sprintf("%d", len(experiences))))
}

// cmdSystems shows all discovered systems
func cmdSystems(ctx context.Context, db *sql.DB, cfg Config, args []string) error {
	// Query all systems with connections
	query := `
		SELECT s.id, s.name, s.pos_x, s.pos_y, s.pos_z,
		       s.security_level, s.faction, s.visit_count,
		       s.last_visited, s.discovered_by,
		       GROUP_CONCAT(c.to_system, ',') as connections
		FROM systems s
		LEFT JOIN connections c ON s.id = c.from_system
		GROUP BY s.id
		ORDER BY s.name
		LIMIT ?
	`

	rows, err := db.QueryContext(ctx, query, cfg.Limit)
	if err != nil {
		return fmt.Errorf("failed to query systems: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var systems []struct {
		ID           string
		Name         string
		PosX, PosY   float64
		PosZ         float64
		PoliceLevel  int
		Faction      string
		VisitCount   int
		LastVisited  sql.NullString
		DiscoveredBy sql.NullString
		Connections  sql.NullString
	}

	for rows.Next() {
		var s struct {
			ID           string
			Name         string
			PosX, PosY   float64
			PosZ         float64
			PoliceLevel  int
			Faction      string
			VisitCount   int
			LastVisited  sql.NullString
			DiscoveredBy sql.NullString
			Connections  sql.NullString
		}

		if err := rows.Scan(&s.ID, &s.Name, &s.PosX, &s.PosY, &s.PosZ,
			&s.PoliceLevel, &s.Faction, &s.VisitCount,
			&s.LastVisited, &s.DiscoveredBy, &s.Connections); err != nil {
			return fmt.Errorf("failed to scan row: %w", err)
		}

		systems = append(systems, s)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating rows: %w", err)
	}

	// Output based on format
	switch cfg.Format {
	case formatJSON:
		return outputJSON(systems)
	default:
		outputSystemsTable(systems)
		return nil
	}
}

// outputSystemsTable displays systems in table format
func outputSystemsTable(systems []struct {
	ID           string
	Name         string
	PosX, PosY   float64
	PosZ         float64
	PoliceLevel  int
	Faction      string
	VisitCount   int
	LastVisited  sql.NullString
	DiscoveredBy sql.NullString
	Connections  sql.NullString
}) {
	fmt.Println(headerStyle.Render("Discovered Systems"))
	fmt.Println(borderStyle.Render(strings.Repeat("─", 80)))

	if len(systems) == 0 {
		fmt.Println(dimStyle.Render("  No systems discovered yet."))
		return
	}

	for i, s := range systems {
		fmt.Printf("\n%s", subHeaderStyle.Render(s.Name))
		fmt.Printf(" %s\n", dimStyle.Render("["+s.ID+"]"))

		// Position
		fmt.Printf("  %s (%.1f, %.1f, %.1f)\n",
			labelStyle.Render("Position:"),
			s.PosX, s.PosY, s.PosZ)

		// Police level
		if s.PoliceLevel > 0 {
			fmt.Printf("  %s %s\n",
				labelStyle.Render("Police:"),
				valueStyle.Render(fmt.Sprintf("%d", s.PoliceLevel)))
		}

		// Faction
		if s.Faction != "" {
			fmt.Printf("  %s %s\n",
				labelStyle.Render("Faction:"),
				valueStyle.Render(s.Faction))
		}

		// Visit count
		fmt.Printf("  %s %s\n",
			labelStyle.Render("Visits:"),
			successStyle.Render(fmt.Sprintf("%d", s.VisitCount)))

		// Last visited
		if s.LastVisited.Valid {
			fmt.Printf("  %s %s\n",
				labelStyle.Render("Last Visited:"),
				dimStyle.Render(formatTimestamp(s.LastVisited.String)))
		}

		// Discovered by
		if s.DiscoveredBy.Valid {
			fmt.Printf("  %s %s\n",
				labelStyle.Render("Discovered By:"),
				valueStyle.Render(s.DiscoveredBy.String))
		}

		// Connections
		if s.Connections.Valid && s.Connections.String != "" {
			connections := strings.Split(s.Connections.String, ",")
			fmt.Printf("  %s %s\n",
				labelStyle.Render("Connections:"),
				valueStyle.Render(fmt.Sprintf("%d system(s)", len(connections))))
			fmt.Printf("    %s\n", dimStyle.Render(strings.Join(connections, ", ")))
		}

		if i < len(systems)-1 {
			fmt.Println(dimStyle.Render(strings.Repeat("─", 40)))
		}
	}

	fmt.Printf("\n%s %s system(s)\n",
		labelStyle.Render("Total:"),
		valueStyle.Render(fmt.Sprintf("%d", len(systems))))
}

// cmdAgents lists all agents in the knowledge base
func cmdAgents(ctx context.Context, db *sql.DB, cfg Config, args []string) error {
	// Query all agents
	query := `
		SELECT id, name, role, faction, status
		FROM agents
		ORDER BY name
	`

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to query agents: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var agents []AgentInfo
	for rows.Next() {
		var a AgentInfo
		if err := rows.Scan(&a.ID, &a.Name, &a.Role, &a.Faction, &a.Status); err != nil {
			return fmt.Errorf("failed to scan row: %w", err)
		}
		agents = append(agents, a)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating rows: %w", err)
	}

	// Output based on format
	switch cfg.Format {
	case formatJSON:
		return outputJSON(agents)
	default:
		outputAgentsTable(agents, db)
		return nil
	}
}

// outputAgentsTable displays agents in table format
func outputAgentsTable(agents []AgentInfo, db *sql.DB) {
	fmt.Println(headerStyle.Render("Registered Agents"))
	fmt.Println(borderStyle.Render(strings.Repeat("─", 80)))

	if len(agents) == 0 {
		fmt.Println(dimStyle.Render("  No agents registered."))
		return
	}

	ctx := context.Background()

	for i, a := range agents {
		// Status indicator
		statusIcon := "✓"
		statusColor := successStyle
		if a.Status != "active" {
			statusIcon = "○"
			statusColor = dimStyle
		}

		fmt.Printf("\n%s %s", statusColor.Render(statusIcon), subHeaderStyle.Render(a.Name))
		fmt.Printf(" %s\n", dimStyle.Render("["+a.ID+"]"))

		fmt.Printf("  %s %s\n",
			labelStyle.Render("Role:"),
			valueStyle.Render(a.Role))

		if a.Faction.Valid {
			fmt.Printf("  %s %s\n",
				labelStyle.Render("Faction:"),
				valueStyle.Render(a.Faction.String))
		}

		// Get experience count
		var expCount int
		if err := db.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM experiences WHERE agent_id = ?", a.ID).Scan(&expCount); err == nil {
			fmt.Printf("  %s %s\n",
				labelStyle.Render("Experiences:"),
				valueStyle.Render(fmt.Sprintf("%d", expCount)))
		}

		// Get systems discovered count
		var sysCount int
		if err := db.QueryRowContext(ctx,
			"SELECT COUNT(DISTINCT location) FROM experiences WHERE agent_id = ? AND location IS NOT NULL", a.ID).Scan(&sysCount); err == nil {
			fmt.Printf("  %s %s\n",
				labelStyle.Render("Systems Visited:"),
				valueStyle.Render(fmt.Sprintf("%d", sysCount)))
		}

		if i < len(agents)-1 {
			fmt.Println(dimStyle.Render(strings.Repeat("─", 40)))
		}
	}

	fmt.Printf("\n%s %s agent(s)\n",
		labelStyle.Render("Total:"),
		valueStyle.Render(fmt.Sprintf("%d", len(agents))))
}

// cmdSummary shows learning statistics for an agent
func cmdSummary(ctx context.Context, db *sql.DB, cfg Config, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: view-learning summary <agent-id>")
	}
	agentID := args[0]

	// Get agent info
	var agentName, agentRole string
	var agentFaction sql.NullString
	err := db.QueryRowContext(ctx,
		"SELECT name, role, faction FROM agents WHERE id = ?",
		agentID).Scan(&agentName, &agentRole, &agentFaction)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("agent not found: %s", agentID)
	}
	if err != nil {
		return fmt.Errorf("failed to query agent: %w", err)
	}

	// Get experience statistics
	stats := struct {
		TotalExperiences  int
		ExperiencesByType map[string]int
		LastActivity      sql.NullString
		UniqueLocations   int
		SuccessRate       float64
		RecentExperiences []knowledge.Experience
	}{
		ExperiencesByType: make(map[string]int),
	}

	// Total experiences
	err = db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM experiences WHERE agent_id = ?",
		agentID).Scan(&stats.TotalExperiences)
	if err != nil {
		return fmt.Errorf("failed to count experiences: %w", err)
	}

	// Experiences by type
	rows, err := db.QueryContext(ctx,
		"SELECT type, COUNT(*) as count FROM experiences WHERE agent_id = ? GROUP BY type",
		agentID)
	if err != nil {
		return fmt.Errorf("failed to query experience types: %w", err)
	}

	for rows.Next() {
		var expType string
		var count int
		if err := rows.Scan(&expType, &count); err != nil {
			_ = rows.Close()
			return fmt.Errorf("failed to scan experience type: %w", err)
		}
		stats.ExperiencesByType[expType] = count
	}
	_ = rows.Close()

	// Last activity
	err = db.QueryRowContext(ctx,
		"SELECT MAX(time) FROM experiences WHERE agent_id = ?",
		agentID).Scan(&stats.LastActivity)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("failed to query last activity: %w", err)
	}

	// Unique locations
	err = db.QueryRowContext(ctx,
		"SELECT COUNT(DISTINCT location) FROM experiences WHERE agent_id = ? AND location IS NOT NULL",
		agentID).Scan(&stats.UniqueLocations)
	if err != nil {
		return fmt.Errorf("failed to count locations: %w", err)
	}

	// Recent experiences
	recentRows, err := db.QueryContext(ctx,
		"SELECT time, type, description, outcome, location FROM experiences WHERE agent_id = ? ORDER BY time DESC LIMIT 5",
		agentID)
	if err != nil {
		return fmt.Errorf("failed to query recent experiences: %w", err)
	}

	for recentRows.Next() {
		var exp knowledge.Experience
		var outcome, location sql.NullString
		if err := recentRows.Scan(&exp.Time, &exp.Type, &exp.Description, &outcome, &location); err != nil {
			_ = recentRows.Close()
			return fmt.Errorf("failed to scan recent experience: %w", err)
		}
		if outcome.Valid {
			exp.Outcome = outcome.String
		}
		if location.Valid {
			exp.Location = location.String
		}
		stats.RecentExperiences = append(stats.RecentExperiences, exp)
	}
	_ = recentRows.Close()

	// Output based on format
	switch cfg.Format {
	case formatJSON:
		return outputJSON(stats)
	default:
		outputSummaryTable(agentID, agentName, agentRole, stats, agentFaction)
		return nil
	}
}

// outputSummaryTable displays agent summary in table format
func outputSummaryTable(agentID, agentName, agentRole string, stats struct {
	TotalExperiences  int
	ExperiencesByType map[string]int
	LastActivity      sql.NullString
	UniqueLocations   int
	SuccessRate       float64
	RecentExperiences []knowledge.Experience
}, agentFaction sql.NullString) {
	fmt.Println(headerStyle.Render(fmt.Sprintf("Agent Summary: %s", agentName)))
	fmt.Printf("%s %s\n\n", dimStyle.Render("ID:"), dimStyle.Render(agentID))

	fmt.Println(subHeaderStyle.Render("Agent Information"))
	fmt.Println(borderStyle.Render(strings.Repeat("─", 40)))
	fmt.Printf("  %s %s\n", labelStyle.Render("Role:"), valueStyle.Render(agentRole))
	if agentFaction.Valid {
		fmt.Printf("  %s %s\n", labelStyle.Render("Faction:"), valueStyle.Render(agentFaction.String))
	}

	fmt.Println("\n" + subHeaderStyle.Render("Learning Statistics"))
	fmt.Println(borderStyle.Render(strings.Repeat("─", 40)))
	fmt.Printf("  %s %s\n", labelStyle.Render("Total Experiences:"), successStyle.Render(fmt.Sprintf("%d", stats.TotalExperiences)))
	fmt.Printf("  %s %s\n", labelStyle.Render("Unique Locations:"), valueStyle.Render(fmt.Sprintf("%d", stats.UniqueLocations)))

	if stats.LastActivity.Valid {
		fmt.Printf("  %s %s\n", labelStyle.Render("Last Activity:"), dimStyle.Render(formatTimestamp(stats.LastActivity.String)))
	}

	if len(stats.ExperiencesByType) > 0 {
		fmt.Println("\n" + subHeaderStyle.Render("Experiences by Type"))
		fmt.Println(borderStyle.Render(strings.Repeat("─", 40)))

		// Sort by count
		types := make([]string, 0, len(stats.ExperiencesByType))
		for t := range stats.ExperiencesByType {
			types = append(types, t)
		}
		sort.Slice(types, func(i, j int) bool {
			return stats.ExperiencesByType[types[i]] > stats.ExperiencesByType[types[j]]
		})

		for _, t := range types {
			count := stats.ExperiencesByType[t]
			bar := strings.Repeat("█", count)
			if len(bar) > 20 {
				bar = bar[:20]
			}
			fmt.Printf("  %s %s %s\n",
				labelStyle.Render(padRight(t+":", 20)),
				successStyle.Render(bar),
				valueStyle.Render(fmt.Sprintf("%d", count)))
		}
	}

	if len(stats.RecentExperiences) > 0 {
		fmt.Println("\n" + subHeaderStyle.Render("Recent Activity"))
		fmt.Println(borderStyle.Render(strings.Repeat("─", 40)))

		for _, exp := range stats.RecentExperiences {
			fmt.Printf("  %s %s\n",
				dimStyle.Render("["+formatTimestamp(exp.Time)+"]"),
				valueStyle.Render(exp.Description))
			if exp.Outcome != "" {
				fmt.Printf("    %s %s\n",
					dimStyle.Render("→"),
					successStyle.Render(exp.Outcome))
			}
		}
	}
}

// outputJSON outputs data as JSON
func outputJSON(data any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(data); err != nil {
		return fmt.Errorf("failed to encode JSON: %w", err)
	}
	return nil
}

// formatTimestamp formats a timestamp string for display
func formatTimestamp(ts string) string {
	// Parse the timestamp
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		// Try parsing as SQLite datetime format
		t, err = time.Parse("2006-01-02 15:04:05", ts)
		if err != nil {
			return ts
		}
	}

	// Format as relative time if recent, otherwise as short date
	now := time.Now()
	diff := now.Sub(t)

	if diff < time.Minute {
		return "just now"
	}
	if diff < time.Hour {
		return fmt.Sprintf("%d min ago", int(diff.Minutes()))
	}
	if diff < 24*time.Hour {
		return fmt.Sprintf("%d hr ago", int(diff.Hours()))
	}
	if diff < 7*24*time.Hour {
		return fmt.Sprintf("%d days ago", int(diff.Hours()/24))
	}

	return t.Format("2006-01-02")
}

// padRight pads a string to the specified width
func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

// Note: Terminal detection for color output is handled automatically by lipgloss.
// When stdout is not a terminal (e.g., redirected to a file or pipe),
// lipgloss will automatically disable colors.
