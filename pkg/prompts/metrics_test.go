package prompts

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestMetrics_SuccessRate(t *testing.T) {
	tests := []struct {
		name         string
		totalUses    int
		successCount int
		want         float64
	}{
		{"no uses", 0, 0, 0},
		{"all success", 10, 10, 1.0},
		{"half success", 10, 5, 0.5},
		{"no success", 10, 0, 0},
		{"one of three", 3, 1, 1.0 / 3.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Metrics{TotalUses: tt.totalUses, SuccessCount: tt.successCount}
			got := m.SuccessRate()
			if got != tt.want {
				t.Errorf("SuccessRate() = %f, want %f", got, tt.want)
			}
		})
	}
}

func TestMetrics_ErrorRate(t *testing.T) {
	tests := []struct {
		name       string
		totalUses  int
		errorCount int
		want       float64
	}{
		{"no uses", 0, 0, 0},
		{"all errors", 10, 10, 1.0},
		{"no errors", 10, 0, 0},
		{"quarter errors", 8, 2, 0.25},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Metrics{TotalUses: tt.totalUses, ErrorCount: tt.errorCount}
			got := m.ErrorRate()
			if got != tt.want {
				t.Errorf("ErrorRate() = %f, want %f", got, tt.want)
			}
		})
	}
}

func TestMetricsCollector_RecordUse_Disabled(t *testing.T) {
	mc := NewMetricsCollector(t.TempDir(), false)

	mc.RecordUse("decision", 1, "miner", true, 0.9, nil)

	m := mc.GetMetrics("decision", 1)
	if m != nil {
		t.Error("GetMetrics should return nil when collector is disabled")
	}
}

func TestMetricsCollector_RecordUse_Success(t *testing.T) {
	mc := NewMetricsCollector(t.TempDir(), true)

	mc.RecordUse("decision", 1, "miner", true, 0.9, nil)

	m := mc.GetMetrics("decision", 1)
	if m == nil {
		t.Fatal("GetMetrics returned nil")
	}
	if m.TotalUses != 1 {
		t.Errorf("TotalUses = %d, want 1", m.TotalUses)
	}
	if m.SuccessCount != 1 {
		t.Errorf("SuccessCount = %d, want 1", m.SuccessCount)
	}
	if m.ErrorCount != 0 {
		t.Errorf("ErrorCount = %d, want 0", m.ErrorCount)
	}
	if m.AvgConfidence != 0.9 {
		t.Errorf("AvgConfidence = %f, want 0.9", m.AvgConfidence)
	}
	if m.PromptType != "decision" {
		t.Errorf("PromptType = %q, want %q", m.PromptType, "decision")
	}
	if m.Version != 1 {
		t.Errorf("Version = %d, want 1", m.Version)
	}
}

func TestMetricsCollector_RecordUse_Error(t *testing.T) {
	mc := NewMetricsCollector(t.TempDir(), true)

	testErr := errors.New("parsing failed")
	mc.RecordUse("decision", 1, "", false, 0.5, testErr)

	m := mc.GetMetrics("decision", 1)
	if m == nil {
		t.Fatal("GetMetrics returned nil")
	}
	if m.ErrorCount != 1 {
		t.Errorf("ErrorCount = %d, want 1", m.ErrorCount)
	}
	if len(m.Errors) != 1 {
		t.Fatalf("Errors length = %d, want 1", len(m.Errors))
	}
	if m.Errors[0].Error != "parsing failed" {
		t.Errorf("Errors[0].Error = %q, want %q", m.Errors[0].Error, "parsing failed")
	}
}

func TestMetricsCollector_RecordUse_ErrorWithNilErr(t *testing.T) {
	mc := NewMetricsCollector(t.TempDir(), true)

	mc.RecordUse("decision", 1, "", false, 0.5, nil)

	m := mc.GetMetrics("decision", 1)
	if m == nil {
		t.Fatal("GetMetrics returned nil")
	}
	if m.ErrorCount != 1 {
		t.Errorf("ErrorCount = %d, want 1", m.ErrorCount)
	}
	if len(m.Errors) != 0 {
		t.Errorf("Errors length = %d, want 0 when err is nil", len(m.Errors))
	}
}

