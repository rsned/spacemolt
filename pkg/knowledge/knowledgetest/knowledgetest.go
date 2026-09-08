// Package knowledgetest opens throwaway knowledge bases for tests.
//
// A fresh knowledge base normally replays the whole migration chain, which
// costs seconds under the race detector. These helpers clone a template that
// was migrated once per process (see knowledge.MigratedTemplate), so opening
// one costs milliseconds.
package knowledgetest

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// Path returns the path of a fresh, empty, fully migrated knowledge database
// file inside tb.TempDir(). Pass it as Config.DBPath to NewSQLiteKB.
func Path(tb testing.TB) string {
	tb.Helper()
	path := filepath.Join(tb.TempDir(), "knowledge.db")
	if err := knowledge.WriteMigratedDB(path); err != nil {
		tb.Fatalf("knowledgetest: %v", err)
	}
	return path
}

// NewKB opens a fresh, empty knowledge base on Path(tb) with a single
// connection and closes it when the test ends.
func NewKB(tb testing.TB) *knowledge.SQLiteKB {
	tb.Helper()
	kb, err := knowledge.NewSQLiteKB(knowledge.Config{
		DBPath:       Path(tb),
		WAL:          false,
		MaxOpenConns: 1,
		MaxIdleConns: 1,
		BusyTimeout:  time.Second,
	})
	if err != nil {
		tb.Fatalf("knowledgetest: open: %v", err)
	}
	tb.Cleanup(func() { _ = kb.Close() })
	return kb
}
