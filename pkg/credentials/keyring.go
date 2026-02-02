package credentials

import (
	"context"
	"fmt"
)

// KeyringProvider loads credentials from the system keyring
//
// This provider uses the OS's secure credential storage:
// - Linux: gnome-keyring, kwallet
// - macOS: Keychain
// - Windows: Credential Manager
//
// NOTE: Phase 4 implementation (enhanced providers).
// Phase 1 provides a stub that returns ErrProviderUnavailable.
type KeyringProvider struct {
	service string // Keyring service name (e.g., "spacemolt-agent-credentials")
}

// NewKeyringProvider creates a new keyring credential provider
//
// service is the keyring service name to use for storing credentials.
func NewKeyringProvider(service string) *KeyringProvider {
	if service == "" {
		service = "spacemolt-agent-credentials"
	}
	return &KeyringProvider{
		service: service,
	}
}

// GetCredentials retrieves credentials from the system keyring
//
// Phase 4: Implement actual keyring access
// Phase 1: Returns ErrProviderUnavailable
func (p *KeyringProvider) GetCredentials(ctx context.Context, agentID string) (*Credentials, error) {
	// TODO: Phase 4 - Implement actual keyring access
	return nil, fmt.Errorf("%w: keyring provider not yet implemented", ErrProviderUnavailable)
}

// StoreCredentials saves credentials to the system keyring
func (p *KeyringProvider) StoreCredentials(ctx context.Context, agentID string, creds *Credentials) error {
	// TODO: Phase 4 - Implement actual keyring access
	return fmt.Errorf("%w: keyring provider not yet implemented", ErrProviderUnavailable)
}

// RemoveCredentials deletes credentials from the system keyring
func (p *KeyringProvider) RemoveCredentials(ctx context.Context, agentID string) error {
	// TODO: Phase 4 - Implement actual keyring access
	return fmt.Errorf("%w: keyring provider not yet implemented", ErrProviderUnavailable)
}

// ListAgents returns all agent IDs with stored credentials
func (p *KeyringProvider) ListAgents(ctx context.Context) ([]string, error) {
	// TODO: Phase 4 - Implement actual keyring access
	return nil, fmt.Errorf("%w: keyring provider not yet implemented", ErrProviderUnavailable)
}

// IsAvailable checks if the keyring provider is available
//
// Phase 4: Will detect if keyring is accessible
func (p *KeyringProvider) IsAvailable() bool {
	return false
}
