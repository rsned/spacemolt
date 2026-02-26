package prompts

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseTemplateFile_Valid(t *testing.T) {
	tests := []struct {
		filename   string
		promptType string
		wantName   string
		wantVer    int
		wantDraft  bool
		wantStatus PromptStatus
	}{
		{
			filename:   "decision.v1.tmpl",
			promptType: "decision",
			wantName:   "decision",
			wantVer:    1,
			wantDraft:  false,
			wantStatus: StatusActive,
		},
		{
			filename:   "decision.v2-draft.tmpl",
			promptType: "decision",
			wantName:   "decision",
			wantVer:    2,
			wantDraft:  true,
			wantStatus: StatusDraft,
		},
		{
			filename:   "feedback.v10.tmpl",
			promptType: "feedback",
			wantName:   "feedback",
			wantVer:    10,
			wantDraft:  false,
			wantStatus: StatusActive,
		},
		{
			filename:   "my-template.v3-draft.tmpl",
			promptType: "decision",
			wantName:   "my-template",
			wantVer:    3,
			wantDraft:  true,
			wantStatus: StatusDraft,
		},
		{
			filename:   "complex.name.v5.tmpl",
			promptType: "decision",
			wantName:   "complex.name",
			wantVer:    5,
			wantDraft:  false,
			wantStatus: StatusActive,
		},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			tv, err := parseTemplateFile(tt.filename, tt.promptType)
			if err != nil {
				t.Fatalf("parseTemplateFile(%q) error = %v", tt.filename, err)
			}
			if tv.Name != tt.wantName {
				t.Errorf("Name = %q, want %q", tv.Name, tt.wantName)
			}
			if tv.Version != tt.wantVer {
				t.Errorf("Version = %d, want %d", tv.Version, tt.wantVer)
			}
			if tv.IsDraft != tt.wantDraft {
				t.Errorf("IsDraft = %v, want %v", tv.IsDraft, tt.wantDraft)
			}
			if tv.Status != tt.wantStatus {
				t.Errorf("Status = %q, want %q", tv.Status, tt.wantStatus)
			}
		})
	}
}

func TestParseTemplateFile_Invalid(t *testing.T) {
	tests := []struct {
		name     string
		filename string
	}{
		{"no version", "decision.tmpl"},
		{"no v prefix", "decision.1.tmpl"},
		{"empty name", ".v1.tmpl"},
		{"missing version number", "decision.v.tmpl"},
		{"letters in version", "decision.vabc.tmpl"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseTemplateFile(tt.filename, "decision")
			if err == nil {
				t.Errorf("parseTemplateFile(%q) expected error, got nil", tt.filename)
			}
		})
	}
}

func setupTestRegistry(t *testing.T) (string, *Registry) {
	t.Helper()
	dir := t.TempDir()

	// Create subdirectories.
	for _, subdir := range []string{"decision", "feedback", "analysis"} {
		if err := os.MkdirAll(filepath.Join(dir, subdir), 0755); err != nil {
			t.Fatal(err)
		}
	}

	// Create template files.
	files := map[string]string{
		"decision/decision.v1.tmpl":       "{{.AgentName}} v1",
		"decision/decision.v2.tmpl":       "{{.AgentName}} v2",
		"decision/decision.v3-draft.tmpl": "{{.AgentName}} v3 draft",
		"feedback/feedback.v1.tmpl":       "feedback v1",
	}
	for path, content := range files {
		fullPath := filepath.Join(dir, path)
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	r := &Registry{
		templatesDir: dir,
		versions:     make(map[string][]TemplateVersion),
	}
	if err := r.Scan(); err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	return dir, r
}

func TestRegistry_ScanVersions(t *testing.T) {
	_, r := setupTestRegistry(t)

	versions := r.GetVersions("decision")
	if len(versions) != 3 {
		t.Errorf("decision versions = %d, want 3", len(versions))
	}

	fbVersions := r.GetVersions("feedback")
	if len(fbVersions) != 1 {
		t.Errorf("feedback versions = %d, want 1", len(fbVersions))
	}
}

func TestRegistry_Scan_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	r := &Registry{
		templatesDir: dir,
		versions:     make(map[string][]TemplateVersion),
	}

	if err := r.Scan(); err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	types := r.GetAllPromptTypes()
	if len(types) != 0 {
		t.Errorf("GetAllPromptTypes() = %v, want empty", types)
	}
}

