package strategy

import (
	"testing"
)

func TestUnifiedRegistryResolveGoStrategy(t *testing.T) {
	ur := NewUnifiedRegistry(nil)
	ur.RegisterGoStrategy("explore", func() Strategy {
		return &mockStrategy{name: "explore"}
	})

	s, err := ur.Resolve("explore")
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if s.Name() != "explore" {
		t.Errorf("Name() = %q, want %q", s.Name(), "explore")
	}
}

func TestUnifiedRegistryResolveNotFound(t *testing.T) {
	ur := NewUnifiedRegistry(nil)
	_, err := ur.Resolve("nonexistent")
	if err == nil {
		t.Error("expected error for unknown strategy")
	}
}

func TestUnifiedRegistryHas(t *testing.T) {
	ur := NewUnifiedRegistry(nil)
	ur.RegisterGoStrategy("fight", func() Strategy {
		return &mockStrategy{name: "fight"}
	})

	if !ur.Has("fight") {
		t.Error("expected Has(fight) = true")
	}
	if ur.Has("nonexistent") {
		t.Error("expected Has(nonexistent) = false")
	}
}

func TestUnifiedRegistryNames(t *testing.T) {
	ur := NewUnifiedRegistry(nil)
	ur.RegisterGoStrategy("alpha", func() Strategy {
		return &mockStrategy{name: "alpha"}
	})
	ur.RegisterGoStrategy("beta", func() Strategy {
		return &mockStrategy{name: "beta"}
	})

	names := ur.Names()
	if len(names) < 2 {
		t.Fatalf("expected at least 2 names, got %d", len(names))
	}
}
