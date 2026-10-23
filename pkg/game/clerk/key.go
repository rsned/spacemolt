// Package clerk is a small client for the two Clerk-gated SpaceMolt HTTP
// endpoints the fleet needs: listing the account's players and minting
// single-use websocket tokens.
//
// It is a leaf package. pkg/game imports it, so it must never import pkg/game.
package clerk

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrNoKey means no Clerk API key could be found in the environment or on
// disk. Callers can fall back to password logins when they see it.
var ErrNoKey = errors.New("clerk: no API key configured")

const (
	envKey     = "SPACEMOLT_CLERK_API_KEY"
	envKeyFile = "SPACEMOLT_CLERK_KEY_FILE"
)

// LoadKey resolves the Clerk API key from, in order: $SPACEMOLT_CLERK_API_KEY,
// the file named by $SPACEMOLT_CLERK_KEY_FILE, then
// ~/.config/spacemolt/clerk_api_key. Whitespace is trimmed. A missing or
// empty file is ErrNoKey, not a hard failure, so an unconfigured host simply
// runs without Clerk. Other read errors are wrapped; they never include the
// key itself.
func LoadKey() (string, error) {
	if k := strings.TrimSpace(os.Getenv(envKey)); k != "" {
		return k, nil
	}
	path := os.Getenv(envKeyFile)
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", ErrNoKey
		}
		path = filepath.Join(home, ".config", "spacemolt", "clerk_api_key")
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", ErrNoKey
	}
	if err != nil {
		return "", fmt.Errorf("clerk: read key file: %w", err)
	}
	k := strings.TrimSpace(string(data))
	if k == "" {
		return "", ErrNoKey
	}
	return k, nil
}
