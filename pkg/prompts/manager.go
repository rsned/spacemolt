package prompts

import (
	"bytes"
	"fmt"
	"path/filepath"
	"sync"
	"text/template"
)

// Manager handles prompt template loading and rendering
type Manager struct {
	templatesDir string
	templates    map[string]*template.Template
	mu           sync.RWMutex
	funcMap      template.FuncMap
}

// Config holds Manager configuration
type Config struct {
	TemplatesDir string
	ConfigPath   string
}

// NewManager creates a new PromptManager
func NewManager(cfg Config) (*Manager, error) {
	if cfg.TemplatesDir == "" {
		cfg.TemplatesDir = "data/prompts/templates"
	}

	m := &Manager{
		templatesDir: cfg.TemplatesDir,
		templates:    make(map[string]*template.Template),
		funcMap:      buildFuncMap(),
	}

	// Load all templates
	if err := m.LoadTemplates(); err != nil {
		return nil, fmt.Errorf("failed to load templates: %w", err)
	}

	return m, nil
}

// LoadTemplates scans and loads all template files
func (m *Manager) LoadTemplates() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Scan templates directory
	patterns := []string{
		filepath.Join(m.templatesDir, "decision", "*.tmpl"),
		filepath.Join(m.templatesDir, "feedback", "*.tmpl"),
		filepath.Join(m.templatesDir, "analysis", "*.tmpl"),
		filepath.Join(m.templatesDir, "improvement", "*.tmpl"),
		filepath.Join(m.templatesDir, "shared", "*.tmpl"),
	}

	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return fmt.Errorf("failed to glob pattern %s: %w", pattern, err)
		}

		for _, path := range matches {
			if err := m.loadTemplate(path); err != nil {
				// Log warning but continue loading other templates.
				// A broken template should not prevent all templates from loading.
				fmt.Printf("Warning: Skipping broken template %s: %v\n", path, err)
				continue
			}
		}
	}

	return nil
}

// loadTemplate loads a single template file
func (m *Manager) loadTemplate(path string) error {
	// Get template name from path (e.g., "decision.v1")
	name := getTemplateName(path)
	baseName := filepath.Base(path)

	// Create a new template with the base filename as the name
	tmpl := template.New(baseName).Funcs(m.funcMap)

	// First parse shared templates if they exist
	sharedPattern := filepath.Join(m.templatesDir, "shared", "*.tmpl")
	tmpl, err := tmpl.ParseGlob(sharedPattern)
	if err != nil {
		// Shared templates may not exist yet, that's okay
		// Continue with just the main template
		tmpl = template.New(baseName).Funcs(m.funcMap)
	}

	// Parse the main template file
	tmpl, err = tmpl.ParseFiles(path)
	if err != nil {
		return fmt.Errorf("failed to parse template: %w", err)
	}

	m.templates[name] = tmpl

	return nil
}

// RenderPrompt renders a prompt template with the given context
func (m *Manager) RenderPrompt(promptType string, version int, ctx *TemplateContext) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Build template key
	key := fmt.Sprintf("%s.v%d", promptType, version)

	tmpl, ok := m.templates[key]
	if !ok {
		return "", fmt.Errorf("template not found: %s", key)
	}

	// Render template - execute the main template file (e.g., "decision.v1.tmpl")
	templateFileName := fmt.Sprintf("%s.v%d.tmpl", promptType, version)

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, templateFileName, ctx); err != nil {
		return "", fmt.Errorf("failed to render template: %w", err)
	}

	return buf.String(), nil
}

// RenderPromptByName renders a prompt using the template name directly
func (m *Manager) RenderPromptByName(templateName string, ctx *TemplateContext) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	tmpl, ok := m.templates[templateName]
	if !ok {
		return "", fmt.Errorf("template not found: %s", templateName)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, ctx); err != nil {
		return "", fmt.Errorf("failed to render template: %w", err)
	}

	return buf.String(), nil
}

// GetAvailableTemplates returns a list of loaded template names
func (m *Manager) GetAvailableTemplates() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.templates))
	for name := range m.templates {
		names = append(names, name)
	}

	return names
}

// HasTemplate checks if a template is loaded
func (m *Manager) HasTemplate(promptType string, version int) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := fmt.Sprintf("%s.v%d", promptType, version)
	_, ok := m.templates[key]
	return ok
}

// ReloadTemplates reloads all templates from disk
func (m *Manager) ReloadTemplates() error {
	return m.LoadTemplates()
}

// getTemplateName extracts template name from file path
// e.g., "/path/to/decision/decision.v1.tmpl" -> "decision.v1"
func getTemplateName(path string) string {
	base := filepath.Base(path)
	// Remove .tmpl extension
	name := base[:len(base)-len(filepath.Ext(base))]
	return name
}

// buildFuncMap creates template helper functions
func buildFuncMap() template.FuncMap {
	return template.FuncMap{
		"join": func(sep string, items []string) string {
			result := ""
			for i, item := range items {
				if i > 0 {
					result += sep
				}
				result += item
			}
			return result
		},
		"percent": func(current, max float64) float64 {
			if max == 0 {
				return 0
			}
			return (current / max) * 100
		},
		"successIcon": func(success bool) string {
			if success {
				return "✓"
			}
			return "✗"
		},
		"default": func(defaultVal, val interface{}) interface{} {
			if val == nil || val == "" {
				return defaultVal
			}
			return val
		},
	}
}
