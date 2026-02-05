package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// Mock agent for testing
type mockAgent struct {
	id          string
	decisionFn  func(ctx context.Context, state *game.State) (Decision, error)
	learnFn     func(result ActionResult) error
	startCalled bool
	stopCalled  bool
}

func (m *mockAgent) ID() string                   { return m.id }
func (m *mockAgent) Name() string                 { return m.id }
func (m *mockAgent) Personality() Personality     { return Personality{} }
func (m *mockAgent) Memory() Memory               { return nil }
func (m *mockAgent) Status() Status               { return Status{} }

func (m *mockAgent) Decide(ctx context.Context, state *game.State) (Decision, error) {
	if m.decisionFn != nil {
		return m.decisionFn(ctx, state)
	}
	return Decision{Action: "wait"}, nil
}

func (m *mockAgent) Learn(result ActionResult) error {
	if m.learnFn != nil {
		return m.learnFn(result)
	}
	return nil
}

func (m *mockAgent) Start(ctx context.Context) error {
	m.startCalled = true
	return nil
}

func (m *mockAgent) Stop() error {
	m.stopCalled = true
	return nil
}

// Tactical Action Queue methods (stubs for testing)
func (m *mockAgent) EnqueueActions(actions []PlannedAction) {}
func (m *mockAgent) DequeueAction() (*PlannedAction, bool) { return nil, false }
func (m *mockAgent) GetActionQueue() []PlannedAction       { return nil }
func (m *mockAgent) ClearActionQueue(reason string)        {}
func (m *mockAgent) SetUsingQueuedAction(using bool)       {}
func (m *mockAgent) IsUsingQueuedAction() bool             { return false }

// Mock game client for testing
type mockGameClient struct {
	state           *game.State
	actionsRecorded []string
}

func newMockGameClient() *mockGameClient {
	return &mockGameClient{
		state: &game.State{
			CurrentTick: 100,
		},
		actionsRecorded: []string{},
	}
}

func (m *mockGameClient) GetState() *game.State {
	return m.state
}

// Connection methods
func (m *mockGameClient) Connect(ctx context.Context) error   { return nil }
func (m *mockGameClient) Close() error                        { return nil }
func (m *mockGameClient) IsConnected() bool                   { return true }
func (m *mockGameClient) Ready() <-chan struct{}              { ch := make(chan struct{}); close(ch); return ch }
func (m *mockGameClient) Login(ctx context.Context) error     { return nil }
func (m *mockGameClient) Register(ctx context.Context, empire string) error { return nil }

// Action methods
func (m *mockGameClient) Undock(ctx context.Context) error {
	m.actionsRecorded = append(m.actionsRecorded, "undock")
	return nil
}

func (m *mockGameClient) Dock(ctx context.Context) error {
	m.actionsRecorded = append(m.actionsRecorded, "dock")
	return nil
}

func (m *mockGameClient) Travel(ctx context.Context, targetPOI string) error {
	m.actionsRecorded = append(m.actionsRecorded, "travel:"+targetPOI)
	return nil
}

func (m *mockGameClient) Jump(ctx context.Context, targetSystem string) error {
	m.actionsRecorded = append(m.actionsRecorded, "jump:"+targetSystem)
	return nil
}

func (m *mockGameClient) Mine(ctx context.Context) error {
	m.actionsRecorded = append(m.actionsRecorded, "mine")
	return nil
}

func (m *mockGameClient) Scan(ctx context.Context) error {
	m.actionsRecorded = append(m.actionsRecorded, "scan")
	return nil
}

func (m *mockGameClient) GetStatus(ctx context.Context) error {
	m.actionsRecorded = append(m.actionsRecorded, "get_status")
	return nil
}

func (m *mockGameClient) GetSystem(ctx context.Context) error {
	m.actionsRecorded = append(m.actionsRecorded, "get_system")
	return nil
}

