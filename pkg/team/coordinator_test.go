package team

import (
	"context"
	"log"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

func newTestLogger() *log.Logger {
	return log.New(os.Stderr, "test: ", 0)
}

func newTestCoordinator() *TeamCoordinator {
	return NewCoordinator(func(_ string) *game.Client { return nil }, newTestLogger())
}

func newTestTeam() Team {
	return Team{
		ID:       "team-1",
		Name:     "Alpha Squad",
		LeaderID: "leader-1",
		Members: []Member{
			{AgentID: "agent-1", Role: "miner", Strategy: "mining"},
			{AgentID: "agent-2", Role: "fighter", Strategy: "combat"},
		},
	}
}

func TestNewCoordinator(t *testing.T) {
	tc := newTestCoordinator()
	if tc == nil {
		t.Fatal("NewCoordinator returned nil")
	}
	if tc.teams == nil {
		t.Error("teams map should be initialized")
	}
	if tc.statusReports == nil {
		t.Error("statusReports map should be initialized")
	}
	if tc.orderQueues == nil {
		t.Error("orderQueues map should be initialized")
	}
}

func TestAddTeam(t *testing.T) {
	tc := newTestCoordinator()
	team := newTestTeam()
	tc.AddTeam(team)

	got := tc.GetTeam("team-1")
	if got == nil {
		t.Fatal("team not found after AddTeam")
	}
	if got.Name != "Alpha Squad" {
		t.Errorf("team name: got %q, want %q", got.Name, "Alpha Squad")
	}
	if len(got.Members) != 2 {
		t.Errorf("member count: got %d, want 2", len(got.Members))
	}
}

func TestAddTeamCreatesOrderQueues(t *testing.T) {
	tc := newTestCoordinator()
	team := newTestTeam()
	tc.AddTeam(team)

	q1 := tc.GetOrderQueue("agent-1")
	q2 := tc.GetOrderQueue("agent-2")
	if q1 == nil {
		t.Error("order queue for agent-1 should exist")
	}
	if q2 == nil {
		t.Error("order queue for agent-2 should exist")
	}
}

func TestRemoveTeam(t *testing.T) {
	tc := newTestCoordinator()
	team := newTestTeam()
	tc.AddTeam(team)

	err := tc.RemoveTeam("team-1")
	if err != nil {
		t.Fatalf("RemoveTeam error: %v", err)
	}

	got := tc.GetTeam("team-1")
	if got != nil {
		t.Error("team should be nil after removal")
	}
}

func TestRemoveTeamNotFound(t *testing.T) {
	tc := newTestCoordinator()
	err := tc.RemoveTeam("nonexistent")
	if err == nil {
		t.Error("expected error for removing nonexistent team")
	}
}

func TestGetTeamNotFound(t *testing.T) {
	tc := newTestCoordinator()
	got := tc.GetTeam("nonexistent")
	if got != nil {
		t.Error("expected nil for nonexistent team")
	}
}

func TestListTeams(t *testing.T) {
	tc := newTestCoordinator()

	teams := tc.ListTeams()
	if len(teams) != 0 {
		t.Errorf("expected 0 teams, got %d", len(teams))
	}

	tc.AddTeam(Team{ID: "t1", Name: "Team 1", Members: []Member{{AgentID: "a1"}}})
	tc.AddTeam(Team{ID: "t2", Name: "Team 2", Members: []Member{{AgentID: "a2"}}})

	teams = tc.ListTeams()
	if len(teams) != 2 {
		t.Errorf("expected 2 teams, got %d", len(teams))
	}
}

func TestGetStatusReportNotFound(t *testing.T) {
	tc := newTestCoordinator()
	sr := tc.GetStatusReport("nonexistent")
	if sr != nil {
		t.Error("expected nil for nonexistent agent status report")
	}
}

func TestGetTeamStatusNotFound(t *testing.T) {
	tc := newTestCoordinator()
	reports := tc.GetTeamStatus("nonexistent")
	if reports != nil {
		t.Error("expected nil for nonexistent team status")
	}
}

func TestGetTeamStatusEmpty(t *testing.T) {
	tc := newTestCoordinator()
	tc.AddTeam(newTestTeam())

	// No status reports collected yet
	reports := tc.GetTeamStatus("team-1")
	if len(reports) != 0 {
		t.Errorf("expected 0 reports, got %d", len(reports))
	}
}

func TestCollectStatusReportsNilClients(t *testing.T) {
	tc := newTestCoordinator()
	tc.AddTeam(newTestTeam())

	// clientProvider returns nil, so no reports should be created
	tc.CollectStatusReports(context.Background())

	sr := tc.GetStatusReport("agent-1")
	if sr != nil {
		t.Error("expected nil report when client is nil")
	}
}

func TestIssueOrder(t *testing.T) {
	tc := newTestCoordinator()
	tc.AddTeam(newTestTeam())

	order := Order{
		ID:       "order-1",
		Type:     "move",
		Target:   "Sol",
		Priority: 5,
		IssuedBy: "leader-1",
	}

	err := tc.IssueOrder("team-1", order)
	if err != nil {
		t.Fatalf("IssueOrder error: %v", err)
	}

	q1 := tc.GetOrderQueue("agent-1")
	if q1 == nil {
		t.Fatal("order queue for agent-1 should exist")
	}
	if q1.Len() != 1 {
		t.Errorf("queue length: got %d, want 1", q1.Len())
	}

	q2 := tc.GetOrderQueue("agent-2")
	if q2 == nil {
		t.Fatal("order queue for agent-2 should exist")
	}
	if q2.Len() != 1 {
		t.Errorf("queue length: got %d, want 1", q2.Len())
	}
}

func TestIssueOrderTeamNotFound(t *testing.T) {
	tc := newTestCoordinator()
	err := tc.IssueOrder("nonexistent", Order{Type: "move"})
	if err == nil {
		t.Error("expected error for nonexistent team")
	}
}

func TestGetOrderQueueNotFound(t *testing.T) {
	tc := newTestCoordinator()
	q := tc.GetOrderQueue("nonexistent")
	if q != nil {
		t.Error("expected nil for nonexistent agent order queue")
	}
}

func TestLogAction(t *testing.T) {
	tc := newTestCoordinator()
	tc.LogAction("team-1", "agent-1", "mine", "success")

	entries := tc.GetActionLog("team-1", 10)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].TeamID != "team-1" {
		t.Errorf("TeamID: got %q, want %q", entries[0].TeamID, "team-1")
	}
	if entries[0].AgentID != "agent-1" {
		t.Errorf("AgentID: got %q, want %q", entries[0].AgentID, "agent-1")
	}
	if entries[0].Action != "mine" {
		t.Errorf("Action: got %q, want %q", entries[0].Action, "mine")
	}
	if entries[0].Status != "success" {
		t.Errorf("Status: got %q, want %q", entries[0].Status, "success")
	}
}

