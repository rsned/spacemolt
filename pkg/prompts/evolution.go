package prompts

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Evolver handles prompt evolution and improvement
type Evolver struct {
	manager   *Manager
	registry  *Registry
	metrics   *MetricsCollector
	llmClient LLMClient // Interface for LLM interactions
}

// LLMClient defines the interface for LLM operations
type LLMClient interface {
	// Query sends a prompt to the LLM and returns the response
	Query(prompt string) (string, error)
}

// NewEvolver creates a new prompt evolver
func NewEvolver(manager *Manager, registry *Registry, metrics *MetricsCollector, llmClient LLMClient) *Evolver {
	return &Evolver{
		manager:   manager,
		registry:  registry,
		metrics:   metrics,
		llmClient: llmClient,
	}
}

// ImprovementSuggestion represents a suggested improvement to a prompt
type ImprovementSuggestion struct {
	PromptType       string
	CurrentVersion   int
	NewVersion       int
	Rationale        string
	Changes          []string
	ExpectedBenefits []string
	CreatedAt        time.Time
}

// AnalyzePrompt analyzes a prompt's metrics and suggests improvements
func (e *Evolver) AnalyzePrompt(promptType string, version int) (*ImprovementSuggestion, error) {
	// Get metrics for the prompt
	m := e.metrics.GetMetrics(promptType, version)
	if m == nil {
		return nil, fmt.Errorf("no metrics found for %s v%d", promptType, version)
	}

	// Build analysis prompt for LLM
	analysisPrompt := e.buildAnalysisPrompt(promptType, version, m)

	// Query LLM for suggestions
	response, err := e.llmClient.Query(analysisPrompt)
	if err != nil {
		return nil, fmt.Errorf("failed to get LLM suggestions: %w", err)
	}

	// Parse response into suggestion
	// (Simplified for now - in production, would use structured output)
	nextVersion, _ := e.registry.GetLatestVersion(promptType)
	nextVersion++

	suggestion := &ImprovementSuggestion{
		PromptType:     promptType,
		CurrentVersion: version,
		NewVersion:     nextVersion,
		Rationale:      response,
		CreatedAt:      time.Now(),
	}

	return suggestion, nil
}

// CreateDraftVersion creates a draft version of a prompt
func (e *Evolver) CreateDraftVersion(promptType string, baseVersion int, content string) error {
	// Get next version number
	nextVersion, _ := e.registry.GetLatestVersion(promptType)
	nextVersion++

	// Create draft filename
	filename := fmt.Sprintf("%s.v%d-draft.tmpl", promptType, nextVersion)
	dir := filepath.Join(e.manager.templatesDir, promptType)
	path := filepath.Join(dir, filename)

	// Ensure directory exists
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Write draft template
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write draft template: %w", err)
	}

	// Reload templates
	if err := e.manager.LoadTemplates(); err != nil {
		return fmt.Errorf("failed to reload templates: %w", err)
	}

	// Rescan registry
	if err := e.registry.Scan(); err != nil {
		return fmt.Errorf("failed to rescan registry: %w", err)
	}

	return nil
}

// PromoteDraft promotes a draft version to active
func (e *Evolver) PromoteDraft(promptType string, version int) error {
	// Find draft file
	draftFilename := fmt.Sprintf("%s.v%d-draft.tmpl", promptType, version)
	activeFilename := fmt.Sprintf("%s.v%d.tmpl", promptType, version)

	dir := filepath.Join(e.manager.templatesDir, promptType)
	draftPath := filepath.Join(dir, draftFilename)
	activePath := filepath.Join(dir, activeFilename)

	// Check if draft exists
	if _, err := os.Stat(draftPath); os.IsNotExist(err) {
		return fmt.Errorf("draft version not found: %s", draftPath)
	}

	// Check if active version already exists
	if _, err := os.Stat(activePath); err == nil {
		return fmt.Errorf("active version already exists: %s", activePath)
	}

	// Rename draft to active
	if err := os.Rename(draftPath, activePath); err != nil {
		return fmt.Errorf("failed to promote draft: %w", err)
	}

	// Reload templates and registry
	if err := e.manager.LoadTemplates(); err != nil {
		return fmt.Errorf("failed to reload templates: %w", err)
	}

	if err := e.registry.Scan(); err != nil {
		return fmt.Errorf("failed to rescan registry: %w", err)
	}

	return nil
}

// buildAnalysisPrompt creates a prompt for analyzing a template's performance
func (e *Evolver) buildAnalysisPrompt(promptType string, version int, m *Metrics) string {
	return fmt.Sprintf(`
You are an AI prompt engineer analyzing the performance of a prompt template.

PROMPT TYPE: %s
VERSION: %d

PERFORMANCE METRICS:
- Total Uses: %d
- Success Rate: %.2f%%
- Error Rate: %.2f%%
- Average Confidence: %.2f

RECENT ERRORS:
%s

Based on these metrics, suggest specific improvements to the prompt template.
Focus on:
1. Reducing error rate
2. Improving confidence scores
3. Making instructions clearer
4. Adding examples or constraints where needed

Provide concrete, actionable suggestions.
`, promptType, version, m.TotalUses, m.SuccessRate()*100, m.ErrorRate()*100, m.AvgConfidence,
		formatErrors(m.Errors))
}

// formatErrors formats error records for display
func formatErrors(errors []ErrorRecord) string {
	if len(errors) == 0 {
		return "  (No recent errors)"
	}

	result := ""
	// Show last 5 errors
	start := 0
	if len(errors) > 5 {
		start = len(errors) - 5
	}

	for _, err := range errors[start:] {
		result += fmt.Sprintf("  - [%s] %s\n", err.Timestamp.Format("2006-01-02 15:04"), err.Error)
	}

	return result
}
