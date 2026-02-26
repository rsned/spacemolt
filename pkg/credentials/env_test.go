package credentials

import (
	"context"
	"os"
	"sort"
	"sync"
	"testing"
)

func TestEnvProvider_GetCredentials(t *testing.T) {
	const prefix = "TEST_CRED_"

	// Clean up all test env vars after each test
	cleanup := func(keys ...string) {
		for _, k := range keys {
			_ = os.Unsetenv(k)
		}
	}

	provider := NewEnvProvider(prefix)
	ctx := context.Background()

	t.Run("success with PASSWORD", func(t *testing.T) {
		keys := []string{
			prefix + "MYAGENT_USERNAME",
			prefix + "MYAGENT_PASSWORD",
			prefix + "MYAGENT_EMPIRE",
		}
		defer cleanup(keys...)

		_ = os.Setenv(keys[0], "theuser")
		_ = os.Setenv(keys[1], "thepass")
		_ = os.Setenv(keys[2], "starborn")

		creds, err := provider.GetCredentials(ctx, "myagent")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if creds.Username != "theuser" {
			t.Errorf("expected username 'theuser', got %q", creds.Username)
		}
		if creds.Password != "thepass" {
			t.Errorf("expected password 'thepass', got %q", creds.Password)
		}
		if creds.Empire != "starborn" {
			t.Errorf("expected empire 'starborn', got %q", creds.Empire)
		}
	})

	t.Run("legacy TOKEN fallback", func(t *testing.T) {
		keys := []string{
			prefix + "AGENT_1_USERNAME",
			prefix + "AGENT_1_TOKEN",
		}
		defer cleanup(keys...)

		_ = os.Setenv(keys[0], "user1")
		_ = os.Setenv(keys[1], "tok1")

		creds, err := provider.GetCredentials(ctx, "agent-1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if creds.Password != "tok1" {
			t.Errorf("expected password from TOKEN, got %q", creds.Password)
		}
	})

	t.Run("default empire", func(t *testing.T) {
		keys := []string{
			prefix + "BOT_USERNAME",
			prefix + "BOT_PASSWORD",
		}
		defer cleanup(keys...)

		_ = os.Setenv(keys[0], "botuser")
		_ = os.Setenv(keys[1], "botpass")

		creds, err := provider.GetCredentials(ctx, "bot")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if creds.Empire != "voidborn" {
			t.Errorf("expected default empire 'voidborn', got %q", creds.Empire)
		}
	})

	t.Run("missing username", func(t *testing.T) {
		keys := []string{prefix + "NOUSER_PASSWORD"}
		defer cleanup(keys...)

		_ = os.Setenv(keys[0], "somepass")

		_, err := provider.GetCredentials(ctx, "nouser")
		if err == nil {
			t.Fatal("expected error for missing username")
		}
		if !IsErrCredentialsNotFound(err) {
			t.Errorf("expected ErrCredentialsNotFound, got: %v", err)
		}
	})

	t.Run("missing password and token", func(t *testing.T) {
		keys := []string{prefix + "NOPASS_USERNAME"}
		defer cleanup(keys...)

		_ = os.Setenv(keys[0], "someuser")

		_, err := provider.GetCredentials(ctx, "nopass")
		if err == nil {
			t.Fatal("expected error for missing password")
		}
		if !IsErrCredentialsNotFound(err) {
			t.Errorf("expected ErrCredentialsNotFound, got: %v", err)
		}
	})

	t.Run("completely empty agent", func(t *testing.T) {
		_, err := provider.GetCredentials(ctx, "nonexistent")
		if err == nil {
			t.Fatal("expected error for nonexistent agent")
		}
		if !IsErrCredentialsNotFound(err) {
			t.Errorf("expected ErrCredentialsNotFound, got: %v", err)
		}
	})

	t.Run("hyphenated agent ID", func(t *testing.T) {
		keys := []string{
			prefix + "MY_COOL_AGENT_USERNAME",
			prefix + "MY_COOL_AGENT_PASSWORD",
		}
		defer cleanup(keys...)

		_ = os.Setenv(keys[0], "cooluser")
		_ = os.Setenv(keys[1], "coolpass")

		creds, err := provider.GetCredentials(ctx, "my-cool-agent")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if creds.Username != "cooluser" {
			t.Errorf("expected 'cooluser', got %q", creds.Username)
		}
	})
}

func TestEnvProvider_DefaultPrefix(t *testing.T) {
	provider := NewEnvProvider("")
	ctx := context.Background()

	key := "SPACEMOLT_AGENT_TESTDEFAULT_USERNAME"
	passKey := "SPACEMOLT_AGENT_TESTDEFAULT_PASSWORD"
	defer func() {
		_ = os.Unsetenv(key)
		_ = os.Unsetenv(passKey)
	}()

	_ = os.Setenv(key, "defuser")
	_ = os.Setenv(passKey, "defpass")

	creds, err := provider.GetCredentials(ctx, "testdefault")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds.Username != "defuser" {
		t.Errorf("expected 'defuser', got %q", creds.Username)
	}
}

