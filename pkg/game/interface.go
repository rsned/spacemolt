package game

import "context"

// GameClient defines the interface for game client operations
// This allows for mocking in tests
type GameClient interface {
	// Connection
	Connect(ctx context.Context) error
	Close() error
	IsConnected() bool
	Ready() <-chan struct{}

	// Authentication
	Login(ctx context.Context) error
	Register(ctx context.Context, empire string) error

	// Actions
	Undock(ctx context.Context) error
	Dock(ctx context.Context) error
	Travel(ctx context.Context, targetPOI string) error
	Jump(ctx context.Context, targetSystem string) error
	Mine(ctx context.Context) error
	Scan(ctx context.Context) error

	// Route Planning
	FindRoute(ctx context.Context, targetSystem string) ([]RouteStep, error)

	// Queries
	GetSystem(ctx context.Context) error
	GetStatus(ctx context.Context) error

	// State
	GetState() *State
}

// Ensure Client implements GameClient interface
var _ GameClient = (*Client)(nil)