func TestRunner_StartAndStop(t *testing.T) {
	agent := &mockAgent{id: "test-agent"}
	client := newMockGameClient()

	config := DefaultRunnerConfig()
	config.DecisionInterval = 100 * time.Millisecond

	runner := NewRunner(agent, client, config)

	ctx := context.Background()

	// Start runner
	if err := runner.Start(ctx); err != nil {
		t.Fatalf("Failed to start runner: %v", err)
	}

	if !agent.startCalled {
		t.Error("Agent.Start() was not called")
	}

	if !runner.IsRunning() {
		t.Error("Runner should be running")
	}

	// Stop runner
	if err := runner.Stop(); err != nil {
		t.Fatalf("Failed to stop runner: %v", err)
	}

	if !agent.stopCalled {
		t.Error("Agent.Stop() was not called")
	}

	// Wait a moment for goroutine to exit
	time.Sleep(200 * time.Millisecond)

	if runner.IsRunning() {
		t.Error("Runner should not be running after stop")
	}
}

func TestRunner_ExecuteCycle_ActionCommand(t *testing.T) {
	decisionCount := 0
	agent := &mockAgent{
		id: "test-agent",
		decisionFn: func(ctx context.Context, state *game.State) (Decision, error) {
			decisionCount++
			return Decision{
				Action:     "mine",
				Reasoning:  "Mining resources",
				Confidence: 0.9,
			}, nil
		},
	}

	client := newMockGameClient()
	client.state.CurrentTick = 100

	config := DefaultRunnerConfig()
	runner := NewRunner(agent, client, config)

	ctx := context.Background()

	// Execute one cycle
	if err := runner.executeCycle(ctx); err != nil {
		t.Fatalf("executeCycle failed: %v", err)
	}

	if decisionCount != 1 {
		t.Errorf("Expected 1 decision, got %d", decisionCount)
	}

	if len(client.actionsRecorded) != 1 {
		t.Fatalf("Expected 1 action recorded, got %d", len(client.actionsRecorded))
	}

	if client.actionsRecorded[0] != "mine" {
		t.Errorf("Expected 'mine' action, got %s", client.actionsRecorded[0])
	}

	// Check that last action tick was updated
	if runner.GetLastActionTick() != 100 {
		t.Errorf("Expected lastActionTick=100, got %d", runner.GetLastActionTick())
	}
}

func TestRunner_ExecuteCycle_QueryCommand(t *testing.T) {
	agent := &mockAgent{
		id: "test-agent",
		decisionFn: func(ctx context.Context, state *game.State) (Decision, error) {
			return Decision{
				Action:     "get_status",
				Reasoning:  "Checking status",
				Confidence: 1.0,
			}, nil
		},
	}

	client := newMockGameClient()
	client.state.CurrentTick = 100

	config := DefaultRunnerConfig()
	runner := NewRunner(agent, client, config)

	ctx := context.Background()

	// Execute cycle
	if err := runner.executeCycle(ctx); err != nil {
		t.Fatalf("executeCycle failed: %v", err)
	}

	if len(client.actionsRecorded) != 1 {
		t.Fatalf("Expected 1 action recorded, got %d", len(client.actionsRecorded))
	}

	if client.actionsRecorded[0] != "get_status" {
		t.Errorf("Expected 'get_status' action, got %s", client.actionsRecorded[0])
	}

	// Check that last action tick was NOT updated (query commands don't consume tick)
	if runner.GetLastActionTick() != 0 {
		t.Errorf("Expected lastActionTick=0 for query command, got %d", runner.GetLastActionTick())
	}
}

func TestRunner_ThrottleActionCommands(t *testing.T) {
	callCount := 0
	agent := &mockAgent{
		id: "test-agent",
		decisionFn: func(ctx context.Context, state *game.State) (Decision, error) {
			callCount++
			return Decision{
				Action:     "mine",
				Reasoning:  "Mining",
				Confidence: 0.9,
			}, nil
		},
	}

	client := newMockGameClient()
	client.state.CurrentTick = 100

	config := DefaultRunnerConfig()
	runner := NewRunner(agent, client, config)
	runner.lastActionTick = 100 // Already acted this tick

	ctx := context.Background()

	// Execute cycle - should be throttled
	if err := runner.executeCycle(ctx); err != nil {
		t.Fatalf("executeCycle failed: %v", err)
	}

	// Agent should still be called for decision
	if callCount != 1 {
		t.Errorf("Expected agent to be called once, got %d", callCount)
	}

	// But action should NOT be executed
	if len(client.actionsRecorded) != 0 {
		t.Errorf("Expected 0 actions (throttled), got %d", len(client.actionsRecorded))
	}

	// Now advance tick and try again
	client.state.CurrentTick = 101
	if err := runner.executeCycle(ctx); err != nil {
		t.Fatalf("executeCycle failed: %v", err)
	}

	// Now action should execute
	if len(client.actionsRecorded) != 1 {
		t.Errorf("Expected 1 action after tick advance, got %d", len(client.actionsRecorded))
	}
}

