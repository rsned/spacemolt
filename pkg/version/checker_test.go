package version

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Note: os, path/filepath still used by TestExtractVersionFromFile and TestGetDocVersionNoFile

func TestParseSemVer(t *testing.T) {
	tests := []struct {
		input   string
		want    SemVer
		wantErr bool
	}{
		{"v0.43.6", SemVer{0, 43, 6}, false},
		{"0.43.6", SemVer{0, 43, 6}, false},
		{"v1.2.3", SemVer{1, 2, 3}, false},
		{"v0.112.0", SemVer{0, 112, 0}, false},
		{"v10.20.30", SemVer{10, 20, 30}, false},
		{"v0.0.0", SemVer{0, 0, 0}, false},
		{"v1.2.3-beta", SemVer{1, 2, 3}, false}, // prefix match
		{"", SemVer{}, true},
		{"invalid", SemVer{}, true},
		{"v1", SemVer{}, true},
		{"v1.2", SemVer{}, true},
		{"vx.y.z", SemVer{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseSemVer(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseSemVer(%q): error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ParseSemVer(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestSemVerString(t *testing.T) {
	tests := []struct {
		ver  SemVer
		want string
	}{
		{SemVer{0, 43, 6}, "v0.43.6"},
		{SemVer{1, 0, 0}, "v1.0.0"},
		{SemVer{0, 0, 0}, "v0.0.0"},
		{SemVer{10, 20, 30}, "v10.20.30"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.ver.String()
			if got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSemVerCompare(t *testing.T) {
	tests := []struct {
		name string
		a, b SemVer
		want int // positive, negative, or 0
	}{
		{"equal", SemVer{1, 2, 3}, SemVer{1, 2, 3}, 0},
		{"major greater", SemVer{2, 0, 0}, SemVer{1, 0, 0}, 1},
		{"major less", SemVer{1, 0, 0}, SemVer{2, 0, 0}, -1},
		{"minor greater", SemVer{1, 3, 0}, SemVer{1, 2, 0}, 1},
		{"minor less", SemVer{1, 2, 0}, SemVer{1, 3, 0}, -1},
		{"patch greater", SemVer{1, 2, 4}, SemVer{1, 2, 3}, 1},
		{"patch less", SemVer{1, 2, 3}, SemVer{1, 2, 4}, -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.a.Compare(tt.b)
			if tt.want == 0 && got != 0 {
				t.Errorf("Compare: got %d, want 0", got)
			} else if tt.want > 0 && got <= 0 {
				t.Errorf("Compare: got %d, want positive", got)
			} else if tt.want < 0 && got >= 0 {
				t.Errorf("Compare: got %d, want negative", got)
			}
		})
	}
}

func TestSemVerMinorDiff(t *testing.T) {
	tests := []struct {
		name string
		a, b SemVer
		want int
	}{
		{"same major, higher minor", SemVer{0, 50, 0}, SemVer{0, 43, 0}, 7},
		{"same major, lower minor", SemVer{0, 43, 0}, SemVer{0, 50, 0}, -7},
		{"same version", SemVer{0, 43, 6}, SemVer{0, 43, 6}, 0},
		{"different major", SemVer{1, 5, 0}, SemVer{2, 3, 0}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.a.MinorDiff(tt.b)
			if got != tt.want {
				t.Errorf("MinorDiff: got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestVersionCheckCheck(t *testing.T) {
	tests := []struct {
		name string
		vc   VersionCheck
		want bool
	}{
		{
			name: "compatible",
			vc:   VersionCheck{MinorDiff: 0},
			want: true,
		},
		{
			name: "major mismatch",
			vc:   VersionCheck{MajorMismatch: true},
			want: false,
		},
		{
			name: "large minor diff still compatible",
			vc:   VersionCheck{MinorDiff: 15},
			want: true,
		},
		{
			name: "small minor diff",
			vc:   VersionCheck{MinorDiff: 5},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.vc.Check()
			if got != tt.want {
				t.Errorf("Check() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestVersionCheckBanner(t *testing.T) {
	t.Run("no warning", func(t *testing.T) {
		vc := VersionCheck{}
		banner := vc.Banner()
		if banner != "" {
			t.Errorf("expected empty banner, got %q", banner)
		}
	})

	t.Run("with warning", func(t *testing.T) {
		vc := VersionCheck{
			WarningMessage: "Test warning message",
		}
		banner := vc.Banner()
		if banner == "" {
			t.Fatal("expected non-empty banner")
		}
		if !strings.Contains(banner, "VERSION MISMATCH") {
			t.Error("banner should contain VERSION MISMATCH")
		}
		if !strings.Contains(banner, "Test warning message") {
			t.Error("banner should contain the warning message")
		}
	})
}

func TestExtractVersionFromFile(t *testing.T) {
	t.Run("valid file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "api.md")
		content := `# Spacemolt API Documentation

This document is accurate for gameserver v0.112.0

## Overview
`
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}

		ver, err := extractVersionFromFile(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := SemVer{0, 112, 0}
		if ver != expected {
			t.Errorf("got %v, want %v", ver, expected)
		}
	})

	t.Run("no version in file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "api.md")
		content := `# Just a document
No version info here.
`
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}

		_, err := extractVersionFromFile(path)
		if err == nil {
			t.Error("expected error for file without version")
		}
	})

	t.Run("file not found", func(t *testing.T) {
		_, err := extractVersionFromFile("/nonexistent/path/api.md")
		if err == nil {
			t.Error("expected error for nonexistent file")
		}
	})

	t.Run("version on later line", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "api.md")
		content := `# API Docs
## Overview
Some text here
More text
This document is accurate for gameserver v1.5.3
`
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}

		ver, err := extractVersionFromFile(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := SemVer{1, 5, 3}
		if ver != expected {
			t.Errorf("got %v, want %v", ver, expected)
		}
	})

	t.Run("version after line 10", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "api.md")
		var lines []string
		for range 15 {
			lines = append(lines, "filler line")
		}
		lines = append(lines, "This document is accurate for gameserver v2.0.0")
		content := strings.Join(lines, "\n")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}

		_, err := extractVersionFromFile(path)
		if err == nil {
			t.Error("expected error when version is past line 10")
		}
	})
}

func TestCheckVersionMajorMismatch(t *testing.T) {
	// CheckVersion uses BuiltForAPIVersion constant (v0.240.0),
	// so a v1.x server would be a major mismatch.
	vc, err := CheckVersion("v1.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !vc.MajorMismatch {
		t.Error("expected major mismatch")
	}
	if vc.ErrorMessage == "" {
		t.Error("expected error message for major mismatch")
	}
	if vc.Check() {
		t.Error("Check() should return false for major mismatch")
	}
}

func TestCheckVersionMinorBehind(t *testing.T) {
	// Server is significantly ahead of BuiltForAPIVersion.
	builtVer, _ := ParseSemVer(BuiltForAPIVersion)
	aheadVer := SemVer{Major: builtVer.Major, Minor: builtVer.Minor + 20, Patch: 0}

	vc, err := CheckVersion(aheadVer.String())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vc.MajorMismatch {
		t.Error("should not have major mismatch")
	}
	if vc.MinorDiff != 20 {
		t.Errorf("MinorDiff: got %d, want 20", vc.MinorDiff)
	}
	if vc.WarningMessage == "" {
		t.Error("expected warning message for large minor diff")
	}
	if !vc.Check() {
		t.Error("should still be compatible despite large minor diff")
	}
}

func TestCheckVersionCompatible(t *testing.T) {
	// Server is close to BuiltForAPIVersion — should be compatible.
	builtVer, _ := ParseSemVer(BuiltForAPIVersion)
	closeVer := SemVer{Major: builtVer.Major, Minor: builtVer.Minor + 2, Patch: 0}

	vc, err := CheckVersion(closeVer.String())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vc.MajorMismatch {
		t.Error("should not have major mismatch")
	}
	if vc.WarningMessage != "" {
		t.Errorf("should have no warning, got %q", vc.WarningMessage)
	}
	if !vc.Check() {
		t.Error("should be compatible")
	}
}

func TestCheckVersionInvalidInput(t *testing.T) {
	_, err := CheckVersion("not-a-version")
	if err == nil {
		t.Error("expected error for invalid version string")
	}
}

func TestBuiltForAPIVersionValid(t *testing.T) {
	ver, err := ParseSemVer(BuiltForAPIVersion)
	if err != nil {
		t.Fatalf("BuiltForAPIVersion %q is not valid semver: %v", BuiltForAPIVersion, err)
	}
	if ver.Major != 0 || ver.Minor < 1 {
		t.Errorf("BuiltForAPIVersion %v looks wrong", ver)
	}
}

func TestGetDocVersionNoFile(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	_, err := GetDocVersion()
	if err == nil {
		t.Error("expected error when no api.md file exists")
	}
}

func TestSemVerRoundTrip(t *testing.T) {
	original := "v1.23.456"
	ver, err := ParseSemVer(original)
	if err != nil {
		t.Fatal(err)
	}
	result := ver.String()
	if result != original {
		t.Errorf("roundtrip: got %q, want %q", result, original)
	}
}
