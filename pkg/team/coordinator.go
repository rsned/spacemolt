package team

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// GameClientProvider returns a game client for the given agent ID.
type GameClientProvider func(agentID string) *game.Client

// TeamCoordinator manages all teams, collects status reports, and dispatches orders.
type TeamCoordinator struct {
	teams          map[string]*Team
	statusReports  map[string]*StatusReport // agentID -> latest report
	orderQueues    map[string]*OrderQueue   // agentID -> order queue
	clientProvider GameClientProvider
	actionLog      []ActionLogEntry
	mu             sync.RWMutex
	logger         *log.Logger
}

// ActionLogEntry records a team action for the combined log.
type ActionLogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	TeamID    string    `json:"team_id"`
	AgentID   string    `json:"agent_id"`
	Action    string    `json:"action"`
	Status    string    `json:"status"`
}

// NewCoordinator creates a new team coordinator.
func NewCoordinator(clientProvider GameClientProvider, logger *log.Logger) *TeamCoordinator {
	return &TeamCoordinator{
		teams:          make(map[string]*Team),
		statusReports:  make(map[string]*StatusReport),
		orderQueues:    make(map[string]*OrderQueue),
		clientProvider: clientProvider,
		logger:         logger,
	}
}

// AddTeam registers a team with the coordinator.
func (tc *TeamCoordinator) AddTeam(t Team) {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	tc.teams[t.ID] = &t
	for _, m := range t.Members {
		if _, ok := tc.orderQueues[m.AgentID]; !ok {
			tc.orderQueues[m.AgentID] = NewOrderQueue()
		}
	}
	tc.logger.Printf("team %q (%s) registered with %d members", t.Name, t.ID, len(t.Members))
}

// RemoveTeam removes a team from the coordinator.
func (tc *TeamCoordinator) RemoveTeam(teamID string) error {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	if _, ok := tc.teams[teamID]; !ok {
		return fmt.Errorf("team %q not found", teamID)
	}
	delete(tc.teams, teamID)
	return nil
}

// GetTeam returns a team by ID.
func (tc *TeamCoordinator) GetTeam(teamID string) *Team {
	tc.mu.RLock()
	defer tc.mu.RUnlock()
	return tc.teams[teamID]
}

// ListTeams returns all registered teams.
func (tc *TeamCoordinator) ListTeams() []Team {
	tc.mu.RLock()
	defer tc.mu.RUnlock()

	teams := make([]Team, 0, len(tc.teams))
	for _, t := range tc.teams {
		teams = append(teams, *t)
	}
	return teams
}

// CollectStatusReports gathers status reports from all team members.
func (tc *TeamCoordinator) CollectStatusReports(_ context.Context) {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	for _, t := range tc.teams {
		for _, m := range t.Members {
			client := tc.clientProvider(m.AgentID)
			if client == nil {
				continue
			}

			state := client.GetState()
			if state == nil {
				continue
			}

			tc.statusReports[m.AgentID] = &StatusReport{
				AgentID:       m.AgentID,
				System:        state.CurrentSystem,
				POI:           state.CurrentPOI,
				Docked:        state.Doc,
				Hull:          state.Hull,
				Fuel:          state.Fuel,
				Credits:       state.Credits,
				CargoUsed:     state.Ship.CargoUsed,
				CargoCapacity: state.Ship.CargoCapacity,
				Timestamp:     time.Now(),
			}
		}
	}
}

// GetStatusReport returns the latest status report for an agent.
func (tc *TeamCoordinator) GetStatusReport(agentID string) *StatusReport {
	tc.mu.RLock()
	defer tc.mu.RUnlock()
	return tc.statusReports[agentID]
}

// GetTeamStatus returns status reports for all members of a team.
func (tc *TeamCoordinator) GetTeamStatus(teamID string) []StatusReport {
	tc.mu.RLock()
	defer tc.mu.RUnlock()

	t, ok := tc.teams[teamID]
	if !ok {
		return nil
	}

	reports := make([]StatusReport, 0, len(t.Members))
	for _, m := range t.Members {
		if sr, ok := tc.statusReports[m.AgentID]; ok {
			reports = append(reports, *sr)
		}
	}
	return reports
}

// IssueOrder dispatches an order to all members of a team (or a specific member).
func (tc *TeamCoordinator) IssueOrder(teamID string, order Order) error {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	t, ok := tc.teams[teamID]
	if !ok {
		return fmt.Errorf("team %q not found", teamID)
	}

	for _, m := range t.Members {
		if q, ok := tc.orderQueues[m.AgentID]; ok {
			q.Push(order)
		}
	}

	tc.actionLog = append(tc.actionLog, ActionLogEntry{
		Timestamp: time.Now(),
		TeamID:    teamID,
		AgentID:   order.IssuedBy,
		Action:    fmt.Sprintf("order_%s", order.Type),
		Status:    fmt.Sprintf("dispatched to %d members", len(t.Members)),
	})

	tc.logger.Printf("order %q dispatched to team %q (%d members)", order.Type, teamID, len(t.Members))
	return nil
}

// GetOrderQueue returns the order queue for an agent.
func (tc *TeamCoordinator) GetOrderQueue(agentID string) *OrderQueue {
	tc.mu.RLock()
	defer tc.mu.RUnlock()
	return tc.orderQueues[agentID]
}

// LogAction records an action in the team log.
func (tc *TeamCoordinator) LogAction(teamID, agentID, action, status string) {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	tc.actionLog = append(tc.actionLog, ActionLogEntry{
		Timestamp: time.Now(),
		TeamID:    teamID,
		AgentID:   agentID,
		Action:    action,
		Status:    status,
	})

	// Keep last 1000 entries.
	if len(tc.actionLog) > 1000 {
		tc.actionLog = tc.actionLog[len(tc.actionLog)-1000:]
	}
}

// GetActionLog returns recent action log entries for a team.
func (tc *TeamCoordinator) GetActionLog(teamID string, limit int) []ActionLogEntry {
	tc.mu.RLock()
	defer tc.mu.RUnlock()

	if limit <= 0 {
		limit = 100
	}

	var entries []ActionLogEntry
	for i := len(tc.actionLog) - 1; i >= 0 && len(entries) < limit; i-- {
		if tc.actionLog[i].TeamID == teamID {
			entries = append(entries, tc.actionLog[i])
		}
	}
	return entries
}
