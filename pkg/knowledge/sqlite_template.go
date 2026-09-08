package knowledge

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// The migration chain is expensive to replay: every DDL statement makes the
// pure-Go SQLite driver re-parse the whole schema, which costs ~0.1s per
// fresh database normally and ~3.5s under the race detector. Test suites open
// several hundred throwaway knowledge bases per run, so they clone a
// template that was migrated once instead of migrating each time. Cloning a
// migrated file and opening it costs ~90ms under -race.
var (
	templateOnce  sync.Once
	templateBytes []byte
	templateErr   error
)

// MigratedTemplate returns the bytes of an empty knowledge database file
// with every migration applied. The migration chain runs at most once per
// process; the result (~1 MB) is cached in memory for later calls.
func MigratedTemplate() ([]byte, error) {
	templateOnce.Do(func() {
		templateBytes, templateErr = buildMigratedTemplate()
	})
	return templateBytes, templateErr
}

// WriteMigratedDB writes a fresh, empty, fully migrated knowledge database
// to path. Opening it with NewSQLiteKB finds the schema current and skips
// the migration chain.
func WriteMigratedDB(path string) error {
	b, err := MigratedTemplate()
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return fmt.Errorf("write migrated db: %w", err)
	}
	return nil
}

func buildMigratedTemplate() ([]byte, error) {
	dir, err := os.MkdirTemp("", "spacemolt-kb-template-*")
	if err != nil {
		return nil, fmt.Errorf("template dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	path := filepath.Join(dir, "template.db")
	// Rollback-journal mode (WAL off) keeps the whole database in one file,
	// so the bytes read back after Close are the complete database.
	kb, err := NewSQLiteKB(Config{
		DBPath:       path,
		WAL:          false,
		MaxOpenConns: 1,
		MaxIdleConns: 1,
		BusyTimeout:  time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("migrate template: %w", err)
	}
	if err := kb.Close(); err != nil {
		return nil, fmt.Errorf("close template: %w", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read template: %w", err)
	}
	return b, nil
}
