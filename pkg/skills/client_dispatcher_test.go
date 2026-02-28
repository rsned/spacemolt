package skills

import (
	"log"
	"os"
	"testing"
	"time"
)

// TestClientDispatcher_RouteField verifies the Route field exists and can be set
func TestClientDispatcher_RouteField(t *testing.T) {
	dispatcher := NewClientDispatcher(nil, log.New(os.Stderr, "[test] ", 0))

	// Route should be nil initially
	if dispatcher.Route != nil {
		t.Error("Expected Route to be nil initially")
	}

	// Can set Route
	dispatcher.Route = &RouteProgress{
		DestinationSystem: "test",
		Route: []RouteStep{
			{SystemID: "haven", Name: "Haven", Jumps: 1},
			{SystemID: "crimson", Name: "Crimson", Jumps: 2},
		},
		CurrentStep: 0,
		Timestamp:   time.Now(),
	}

	if dispatcher.Route == nil {
		t.Error("Expected Route to be non-nil after setting")
	}

	// Verify route structure
	if dispatcher.Route.DestinationSystem != "test" {
		t.Errorf("DestinationSystem = %s, want test", dispatcher.Route.DestinationSystem)
	}
	if len(dispatcher.Route.Route) != 2 {
		t.Errorf("Route length = %d, want 2", len(dispatcher.Route.Route))
	}
	if dispatcher.Route.Route[0].SystemID != "haven" {
		t.Errorf("First step system_id = %s, want haven", dispatcher.Route.Route[0].SystemID)
	}
	if dispatcher.Route.Route[1].SystemID != "crimson" {
		t.Errorf("Second step system_id = %s, want crimson", dispatcher.Route.Route[1].SystemID)
	}
	if dispatcher.Route.CurrentStep != 0 {
		t.Errorf("CurrentStep = %d, want 0", dispatcher.Route.CurrentStep)
	}
}

// TestDispatch_findRouteStruct verifies route structure can be created and stored
func TestDispatch_findRouteStruct(t *testing.T) {
	dispatcher := &ClientDispatcher{
		Client: nil,
		Logger: log.New(os.Stderr, "[test] ", 0),
	}

	// Test that Route field can be populated with proper structure
	dispatcher.Route = &RouteProgress{
		DestinationSystem: "haven",
		Route: []RouteStep{
			{SystemID: "haven", Name: "Haven", Jumps: 1},
			{SystemID: "crimson", Name: "Crimson", Jumps: 2},
		},
		CurrentStep: 0,
		Timestamp:   time.Now(),
	}

	if dispatcher.Route == nil {
		t.Error("Expected Route to be set")
	}
	if dispatcher.Route.DestinationSystem != "haven" {
		t.Errorf("DestinationSystem = %s, want haven", dispatcher.Route.DestinationSystem)
	}
	if len(dispatcher.Route.Route) != 2 {
		t.Errorf("Route length = %d, want 2", len(dispatcher.Route.Route))
	}
}
