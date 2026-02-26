package credentials

import (
	"context"
	"database/sql"
	"path/filepath"
	"sort"
	"sync"
	"testing"

	_ "modernc.org/sqlite"
)

// newTestEncryptor creates an encryptor with a fixed test key.
func newTestEncryptor(t *testing.T) *Encryptor {
	t.Helper()
	key := make([]byte, 32)
	for i := range 32 {
		key[i] = byte(i + 1)
	}
	enc, err := NewEncryptor(key)
	if err != nil {
		t.Fatalf("failed to create test encryptor: %v", err)
	}
	return enc
}

func TestSQLiteProvider_NewSQLiteProvider(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	enc := newTestEncryptor(t)

	provider, err := NewSQLiteProvider(dbPath, enc)
	if err != nil {
		t.Fatalf("NewSQLiteProvider failed: %v", err)
	}
	defer func() { _ = provider.Close() }()

	if !provider.ready {
		t.Fatal("provider should be ready after init")
	}
}

func TestSQLiteProvider_NewSQLiteProvider_InvalidPath(t *testing.T) {
	enc := newTestEncryptor(t)

	// Using a path that should fail to open
	_, err := NewSQLiteProvider("/nonexistent/deeply/nested/path/test.db", enc)
	if err == nil {
		t.Fatal("expected error for invalid database path")
	}
}

func TestSQLiteProvider_StoreAndGet(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	enc := newTestEncryptor(t)

	provider, err := NewSQLiteProvider(dbPath, enc)
	if err != nil {
		t.Fatalf("NewSQLiteProvider failed: %v", err)
	}
	defer func() { _ = provider.Close() }()

	ctx := context.Background()

	creds := &Credentials{
		Username: "explorer-7",
		Password: "super-secret-password",
		Empire:   "starborn",
	}

	err = provider.StoreCredentials(ctx, "explorer-7", creds)
	if err != nil {
		t.Fatalf("StoreCredentials failed: %v", err)
	}

	got, err := provider.GetCredentials(ctx, "explorer-7")
	if err != nil {
		t.Fatalf("GetCredentials failed: %v", err)
	}

	if got.Username != "explorer-7" {
		t.Errorf("username: want 'explorer-7', got %q", got.Username)
	}
	if got.Password != "super-secret-password" {
		t.Errorf("password: want 'super-secret-password', got %q", got.Password)
	}
	if got.Empire != "starborn" {
		t.Errorf("empire: want 'starborn', got %q", got.Empire)
	}
}

func TestSQLiteProvider_GetNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	enc := newTestEncryptor(t)

	provider, err := NewSQLiteProvider(dbPath, enc)
	if err != nil {
		t.Fatalf("NewSQLiteProvider failed: %v", err)
	}
	defer func() { _ = provider.Close() }()

	ctx := context.Background()

	_, err = provider.GetCredentials(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent agent")
	}
	if !IsErrCredentialsNotFound(err) {
		t.Errorf("expected ErrCredentialsNotFound, got: %v", err)
	}
}

func TestSQLiteProvider_DefaultEmpire(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	enc := newTestEncryptor(t)

	provider, err := NewSQLiteProvider(dbPath, enc)
	if err != nil {
		t.Fatalf("NewSQLiteProvider failed: %v", err)
	}
	defer func() { _ = provider.Close() }()

	ctx := context.Background()

	err = provider.StoreCredentials(ctx, "agent-1", &Credentials{
		Username: "user",
		Password: "pass",
		Empire:   "",
	})
	if err != nil {
		t.Fatalf("StoreCredentials failed: %v", err)
	}

	got, err := provider.GetCredentials(ctx, "agent-1")
	if err != nil {
		t.Fatalf("GetCredentials failed: %v", err)
	}
	if got.Empire != "voidborn" {
		t.Errorf("expected default empire 'voidborn', got %q", got.Empire)
	}
}

func TestSQLiteProvider_OverwriteCredentials(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	enc := newTestEncryptor(t)

	provider, err := NewSQLiteProvider(dbPath, enc)
	if err != nil {
		t.Fatalf("NewSQLiteProvider failed: %v", err)
	}
	defer func() { _ = provider.Close() }()

	ctx := context.Background()

	_ = provider.StoreCredentials(ctx, "agent-1", &Credentials{
		Username: "old", Password: "oldpass", Empire: "e",
	})

	_ = provider.StoreCredentials(ctx, "agent-1", &Credentials{
		Username: "new", Password: "newpass", Empire: "f",
	})

	got, err := provider.GetCredentials(ctx, "agent-1")
	if err != nil {
		t.Fatalf("GetCredentials failed: %v", err)
	}
	if got.Username != "new" {
		t.Errorf("expected overwritten username 'new', got %q", got.Username)
	}
	if got.Password != "newpass" {
		t.Errorf("expected overwritten password 'newpass', got %q", got.Password)
	}
}

