package prompts

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"text/template"
	"time"
)

// mockLLMClient implements LLMClient for testing.
type mockLLMClient struct {
	response string
	err      error
}

func (m *mockLLMClient) Query(prompt string) (string, error) {
	return m.response, m.err
}

func TestFormatErrors_Empty(t *testing.T) {
	result := formatErrors(nil)
	if result != "  (No recent errors)" {
		t.Errorf("formatErrors(nil) = %q, want %q", result, "  (No recent errors)")
	}

	result = formatErrors([]ErrorRecord{})
	if result != "  (No recent errors)" {
		t.Errorf("formatErrors([]) = %q, want %q", result, "  (No recent errors)")
	}
}

func TestFormatErrors_FewErrors(t *testing.T) {
	errs := []ErrorRecord{
		{
			Timestamp: time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
			Error:     "parse error",
		},
		{
			Timestamp: time.Date(2025, 1, 15, 11, 0, 0, 0, time.UTC),
			Error:     "timeout",
		},
	}

	result := formatErrors(errs)
	if result == "  (No recent errors)" {
		t.Error("formatErrors should not return 'no errors' when errors exist")
	}
	if !contains(result, "parse error") {
		t.Error("formatErrors should contain 'parse error'")
	}
	if !contains(result, "timeout") {
		t.Error("formatErrors should contain 'timeout'")
	}
}

func TestFormatErrors_MoreThanFive(t *testing.T) {
	errs := make([]ErrorRecord, 8)
	for i := range errs {
		errs[i] = ErrorRecord{
			Timestamp: time.Date(2025, 1, 15, i, 0, 0, 0, time.UTC),
			Error:     "error",
		}
	}
	errs[0].Error = "oldest_error"
	errs[2].Error = "should_be_skipped"
	errs[7].Error = "newest_error"

	result := formatErrors(errs)

	// Should only contain last 5 errors (indices 3-7).
	if contains(result, "oldest_error") {
		t.Error("formatErrors should not contain oldest error when >5 errors")
	}
	if contains(result, "should_be_skipped") {
		t.Error("formatErrors should not contain error at index 2 when >5 errors")
	}
	if !contains(result, "newest_error") {
		t.Error("formatErrors should contain newest error")
	}
}

func TestNewEvolver(t *testing.T) {
	m := &Manager{templatesDir: "/tmp/test"}
	r := &Registry{versions: make(map[string][]TemplateVersion)}
	mc := NewMetricsCollector("/tmp/metrics", true)
	llm := &mockLLMClient{}

	e := NewEvolver(m, r, mc, llm)
	if e == nil {
		t.Fatal("NewEvolver() returned nil")
	}
	if e.manager != m {
		t.Error("manager not set")
	}
	if e.registry != r {
		t.Error("registry not set")
	}
	if e.metrics != mc {
		t.Error("metrics not set")
	}
	if e.llmClient != llm {
		t.Error("llmClient not set")
	}
}

func TestEvolver_AnalyzePrompt_NoMetrics(t *testing.T) {
	mc := NewMetricsCollector(t.TempDir(), true)
	r := &Registry{versions: make(map[string][]TemplateVersion)}
	e := NewEvolver(nil, r, mc, &mockLLMClient{})

	_, err := e.AnalyzePrompt("decision", 1)
	if err == nil {
		t.Error("AnalyzePrompt() expected error when no metrics exist")
	}
}

func TestEvolver_AnalyzePrompt_LLMError(t *testing.T) {
	mc := NewMetricsCollector(t.TempDir(), true)
	mc.RecordUse("decision", 1, "", true, 0.9, nil)

	r := &Registry{versions: make(map[string][]TemplateVersion)}
	llm := &mockLLMClient{err: errors.New("LLM unavailable")}
	e := NewEvolver(nil, r, mc, llm)

	_, err := e.AnalyzePrompt("decision", 1)
	if err == nil {
		t.Error("AnalyzePrompt() expected error when LLM fails")
	}
}

func TestEvolver_AnalyzePrompt_Success(t *testing.T) {
	mc := NewMetricsCollector(t.TempDir(), true)
	mc.RecordUse("decision", 1, "", true, 0.9, nil)

	r := &Registry{
		versions: map[string][]TemplateVersion{
			"decision": {
				{Name: "decision", Version: 1, Status: StatusActive},
			},
		},
	}
	llm := &mockLLMClient{response: "Improve clarity of instructions"}
	e := NewEvolver(nil, r, mc, llm)

	suggestion, err := e.AnalyzePrompt("decision", 1)
	if err != nil {
		t.Fatalf("AnalyzePrompt() error = %v", err)
	}
	if suggestion == nil {
		t.Fatal("AnalyzePrompt() returned nil suggestion")
	}
	if suggestion.PromptType != "decision" {
		t.Errorf("PromptType = %q, want %q", suggestion.PromptType, "decision")
	}
	if suggestion.CurrentVersion != 1 {
		t.Errorf("CurrentVersion = %d, want 1", suggestion.CurrentVersion)
	}
	if suggestion.NewVersion != 2 {
		t.Errorf("NewVersion = %d, want 2", suggestion.NewVersion)
	}
	if suggestion.Rationale != "Improve clarity of instructions" {
		t.Errorf("Rationale = %q, want %q", suggestion.Rationale, "Improve clarity of instructions")
	}
}