func TestEnvProvider_StoreCredentials(t *testing.T) {
	provider := NewEnvProvider("TEST_STORE_")
	ctx := context.Background()

	err := provider.StoreCredentials(ctx, "agent-1", &Credentials{
		Username: "user",
		Password: "pass",
	})
	if err == nil {
		t.Fatal("expected error from StoreCredentials on env provider")
	}
}

func TestEnvProvider_RemoveCredentials(t *testing.T) {
	provider := NewEnvProvider("TEST_RM_")
	ctx := context.Background()

	err := provider.RemoveCredentials(ctx, "agent-1")
	if err == nil {
		t.Fatal("expected error from RemoveCredentials on env provider")
	}
}

func TestEnvProvider_ListAgents(t *testing.T) {
	const prefix = "TEST_LIST_"
	provider := NewEnvProvider(prefix)
	ctx := context.Background()

	keys := []string{
		prefix + "ALPHA_USERNAME",
		prefix + "ALPHA_PASSWORD",
		prefix + "BETA_3_USERNAME",
		prefix + "BETA_3_PASSWORD",
	}
	defer func() {
		for _, k := range keys {
			_ = os.Unsetenv(k)
		}
	}()

	for _, k := range keys {
		_ = os.Setenv(k, "val")
	}

	agents, err := provider.ListAgents(ctx)
	if err != nil {
		t.Fatalf("ListAgents failed: %v", err)
	}

	sort.Strings(agents)

	if len(agents) != 2 {
		t.Fatalf("expected 2 agents, got %d: %v", len(agents), agents)
	}

	if agents[0] != "alpha" {
		t.Errorf("expected 'alpha', got %q", agents[0])
	}
	if agents[1] != "beta-3" {
		t.Errorf("expected 'beta-3', got %q", agents[1])
	}
}

func TestEnvProvider_ListAgents_Empty(t *testing.T) {
	provider := NewEnvProvider("UNIQUE_NOAGENTS_PREFIX_")
	ctx := context.Background()

	agents, err := provider.ListAgents(ctx)
	if err != nil {
		t.Fatalf("ListAgents failed: %v", err)
	}
	if len(agents) != 0 {
		t.Errorf("expected 0 agents, got %d", len(agents))
	}
}

func TestEnvProvider_ConcurrentGet(t *testing.T) {
	const prefix = "TEST_CONC_"
	provider := NewEnvProvider(prefix)
	ctx := context.Background()

	keys := []string{
		prefix + "WORKER_USERNAME",
		prefix + "WORKER_PASSWORD",
	}
	defer func() {
		for _, k := range keys {
			_ = os.Unsetenv(k)
		}
	}()

	_ = os.Setenv(keys[0], "workeruser")
	_ = os.Setenv(keys[1], "workerpass")

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			creds, err := provider.GetCredentials(ctx, "worker")
			if err != nil {
				t.Errorf("concurrent GetCredentials failed: %v", err)
				return
			}
			if creds.Username != "workeruser" {
				t.Errorf("expected 'workeruser', got %q", creds.Username)
			}
		}()
	}
	wg.Wait()
}

func TestEnvKeyGeneration(t *testing.T) {
	provider := NewEnvProvider("PFX_")

	tests := []struct {
		agentID  string
		field    string
		expected string
	}{
		{"explorer-7", "USERNAME", "PFX_EXPLORER_7_USERNAME"},
		{"miner", "PASSWORD", "PFX_MINER_PASSWORD"},
		{"my-cool-agent", "EMPIRE", "PFX_MY_COOL_AGENT_EMPIRE"},
	}

	for _, tt := range tests {
		got := provider.envKey(tt.agentID, tt.field)
		if got != tt.expected {
			t.Errorf("envKey(%q, %q) = %q, want %q", tt.agentID, tt.field, got, tt.expected)
		}
	}
}

func TestSplitFirst(t *testing.T) {
	tests := []struct {
		s, sep     string
		wantA      string
		wantB      string
	}{
		{"key=value", "=", "key", "value"},
		{"key=val=ue", "=", "key", "val=ue"},
		{"noequals", "=", "noequals", ""},
		{"", "=", "", ""},
		{"=value", "=", "", "value"},
	}

	for _, tt := range tests {
		a, b := splitFirst(tt.s, tt.sep)
		if a != tt.wantA || b != tt.wantB {
			t.Errorf("splitFirst(%q, %q) = (%q, %q), want (%q, %q)", tt.s, tt.sep, a, b, tt.wantA, tt.wantB)
		}
	}
}

func TestNormalizeEnvAgentID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"EXPLORER_7", "explorer-7"},
		{"MINER", "miner"},
		{"", ""},
		{"ALPHA", "alpha"},
	}

	for _, tt := range tests {
		got := normalizeEnvAgentID(tt.input)
		if got != tt.expected {
			t.Errorf("normalizeEnvAgentID(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestIsAllDigits(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"123", true},
		{"0", true},
		{"", false},
		{"12a3", false},
		{"abc", false},
		{" ", false},
	}

	for _, tt := range tests {
		got := isAllDigits(tt.input)
		if got != tt.want {
			t.Errorf("isAllDigits(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
