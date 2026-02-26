package credentials

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
)

func TestFileProvider_StoreAndGet(t *testing.T) {
	tmpDir := t.TempDir()
	provider := NewFileProvider(tmpDir)
	ctx := context.Background()

	creds := &Credentials{
		Username: "explorer",
		Password: "secret-pass",
		Empire:   "voidborn",
	}

	err := provider.StoreCredentials(ctx, "explorer-7", creds)
	if err != nil {
		t.Fatalf("StoreCredentials failed: %v", err)
	}

	got, err := provider.GetCredentials(ctx, "explorer-7")
	if err != nil {
		t.Fatalf("GetCredentials failed: %v", err)
	}

	if got.Username != "explorer" {
		t.Errorf("username: want 'explorer', got %q", got.Username)
	}
	if got.Password != "secret-pass" {
		t.Errorf("password: want 'secret-pass', got %q", got.Password)
	}
	if got.Empire != "voidborn" {
		t.Errorf("empire: want 'voidborn', got %q", got.Empire)
	}
}

func TestFileProvider_GetNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	provider := NewFileProvider(tmpDir)
	ctx := context.Background()

	_, err := provider.GetCredentials(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent agent")
	}
	if !IsErrCredentialsNotFound(err) {
		t.Errorf("expected ErrCredentialsNotFound, got: %v", err)
	}
}

