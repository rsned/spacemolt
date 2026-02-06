package credentials

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
)

// EnvProvider loads credentials from environment variables
//
// Environment variable naming convention:
//   SPACEMOLT_AGENT_<AGENT_ID>_USERNAME
//   SPACEMOLT_AGENT_<AGENT_ID>_TOKEN
//   SPACEMOLT_AGENT_<AGENT_ID>_EMPIRE
//
// Example for agent "explorer-7":
//   SPACEMOLT_AGENT_EXPLORER_7_USERNAME="explorer-7"
//   SPACEMOLT_AGENT_EXPLORER_7_TOKEN="abc123"
//   SPACEMOLT_AGENT_EXPLORER_7_EMPIRE="voidborn"
//
// Note: Agent ID is uppercased and hyphens are replaced with underscores
// to create valid environment variable names.
type EnvProvider struct {
	prefix string
	mu     sync.RWMutex
}

// NewEnvProvider creates a provider that loads credentials from environment variables
func NewEnvProvider(prefix string) *EnvProvider {
	if prefix == "" {
		prefix = "SPACEMOLT_AGENT_"
	}
	return &EnvProvider{
		prefix: prefix,
	}
}

// GetCredentials retrieves credentials from environment variables for an agent
func (p *EnvProvider) GetCredentials(ctx context.Context, agentID string) (*Credentials, error) {
	usernameKey := p.envKey(agentID, "USERNAME")
	passwordKey := p.envKey(agentID, "PASSWORD")
	tokenKey := p.envKey(agentID, "TOKEN") // Legacy support
	empireKey := p.envKey(agentID, "EMPIRE")

	username := os.Getenv(usernameKey)
	password := os.Getenv(passwordKey)

	// Backward compatibility: check TOKEN if PASSWORD not set
	if password == "" {
		password = os.Getenv(tokenKey)
	}

	empire := os.Getenv(empireKey)

	// Check if required fields are present
	if username == "" || password == "" {
		return nil, fmt.Errorf(
			"%w: environment variables %s and %s (or %s) not set",
			ErrCredentialsNotFound,
			usernameKey,
			passwordKey,
			tokenKey,
		)
	}

	// Default empire if not specified
	if empire == "" {
		empire = "voidborn"
	}

	return &Credentials{
		Username: username,
		Password: password,
		Empire:   empire,
	}, nil
}

// StoreCredentials is a no-op for environment provider
//
// Environment variables cannot be set from within a running process
// for security reasons. Returns an error explaining this limitation.
func (p *EnvProvider) StoreCredentials(ctx context.Context, agentID string, creds *Credentials) error {
	return fmt.Errorf(
		"environment provider does not support storing credentials: "+
			"set %s and %s environment variables instead",
		p.envKey(agentID, "USERNAME"),
		p.envKey(agentID, "PASSWORD"),
	)
}

// RemoveCredentials is a no-op for environment provider
func (p *EnvProvider) RemoveCredentials(ctx context.Context, agentID string) error {
	return fmt.Errorf("environment provider does not support removing credentials")
}

// ListAgents returns all agent IDs that have credentials in environment variables
func (p *EnvProvider) ListAgents(ctx context.Context) ([]string, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// Get all environment variables
	envVars := os.Environ()

	agentMap := make(map[string]bool)

	for _, envVar := range envVars {
		// Check if this is a SPACEMOLT_AGENT_ variable
		if !strings.HasPrefix(envVar, p.prefix) {
			continue
		}

		// Extract the part after the prefix
		// Format: PREFIX_AGENTID_FIELD=value
		rest := strings.TrimPrefix(envVar, p.prefix)
		if rest == envVar || len(rest) == 0 {
			// Prefix wasn't found
			continue
		}

		// Split on = to get the key part
		keyPart, _ := splitFirst(rest, "=")
		if keyPart == "" {
			continue
		}

		// Only count if this is a USERNAME field (to avoid duplicates)
		if !strings.HasSuffix(keyPart, "_USERNAME") {
			continue
		}

		// Remove the _USERNAME suffix to get the agent ID in env format
		envAgentID := strings.TrimSuffix(keyPart, "_USERNAME")

		// Convert back from ENV format: EXPLORER_7 -> explorer-7
		agentID := normalizeEnvAgentID(envAgentID)
		if agentID != "" {
			agentMap[agentID] = true
		}
	}

	// Convert map to slice
	agents := make([]string, 0, len(agentMap))
	for agent := range agentMap {
		agents = append(agents, agent)
	}

	return agents, nil
}

// envKey constructs the environment variable name for an agent field
func (p *EnvProvider) envKey(agentID, field string) string {
	// Convert agent ID to env format: explorer-7 -> EXPLORER_7
	normalized := strings.ToUpper(agentID)
	normalized = strings.ReplaceAll(normalized, "-", "_")

	return p.prefix + normalized + "_" + field
}

// splitFirst splits a string on the first occurrence of sep
func splitFirst(s, sep string) (string, string) {
	idx := strings.Index(s, sep)
	if idx == -1 {
		return s, ""
	}
	return s[:idx], s[idx+len(sep):]
}

// normalizeEnvAgentID converts an env format agent ID back to normal format
// EXPLORER_7 -> explorer-7
func normalizeEnvAgentID(envID string) string {
	if envID == "" {
		return ""
	}

	// Split by underscores
	parts := strings.Split(envID, "_")

	// If we have numbers at the end, they might have been hyphens
	// EXPLORER_7 -> EXPLORER-7 -> explorer-7
	for i := 1; i < len(parts); i++ {
		if isAllDigits(parts[i]) {
			parts[i] = "-" + parts[i]
		} else {
			parts[i] = "_" + parts[i]
		}
	}

	result := strings.ToLower(strings.Join(parts, ""))
	return result
}

// isAllDigits checks if a string consists only of digits
func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
