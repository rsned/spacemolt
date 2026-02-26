package game

import (
	"os"
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
