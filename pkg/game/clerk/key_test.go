package clerk

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadKey_EnvWins(t *testing.T) {
	f := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(f, []byte("from-file"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SPACEMOLT_CLERK_API_KEY", " ak_env \n")
	t.Setenv("SPACEMOLT_CLERK_KEY_FILE", f)
	got, err := LoadKey()
	if err != nil || got != "ak_env" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestLoadKey_FileFallbackTrimsWhitespace(t *testing.T) {
	f := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(f, []byte("  ak_x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SPACEMOLT_CLERK_API_KEY", "")
	t.Setenv("SPACEMOLT_CLERK_KEY_FILE", f)
	got, err := LoadKey()
	if err != nil || got != "ak_x" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestLoadKey_NoneIsErrNoKey(t *testing.T) {
	t.Setenv("SPACEMOLT_CLERK_API_KEY", "")
	t.Setenv("SPACEMOLT_CLERK_KEY_FILE", filepath.Join(t.TempDir(), "missing"))
	if _, err := LoadKey(); !errors.Is(err, ErrNoKey) {
		t.Fatalf("err = %v, want ErrNoKey", err)
	}
}

func TestLoadKey_EmptyFileIsErrNoKey(t *testing.T) {
	f := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(f, []byte(" \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SPACEMOLT_CLERK_API_KEY", "")
	t.Setenv("SPACEMOLT_CLERK_KEY_FILE", f)
	if _, err := LoadKey(); !errors.Is(err, ErrNoKey) {
		t.Fatalf("err = %v, want ErrNoKey", err)
	}
}
