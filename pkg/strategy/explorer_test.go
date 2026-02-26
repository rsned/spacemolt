package strategy

import (
	"context"
	"log"
	"os"
	"testing"
)

func TestExplorerStrategyRequiresKnowledgeBase(t *testing.T) {
	s := &ExplorerStrategy{}

	cfg := Config{
		AgentID:       "test-explorer",
		Logger:        log.New(os.Stderr, "", 0),
		KnowledgeBase: nil, // no KB
	}

	// Run should return an error when no knowledge base is provided.
	// We pass a nil client since the error should happen before client is used.
	err := s.Run(context.Background(), nil, cfg)
	if err == nil {
		t.Fatal("expected error when KnowledgeBase is nil")
	}
	if err.Error() != "explorer strategy requires a knowledge base" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestExplorerStrategyName(t *testing.T) {
	s := &ExplorerStrategy{}
	if got := s.Name(); got != "explorer" {
		t.Errorf("Name() = %q, want %q", got, "explorer")
	}
}

func TestExplorerStrategyDescription(t *testing.T) {
	s := &ExplorerStrategy{}
	desc := s.Description()
	if desc == "" {
		t.Error("Description() is empty")
	}
}

func TestExplorerStatusTransitions(t *testing.T) {
	s := &ExplorerStrategy{}

	// Default status.
	if got := s.CurrentStatus(); got != "idle" {
		t.Errorf("initial status = %q, want %q", got, "idle")
	}

	// Set various statuses.
	transitions := []string{
		"exploring",
		"exploring system Alpha",
		"emergency: low hull",
		"emergency: low fuel",
		"stopped",
	}

	for _, status := range transitions {
		s.setStatus(status)
		if got := s.CurrentStatus(); got != status {
			t.Errorf("after setStatus(%q), CurrentStatus() = %q", status, got)
		}
	}
}
