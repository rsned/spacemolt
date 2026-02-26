package credentials

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
)

func TestFallbackProvider_GetCredentials(t *testing.T) {
	ctx := context.Background()

	t.Run("no providers", func(t *testing.T) {
		fb := NewFallbackProvider()
		_, err := fb.GetCredentials(ctx, "agent-1")
		if err == nil {
			t.Fatal("expected error with no providers")
		}
		if !IsErrCredentialsNotFound(err) {
			t.Errorf("expected ErrCredentialsNotFound, got: %v", err)
		}
	})

	t.Run("first provider succeeds", func(t *testing.T) {
		p1 := NewStaticProvider("user1", "pass1", "emp1")
		p2 := NewStaticProvider("user2", "pass2", "emp2")
		fb := NewFallbackProvider(p1, p2)

		creds, err := fb.GetCredentials(ctx, "agent-1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if creds.Username != "user1" {
			t.Errorf("expected user1, got %q", creds.Username)
		}
	})

	t.Run("falls through not-found to second", func(t *testing.T) {
		p1 := &mockProvider{
			getFunc: func(_ context.Context, _ string) (*Credentials, error) {
				return nil, ErrCredentialsNotFound
			},
		}
		p2 := NewStaticProvider("user2", "pass2", "emp2")
		fb := NewFallbackProvider(p1, p2)

		creds, err := fb.GetCredentials(ctx, "agent-1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if creds.Username != "user2" {
			t.Errorf("expected user2, got %q", creds.Username)
		}
	})

	t.Run("falls through unavailable to second", func(t *testing.T) {
		p1 := &mockProvider{
			getFunc: func(_ context.Context, _ string) (*Credentials, error) {
				return nil, ErrProviderUnavailable
			},
		}
		p2 := NewStaticProvider("user2", "pass2", "emp2")
		fb := NewFallbackProvider(p1, p2)

		creds, err := fb.GetCredentials(ctx, "agent-1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if creds.Username != "user2" {
			t.Errorf("expected user2, got %q", creds.Username)
		}
	})

	t.Run("all fail returns not found", func(t *testing.T) {
		p1 := &mockProvider{
			getFunc: func(_ context.Context, _ string) (*Credentials, error) {
				return nil, ErrCredentialsNotFound
			},
		}
		p2 := &mockProvider{
			getFunc: func(_ context.Context, _ string) (*Credentials, error) {
				return nil, ErrProviderUnavailable
			},
		}
		fb := NewFallbackProvider(p1, p2)

		_, err := fb.GetCredentials(ctx, "agent-1")
		if err == nil {
			t.Fatal("expected error when all providers fail")
		}
		if !IsErrCredentialsNotFound(err) {
			t.Errorf("expected ErrCredentialsNotFound, got: %v", err)
		}
	})

	t.Run("falls through arbitrary error", func(t *testing.T) {
		p1 := &mockProvider{
			getFunc: func(_ context.Context, _ string) (*Credentials, error) {
				return nil, errors.New("some transient error")
			},
		}
		p2 := NewStaticProvider("user2", "pass2", "emp2")
		fb := NewFallbackProvider(p1, p2)

		creds, err := fb.GetCredentials(ctx, "agent-1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if creds.Username != "user2" {
			t.Errorf("expected user2, got %q", creds.Username)
		}
	})
}

func TestFallbackProvider_StoreCredentials(t *testing.T) {
	ctx := context.Background()
	creds := &Credentials{Username: "u", Password: "p", Empire: "e"}

	t.Run("first provider succeeds", func(t *testing.T) {
		called := false
		p1 := &mockProvider{
			storeFunc: func(_ context.Context, _ string, _ *Credentials) error {
				called = true
				return nil
			},
		}
		p2 := &mockProvider{
			storeFunc: func(_ context.Context, _ string, _ *Credentials) error {
				t.Error("second provider should not be called")
				return nil
			},
		}
		fb := NewFallbackProvider(p1, p2)

		err := fb.StoreCredentials(ctx, "agent-1", creds)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !called {
			t.Error("first provider should have been called")
		}
	})

	t.Run("first unavailable falls to second", func(t *testing.T) {
		p1 := &mockProvider{
			storeFunc: func(_ context.Context, _ string, _ *Credentials) error {
				return ErrProviderUnavailable
			},
		}
		secondCalled := false
		p2 := &mockProvider{
			storeFunc: func(_ context.Context, _ string, _ *Credentials) error {
				secondCalled = true
				return nil
			},
		}
		fb := NewFallbackProvider(p1, p2)

		err := fb.StoreCredentials(ctx, "agent-1", creds)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !secondCalled {
			t.Error("second provider should have been called")
		}
	})

	t.Run("all fail", func(t *testing.T) {
		p1 := &mockProvider{
			storeFunc: func(_ context.Context, _ string, _ *Credentials) error {
				return ErrProviderUnavailable
			},
		}
		p2 := &mockProvider{
			storeFunc: func(_ context.Context, _ string, _ *Credentials) error {
				return errors.New("disk full")
			},
		}
		fb := NewFallbackProvider(p1, p2)

		err := fb.StoreCredentials(ctx, "agent-1", creds)
		if err == nil {
			t.Fatal("expected error when all providers fail")
		}
	})

	t.Run("no providers", func(t *testing.T) {
		fb := NewFallbackProvider()
		err := fb.StoreCredentials(ctx, "agent-1", creds)
		if err == nil {
			t.Fatal("expected error with no providers")
		}
	})
}