func TestMetricsCollector_RecordUse_MultipleUses(t *testing.T) {
	mc := NewMetricsCollector(t.TempDir(), true)

	mc.RecordUse("decision", 1, "miner", true, 0.8, nil)
	mc.RecordUse("decision", 1, "miner", true, 1.0, nil)
	mc.RecordUse("decision", 1, "explorer", false, 0.6, nil)

	m := mc.GetMetrics("decision", 1)
	if m == nil {
		t.Fatal("GetMetrics returned nil")
	}
	if m.TotalUses != 3 {
		t.Errorf("TotalUses = %d, want 3", m.TotalUses)
	}
	if m.SuccessCount != 2 {
		t.Errorf("SuccessCount = %d, want 2", m.SuccessCount)
	}
	if m.ErrorCount != 1 {
		t.Errorf("ErrorCount = %d, want 1", m.ErrorCount)
	}
}

func TestMetricsCollector_RecordUse_RoleMetrics(t *testing.T) {
	mc := NewMetricsCollector(t.TempDir(), true)

	mc.RecordUse("decision", 1, "miner", true, 0.8, nil)
	mc.RecordUse("decision", 1, "miner", true, 1.0, nil)
	mc.RecordUse("decision", 1, "explorer", false, 0.6, nil)

	m := mc.GetMetrics("decision", 1)
	if m == nil {
		t.Fatal("GetMetrics returned nil")
	}

	minerMetrics, ok := m.ByRole["miner"]
	if !ok {
		t.Fatal("miner role metrics not found")
	}
	if minerMetrics.Uses != 2 {
		t.Errorf("miner Uses = %d, want 2", minerMetrics.Uses)
	}
	if minerMetrics.SuccessCount != 2 {
		t.Errorf("miner SuccessCount = %d, want 2", minerMetrics.SuccessCount)
	}

	explorerMetrics, ok := m.ByRole["explorer"]
	if !ok {
		t.Fatal("explorer role metrics not found")
	}
	if explorerMetrics.Uses != 1 {
		t.Errorf("explorer Uses = %d, want 1", explorerMetrics.Uses)
	}
	if explorerMetrics.SuccessCount != 0 {
		t.Errorf("explorer SuccessCount = %d, want 0", explorerMetrics.SuccessCount)
	}
}

func TestMetricsCollector_RecordUse_EmptyRole(t *testing.T) {
	mc := NewMetricsCollector(t.TempDir(), true)

	mc.RecordUse("decision", 1, "", true, 0.9, nil)

	m := mc.GetMetrics("decision", 1)
	if m == nil {
		t.Fatal("GetMetrics returned nil")
	}
	if len(m.ByRole) != 0 {
		t.Errorf("ByRole length = %d, want 0 for empty role", len(m.ByRole))
	}
}

func TestMetricsCollector_RecordUse_ZeroConfidence(t *testing.T) {
	mc := NewMetricsCollector(t.TempDir(), true)

	mc.RecordUse("decision", 1, "", true, 0, nil)

	m := mc.GetMetrics("decision", 1)
	if m == nil {
		t.Fatal("GetMetrics returned nil")
	}
	// Zero confidence should not update the average.
	if m.AvgConfidence != 0 {
		t.Errorf("AvgConfidence = %f, want 0 when confidence is 0", m.AvgConfidence)
	}
}

func TestMetricsCollector_GetMetrics_NotFound(t *testing.T) {
	mc := NewMetricsCollector(t.TempDir(), true)

	m := mc.GetMetrics("nonexistent", 1)
	if m != nil {
		t.Error("GetMetrics should return nil for nonexistent metrics")
	}
}

func TestMetricsCollector_GetAllMetrics(t *testing.T) {
	mc := NewMetricsCollector(t.TempDir(), true)

	mc.RecordUse("decision", 1, "", true, 0.9, nil)
	mc.RecordUse("feedback", 1, "", true, 0.8, nil)

	all := mc.GetAllMetrics()
	if len(all) != 2 {
		t.Errorf("GetAllMetrics length = %d, want 2", len(all))
	}

	// Verify it returns a copy.
	all["new_key"] = &Metrics{}
	if len(mc.GetAllMetrics()) != 2 {
		t.Error("GetAllMetrics should return a copy, not a reference")
	}
}