func TestSQLiteProvider_RemoveCredentials(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	enc := newTestEncryptor(t)

	provider, err := NewSQLiteProvider(dbPath, enc)
	if err != nil {
		t.Fatalf("NewSQLiteProvider failed: %v", err)
	}
	defer func() { _ = provider.Close() }()

	ctx := context.Background()

	_ = provider.StoreCredentials(ctx, "agent-1", &Credentials{
		Username: "u", Password: "p", Empire: "e",
	})

	err = provider.RemoveCredentials(ctx, "agent-1")
	if err != nil {
		t.Fatalf("RemoveCredentials failed: %v", err)
	}

	_, err = provider.GetCredentials(ctx, "agent-1")
	if !IsErrCredentialsNotFound(err) {
		t.Errorf("expected not found after remove, got: %v", err)
	}
}

func TestSQLiteProvider_RemoveNonexistent(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	enc := newTestEncryptor(t)

	provider, err := NewSQLiteProvider(dbPath, enc)
	if err != nil {
		t.Fatalf("NewSQLiteProvider failed: %v", err)
	}
	defer func() { _ = provider.Close() }()

	ctx := context.Background()

	err = provider.RemoveCredentials(ctx, "ghost")
	if err == nil {
		t.Fatal("expected error for nonexistent removal")
	}
	if !IsErrCredentialsNotFound(err) {
		t.Errorf("expected ErrCredentialsNotFound, got: %v", err)
	}
}

func TestSQLiteProvider_ListAgents(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	enc := newTestEncryptor(t)

	provider, err := NewSQLiteProvider(dbPath, enc)
	if err != nil {
		t.Fatalf("NewSQLiteProvider failed: %v", err)
	}
	defer func() { _ = provider.Close() }()

	ctx := context.Background()

	// Empty
	agents, err := provider.ListAgents(ctx)
	if err != nil {
		t.Fatalf("ListAgents failed: %v", err)
	}
	if len(agents) != 0 {
		t.Errorf("expected 0 agents, got %d", len(agents))
	}

	// Add agents
	for _, id := range []string{"charlie", "alpha", "bravo"} {
		_ = provider.StoreCredentials(ctx, id, &Credentials{
			Username: id, Password: "pass", Empire: "e",
		})
	}

	agents, err = provider.ListAgents(ctx)
	if err != nil {
		t.Fatalf("ListAgents failed: %v", err)
	}
	// SQLite orders by agent_id
	sort.Strings(agents)
	if len(agents) != 3 {
		t.Fatalf("expected 3 agents, got %d", len(agents))
	}
	if agents[0] != "alpha" || agents[1] != "bravo" || agents[2] != "charlie" {
		t.Errorf("unexpected agents: %v", agents)
	}
}

func TestSQLiteProvider_NotReady(t *testing.T) {
	// Create provider with NewSQLiteProviderWithDB but don't init
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer func() { _ = db.Close() }()

	enc := newTestEncryptor(t)
	provider := NewSQLiteProviderWithDB(db, enc)
	// provider.ready is false because init() wasn't called

	ctx := context.Background()

	_, err = provider.GetCredentials(ctx, "agent-1")
	if err == nil {
		t.Fatal("expected error when not ready")
	}

	err = provider.StoreCredentials(ctx, "agent-1", &Credentials{
		Username: "u", Password: "p",
	})
	if err == nil {
		t.Fatal("expected error when not ready")
	}

	err = provider.RemoveCredentials(ctx, "agent-1")
	if err == nil {
		t.Fatal("expected error when not ready")
	}

	_, err = provider.ListAgents(ctx)
	if err == nil {
		t.Fatal("expected error when not ready")
	}
}

func TestSQLiteProvider_NilEncryptor(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Create provider with nil encryptor
	provider, err := NewSQLiteProvider(dbPath, nil)
	if err != nil {
		t.Fatalf("NewSQLiteProvider failed: %v", err)
	}
	defer func() { _ = provider.Close() }()

	ctx := context.Background()

	_, err = provider.GetCredentials(ctx, "agent-1")
	if err == nil {
		t.Fatal("expected error with nil encryptor")
	}

	err = provider.StoreCredentials(ctx, "agent-1", &Credentials{
		Username: "u", Password: "p",
	})
	if err == nil {
		t.Fatal("expected error with nil encryptor")
	}
}

