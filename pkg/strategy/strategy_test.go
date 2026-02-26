package strategy

import (
	"sort"
	"testing"
)

func TestNewRegistry(t *testing.T) {
	r := NewRegistry()
	if r == nil {
		t.Fatal("NewRegistry returned nil")
	}
	if len(r.strategies) != 0 {
		t.Fatalf("new registry should be empty, got %d strategies", len(r.strategies))
	}
}

func TestRegistryRegisterAndGet(t *testing.T) {
	r := NewRegistry()

	s := &MiningStrategy{}
	r.Register(s)

	got := r.Get("mining")
	if got == nil {
		t.Fatal("Get returned nil for registered strategy")
	}
	if got.Name() != "mining" {
		t.Errorf("got Name() = %q, want %q", got.Name(), "mining")
	}
}

func TestRegistryGetNotFound(t *testing.T) {
	r := NewRegistry()
	got := r.Get("nonexistent")
	if got != nil {
		t.Errorf("Get should return nil for unregistered strategy, got %v", got)
	}
}

func TestRegistryRegisterDuplicatePanics(t *testing.T) {
	r := NewRegistry()
	r.Register(&MiningStrategy{})

	defer func() {
		rec := recover()
		if rec == nil {
			t.Fatal("expected panic on duplicate registration")
		}
	}()

	r.Register(&MiningStrategy{})
}

func TestRegistryList(t *testing.T) {
	r := NewRegistry()
	r.Register(&MiningStrategy{})
	r.Register(&ExplorerStrategy{})

	infos := r.List()
	if len(infos) != 2 {
		t.Fatalf("List returned %d infos, want 2", len(infos))
	}

	// Sort for deterministic comparison.
	sort.Slice(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })

	if infos[0].Name != "explorer" {
		t.Errorf("infos[0].Name = %q, want %q", infos[0].Name, "explorer")
	}
	if infos[1].Name != "mining" {
		t.Errorf("infos[1].Name = %q, want %q", infos[1].Name, "mining")
	}

	// Verify descriptions are non-empty.
	for _, info := range infos {
		if info.Description == "" {
			t.Errorf("strategy %q has empty description", info.Name)
		}
	}
}

func TestRegistryListEmpty(t *testing.T) {
	r := NewRegistry()
	infos := r.List()
	if len(infos) != 0 {
		t.Fatalf("List on empty registry returned %d items, want 0", len(infos))
	}
}

func TestDefaultRegistry(t *testing.T) {
	r := DefaultRegistry()
	if r == nil {
		t.Fatal("DefaultRegistry returned nil")
	}

	expected := []string{"mining", "explorer", "fighter", "trader", "idle"}
	for _, name := range expected {
		s := r.Get(name)
		if s == nil {
			t.Errorf("DefaultRegistry missing strategy %q", name)
			continue
		}
		if s.Name() != name {
			t.Errorf("strategy Name() = %q, want %q", s.Name(), name)
		}
		if s.Description() == "" {
			t.Errorf("strategy %q has empty Description()", name)
		}
	}

	infos := r.List()
	if len(infos) != len(expected) {
		t.Errorf("DefaultRegistry has %d strategies, want %d", len(infos), len(expected))
	}
}

func TestAllStrategiesNameAndDescription(t *testing.T) {
	strategies := []Strategy{
		&MiningStrategy{},
		&ExplorerStrategy{},
		&FighterStrategy{},
		&TraderStrategy{},
		&SmartIdleStrategy{},
	}

	names := make(map[string]bool)
	for _, s := range strategies {
		name := s.Name()
		if name == "" {
			t.Error("strategy has empty Name()")
		}
		if names[name] {
			t.Errorf("duplicate strategy name: %q", name)
		}
		names[name] = true

		desc := s.Description()
		if desc == "" {
			t.Errorf("strategy %q has empty Description()", name)
		}
	}
}

func TestAllStrategiesDefaultStatus(t *testing.T) {
	strategies := []Strategy{
		&MiningStrategy{},
		&ExplorerStrategy{},
		&FighterStrategy{},
		&TraderStrategy{},
		&SmartIdleStrategy{},
	}

	for _, s := range strategies {
		status := s.CurrentStatus()
		if status != "idle" {
			t.Errorf("strategy %q default CurrentStatus() = %q, want %q", s.Name(), status, "idle")
		}
	}
}

