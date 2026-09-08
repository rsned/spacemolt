package knowledge

import (
	"context"
	"sync"
	"testing"
	"time"
)

// Every connection the pool opens must carry busy_timeout. Setting the
// pragma with one db.Exec reaches only the connection that statement lands
// on; the others fail instantly on any contention (2026-09-08: ~8k dropped
// sighting batches a day across the fleet).
func TestNewSQLiteKB_EveryPoolConnectionHasBusyTimeout(t *testing.T) {
	kb, err := NewSQLiteKB(Config{DBPath: testDBPath(t), MaxOpenConns: 4, MaxIdleConns: 4, BusyTimeout: 1234 * time.Millisecond})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = kb.Close() }()

	ctx := context.Background()
	got := make([]int, 4)
	hold := make(chan struct{})
	var wg sync.WaitGroup
	for i := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := kb.db.Conn(ctx)
			if err != nil {
				t.Errorf("conn %d: %v", i, err)
				return
			}
			defer func() { _ = c.Close() }()
			if err := c.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&got[i]); err != nil {
				t.Errorf("conn %d: %v", i, err)
			}
			<-hold // keep it open so the pool must hand out four distinct connections
		}()
	}
	time.Sleep(200 * time.Millisecond)
	close(hold)
	wg.Wait()
	for i, v := range got {
		if v != 1234 {
			t.Errorf("pool connection %d busy_timeout = %d, want 1234", i, v)
		}
	}
}

// A writer that meets a held write lock must wait for it, not fail.
func TestNewSQLiteKB_ConcurrentWriterWaitsForLock(t *testing.T) {
	kb, err := NewSQLiteKB(Config{DBPath: testDBPath(t), MaxOpenConns: 4, MaxIdleConns: 4, BusyTimeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = kb.Close() }()

	tx, err := kb.db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := tx.Exec(`INSERT INTO seen_players (player_id, username, first_seen_utc, last_seen_utc) VALUES ('p1', 'one', 't', 't')`); err != nil {
		t.Fatalf("holder insert: %v", err)
	}
	const holdFor = 300 * time.Millisecond
	go func() {
		time.Sleep(holdFor)
		_ = tx.Commit()
	}()

	start := time.Now()
	_, err = kb.db.Exec(`INSERT INTO seen_players (player_id, username, first_seen_utc, last_seen_utc) VALUES ('p2', 'two', 't', 't')`)
	waited := time.Since(start)
	if err != nil {
		t.Fatalf("second writer failed after %v: %v (want it to wait for the lock)", waited, err)
	}
	if waited < holdFor/2 {
		t.Fatalf("second writer returned after %v; expected to wait ~%v for the held lock", waited, holdFor)
	}
}
