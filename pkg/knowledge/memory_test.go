package knowledge

import (
	"context"
	"testing"
)

func TestRememberConnection_Deduplicates(t *testing.T) {
	t.Parallel()
	kb := NewMemoryKB()
	ctx := context.Background()

	// Remember the same connection multiple times
	err := kb.RememberConnection(ctx, "A", "B")
	if err != nil {
		t.Fatalf("First RememberConnection failed: %v", err)
	}
	err = kb.RememberConnection(ctx, "A", "B")
	if err != nil {
		t.Fatalf("Second RememberConnection failed: %v", err)
	}
	err = kb.RememberConnection(ctx, "A", "B")
	if err != nil {
		t.Fatalf("Third RememberConnection failed: %v", err)
	}

	kb.mu.Lock()
	conns := kb.connections["A"]
	kb.mu.Unlock()

	if len(conns) != 1 {
		t.Errorf("Expected 1 connection, got %d", len(conns))
	}
	if len(conns) > 0 && conns[0].SystemID != "B" {
		t.Errorf("Expected connection to B, got %s", conns[0].SystemID)
	}
}

func TestRememberConnection_MultipleDistinct(t *testing.T) {
	t.Parallel()
	kb := NewMemoryKB()
	ctx := context.Background()

	// Remember multiple distinct connections from A
	err := kb.RememberConnection(ctx, "A", "B")
	if err != nil {
		t.Fatalf("RememberConnection A->B failed: %v", err)
	}
	err = kb.RememberConnection(ctx, "A", "C")
	if err != nil {
		t.Fatalf("RememberConnection A->C failed: %v", err)
	}
	err = kb.RememberConnection(ctx, "A", "D")
	if err != nil {
		t.Fatalf("RememberConnection A->D failed: %v", err)
	}

	kb.mu.Lock()
	conns := kb.connections["A"]
	kb.mu.Unlock()

	if len(conns) != 3 {
		t.Errorf("Expected 3 connections, got %d", len(conns))
	}
}

func TestRememberConnection_DifferentOrigins(t *testing.T) {
	t.Parallel()
	kb := NewMemoryKB()
	ctx := context.Background()

	// Remember connections from different origins
	err := kb.RememberConnection(ctx, "A", "B")
	if err != nil {
		t.Fatalf("RememberConnection A->B failed: %v", err)
	}
	err = kb.RememberConnection(ctx, "C", "B")
	if err != nil {
		t.Fatalf("RememberConnection C->B failed: %v", err)
	}

	kb.mu.Lock()
	connsA := kb.connections["A"]
	connsC := kb.connections["C"]
	kb.mu.Unlock()

	if len(connsA) != 1 {
		t.Errorf("Expected 1 connection from A, got %d", len(connsA))
	}
	if len(connsC) != 1 {
		t.Errorf("Expected 1 connection from C, got %d", len(connsC))
	}
}