func TestRegistry_Scan_NonexistentSubdirs(t *testing.T) {
	dir := t.TempDir()
	// Don't create any subdirectories.
	r := &Registry{
		templatesDir: dir,
		versions:     make(map[string][]TemplateVersion),
	}

	if err := r.Scan(); err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
}

func TestRegistry_GetLatestVersion(t *testing.T) {
	_, r := setupTestRegistry(t)

	latest, ok := r.GetLatestVersion("decision")
	if !ok {
		t.Fatal("GetLatestVersion(decision) returned false")
	}
	// v3 is draft, so latest active should be v2.
	if latest != 2 {
		t.Errorf("GetLatestVersion(decision) = %d, want 2", latest)
	}
}

func TestRegistry_GetLatestVersion_NotFound(t *testing.T) {
	_, r := setupTestRegistry(t)

	_, ok := r.GetLatestVersion("nonexistent")
	if ok {
		t.Error("GetLatestVersion(nonexistent) should return false")
	}
}

func TestRegistry_GetLatestVersion_OnlyDrafts(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "decision"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "decision", "decision.v1-draft.tmpl"), []byte("draft"), 0644); err != nil {
		t.Fatal(err)
	}

	r := &Registry{
		templatesDir: dir,
		versions:     make(map[string][]TemplateVersion),
	}
	if err := r.Scan(); err != nil {
		t.Fatal(err)
	}

	_, ok := r.GetLatestVersion("decision")
	if ok {
		t.Error("GetLatestVersion should return false when only drafts exist")
	}
}

func TestRegistry_HasVersion(t *testing.T) {
	_, r := setupTestRegistry(t)

	if !r.HasVersion("decision", 1) {
		t.Error("HasVersion(decision, 1) = false, want true")
	}
	if !r.HasVersion("decision", 2) {
		t.Error("HasVersion(decision, 2) = false, want true")
	}
	// v3 is a draft, should not be found by HasVersion.
	if r.HasVersion("decision", 3) {
		t.Error("HasVersion(decision, 3) = true, want false for draft")
	}
	if r.HasVersion("decision", 99) {
		t.Error("HasVersion(decision, 99) = true, want false")
	}
	if r.HasVersion("nonexistent", 1) {
		t.Error("HasVersion(nonexistent, 1) = true, want false")
	}
}

func TestRegistry_GetAllPromptTypes(t *testing.T) {
	_, r := setupTestRegistry(t)

	types := r.GetAllPromptTypes()
	typeSet := make(map[string]bool)
	for _, tp := range types {
		typeSet[tp] = true
	}

	if !typeSet["decision"] {
		t.Error("GetAllPromptTypes missing 'decision'")
	}
	if !typeSet["feedback"] {
		t.Error("GetAllPromptTypes missing 'feedback'")
	}
}

func TestRegistry_GetVersions_NotFound(t *testing.T) {
	_, r := setupTestRegistry(t)

	versions := r.GetVersions("nonexistent")
	if versions != nil {
		t.Errorf("GetVersions(nonexistent) = %v, want nil", versions)
	}
}

func TestNewRegistry_WithTemplates(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "decision"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "decision", "decision.v1.tmpl"), []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	r, err := NewRegistry(dir)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	if !r.HasVersion("decision", 1) {
		t.Error("Registry should contain decision v1 after creation")
	}
}

func TestRegistry_Scan_SkipsNonTmplFiles(t *testing.T) {
	dir := t.TempDir()
	decisionDir := filepath.Join(dir, "decision")
	if err := os.MkdirAll(decisionDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create non-tmpl files.
	for _, name := range []string{"readme.md", "notes.txt", "data.yaml"} {
		if err := os.WriteFile(filepath.Join(decisionDir, name), []byte("content"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	// Create one valid template.
	if err := os.WriteFile(filepath.Join(decisionDir, "decision.v1.tmpl"), []byte("ok"), 0644); err != nil {
		t.Fatal(err)
	}

	r := &Registry{
		templatesDir: dir,
		versions:     make(map[string][]TemplateVersion),
	}
	if err := r.Scan(); err != nil {
		t.Fatal(err)
	}

	versions := r.GetVersions("decision")
	if len(versions) != 1 {
		t.Errorf("Should have exactly 1 version, got %d", len(versions))
	}
}
