package knowledge

import "context"

// Base provides the KB interface for both SQLite and in-memory implementations
type Base interface {
	Close() error
	RememberSystem(ctx context.Context, sys System) error
	GetSystem(ctx context.Context, systemID string) (*System, error)
	GetUnknownConnections(ctx context.Context, systemID string) ([]string, error)
	RememberConnection(ctx context.Context, fromSystem, toSystem string) error
	RememberPOI(ctx context.Context, poi POI) error
	AddExperience(ctx context.Context, agentID, expType, description, outcome, location string) error
	GetRecentExperiences(ctx context.Context, agentID string, limit int) ([]Experience, error)
	RegisterAgent(ctx context.Context, agentID, name, role, faction string, personality []byte) error

	// GetSystems returns all known systems
	// Note: This method does not take a context for API compatibility with existing callers
	GetSystems() []System
}
