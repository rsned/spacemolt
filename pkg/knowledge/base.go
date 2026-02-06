package knowledge

import (
	"context"
	"time"
)

// Base provides the KB interface for both SQLite and in-memory implementations
type Base interface {
	Close() error
	RememberSystem(ctx context.Context, sys System) error
	GetSystem(ctx context.Context, systemID string) (*System, error)
	GetUnknownConnections(ctx context.Context, systemID string) ([]string, error)
	RememberConnection(ctx context.Context, fromSystem, toSystem string) error
	RememberPOI(ctx context.Context, poi POI) error
	GetPOIs(ctx context.Context, systemID string) ([]POI, error)
	AddExperience(ctx context.Context, agentID, expType, description, outcome, location string) error
	GetRecentExperiences(ctx context.Context, agentID string, limit int) ([]Experience, error)
	RegisterAgent(ctx context.Context, agentID, name, role, faction string, personality []byte) error

	// GetSystems returns all known systems
	// Note: This method does not take a context for API compatibility with existing callers
	GetSystems() []System

	// Market data methods
	StoreMarketSnapshot(ctx context.Context, snapshot MarketSnapshot, agentID string) error
	GetMarketSnapshots(ctx context.Context, systemID, stationID string, limit int) ([]MarketSnapshot, error)
	GetLatestMarketSnapshot(ctx context.Context, systemID, stationID string) (*MarketSnapshot, error)
	GetMarketItems(ctx context.Context, itemType string) ([]string, error)
}

// MarketListing represents a single market listing
type MarketListing struct {
	ItemID      string
	ItemType    string
	Quantity    float64
	PricePerUnit float64
	TotalPrice  float64
	Type        string // 'buy' or 'sell'
	ListedBy    string
}

// MarketSnapshot represents a captured market state
type MarketSnapshot struct {
	SystemID    string
	SystemName  string
	StationID   string
	StationName string
	GameTick    int64
	Listings    []MarketListing
	CapturedAt  time.Time
}