func TestSQLiteProvider_EncryptionVerification(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	enc := newTestEncryptor(t)

	provider, err := NewSQLiteProvider(dbPath, enc)
	if err != nil {
		t.Fatalf("NewSQLiteProvider failed: %v", err)
	}

	ctx := context.Background()
	plainPassword := "my-secret-password-12345"

	_ = provider.StoreCredentials(ctx, "agent-1", &Credentials{
		Username: "user", Password: plainPassword, Empire: "e",
	})

	// Read the raw token from DB to verify it's encrypted
	var rawToken string
	err = provider.db.QueryRow(
		"SELECT token FROM agent_credentials WHERE agent_id = ?", "agent-1",
	).Scan(&rawToken)
	if err != nil {
		t.Fatalf("failed to query raw token: %v", err)
	}

	if rawToken == plainPassword {
		t.Fatal("password should be encrypted in database, but it's stored in plain text")
	}

	// Verify we can still decrypt via the provider
	got, err := provider.GetCredentials(ctx, "agent-1")
	if err != nil {
		t.Fatalf("GetCredentials failed: %v", err)
	}
	if got.Password != plainPassword {
		t.Errorf("decrypted password mismatch: want %q, got %q", plainPassword, got.Password)
	}

	_ = provider.Close()
}

func TestSQLiteProvider_WrongKeyCannotDecrypt(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	enc := newTestEncryptor(t)

	provider, err := NewSQLiteProvider(dbPath, enc)
	if err != nil {
		t.Fatalf("NewSQLiteProvider failed: %v", err)
	}

	ctx := context.Background()
	_ = provider.StoreCredentials(ctx, "agent-1", &Credentials{
		Username: "user", Password: "secret", Empire: "e",
	})
	_ = provider.Close()

	// Open with a different key
	wrongKey := make([]byte, 32)
	for i := range 32 {
		wrongKey[i] = byte(i + 100)
	}
	wrongEnc, _ := NewEncryptor(wrongKey)
	provider2, err := NewSQLiteProvider(dbPath, wrongEnc)
	if err != nil {
		t.Fatalf("NewSQLiteProvider failed: %v", err)
	}
	defer func() { _ = provider2.Close() }()

	_, err = provider2.GetCredentials(ctx, "agent-1")
	if err == nil {
		t.Fatal("expected decryption failure with wrong key")
	}
}

func TestSQLiteProvider_Close(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	enc := newTestEncryptor(t)

	provider, err := NewSQLiteProvider(dbPath, enc)
	if err != nil {
		t.Fatalf("NewSQLiteProvider failed: %v", err)
	}

	err = provider.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Close again should be safe (db is nil after first close? No, it stays set)
	// The underlying sql.DB may return an error on double close
}

func TestSQLiteProvider_CloseNilDB(t *testing.T) {
	provider := &SQLiteProvider{}
	err := provider.Close()
	if err != nil {
		t.Fatalf("Close on nil db should not error: %v", err)
	}
}

func TestSQLiteProvider_ConcurrentAccess(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	enc := newTestEncryptor(t)

	provider, err := NewSQLiteProvider(dbPath, enc)
	if err != nil {
		t.Fatalf("NewSQLiteProvider failed: %v", err)
	}
	defer func() { _ = provider.Close() }()

	ctx := context.Background()

	// Pre-populate
	_ = provider.StoreCredentials(ctx, "shared-agent", &Credentials{
		Username: "user", Password: "pass", Empire: "e",
	})

	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			if idx%2 == 0 {
				_ = provider.StoreCredentials(ctx, "shared-agent", &Credentials{
					Username: "user", Password: "pass", Empire: "e",
				})
			} else {
				_, _ = provider.GetCredentials(ctx, "shared-agent")
			}
		}(i)
	}
	wg.Wait()
}

func TestSQLiteProvider_ExistingTable(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	enc := newTestEncryptor(t)

	// Create provider - creates table
	p1, err := NewSQLiteProvider(dbPath, enc)
	if err != nil {
		t.Fatalf("first NewSQLiteProvider failed: %v", err)
	}

	ctx := context.Background()
	_ = p1.StoreCredentials(ctx, "agent-1", &Credentials{
		Username: "u", Password: "p", Empire: "e",
	})
	_ = p1.Close()

	// Open again - table already exists
	p2, err := NewSQLiteProvider(dbPath, enc)
	if err != nil {
		t.Fatalf("second NewSQLiteProvider failed: %v", err)
	}
	defer func() { _ = p2.Close() }()

	got, err := p2.GetCredentials(ctx, "agent-1")
	if err != nil {
		t.Fatalf("GetCredentials after reopen failed: %v", err)
	}
	if got.Username != "u" {
		t.Errorf("expected 'u', got %q", got.Username)
	}
}

func TestSQLiteProviderWithDB(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer func() { _ = db.Close() }()

	enc := newTestEncryptor(t)
	provider := NewSQLiteProviderWithDB(db, enc)

	if provider.ready {
		t.Fatal("provider should not be ready without init")
	}
	if provider.db != db {
		t.Fatal("provider should use the provided db")
	}
}
