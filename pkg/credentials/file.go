package credentials

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// FileProvider loads credentials from agent-specific files
//
// Each agent has its own credentials file in the agent's directory:
// data/agents/<agent-id>/credentials.json
//
// File format:
// {
//   "username": "agent-name",
//   "token": "agent-token",
//   "empire": "voidborn"
// }
type FileProvider struct {
	agentsDir string
	mu        sync.RWMutex
}

// FileCredentials represents the JSON format of credential files
// Supports both 'password' (new) and 'token' (legacy) for migration
type FileCredentials struct {
	Username string `json:"username"`
	Password string `json:"password"`         // New field (v0.38.0+)
	Token    string `json:"token,omitempty"`  // Legacy field for backward compatibility
	Empire   string `json:"empire"`
}

// NewFileProvider creates a new file-based credential provider
//
// agentsDir is the base directory containing agent subdirectories
// (e.g., "data/agents")
func NewFileProvider(agentsDir string) *FileProvider {
	return &FileProvider{
		agentsDir: agentsDir,
	}
}

// GetCredentials retrieves credentials for an agent from its credential file
func (p *FileProvider) GetCredentials(ctx context.Context, agentID string) (*Credentials, error) {
	credPath := p.credentialPath(agentID)

	// Check if file exists
	if _, err := os.Stat(credPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("%w: no credential file found at %s", ErrCredentialsNotFound, credPath)
	}

	// Read file
	data, err := os.ReadFile(credPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read credential file: %w", err)
	}

	// Parse JSON
	var fileCreds FileCredentials
	if err := json.Unmarshal(data, &fileCreds); err != nil {
		return nil, fmt.Errorf("failed to parse credential file: %w", err)
	}

	// Auto-migrate: if token exists but password doesn't, copy token to password
	password := fileCreds.Password
	if password == "" && fileCreds.Token != "" {
		password = fileCreds.Token
		// Auto-save with new format
		_ = p.StoreCredentials(ctx, agentID, &Credentials{
			Username: fileCreds.Username,
			Password: password,
			Empire:   fileCreds.Empire,
		})
	}

	// Validate
	if fileCreds.Username == "" || password == "" {
		return nil, fmt.Errorf("%w: invalid credentials (missing username or password)", ErrCredentialsInvalid)
	}

	// Set default empire if not specified
	if fileCreds.Empire == "" {
		fileCreds.Empire = "voidborn"
	}

	return &Credentials{
		Username: fileCreds.Username,
		Password: password,
		Empire:   fileCreds.Empire,
	}, nil
}

// StoreCredentials saves credentials for an agent to its credential file
func (p *FileProvider) StoreCredentials(ctx context.Context, agentID string, creds *Credentials) error {
	credPath := p.credentialPath(agentID)

	// Ensure agent directory exists
	agentDir := filepath.Dir(credPath)
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		return fmt.Errorf("failed to create agent directory: %w", err)
	}

	// Prepare file data (always write as 'password', not 'token')
	fileCreds := FileCredentials{
		Username: creds.Username,
		Password: creds.Password,
		Empire:   creds.Empire,
	}

	if fileCreds.Empire == "" {
		fileCreds.Empire = "voidborn"
	}

	// Marshal to JSON with indentation for readability
	data, err := json.MarshalIndent(fileCreds, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal credentials: %w", err)
	}

	// Write to file
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := os.WriteFile(credPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write credential file: %w", err)
	}

	return nil
}

// RemoveCredentials deletes an agent's credential file
func (p *FileProvider) RemoveCredentials(ctx context.Context, agentID string) error {
	credPath := p.credentialPath(agentID)

	// Check if file exists
	if _, err := os.Stat(credPath); os.IsNotExist(err) {
		return fmt.Errorf("%w: %s", ErrCredentialsNotFound, agentID)
	}

	// Remove file
	if err := os.Remove(credPath); err != nil {
		return fmt.Errorf("failed to remove credential file: %w", err)
	}

	return nil
}

// ListAgents returns all agent IDs that have credential files
func (p *FileProvider) ListAgents(ctx context.Context) ([]string, error) {
	// Read agent directories
	entries, err := os.ReadDir(p.agentsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil // No agents directory yet
		}
		return nil, fmt.Errorf("failed to read agents directory: %w", err)
	}

	var agents []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Check if credentials file exists
		credPath := p.credentialPath(entry.Name())
		if _, err := os.Stat(credPath); err == nil {
			agents = append(agents, entry.Name())
		}
	}

	return agents, nil
}

// credentialPath returns the full path to an agent's credential file
func (p *FileProvider) credentialPath(agentID string) string {
	return filepath.Join(p.agentsDir, agentID, "credentials.json")
}

// ValidatePermissions checks that credential files have secure permissions (0600)
func (p *FileProvider) ValidatePermissions(agentID string) error {
	credPath := p.credentialPath(agentID)

	info, err := os.Stat(credPath)
	if err != nil {
		return err
	}

	perm := info.Mode().Perm()
	if perm != 0600 {
		return fmt.Errorf("insecure file permissions: %v (expected 0600)", perm)
	}

	return nil
}

// SetPermissions sets secure permissions on credential files
func (p *FileProvider) SetPermissions(agentID string) error {
	credPath := p.credentialPath(agentID)
	return os.Chmod(credPath, 0600)
}