func TestLogActionTruncation(t *testing.T) {
	tc := newTestCoordinator()

	// Add more than 1000 entries
	for i := range 1100 {
		_ = i
		tc.LogAction("team-1", "agent-1", "mine", "ok")
	}

	// Internal log should be capped at 1000
	tc.mu.RLock()
	logLen := len(tc.actionLog)
	tc.mu.RUnlock()

	if logLen > 1000 {
		t.Errorf("action log should be capped at 1000, got %d", logLen)
	}
}

func TestGetActionLogLimit(t *testing.T) {
	tc := newTestCoordinator()

	for range 20 {
		tc.LogAction("team-1", "agent-1", "mine", "ok")
	}

	entries := tc.GetActionLog("team-1", 5)
	if len(entries) != 5 {
		t.Errorf("expected 5 entries, got %d", len(entries))
	}
}

func TestGetActionLogDefaultLimit(t *testing.T) {
	tc := newTestCoordinator()

	for range 5 {
		tc.LogAction("team-1", "agent-1", "mine", "ok")
	}

	entries := tc.GetActionLog("team-1", 0) // 0 should default to 100
	if len(entries) != 5 {
		t.Errorf("expected 5 entries, got %d", len(entries))
	}
}

func TestGetActionLogFiltersByTeam(t *testing.T) {
	tc := newTestCoordinator()

	tc.LogAction("team-1", "a1", "mine", "ok")
	tc.LogAction("team-2", "a2", "trade", "ok")
	tc.LogAction("team-1", "a1", "dock", "ok")

	entries := tc.GetActionLog("team-1", 10)
	if len(entries) != 2 {
		t.Errorf("expected 2 entries for team-1, got %d", len(entries))
	}

	entries2 := tc.GetActionLog("team-2", 10)
	if len(entries2) != 1 {
		t.Errorf("expected 1 entry for team-2, got %d", len(entries2))
	}
}