func TestRunner_DecisionError_Retries(t *testing.T) {
	errorCount := 0
	agent := &mockAgent{
		id: "test-agent",
		decisionFn: func(ctx context.Context, state *game.State) (Decision, error) {
			errorCount++
			if errorCount < 3 {
				return Decision{}, errors.New("temporary error")
			}
			return Decision{Action: "wait"}, nil
		},
	}

	client := newMockGameClient()
	config := DefaultRunnerConfig()
	config.MaxRetries = 5
	config.DecisionInterval = 50 * time.Millisecond

	runner := NewRunner(agent, client, config)

	ctx := context.Background()
	if err := runner.Start(ctx); err != nil {
		t.Fatalf("Failed to start runner: %v", err)
	}
	defer runner.Stop()

	// Wait for several cycles
	time.Sleep(300 * time.Millisecond)

	// Should have retried and eventually succeeded
	if errorCount < 3 {
		t.Errorf("Expected at least 3 decision calls, got %d", errorCount)
	}

	if runner.HasCrashed() {
		t.Error("Runner should not have crashed after recovering")
	}
}

func TestRunner_MaxRetries_Stops(t *testing.T) {
	agent := &mockAgent{
		id: "test-agent",
		decisionFn: func(ctx context.Context, state *game.State) (Decision, error) {
			return Decision{}, errors.New("persistent error")
		},
	}

	client := newMockGameClient()
	config := DefaultRunnerConfig()
	config.MaxRetries = 3
	config.DecisionInterval = 50 * time.Millisecond

	runner := NewRunner(agent, client, config)

	ctx := context.Background()
	if err := runner.Start(ctx); err != nil {
		t.Fatalf("Failed to start runner: %v", err)
	}

	// Wait for runner to crash
	time.Sleep(300 * time.Millisecond)

	if !runner.HasCrashed() {
		t.Error("Runner should have crashed after max retries")
	}

	if runner.IsRunning() {
		t.Error("Runner should have stopped after crashing")
	}
}

func TestRunner_ContextCancellation(t *testing.T) {
	agent := &mockAgent{id: "test-agent"}
	client := newMockGameClient()

	config := DefaultRunnerConfig()
	config.DecisionInterval = 50 * time.Millisecond

	runner := NewRunner(agent, client, config)

	ctx, cancel := context.WithCancel(context.Background())

	if err := runner.Start(ctx); err != nil {
		t.Fatalf("Failed to start runner: %v", err)
	}

	// Cancel context
	cancel()

	// Wait for runner to stop
	time.Sleep(200 * time.Millisecond)

	if runner.IsRunning() {
		t.Error("Runner should have stopped after context cancellation")
	}
}

func TestRunner_WaitAction(t *testing.T) {
	agent := &mockAgent{
		id: "test-agent",
		decisionFn: func(ctx context.Context, state *game.State) (Decision, error) {
			return Decision{
				Action:     "wait",
				Reasoning:  "Waiting for next tick",
				Confidence: 1.0,
			}, nil
		},
	}

	client := newMockGameClient()
	config := DefaultRunnerConfig()
	runner := NewRunner(agent, client, config)

	ctx := context.Background()

	// Execute cycle with wait action
	if err := runner.executeCycle(ctx); err != nil {
		t.Fatalf("executeCycle failed: %v", err)
	}

	// No actions should be recorded for "wait"
	if len(client.actionsRecorded) != 0 {
		t.Errorf("Expected 0 actions for 'wait', got %d", len(client.actionsRecorded))
	}
}

