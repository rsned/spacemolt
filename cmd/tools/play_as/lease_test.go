package main

import (
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

func TestOperatorLease_WritesTimestampPidAndReleases(t *testing.T) {
	t.Chdir(t.TempDir())
	l := acquireOperatorLease("miner-1", log.New(io.Discard, "", 0))

	data, err := os.ReadFile(game.DisplacedLeasePath("miner-1"))
	if err != nil {
		t.Fatalf("lease not written: %v", err)
	}
	fields := strings.Fields(string(data))
	if len(fields) != 2 {
		t.Fatalf("lease = %q, want \"<RFC3339> <pid>\"", data)
	}
	if _, err := time.Parse(time.RFC3339, fields[0]); err != nil {
		t.Errorf("timestamp %q: %v", fields[0], err)
	}
	if fields[1] != strconv.Itoa(os.Getpid()) {
		t.Errorf("pid = %q, want %d", fields[1], os.Getpid())
	}

	l.release()
	l.release() // idempotent
	if _, err := os.Stat(game.DisplacedLeasePath("miner-1")); !os.IsNotExist(err) {
		t.Errorf("lease still present after release (stat err=%v)", err)
	}
}