func TestFileProvider_DefaultEmpire(t *testing.T) {
	tmpDir := t.TempDir()
	provider := NewFileProvider(tmpDir)
	ctx := context.Background()

	err := provider.StoreCredentials(ctx, "agent-1", &Credentials{
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

func TestFileProvider_OverwriteCredentials(t *testing.T) {
	tmpDir := t.TempDir()
	provider := NewFileProvider(tmpDir)
	ctx := context.Background()

	_ = provider.StoreCredentials(ctx, "agent-1", &Credentials{
		Username: "old-user",
		Password: "old-pass",
		Empire:   "voidborn",
	})

	_ = provider.StoreCredentials(ctx, "agent-1", &Credentials{
		Username: "new-user",
		Password: "new-pass",
		Empire:   "starborn",
	})

	got, err := provider.GetCredentials(ctx, "agent-1")
	if err != nil {
		t.Fatalf("GetCredentials failed: %v", err)
	}
	if got.Username != "new-user" {
		t.Errorf("expected overwritten username 'new-user', got %q", got.Username)
	}
	if got.Password != "new-pass" {
		t.Errorf("expected overwritten password 'new-pass', got %q", got.Password)
	}
}

func TestFileProvider_LegacyTokenMigration(t *testing.T) {
	tmpDir := t.TempDir()
	provider := NewFileProvider(tmpDir)
	ctx := context.Background()

	// Manually write a legacy format file with "token" instead of "password"
	agentDir := filepath.Join(tmpDir, "legacy-agent")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}

	legacyData := FileCredentials{
		Username: "legacy-user",
		Token:    "legacy-token",
		Empire:   "voidborn",
	}
	data, _ := json.MarshalIndent(legacyData, "", "  ")
	credFile := filepath.Join(agentDir, "credentials.json")
	if err := os.WriteFile(credFile, data, 0600); err != nil {
		t.Fatalf("failed to write legacy file: %v", err)
	}

	got, err := provider.GetCredentials(ctx, "legacy-agent")
	if err != nil {
		t.Fatalf("GetCredentials failed: %v", err)
	}
	if got.Password != "legacy-token" {
		t.Errorf("expected password from legacy token, got %q", got.Password)
	}

	// After migration, the file should have password field
	migratedData, err := os.ReadFile(credFile)
	if err != nil {
		t.Fatalf("failed to read migrated file: %v", err)
	}
	var migrated FileCredentials
	if err := json.Unmarshal(migratedData, &migrated); err != nil {
		t.Fatalf("failed to parse migrated file: %v", err)
	}
	if migrated.Password != "legacy-token" {
		t.Errorf("migrated file should have password='legacy-token', got %q", migrated.Password)
	}
}

func TestFileProvider_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	provider := NewFileProvider(tmpDir)
	ctx := context.Background()

	agentDir := filepath.Join(tmpDir, "bad-agent")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	credFile := filepath.Join(agentDir, "credentials.json")
	if err := os.WriteFile(credFile, []byte("{invalid json"), 0600); err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	_, err := provider.GetCredentials(ctx, "bad-agent")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestFileProvider_EmptyUsernameOrPassword(t *testing.T) {
	tmpDir := t.TempDir()
	provider := NewFileProvider(tmpDir)
	ctx := context.Background()

	agentDir := filepath.Join(tmpDir, "empty-agent")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}

	// Write file with empty username and password
	data, _ := json.Marshal(FileCredentials{Username: "", Password: ""})
	credFile := filepath.Join(agentDir, "credentials.json")
	if err := os.WriteFile(credFile, data, 0600); err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	_, err := provider.GetCredentials(ctx, "empty-agent")
	if err == nil {
		t.Fatal("expected error for empty credentials")
	}
}

func TestFileProvider_RemoveCredentials(t *testing.T) {
	tmpDir := t.TempDir()
	provider := NewFileProvider(tmpDir)
	ctx := context.Background()

	_ = provider.StoreCredentials(ctx, "agent-1", &Credentials{
		Username: "u", Password: "p", Empire: "e",
	})

	err := provider.RemoveCredentials(ctx, "agent-1")
	if err != nil {
		t.Fatalf("RemoveCredentials failed: %v", err)
	}

	// Verify gone
	_, err = provider.GetCredentials(ctx, "agent-1")
	if !IsErrCredentialsNotFound(err) {
		t.Errorf("expected not found after remove, got: %v", err)
	}
}

func TestFileProvider_RemoveNonexistent(t *testing.T) {
	tmpDir := t.TempDir()
	provider := NewFileProvider(tmpDir)
	ctx := context.Background()

	err := provider.RemoveCredentials(ctx, "ghost")
	if err == nil {
		t.Fatal("expected error for nonexistent removal")
	}
	if !IsErrCredentialsNotFound(err) {
		t.Errorf("expected ErrCredentialsNotFound, got: %v", err)
	}
}

func TestFileProvider_ListAgents(t *testing.T) {
	tmpDir := t.TempDir()
	provider := NewFileProvider(tmpDir)
	ctx := context.Background()

	// Empty dir
	agents, err := provider.ListAgents(ctx)
	if err != nil {
		t.Fatalf("ListAgents failed: %v", err)
	}
	if len(agents) != 0 {
		t.Errorf("expected 0 agents, got %d", len(agents))
	}

	// Add agents
	for _, id := range []string{"alpha", "beta", "gamma"} {
		_ = provider.StoreCredentials(ctx, id, &Credentials{
			Username: id, Password: "pass", Empire: "e",
		})
	}

	agents, err = provider.ListAgents(ctx)
	if err != nil {
		t.Fatalf("ListAgents failed: %v", err)
	}
	sort.Strings(agents)
	if len(agents) != 3 {
		t.Fatalf("expected 3 agents, got %d", len(agents))
	}
	if agents[0] != "alpha" || agents[1] != "beta" || agents[2] != "gamma" {
		t.Errorf("unexpected agents: %v", agents)
	}
}

func TestFileProvider_ListAgents_NonexistentDir(t *testing.T) {
	provider := NewFileProvider(filepath.Join(t.TempDir(), "does-not-exist"))
	ctx := context.Background()

	agents, err := provider.ListAgents(ctx)
	if err != nil {
		t.Fatalf("ListAgents should not error for missing dir: %v", err)
	}
	if len(agents) != 0 {
		t.Errorf("expected 0 agents, got %d", len(agents))
	}
}

func TestFileProvider_ListAgents_SkipsNonDirs(t *testing.T) {
	tmpDir := t.TempDir()
	provider := NewFileProvider(tmpDir)
	ctx := context.Background()

	// Create a regular file in the agents dir
	if err := os.WriteFile(filepath.Join(tmpDir, "not-a-dir"), []byte("hi"), 0644); err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	// Create an agent
	_ = provider.StoreCredentials(ctx, "real-agent", &Credentials{
		Username: "u", Password: "p", Empire: "e",
	})

	agents, err := provider.ListAgents(ctx)
	if err != nil {
		t.Fatalf("ListAgents failed: %v", err)
	}
	if len(agents) != 1 || agents[0] != "real-agent" {
		t.Errorf("expected [real-agent], got %v", agents)
	}
}

func TestFileProvider_ListAgents_SkipsDirsWithoutCredFile(t *testing.T) {
	tmpDir := t.TempDir()
	provider := NewFileProvider(tmpDir)
	ctx := context.Background()

	// Create a directory without credentials.json
	if err := os.MkdirAll(filepath.Join(tmpDir, "no-creds-agent"), 0755); err != nil {
		t.Fatalf("failed to mkdir: %v", err)
	}

	agents, err := provider.ListAgents(ctx)
	if err != nil {
		t.Fatalf("ListAgents failed: %v", err)
	}
	if len(agents) != 0 {
		t.Errorf("expected 0 agents, got %v", agents)
	}
}

func TestFileProvider_FilePermissions(t *testing.T) {
	tmpDir := t.TempDir()
	provider := NewFileProvider(tmpDir)
	ctx := context.Background()

	_ = provider.StoreCredentials(ctx, "perm-agent", &Credentials{
		Username: "u", Password: "p", Empire: "e",
	})

	t.Run("validate correct permissions", func(t *testing.T) {
		err := provider.ValidatePermissions("perm-agent")
		if err != nil {
			t.Fatalf("ValidatePermissions failed: %v", err)
		}
	})

	t.Run("validate nonexistent agent", func(t *testing.T) {
		err := provider.ValidatePermissions("nonexistent")
		if err == nil {
			t.Fatal("expected error for nonexistent agent")
		}
	})

	t.Run("validate wrong permissions", func(t *testing.T) {
		credPath := provider.credentialPath("perm-agent")
		_ = os.Chmod(credPath, 0644)

		err := provider.ValidatePermissions("perm-agent")
		if err == nil {
			t.Fatal("expected error for wrong permissions")
		}

		// Restore
		_ = os.Chmod(credPath, 0600)
	})

	t.Run("set permissions", func(t *testing.T) {
		credPath := provider.credentialPath("perm-agent")
		_ = os.Chmod(credPath, 0644)

		err := provider.SetPermissions("perm-agent")
		if err != nil {
			t.Fatalf("SetPermissions failed: %v", err)
		}

		info, _ := os.Stat(credPath)
		if info.Mode().Perm() != 0600 {
			t.Errorf("expected 0600, got %v", info.Mode().Perm())
		}
	})
}

func TestFileProvider_ConcurrentStoreAndGet(t *testing.T) {
	tmpDir := t.TempDir()
	provider := NewFileProvider(tmpDir)
	ctx := context.Background()

	// Pre-create credential
	_ = provider.StoreCredentials(ctx, "conc-agent", &Credentials{
		Username: "user", Password: "pass", Empire: "e",
	})

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = provider.StoreCredentials(ctx, "conc-agent", &Credentials{
				Username: "user", Password: "pass", Empire: "e",
			})
		}()
		go func() {
			defer wg.Done()
			_, _ = provider.GetCredentials(ctx, "conc-agent")
		}()
	}
	wg.Wait()
}

func TestFileProvider_CredentialPath(t *testing.T) {
	provider := NewFileProvider("/base/agents")
	got := provider.credentialPath("my-agent")
	want := filepath.Join("/base/agents", "my-agent", "credentials.json")
	if got != want {
		t.Errorf("credentialPath = %q, want %q", got, want)
	}
}