func TestRunner_InvalidAction(t *testing.T) {
	agent := &mockAgent{
		id: "test-agent",
		decisionFn: func(ctx context.Context, state *game.State) (Decision, error) {
			return Decision{
				Action:     "invalid_action",
				Confidence: 1.0,
			}, nil
		},
	}

	client := newMockGameClient()
	config := DefaultRunnerConfig()
	runner := NewRunner(agent, client, config)

	ctx := context.Background()

	// Execute cycle with invalid action
	err := runner.executeCycle(ctx)
	if err == nil {
		t.Error("Expected error for invalid action")
	}

	if len(client.actionsRecorded) != 0 {
		t.Errorf("Expected 0 actions for invalid action, got %d", len(client.actionsRecorded))
	}
}

func TestRunner_LearningCallback(t *testing.T) {
	learnCount := 0
	var lastResult ActionResult

	agent := &mockAgent{
		id: "test-agent",
		decisionFn: func(ctx context.Context, state *game.State) (Decision, error) {
			return Decision{Action: "mine", Confidence: 0.8}, nil
		},
		learnFn: func(result ActionResult) error {
			learnCount++
			lastResult = result
			return nil
		},
	}

	client := newMockGameClient()
	client.state.CurrentTick = 100

	config := DefaultRunnerConfig()
	runner := NewRunner(agent, client, config)

	ctx := context.Background()

	// Execute cycle
	if err := runner.executeCycle(ctx); err != nil {
		t.Fatalf("executeCycle failed: %v", err)
	}

	if learnCount != 1 {
		t.Errorf("Expected Learn to be called once, got %d", learnCount)
	}

	if !lastResult.Success {
		t.Error("Expected successful result")
	}

	if lastResult.Error != nil {
		t.Errorf("Expected no error in result, got %v", lastResult.Error)
	}
}

func TestIsActionCommand(t *testing.T) {
	tests := []struct {
		action   string
		expected bool
	}{
		// Action commands (consume tick)
		{"mine", true},
		{"travel", true},
		{"jump", true},
		{"dock", true},
		{"undock", true},
		{"attack", true},
		{"scan", true},
		{"craft", true},

		// Query commands (do not consume tick)
		{"get_status", false},
		{"get_system", false},
		{"get_ship", false},
		{"get_skills", false},
		{"get_map", false},
		{"help", false},
		{"wait", false},
	}

	for _, tt := range tests {
		t.Run(tt.action, func(t *testing.T) {
			result := isActionCommand(tt.action)
			if result != tt.expected {
				t.Errorf("isActionCommand(%q) = %v, want %v", tt.action, result, tt.expected)
			}
		})
	}
}

func TestRunner_DoubleStart(t *testing.T) {
	agent := &mockAgent{id: "test-agent"}
	client := newMockGameClient()

	config := DefaultRunnerConfig()
	runner := NewRunner(agent, client, config)

	ctx := context.Background()

	// Start once
	if err := runner.Start(ctx); err != nil {
		t.Fatalf("First start failed: %v", err)
	}
	defer runner.Stop()

	// Try to start again
	err := runner.Start(ctx)
	if err == nil {
		t.Error("Expected error when starting already running runner")
	}
}

func TestRunner_GettersSetters(t *testing.T) {
	agent := &mockAgent{id: "test-agent"}
	client := newMockGameClient()

	config := DefaultRunnerConfig()
	runner := NewRunner(agent, client, config)

	// Test getters
	if runner.GetAgent() != agent {
		t.Error("GetAgent() returned wrong agent")
	}

	if runner.GetGameClient() != client {
		t.Error("GetGameClient() returned wrong client")
	}

	if runner.GetLastActionTick() != 0 {
		t.Errorf("Expected initial lastActionTick=0, got %d", runner.GetLastActionTick())
	}

	if runner.GetCrashCount() != 0 {
		t.Errorf("Expected initial crashCount=0, got %d", runner.GetCrashCount())
	}

	// Update values
	runner.mu.Lock()
	runner.lastActionTick = 123
	runner.crashCount = 5
	runner.mu.Unlock()

	if runner.GetLastActionTick() != 123 {
		t.Errorf("Expected lastActionTick=123, got %d", runner.GetLastActionTick())
	}

	if runner.GetCrashCount() != 5 {
		t.Errorf("Expected crashCount=5, got %d", runner.GetCrashCount())
	}
}