func TestStrategySetStatusConcurrency(t *testing.T) {
	strategies := []interface {
		Strategy
		setStatus(string)
	}{
		&MiningStrategy{},
		&ExplorerStrategy{},
		&FighterStrategy{},
		&TraderStrategy{},
		&SmartIdleStrategy{},
	}

	for _, s := range strategies {
		t.Run(s.Name(), func(t *testing.T) {
			done := make(chan struct{})
			go func() {
				for range 100 {
					s.setStatus("working")
				}
				close(done)
			}()

			for range 100 {
				_ = s.CurrentStatus()
			}

			<-done
		})
	}
}

func TestMiningStrategySetStatus(t *testing.T) {
	s := &MiningStrategy{}
	if s.CurrentStatus() != "idle" {
		t.Fatalf("initial status = %q, want %q", s.CurrentStatus(), "idle")
	}
	s.setStatus("mining run #1")
	if got := s.CurrentStatus(); got != "mining run #1" {
		t.Errorf("CurrentStatus() = %q, want %q", got, "mining run #1")
	}
}

func TestExplorerStrategySetStatus(t *testing.T) {
	s := &ExplorerStrategy{}
	s.setStatus("exploring system Alpha")
	if got := s.CurrentStatus(); got != "exploring system Alpha" {
		t.Errorf("CurrentStatus() = %q, want %q", got, "exploring system Alpha")
	}
}

func TestFighterStrategySetStatus(t *testing.T) {
	s := &FighterStrategy{}
	s.setStatus("patrolling Sol")
	if got := s.CurrentStatus(); got != "patrolling Sol" {
		t.Errorf("CurrentStatus() = %q, want %q", got, "patrolling Sol")
	}
}

func TestTraderStrategySetStatus(t *testing.T) {
	s := &TraderStrategy{}
	s.setStatus("trading at Sol (credits: 1000)")
	if got := s.CurrentStatus(); got != "trading at Sol (credits: 1000)" {
		t.Errorf("CurrentStatus() = %q, want %q", got, "trading at Sol (credits: 1000)")
	}
}

func TestSmartIdleStrategySetStatus(t *testing.T) {
	s := &SmartIdleStrategy{}
	s.setStatus("idle (LLM-guided)")
	if got := s.CurrentStatus(); got != "idle (LLM-guided)" {
		t.Errorf("CurrentStatus() = %q, want %q", got, "idle (LLM-guided)")
	}
}

func TestSafePercent(t *testing.T) {
	tests := []struct {
		name     string
		value    float64
		max      float64
		expected float64
	}{
		{"normal", 50, 100, 50},
		{"full", 100, 100, 100},
		{"empty", 0, 100, 0},
		{"zero max", 50, 0, 0},
		{"negative max", 50, -10, 0},
		{"over max", 150, 100, 150},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := safePercent(tt.value, tt.max)
			if got != tt.expected {
				t.Errorf("safePercent(%v, %v) = %v, want %v", tt.value, tt.max, got, tt.expected)
			}
		})
	}
}

func TestDockedStr(t *testing.T) {
	if got := dockedStr(true); got != "docked at a station" {
		t.Errorf("dockedStr(true) = %q, want %q", got, "docked at a station")
	}
	if got := dockedStr(false); got != "in open space" {
		t.Errorf("dockedStr(false) = %q, want %q", got, "in open space")
	}
}

func TestInfoStruct(t *testing.T) {
	info := Info{
		Name:        "test",
		Description: "a test strategy",
	}
	if info.Name != "test" || info.Description != "a test strategy" {
		t.Errorf("Info fields not set correctly: %+v", info)
	}
}

func TestConfigStruct(t *testing.T) {
	var statusUpdates []string
	cfg := Config{
		AgentID: "agent-1",
		Parameters: map[string]string{
			"station_action": "craft-sell",
		},
		OnStatusUpdate: func(status string) {
			statusUpdates = append(statusUpdates, status)
		},
	}

	cfg.OnStatusUpdate("testing")
	if len(statusUpdates) != 1 || statusUpdates[0] != "testing" {
		t.Errorf("OnStatusUpdate callback not working: got %v", statusUpdates)
	}
	if cfg.AgentID != "agent-1" {
		t.Errorf("AgentID = %q, want %q", cfg.AgentID, "agent-1")
	}
	if cfg.Parameters["station_action"] != "craft-sell" {
		t.Errorf("Parameters[station_action] = %q, want %q", cfg.Parameters["station_action"], "craft-sell")
	}
}