func TestGetActionLogEmpty(t *testing.T) {
	tc := newTestCoordinator()
	entries := tc.GetActionLog("team-1", 10)
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

// OrderQueue tests

func TestNewOrderQueue(t *testing.T) {
	q := NewOrderQueue()
	if q == nil {
		t.Fatal("NewOrderQueue returned nil")
	}
	if q.Len() != 0 {
		t.Errorf("new queue should be empty, got len %d", q.Len())
	}
}

func TestOrderQueuePushPop(t *testing.T) {
	q := NewOrderQueue()
	q.Push(Order{ID: "o1", Type: "move", Priority: 1})
	q.Push(Order{ID: "o2", Type: "mine", Priority: 5})
	q.Push(Order{ID: "o3", Type: "dock", Priority: 3})

	if q.Len() != 3 {
		t.Fatalf("queue length: got %d, want 3", q.Len())
	}

	// Should pop highest priority first (5)
	o := q.Pop()
	if o == nil {
		t.Fatal("Pop returned nil")
	}
	if o.ID != "o2" {
		t.Errorf("expected o2 (priority 5), got %q", o.ID)
	}

	// Next highest (3)
	o = q.Pop()
	if o == nil {
		t.Fatal("Pop returned nil")
	}
	if o.ID != "o3" {
		t.Errorf("expected o3 (priority 3), got %q", o.ID)
	}

	// Last one (1)
	o = q.Pop()
	if o == nil {
		t.Fatal("Pop returned nil")
	}
	if o.ID != "o1" {
		t.Errorf("expected o1 (priority 1), got %q", o.ID)
	}

	// Empty
	o = q.Pop()
	if o != nil {
		t.Error("Pop on empty queue should return nil")
	}
}

func TestOrderQueuePopEmpty(t *testing.T) {
	q := NewOrderQueue()
	o := q.Pop()
	if o != nil {
		t.Error("Pop on empty queue should return nil")
	}
}

func TestOrderQueueExpiredOrders(t *testing.T) {
	q := NewOrderQueue()

	// Add an expired order
	q.Push(Order{
		ID:        "expired",
		Type:      "move",
		Priority:  10,
		ExpiresAt: time.Now().Add(-time.Hour),
	})

	// Add a valid order
	q.Push(Order{
		ID:       "valid",
		Type:     "mine",
		Priority: 1,
	})

	o := q.Pop()
	if o == nil {
		t.Fatal("Pop returned nil")
	}
	if o.ID != "valid" {
		t.Errorf("expected valid order, got %q (expired should be filtered)", o.ID)
	}

	if q.Len() != 0 {
		t.Errorf("queue should be empty after popping, got %d", q.Len())
	}
}

func TestOrderQueueNoExpiry(t *testing.T) {
	q := NewOrderQueue()
	q.Push(Order{
		ID:       "no-expiry",
		Type:     "move",
		Priority: 1,
		// ExpiresAt is zero value -- should not expire
	})

	o := q.Pop()
	if o == nil {
		t.Fatal("Pop returned nil")
	}
	if o.ID != "no-expiry" {
		t.Errorf("expected no-expiry, got %q", o.ID)
	}
}

func TestOrderQueueConcurrent(t *testing.T) {
	q := NewOrderQueue()

	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			q.Push(Order{
				ID:       "order",
				Type:     "move",
				Priority: id,
			})
		}(i)
	}
	wg.Wait()

	if q.Len() != 50 {
		t.Errorf("expected 50 orders, got %d", q.Len())
	}

	// Pop all concurrently
	var wg2 sync.WaitGroup
	for range 50 {
		wg2.Add(1)
		go func() {
			defer wg2.Done()
			q.Pop()
		}()
	}
	wg2.Wait()

	if q.Len() != 0 {
		t.Errorf("expected 0 orders after popping all, got %d", q.Len())
	}
}

