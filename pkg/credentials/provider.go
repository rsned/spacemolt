package credentials

import (
	"context"
	"errors"
)

// Common errors
var (
	// ErrCredentialsNotFound is returned when no credentials exist for an agent
	ErrCredentialsNotFound = errors.New("credentials not found")

	// ErrCredentialsInvalid is returned when credentials are invalid
	ErrCredentialsInvalid = errors.New("credentials invalid")

	// ErrProviderUnavailable is returned when a provider cannot be used
	ErrProviderUnavailable = errors.New("provider unavailable")
)

// IsErrCredentialsNotFound checks if err is or wraps ErrCredentialsNotFound
func IsErrCredentialsNotFound(err error) bool {
	return errors.Is(err, ErrCredentialsNotFound)
}

// Credentials represents authentication credentials for an agent
type Credentials struct {
	Username string
	Token     string
	Empire    string // Faction/empire for registration (e.g., "voidborn")
}

// Provider provides credentials for agents
//
// The Provider interface allows flexibility in where and how credentials
// are stored, supporting multiple backends (file, SQLite, keyring, etc.)
type Provider interface {
	// GetCredentials retrieves credentials for an agent
	//
	// Returns ErrCredentialsNotFound if no credentials exist for the agent.
	// Returns ErrProviderUnavailable if the provider cannot be used.
	GetCredentials(ctx context.Context, agentID string) (*Credentials, error)

	// StoreCredentials saves credentials for an agent
	//
	// If credentials already exist, they should be overwritten.
	StoreCredentials(ctx context.Context, agentID string, creds *Credentials) error

	// RemoveCredentials deletes credentials for an agent
	//
	// Should return ErrCredentialsNotFound if credentials don't exist.
	RemoveCredentials(ctx context.Context, agentID string) error

	// ListAgents returns all agent IDs that have stored credentials
	//
	// Returns an empty slice if no credentials are stored.
	ListAgents(ctx context.Context) ([]string, error)
}

// StaticProvider returns a fixed set of credentials for all agents
//
// This is useful for testing or single-credential scenarios.
type StaticProvider struct {
	creds *Credentials
}

// NewStaticProvider creates a provider that always returns the same credentials
func NewStaticProvider(username, token, empire string) *StaticProvider {
	return &StaticProvider{
		creds: &Credentials{
			Username: username,
			Token:     token,
			Empire:    empire,
		},
	}
}

// GetCredentials returns the static credentials for any agent ID
func (p *StaticProvider) GetCredentials(ctx context.Context, agentID string) (*Credentials, error) {
	return p.creds, nil
}

// StoreCredentials is a no-op for static provider (credentials cannot be changed)
func (p *StaticProvider) StoreCredentials(ctx context.Context, agentID string, creds *Credentials) error {
	// Static provider doesn't support storing different credentials
	return nil
}

// RemoveCredentials is a no-op for static provider
func (p *StaticProvider) RemoveCredentials(ctx context.Context, agentID string) error {
	return nil
}

// ListAgents returns nil since static provider doesn't track specific agents
func (p *StaticProvider) ListAgents(ctx context.Context) ([]string, error) {
	return nil, nil
}
