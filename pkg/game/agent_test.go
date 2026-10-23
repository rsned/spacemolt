package game

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"path/filepath"
	"testing"
)

func TestLoadCredentials(t *testing.T) {
	t.Run("valid credentials", func(t *testing.T) {
		dir := t.TempDir()
		data := `{"username":"testuser","password":"testpass","empire":"solarian"}`
		if err := os.WriteFile(filepath.Join(dir, "credentials.json"), []byte(data), 0644); err != nil {
			t.Fatal(err)
		}

		creds, err := LoadCredentials(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if creds.Username != "testuser" {
			t.Errorf("Username = %q, want %q", creds.Username, "testuser")
		}
		if creds.Password != "testpass" {
			t.Errorf("Password = %q, want %q", creds.Password, "testpass")
		}
		if creds.Empire != "solarian" {
			t.Errorf("Empire = %q, want %q", creds.Empire, "solarian")
		}
	})

	t.Run("missing file", func(t *testing.T) {
		dir := t.TempDir()
		_, err := LoadCredentials(dir)
		if err == nil {
			t.Error("expected error for missing file")
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "credentials.json"), []byte("{invalid}"), 0644); err != nil {
			t.Fatal(err)
		}
		_, err := LoadCredentials(dir)
		if err == nil {
			t.Error("expected error for invalid JSON")
		}
	})

	t.Run("missing username", func(t *testing.T) {
		dir := t.TempDir()
		data := `{"username":"","password":"testpass","empire":"solarian"}`
		if err := os.WriteFile(filepath.Join(dir, "credentials.json"), []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
		_, err := LoadCredentials(dir)
		if err == nil {
			t.Error("expected error for missing username")
		}
	})

	t.Run("missing password", func(t *testing.T) {
		dir := t.TempDir()
		data := `{"username":"testuser","password":"","empire":"solarian"}`
		if err := os.WriteFile(filepath.Join(dir, "credentials.json"), []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
		_, err := LoadCredentials(dir)
		if err == nil {
			t.Error("expected error for missing password")
		}
	})

	t.Run("missing empire", func(t *testing.T) {
		dir := t.TempDir()
		data := `{"username":"testuser","password":"testpass","empire":""}`
		if err := os.WriteFile(filepath.Join(dir, "credentials.json"), []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
		_, err := LoadCredentials(dir)
		if err == nil {
			t.Error("expected error for missing empire")
		}
	})
}

func TestSimpleHandler(t *testing.T) {
	// SimpleHandler just logs - verify it doesn't panic with nil fields
	client := NewClient("wss://test.example.com", "user", "pass", nil)
	handler := &SimpleHandler{
		Client: client,
		Logger: client.debugLogger,
	}

	// Should not panic
	handler.OnConnected(&State{Credits: 100})
	handler.OnDisconnected(nil)
}

// An unconfigured host runs on passwords: no key means no minter, and the boot
// line names why.
func TestResolveTokenMinter_NoKeyIsNil(t *testing.T) {
	t.Setenv("SPACEMOLT_CLERK_API_KEY", "")
	t.Setenv("SPACEMOLT_CLERK_KEY_FILE", filepath.Join(t.TempDir(), "missing"))

	var buf bytes.Buffer
	m := resolveTokenMinter(&Credentials{Username: "u", PlayerID: "p1"}, log.New(&buf, "", 0))
	if m != nil {
		t.Fatal("resolveTokenMinter returned a minter with no key configured")
	}
	if got := buf.String(); strings.Count(got, "auth=password reason=no_key") != 1 {
		t.Errorf("boot log = %q, want one auth=password reason=no_key line", got)
	}
}

// With player_id already in credentials.json the minter must not list the
// account's players: that call is metered too, and every worker making it at
// boot would spend the budget the gate exists to protect.
func TestResolveTokenMinter_UsesPlayerIDWithoutListing(t *testing.T) {
	t.Setenv("SPACEMOLT_CLERK_API_KEY", "k-test")

	var mu sync.Mutex
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		if strings.HasSuffix(r.URL.Path, "/ws-token") {
			_, _ = w.Write([]byte(`{"token":"tok-xyz","expires_in":300}`))
			return
		}
		_, _ = w.Write([]byte(`{"players":[]}`))
	}))
	t.Cleanup(srv.Close)
	old := clerkBaseURL
	clerkBaseURL = srv.URL
	t.Cleanup(func() { clerkBaseURL = old })

	var buf bytes.Buffer
	m := resolveTokenMinter(&Credentials{Username: "u", PlayerID: "p1"}, log.New(&buf, "", 0))
	if m == nil {
		t.Fatal("resolveTokenMinter returned nil with a key and a player_id")
	}
	if got := buf.String(); strings.Count(got, "auth=clerk player_id=p1") != 1 {
		t.Errorf("boot log = %q, want one auth=clerk player_id=p1 line", got)
	}
	tok, err := m(context.Background())
	if err != nil || tok != "tok-xyz" {
		t.Fatalf("mint = %q, %v; want tok-xyz", tok, err)
	}

	mu.Lock()
	defer mu.Unlock()
	for _, p := range paths {
		if p == "/api/registration-code" {
			t.Errorf("listed players although creds.PlayerID was set; requests=%v", paths)
		}
	}
	if len(paths) != 1 || paths[0] != "/api/player/p1/ws-token" {
		t.Errorf("requests = %v, want only the p1 ws-token mint", paths)
	}
	if strings.Contains(buf.String(), "k-test") {
		t.Error("the Clerk key leaked into the log")
	}
}