func TestFallbackProvider_RemoveCredentials(t *testing.T) {
	ctx := context.Background()

	t.Run("at least one succeeds", func(t *testing.T) {
		p1 := &mockProvider{
			removeFunc: func(_ context.Context, _ string) error {
				return errors.New("provider 1 error")
			},
		}
		p2 := &mockProvider{
			removeFunc: func(_ context.Context, _ string) error {
				return nil
			},
		}
		fb := NewFallbackProvider(p1, p2)

		err := fb.RemoveCredentials(ctx, "agent-1")
		if err != nil {
			t.Fatalf("expected nil when at least one provider succeeds, got: %v", err)
		}
	})

	t.Run("all fail", func(t *testing.T) {
		p1 := &mockProvider{
			removeFunc: func(_ context.Context, _ string) error {
				return errors.New("err1")
			},
		}
		p2 := &mockProvider{
			removeFunc: func(_ context.Context, _ string) error {
				return errors.New("err2")
			},
		}
		fb := NewFallbackProvider(p1, p2)

		err := fb.RemoveCredentials(ctx, "agent-1")
		if err == nil {
			t.Fatal("expected error when all providers fail")
		}
	})

	t.Run("no providers", func(t *testing.T) {
		fb := NewFallbackProvider()
		err := fb.RemoveCredentials(ctx, "agent-1")
		if err == nil {
			t.Fatal("expected error with no providers")
		}
		if !IsErrCredentialsNotFound(err) {
			t.Errorf("expected ErrCredentialsNotFound, got: %v", err)
		}
	})
}

func TestFallbackProvider_ListAgents(t *testing.T) {
	ctx := context.Background()

	t.Run("aggregates and deduplicates", func(t *testing.T) {
		p1 := &mockProvider{
			listFunc: func(_ context.Context) ([]string, error) {
				return []string{"a", "b"}, nil
			},
		}
		p2 := &mockProvider{
			listFunc: func(_ context.Context) ([]string, error) {
				return []string{"b", "c"}, nil
			},
		}
		fb := NewFallbackProvider(p1, p2)

		agents, err := fb.ListAgents(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		sort.Strings(agents)
		if len(agents) != 3 {
			t.Fatalf("expected 3 agents, got %d", len(agents))
		}
		if agents[0] != "a" || agents[1] != "b" || agents[2] != "c" {
			t.Errorf("unexpected agents: %v", agents)
		}
	})

	t.Run("skips erroring providers", func(t *testing.T) {
		p1 := &mockProvider{
			listFunc: func(_ context.Context) ([]string, error) {
				return nil, errors.New("broken")
			},
		}
		p2 := &mockProvider{
			listFunc: func(_ context.Context) ([]string, error) {
				return []string{"x"}, nil
			},
		}
		fb := NewFallbackProvider(p1, p2)

		agents, err := fb.ListAgents(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(agents) != 1 || agents[0] != "x" {
			t.Errorf("expected [x], got %v", agents)
		}
	})

	t.Run("no providers", func(t *testing.T) {
		fb := NewFallbackProvider()
		agents, err := fb.ListAgents(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(agents) != 0 {
			t.Errorf("expected 0 agents, got %d", len(agents))
		}
	})

	t.Run("all providers error", func(t *testing.T) {
		p1 := &mockProvider{
			listFunc: func(_ context.Context) ([]string, error) {
				return nil, errors.New("err")
			},
		}
		fb := NewFallbackProvider(p1)

		agents, err := fb.ListAgents(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(agents) != 0 {
			t.Errorf("expected 0 agents, got %d", len(agents))
		}
	})
}

func TestFallbackProvider_AddProvider(t *testing.T) {
	ctx := context.Background()

	fb := NewFallbackProvider()
	_, err := fb.GetCredentials(ctx, "agent-1")
	if err == nil {
		t.Fatal("expected error with no providers")
	}

	fb.AddProvider(NewStaticProvider("user", "pass", "emp"))
	creds, err := fb.GetCredentials(ctx, "agent-1")
	if err != nil {
		t.Fatalf("unexpected error after adding provider: %v", err)
	}
	if creds.Username != "user" {
		t.Errorf("expected 'user', got %q", creds.Username)
	}
}

func TestFallbackProvider_ConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	fb := NewFallbackProvider(NewStaticProvider("u", "p", "e"))

	var wg sync.WaitGroup
	for range 30 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, err := fb.GetCredentials(ctx, "agent")
			if err != nil {
				t.Errorf("concurrent get failed: %v", err)
			}
		}()
		go func() {
			defer wg.Done()
			_, err := fb.ListAgents(ctx)
			if err != nil {
				t.Errorf("concurrent list failed: %v", err)
			}
		}()
	}
	wg.Wait()
}

func TestProviderType(t *testing.T) {
	tests := []struct {
		provider Provider
		expected string
	}{
		{&FileProvider{}, "file"},
		{&SQLiteProvider{}, "sqlite"},
		{&KeyringProvider{}, "keyring"},
		{&EnvProvider{}, "env"},
		{&LegacyProvider{}, "legacy"},
		{&StaticProvider{}, "static"},
		{&FallbackProvider{}, "fallback"},
		{&mockProvider{}, "unknown"},
	}

	for _, tt := range tests {
		got := providerType(tt.provider)
		if got != tt.expected {
			t.Errorf("providerType(%T) = %q, want %q", tt.provider, got, tt.expected)
		}
	}
}