func TestCoordinatorConcurrentAccess(t *testing.T) {
	tc := newTestCoordinator()
	team := newTestTeam()
	tc.AddTeam(team)

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(4)
		go func() {
			defer wg.Done()
			_ = tc.ListTeams()
		}()
		go func() {
			defer wg.Done()
			_ = tc.GetTeam("team-1")
		}()
		go func() {
			defer wg.Done()
			tc.LogAction("team-1", "agent-1", "mine", "ok")
		}()
		go func() {
			defer wg.Done()
			_ = tc.GetActionLog("team-1", 10)
		}()
	}
	wg.Wait()
}

func TestIssueOrderCreatesActionLog(t *testing.T) {
	tc := newTestCoordinator()
	tc.AddTeam(newTestTeam())

	order := Order{
		ID:       "order-1",
		Type:     "regroup",
		IssuedBy: "leader-1",
	}

	if err := tc.IssueOrder("team-1", order); err != nil {
		t.Fatal(err)
	}

	entries := tc.GetActionLog("team-1", 10)
	if len(entries) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(entries))
	}
	if entries[0].Action != "order_regroup" {
		t.Errorf("action: got %q, want %q", entries[0].Action, "order_regroup")
	}
}

func TestMemberStruct(t *testing.T) {
	m := Member{
		AgentID:  "a1",
		Role:     "scout",
		Strategy: "explore",
	}
	if m.AgentID != "a1" {
		t.Errorf("AgentID: got %q, want %q", m.AgentID, "a1")
	}
	if m.Role != "scout" {
		t.Errorf("Role: got %q, want %q", m.Role, "scout")
	}
}

func TestStatusReportStruct(t *testing.T) {
	sr := StatusReport{
		AgentID:       "a1",
		System:        "Sol",
		POI:           "Station Alpha",
		Docked:        true,
		Hull:          100.0,
		Fuel:          50.0,
		Credits:       1000.0,
		CargoUsed:     25.0,
		CargoCapacity: 100.0,
		Timestamp:     time.Now(),
	}

	if sr.AgentID != "a1" {
		t.Errorf("AgentID: got %q, want %q", sr.AgentID, "a1")
	}
	if !sr.Docked {
		t.Error("Docked should be true")
	}
	if sr.Hull != 100.0 {
		t.Errorf("Hull: got %f, want 100.0", sr.Hull)
	}
}

func TestOrderStruct(t *testing.T) {
	expires := time.Now().Add(time.Hour)
	o := Order{
		ID:        "o1",
		Type:      "attack",
		Target:    "enemy-base",
		Priority:  10,
		IssuedBy:  "commander",
		ExpiresAt: expires,
	}

	if o.ID != "o1" {
		t.Errorf("ID: got %q, want %q", o.ID, "o1")
	}
	if o.Type != "attack" {
		t.Errorf("Type: got %q, want %q", o.Type, "attack")
	}
	if o.Priority != 10 {
		t.Errorf("Priority: got %d, want 10", o.Priority)
	}
	if !o.ExpiresAt.Equal(expires) {
		t.Error("ExpiresAt mismatch")
	}
}
