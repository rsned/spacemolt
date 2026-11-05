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

// A Clerk-only agent — an account registered on the website, whose password we
// never held — authenticates with player_id alone. Its credentials.json carries
// no password, and that must load; without a player_id it still may not.
func TestLoadCredentials_ClerkOnlyNeedsPlayerID(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{"clerk-only", `{"username":"JohnnyCab","empire":"outerrim","player_id":"8fd9799a33dd53f3769b7558ca510257"}`, false},
		{"neither password nor player_id", `{"username":"JohnnyCab","empire":"outerrim"}`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "credentials.json"), []byte(tt.body), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := LoadCredentials(dir)
			if (err != nil) != tt.wantErr {
				t.Fatalf("LoadCredentials err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
