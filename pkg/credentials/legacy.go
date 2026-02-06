package credentials

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
)

// LegacyProvider provides a single set of credentials for all agents
//
// This provider maintains backward compatibility with the original
// .spacemolt-credentials.json file that stored credentials for a
// single player account.
//
// All agents receive the same username and token, but each will
// append its agent ID to the username when connecting, creating
// unique identities: username + "_" + agentID
type LegacyProvider struct {
	credsFile string
	mu        sync.RWMutex
	creds     *Credentials
}

// LegacyCredentials represents the JSON format of the legacy credentials file
// Supports both 'password' (new) and 'token' (legacy) for migration
type LegacyCredentials struct {
	Username string `json:"username"`
	Password string `json:"password"`         // New field (v0.38.0+)
	Token    string `json:"token,omitempty"`  // Legacy field for backward compatibility
}

// NewLegacyProvider creates a provider that loads credentials from a file
//
// credsFile is typically ".spacemolt-credentials.json"
func NewLegacyProvider(credsFile string) *LegacyProvider {
	return &LegacyProvider{
		credsFile: credsFile,
	}
}

// loadCredentials reads and parses the legacy credentials file
func (p *LegacyProvider) loadCredentials() (*Credentials, error) {
	p.mu.RLock()
	if p.creds != nil {
		// Check if file still exists before returning cached credentials
		// This handles the case where the file is deleted after caching
		if _, err := os.Stat(p.credsFile); os.IsNotExist(err) {
			p.mu.RUnlock()
			p.mu.Lock()
			p.creds = nil
			p.mu.Unlock()
			return nil, fmt.Errorf("%w: %s", ErrCredentialsNotFound, p.credsFile)
		}
		p.mu.RUnlock()
		return p.creds, nil
	}
	p.mu.RUnlock()

	// Read file
	data, err := os.ReadFile(p.credsFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrCredentialsNotFound, p.credsFile)
		}
		return nil, fmt.Errorf("failed to read credentials file: %w", err)
	}

	// Parse JSON
	var legacyCreds LegacyCredentials
	if err := json.Unmarshal(data, &legacyCreds); err != nil {
		return nil, fmt.Errorf("failed to parse credentials file: %w", err)
	}

	// Auto-migrate: if token exists but password doesn't, copy token to password
	password := legacyCreds.Password
	if password == "" && legacyCreds.Token != "" {
		password = legacyCreds.Token
	}

	// Validate
	if legacyCreds.Username == "" || password == "" {
		return nil, fmt.Errorf("%w: invalid credentials (missing username or password)", ErrCredentialsInvalid)
	}

	p.mu.Lock()
	p.creds = &Credentials{
		Username: legacyCreds.Username,
		Password: password,
		Empire:   "voidborn", // Default empire for legacy credentials
	}
	p.mu.Unlock()

	return p.creds, nil
}

// GetCredentials retrieves the legacy credentials for an agent
//
// The username is modified to be unique per agent by appending
// the agent ID: username_agentID
func (p *LegacyProvider) GetCredentials(ctx context.Context, agentID string) (*Credentials, error) {
	creds, err := p.loadCredentials()
	if err != nil {
		return nil, err
	}

	// Return agent-specific credentials by appending agent ID to username
	return &Credentials{
		Username: fmt.Sprintf("%s_%s", creds.Username, agentID),
		Password: creds.Password,
		Empire:   creds.Empire,
	}, nil
}

// StoreCredentials saves credentials to the legacy file
//
// Note: This will overwrite the existing credentials file,
// affecting all agents that use the legacy provider.
func (p *LegacyProvider) StoreCredentials(ctx context.Context, agentID string, creds *Credentials) error {
	// For legacy provider, we ignore agentID and store a single credential set
	legacyCreds := LegacyCredentials{
		Username: creds.Username,
		Password: creds.Password,
	}

	data, err := json.MarshalIndent(legacyCreds, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal credentials: %w", err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if err := os.WriteFile(p.credsFile, data, 0600); err != nil {
		return fmt.Errorf("failed to write credentials file: %w", err)
	}

	// Update cached credentials
	p.creds = creds

	return nil
}

// RemoveCredentials deletes the legacy credentials file
//
// This will affect all agents using the legacy provider.
func (p *LegacyProvider) RemoveCredentials(ctx context.Context, agentID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Clear cache
	p.creds = nil

	// Remove file
	if err := os.Remove(p.credsFile); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%w: %s", ErrCredentialsNotFound, p.credsFile)
		}
		return fmt.Errorf("failed to remove credentials file: %w", err)
	}

	return nil
}

// ListAgents returns a single entry if credentials exist, or empty if not
//
// Since legacy provider uses a single credential set for all agents,
// this returns either ["legacy"] or [] depending on whether credentials exist.
func (p *LegacyProvider) ListAgents(ctx context.Context) ([]string, error) {
	_, err := p.loadCredentials()
	if err != nil {
		if errors.Is(err, ErrCredentialsNotFound) {
			return []string{}, nil
		}
		return nil, err
	}

	return []string{"legacy"}, nil
}

// Exists checks if the legacy credentials file exists
func (p *LegacyProvider) Exists() bool {
	_, err := os.Stat(p.credsFile)
	return err == nil
}
