package prompts

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// PromptStatus represents the status of a template version
type PromptStatus string

const (
	StatusActive     PromptStatus = "active"
	StatusDraft      PromptStatus = "draft"
	StatusDeprecated PromptStatus = "deprecated"
)

// TemplateVersion represents a specific version of a prompt template
type TemplateVersion struct {
	Name    string // e.g., "decision"
	Version int    // e.g., 1, 2, 3
	Status  PromptStatus
	Path    string
	IsDraft bool // Derived from filename (e.g., decision.v1-draft.tmpl)
}

// Registry tracks available prompt templates and their versions
type Registry struct {
	templatesDir string
	versions     map[string][]TemplateVersion // promptType -> versions
}

// NewRegistry creates a new prompt registry
func NewRegistry(templatesDir string) (*Registry, error) {
	r := &Registry{
		templatesDir: templatesDir,
		versions:     make(map[string][]TemplateVersion),
	}

	if err := r.Scan(); err != nil {
		return nil, fmt.Errorf("failed to scan templates: %w", err)
	}

	return r, nil
}

// Scan scans the templates directory for all template files
func (r *Registry) Scan() error {
	// Scan each subdirectory
	subdirs := []string{"decision", "feedback", "analysis", "improvement"}

	for _, subdir := range subdirs {
		dir := filepath.Join(r.templatesDir, subdir)
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			continue // Skip if directory doesn't exist yet
		}

		files, err := os.ReadDir(dir)
		if err != nil {
			return fmt.Errorf("failed to read directory %s: %w", dir, err)
		}

		for _, file := range files {
			if file.IsDir() || !strings.HasSuffix(file.Name(), ".tmpl") {
				continue
			}

			// Parse template name and version
			tv, err := parseTemplateFile(file.Name(), subdir)
			if err != nil {
				// Log error but continue
				fmt.Printf("Warning: Failed to parse template file %s: %v\n", file.Name(), err)
				continue
			}

			tv.Path = filepath.Join(dir, file.Name())

			// Add to registry
			r.versions[tv.Name] = append(r.versions[tv.Name], tv)
		}
	}

	return nil
}

// GetVersions returns all versions for a given prompt type
func (r *Registry) GetVersions(promptType string) []TemplateVersion {
	return r.versions[promptType]
}

// GetLatestVersion returns the highest version number (excluding drafts)
func (r *Registry) GetLatestVersion(promptType string) (int, bool) {
	versions := r.versions[promptType]
	if len(versions) == 0 {
		return 0, false
	}

	maxVer := 0
	found := false
	for _, v := range versions {
		if !v.IsDraft && v.Version > maxVer {
			maxVer = v.Version
			found = true
		}
	}

	return maxVer, found
}

// HasVersion checks if a specific version exists
func (r *Registry) HasVersion(promptType string, version int) bool {
	versions := r.versions[promptType]
	for _, v := range versions {
		if v.Version == version && !v.IsDraft {
			return true
		}
	}
	return false
}

// GetAllPromptTypes returns all registered prompt types
func (r *Registry) GetAllPromptTypes() []string {
	types := make([]string, 0, len(r.versions))
	for t := range r.versions {
		types = append(types, t)
	}
	return types
}

// parseTemplateFile extracts version info from filename
// Examples:
//   - "decision.v1.tmpl" -> {Name: "decision", Version: 1, IsDraft: false}
//   - "decision.v2-draft.tmpl" -> {Name: "decision", Version: 2, IsDraft: true}
func parseTemplateFile(filename, promptType string) (TemplateVersion, error) {
	// Remove .tmpl extension
	name := strings.TrimSuffix(filename, ".tmpl")

	// Pattern: {name}.v{number}[-draft]
	re := regexp.MustCompile(`^(.+)\.v(\d+)(-draft)?$`)
	matches := re.FindStringSubmatch(name)

	if matches == nil {
		return TemplateVersion{}, fmt.Errorf("invalid template filename format: %s", filename)
	}

	templateName := matches[1]
	versionStr := matches[2]
	isDraft := matches[3] == "-draft"

	version, err := strconv.Atoi(versionStr)
	if err != nil {
		return TemplateVersion{}, fmt.Errorf("invalid version number: %s", versionStr)
	}

	status := StatusActive
	if isDraft {
		status = StatusDraft
	}

	return TemplateVersion{
		Name:    templateName,
		Version: version,
		Status:  status,
		IsDraft: isDraft,
	}, nil
}