func TestEvolver_BuildAnalysisPrompt(t *testing.T) {
	mc := NewMetricsCollector(t.TempDir(), true)
	e := NewEvolver(nil, nil, mc, nil)

	m := &Metrics{
		TotalUses:     100,
		SuccessCount:  80,
		ErrorCount:    20,
		AvgConfidence: 0.75,
	}

	result := e.buildAnalysisPrompt("decision", 1, m)

	if !contains(result, "decision") {
		t.Error("analysis prompt should contain prompt type")
	}
	if !contains(result, "100") {
		t.Error("analysis prompt should contain total uses")
	}
	if !contains(result, "80.00%") {
		t.Error("analysis prompt should contain success rate")
	}
}

func TestEvolver_CreateDraftVersion(t *testing.T) {
	dir := t.TempDir()

	// Create the templates directory structure.
	decisionDir := filepath.Join(dir, "decision")
	if err := os.MkdirAll(decisionDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(decisionDir, "decision.v1.tmpl"), []byte("v1"), 0644); err != nil {
		t.Fatal(err)
	}

	mgr := &Manager{
		templatesDir: dir,
		templates:    make(map[string]*template.Template),
		funcMap:      buildFuncMap(),
	}
	r := &Registry{
		templatesDir: dir,
		versions: map[string][]TemplateVersion{
			"decision": {
				{Name: "decision", Version: 1, Status: StatusActive},
			},
		},
	}

	e := NewEvolver(mgr, r, nil, nil)

	err := e.CreateDraftVersion("decision", 1, "improved template content")
	if err != nil {
		t.Fatalf("CreateDraftVersion() error = %v", err)
	}

	// Check that draft file was created.
	draftPath := filepath.Join(decisionDir, "decision.v2-draft.tmpl")
	data, err := os.ReadFile(draftPath)
	if err != nil {
		t.Fatalf("Draft file not created: %v", err)
	}
	if string(data) != "improved template content" {
		t.Errorf("Draft content = %q, want %q", string(data), "improved template content")
	}
}

func TestEvolver_PromoteDraft(t *testing.T) {
	dir := t.TempDir()

	decisionDir := filepath.Join(dir, "decision")
	if err := os.MkdirAll(decisionDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Create draft file.
	draftPath := filepath.Join(decisionDir, "decision.v2-draft.tmpl")
	if err := os.WriteFile(draftPath, []byte("draft content"), 0644); err != nil {
		t.Fatal(err)
	}

	mgr := &Manager{
		templatesDir: dir,
		templates:    make(map[string]*template.Template),
		funcMap:      buildFuncMap(),
	}
	r := &Registry{
		templatesDir: dir,
		versions:     make(map[string][]TemplateVersion),
	}

	e := NewEvolver(mgr, r, nil, nil)

	err := e.PromoteDraft("decision", 2)
	if err != nil {
		t.Fatalf("PromoteDraft() error = %v", err)
	}

	// Draft file should be renamed to active.
	activePath := filepath.Join(decisionDir, "decision.v2.tmpl")
	if _, err := os.Stat(activePath); os.IsNotExist(err) {
		t.Error("Active file not found after promotion")
	}
	if _, err := os.Stat(draftPath); !os.IsNotExist(err) {
		t.Error("Draft file should not exist after promotion")
	}
}

func TestEvolver_PromoteDraft_NoDraft(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "decision"), 0755); err != nil {
		t.Fatal(err)
	}

	mgr := &Manager{templatesDir: dir}
	r := &Registry{templatesDir: dir, versions: make(map[string][]TemplateVersion)}
	e := NewEvolver(mgr, r, nil, nil)

	err := e.PromoteDraft("decision", 2)
	if err == nil {
		t.Error("PromoteDraft() expected error when draft does not exist")
	}
}

func TestEvolver_PromoteDraft_ActiveAlreadyExists(t *testing.T) {
	dir := t.TempDir()
	decisionDir := filepath.Join(dir, "decision")
	if err := os.MkdirAll(decisionDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Create both draft and active files.
	if err := os.WriteFile(filepath.Join(decisionDir, "decision.v2-draft.tmpl"), []byte("draft"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(decisionDir, "decision.v2.tmpl"), []byte("active"), 0644); err != nil {
		t.Fatal(err)
	}

	mgr := &Manager{templatesDir: dir}
	r := &Registry{templatesDir: dir, versions: make(map[string][]TemplateVersion)}
	e := NewEvolver(mgr, r, nil, nil)

	err := e.PromoteDraft("decision", 2)
	if err == nil {
		t.Error("PromoteDraft() expected error when active version already exists")
	}
}
