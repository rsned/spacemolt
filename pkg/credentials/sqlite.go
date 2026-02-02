package credentials

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	_ "modernc.org/sqlite"
)

// SQLiteProvider loads credentials from a SQLite database
//
// This provider stores credentials in the agent_credentials table,
// with encrypted tokens for security using AES-256-GCM.
type SQLiteProvider struct {
	db        *sql.DB
	encryptor *Encryptor
	mu        sync.RWMutex
	ready     bool
}

// NewSQLiteProvider creates a new SQLite credential provider
//
// The database must have the agent_credentials table created.
// See knowledge/sqlite_migrations.go for schema.
// The encryptor is used to encrypt/decrypt tokens in the database.
func NewSQLiteProvider(dbPath string, encryptor *Encryptor) (*SQLiteProvider, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	provider := &SQLiteProvider{
		db:        db,
		encryptor: encryptor,
	}

	// Verify table exists
	if err := provider.init(); err != nil {
		_ = db.Close()
		return nil, err
	}

	return provider, nil
}

// NewSQLiteProviderWithDB creates a provider using an existing DB connection
//
// Useful when sharing a database connection with the knowledge base.
// The encryptor is used to encrypt/decrypt tokens in the database.
func NewSQLiteProviderWithDB(db *sql.DB, encryptor *Encryptor) *SQLiteProvider {
	return &SQLiteProvider{
		db:        db,
		encryptor: encryptor,
	}
}

// init verifies the credentials table exists
func (p *SQLiteProvider) init() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Check if table exists
	var tableName string
	err := p.db.QueryRow(`
		SELECT name FROM sqlite_master
		WHERE type='table' AND name='agent_credentials'
	`).Scan(&tableName)

	if err == sql.ErrNoRows {
		// Table doesn't exist yet, create it
		return p.createTable()
	}

	if err != nil {
		return fmt.Errorf("failed to check for table: %w", err)
	}

	p.ready = true
	return nil
}

// createTable creates the agent_credentials table
func (p *SQLiteProvider) createTable() error {
	_, err := p.db.Exec(`
		CREATE TABLE IF NOT EXISTS agent_credentials (
			agent_id TEXT PRIMARY KEY,
			username TEXT NOT NULL,
			token TEXT NOT NULL,
			empires TEXT DEFAULT 'voidborn',
			created_at TEXT NOT NULL,
			last_used TEXT
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}

	p.ready = true
	return nil
}

// GetCredentials retrieves credentials from the database
func (p *SQLiteProvider) GetCredentials(ctx context.Context, agentID string) (*Credentials, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if !p.ready {
		return nil, fmt.Errorf("%w: database not initialized", ErrProviderUnavailable)
	}

	if p.encryptor == nil {
		return nil, fmt.Errorf("%w: encryptor not configured", ErrProviderUnavailable)
	}

	var username, encryptedToken, empire string
	err := p.db.QueryRowContext(ctx, `
		SELECT username, token, empires
		FROM agent_credentials
		WHERE agent_id = ?
	`, agentID).Scan(&username, &encryptedToken, &empire)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("%w: agent %s", ErrCredentialsNotFound, agentID)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to query credentials: %w", err)
	}

	// Decrypt the token
	token, err := p.encryptor.Decrypt(encryptedToken)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt token: %w", err)
	}

	return &Credentials{
		Username: username,
		Token:     token,
		Empire:    empire,
	}, nil
}

// StoreCredentials saves credentials to the database
func (p *SQLiteProvider) StoreCredentials(ctx context.Context, agentID string, creds *Credentials) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.ready {
		return fmt.Errorf("%w: database not initialized", ErrProviderUnavailable)
	}

	if p.encryptor == nil {
		return fmt.Errorf("%w: encryptor not configured", ErrProviderUnavailable)
	}

	// Set default empire
	empires := creds.Empire
	if empires == "" {
		empires = "voidborn"
	}

	// Encrypt the token before storing
	encryptedToken, err := p.encryptor.Encrypt(creds.Token)
	if err != nil {
		return fmt.Errorf("failed to encrypt token: %w", err)
	}

	// Insert or replace credentials
	_, err = p.db.ExecContext(ctx, `
		INSERT INTO agent_credentials (agent_id, username, token, empires, created_at, last_used)
		VALUES (?, ?, ?, ?, datetime('now'), datetime('now'))
		ON CONFLICT(agent_id) DO UPDATE SET
			username = excluded.username,
			token = excluded.token,
			empires = excluded.empires,
			last_used = datetime('now')
	`, agentID, creds.Username, encryptedToken, empires)

	if err != nil {
		return fmt.Errorf("failed to store credentials: %w", err)
	}

	return nil
}

// RemoveCredentials deletes credentials from the database
func (p *SQLiteProvider) RemoveCredentials(ctx context.Context, agentID string) error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if !p.ready {
		return fmt.Errorf("%w: database not initialized", ErrProviderUnavailable)
	}

	result, err := p.db.ExecContext(ctx, `
		DELETE FROM agent_credentials WHERE agent_id = ?
	`, agentID)

	if err != nil {
		return fmt.Errorf("failed to remove credentials: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get affected rows: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("%w: agent %s", ErrCredentialsNotFound, agentID)
	}

	return nil
}

// ListAgents returns all agent IDs with stored credentials
func (p *SQLiteProvider) ListAgents(ctx context.Context) ([]string, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if !p.ready {
		return nil, fmt.Errorf("%w: database not initialized", ErrProviderUnavailable)
	}

	rows, err := p.db.QueryContext(ctx, `
		SELECT agent_id FROM agent_credentials ORDER BY agent_id
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query agents: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var agents []string
	for rows.Next() {
		var agentID string
		if err := rows.Scan(&agentID); err != nil {
			return nil, fmt.Errorf("failed to scan agent: %w", err)
		}
		agents = append(agents, agentID)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating agents: %w", err)
	}

	return agents, nil
}

// Close closes the database connection
//
// Only call this if the provider was created with NewSQLiteProvider.
// Do not call if created with NewSQLiteProviderWithDB (connection is shared).
func (p *SQLiteProvider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.db != nil {
		return p.db.Close()
	}

	return nil
}
