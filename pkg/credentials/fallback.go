package credentials

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// FallbackProvider tries multiple providers in order until one succeeds
//
// Providers are tried in the order they were added. The first provider
// that successfully returns credentials is used. This allows for flexible
// credential sourcing with automatic fallback.
//
// Common pattern:
//  1. Try agent-specific file (most specific)
//  2. Try SQLite database (centralized storage)
//  3. Try environment variables (container-friendly)
//  4. Try legacy single-credential file (backward compatibility)
type FallbackProvider struct {
	providers []Provider
	mu        sync.RWMutex
}

// NewFallbackProvider creates a provider that tries multiple sources
func NewFallbackProvider(providers ...Provider) *FallbackProvider {
	return &FallbackProvider{
		providers: providers,
	}
}

// AddProvider appends a provider to the fallback chain
//
// Providers are tried in the order they were added.
func (p *FallbackProvider) AddProvider(provider Provider) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.providers = append(p.providers, provider)
}

// GetCredentials tries each provider in order until credentials are found
//
// Returns the first successful result from any provider.
// Returns ErrCredentialsNotFound only if all providers fail.
func (p *FallbackProvider) GetCredentials(ctx context.Context, agentID string) (*Credentials, error) {
	p.mu.RLock()
	providers := make([]Provider, len(p.providers))
	copy(providers, p.providers)
	p.mu.RUnlock()

	var lastErr error
	var providerNames []string

	for _, provider := range providers {
		providerName := providerType(provider)
		providerNames = append(providerNames, providerName)

		creds, err := provider.GetCredentials(ctx, agentID)
		if err == nil {
			// Success! Return credentials
			return creds, nil
		}

		// Track this error
		if lastErr == nil {
			lastErr = err
		}

		// If provider is unavailable (not just credentials not found), skip
		if errors.Is(err, ErrProviderUnavailable) {
			continue
		}

		// If credentials not found, try next provider
		if errors.Is(err, ErrCredentialsNotFound) {
			continue
		}

		// For other errors, log but continue
		// (Could add logging here in production)
	}

	// All providers failed
	if lastErr != nil {
		return nil, fmt.Errorf(
			"%w: tried providers %s, last error: %v",
			ErrCredentialsNotFound,
			strings.Join(providerNames, " → "),
			lastErr,
		)
	}

	return nil, fmt.Errorf(
		"%w: no providers configured (tried: %s)",
		ErrCredentialsNotFound,
		strings.Join(providerNames, ", "),
	)
}

// StoreCredentials stores credentials in the first available provider
//
// Tries providers in order until one successfully stores the credentials.
// Returns an error if all providers fail.
func (p *FallbackProvider) StoreCredentials(ctx context.Context, agentID string, creds *Credentials) error {
	p.mu.RLock()
	providers := make([]Provider, len(p.providers))
	copy(providers, p.providers)
	p.mu.RUnlock()

	var lastErr error
	var providerNames []string

	for _, provider := range providers {
		providerName := providerType(provider)
		providerNames = append(providerNames, providerName)

		err := provider.StoreCredentials(ctx, agentID, creds)
		if err == nil {
			// Success!
			return nil
		}

		// Track this error
		lastErr = err

		// If provider unavailable, try next
		if errors.Is(err, ErrProviderUnavailable) {
			continue
		}
	}

	if lastErr != nil {
		return fmt.Errorf(
			"failed to store credentials in any provider (tried: %s): %w",
			strings.Join(providerNames, ", "),
			lastErr,
		)
	}

	return fmt.Errorf("no providers configured")
}

// RemoveCredentials removes credentials from all providers
//
// Attempts to remove from all providers, even if some fail.
// Returns the first error encountered, but continues trying.
func (p *FallbackProvider) RemoveCredentials(ctx context.Context, agentID string) error {
	p.mu.RLock()
	providers := make([]Provider, len(p.providers))
	copy(providers, p.providers)
	p.mu.RUnlock()

	var firstErr error
	var successCount int

	for _, provider := range providers {
		err := provider.RemoveCredentials(ctx, agentID)
		if err == nil {
			successCount++
		} else if firstErr == nil {
			firstErr = err
		}
		// Continue trying other providers even if some fail
	}

	// If at least one provider succeeded, consider it a success
	if successCount > 0 {
		return nil
	}

	if firstErr != nil {
		return fmt.Errorf("failed to remove credentials from all providers: %w", firstErr)
	}

	return ErrCredentialsNotFound
}

// ListAgents aggregates agent lists from all providers
//
// Returns a deduplicated list of all agent IDs across all providers.
func (p *FallbackProvider) ListAgents(ctx context.Context) ([]string, error) {
	p.mu.RLock()
	providers := make([]Provider, len(p.providers))
	copy(providers, p.providers)
	p.mu.RUnlock()

	agentMap := make(map[string]bool)

	for _, provider := range providers {
		agents, err := provider.ListAgents(ctx)
		if err != nil {
			// Continue to next provider on error
			continue
		}

		for _, agent := range agents {
			agentMap[agent] = true
		}
	}

	// Convert map to slice
	result := make([]string, 0, len(agentMap))
	for agent := range agentMap {
		result = append(result, agent)
	}

	return result, nil
}

// providerType returns a string representation of the provider type
func providerType(p Provider) string {
	switch p.(type) {
	case *FileProvider:
		return "file"
	case *SQLiteProvider:
		return "sqlite"
	case *KeyringProvider:
		return "keyring"
	case *EnvProvider:
		return "env"
	case *LegacyProvider:
		return "legacy"
	case *StaticProvider:
		return "static"
	case *FallbackProvider:
		return "fallback"
	default:
		return "unknown"
	}
}