func TestMetricsCollector_GetAllMetrics_Empty(t *testing.T) {
	mc := NewMetricsCollector(t.TempDir(), true)

	all := mc.GetAllMetrics()
	if all == nil {
		t.Error("GetAllMetrics should return non-nil empty map")
	}
	if len(all) != 0 {
		t.Errorf("GetAllMetrics length = %d, want 0", len(all))
	}
}

func TestMetricsCollector_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	mc := NewMetricsCollector(dir, true)

	mc.RecordUse("decision", 1, "miner", true, 0.9, nil)
	mc.RecordUse("decision", 1, "miner", false, 0.7, errors.New("bad parse"))
	mc.RecordUse("feedback", 2, "", true, 0.85, nil)

	if err := mc.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Verify files exist.
	for _, name := range []string{"decision.v1.yaml", "feedback.v2.yaml"} {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("Expected file %s not found", name)
		}
	}

	// Load into a new collector.
	mc2 := NewMetricsCollector(dir, true)
	if err := mc2.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	m := mc2.GetMetrics("decision", 1)
	if m == nil {
		t.Fatal("Loaded decision.v1 metrics is nil")
	}
	if m.TotalUses != 2 {
		t.Errorf("Loaded TotalUses = %d, want 2", m.TotalUses)
	}
	if m.SuccessCount != 1 {
		t.Errorf("Loaded SuccessCount = %d, want 1", m.SuccessCount)
	}
	if m.ErrorCount != 1 {
		t.Errorf("Loaded ErrorCount = %d, want 1", m.ErrorCount)
	}

	m2 := mc2.GetMetrics("feedback", 2)
	if m2 == nil {
		t.Fatal("Loaded feedback.v2 metrics is nil")
	}
	if m2.TotalUses != 1 {
		t.Errorf("Loaded feedback TotalUses = %d, want 1", m2.TotalUses)
	}
}

func TestMetricsCollector_Save_Disabled(t *testing.T) {
	mc := NewMetricsCollector(t.TempDir(), false)
	if err := mc.Save(); err != nil {
		t.Errorf("Save() on disabled collector returned error: %v", err)
	}
}

func TestMetricsCollector_Load_Disabled(t *testing.T) {
	mc := NewMetricsCollector(t.TempDir(), false)
	if err := mc.Load(); err != nil {
		t.Errorf("Load() on disabled collector returned error: %v", err)
	}
}

func TestMetricsCollector_Load_NonexistentDir(t *testing.T) {
	mc := NewMetricsCollector("/nonexistent/path", true)
	if err := mc.Load(); err != nil {
		t.Errorf("Load() on nonexistent dir should return nil, got: %v", err)
	}
}

func TestMetricsCollector_Load_SkipsNonYAML(t *testing.T) {
	dir := t.TempDir()
	// Create a non-YAML file.
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	// Create a subdirectory (should be skipped).
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0755); err != nil {
		t.Fatal(err)
	}

	mc := NewMetricsCollector(dir, true)
	if err := mc.Load(); err != nil {
		t.Errorf("Load() error = %v", err)
	}
	if len(mc.GetAllMetrics()) != 0 {
		t.Error("Should not have loaded any metrics from non-YAML files")
	}
}

func TestMetricsCollector_ConcurrentAccess(t *testing.T) {
	mc := NewMetricsCollector(t.TempDir(), true)

	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mc.RecordUse("decision", 1, "miner", true, 0.8, nil)
		}()
	}
	wg.Wait()

	m := mc.GetMetrics("decision", 1)
	if m == nil {
		t.Fatal("GetMetrics returned nil after concurrent writes")
	}
	if m.TotalUses != 100 {
		t.Errorf("TotalUses = %d, want 100 after 100 concurrent writes", m.TotalUses)
	}
}

func TestMetricsCollector_DifferentVersions(t *testing.T) {
	mc := NewMetricsCollector(t.TempDir(), true)

	mc.RecordUse("decision", 1, "", true, 0.9, nil)
	mc.RecordUse("decision", 2, "", true, 0.95, nil)

	m1 := mc.GetMetrics("decision", 1)
	m2 := mc.GetMetrics("decision", 2)

	if m1 == nil || m2 == nil {
		t.Fatal("Both versions should have metrics")
	}
	if m1.TotalUses != 1 || m2.TotalUses != 1 {
		t.Error("Each version should have 1 use")
	}
}
