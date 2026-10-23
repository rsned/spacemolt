package game

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCredentials_PlayerIDOptional(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"absent", `{"username":"a","password":"p","empire":"e"}`, ""},
		{"present", `{"username":"a","password":"p","empire":"e","player_id":"pl-1"}`, "pl-1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "credentials.json"), []byte(tt.body), 0o600); err != nil {
				t.Fatal(err)
			}
			creds, err := LoadCredentials(dir)
			if err != nil {
				t.Fatalf("LoadCredentials: %v", err)
			}
			if creds.PlayerID != tt.want {
				t.Errorf("PlayerID = %q, want %q", creds.PlayerID, tt.want)
			}
		})
	}
}
