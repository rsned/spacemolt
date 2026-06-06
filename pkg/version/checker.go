package version

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// BuiltForAPIVersion is the server API version this client was built against.
// Update this constant when updating response structs and command signatures
// to match a new server API version.
const BuiltForAPIVersion = "v0.347.0"

// SemVer represents a semantic version (Major.Minor.Patch)
type SemVer struct {
	Major int
	Minor int
	Patch int
}

// ParseSemVer parses a semantic version string like "v0.43.6" or "0.43.6"
func ParseSemVer(s string) (SemVer, error) {
	// Remove 'v' prefix if present
	s = strings.TrimPrefix(s, "v")

	// Match pattern: digits.digits.digits
	re := regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)`)
	matches := re.FindStringSubmatch(s)
	if matches == nil {
		return SemVer{}, fmt.Errorf("invalid semver format: %s", s)
	}

	major, _ := strconv.Atoi(matches[1])
	minor, _ := strconv.Atoi(matches[2])
	patch, _ := strconv.Atoi(matches[3])

	return SemVer{Major: major, Minor: minor, Patch: patch}, nil
}

// String returns the string representation of the SemVer
func (v SemVer) String() string {
	return fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// Compare compares two versions and returns the difference
// Positive values mean v is greater than other, negative means v is less than other
func (v SemVer) Compare(other SemVer) int {
	if v.Major != other.Major {
		return v.Major - other.Major
	}
	if v.Minor != other.Minor {
		return v.Minor - other.Minor
	}
	return v.Patch - other.Patch
}

// MinorDiff returns the difference in minor versions
func (v SemVer) MinorDiff(other SemVer) int {
	// Only compare minor if major is the same
	if v.Major != other.Major {
		return 0 // Not applicable across major versions
	}
	return v.Minor - other.Minor
}

// VersionCheck holds the result of a version compatibility check
type VersionCheck struct {
	ServerVersion   SemVer
	ExpectedVersion SemVer
	MajorMismatch   bool
	MinorDiff       int // Difference in minor versions (0 if major differs)
	WarningMessage  string
	ErrorMessage    string
}

// Check returns true if versions are compatible (no major mismatch, minor diff <= 10)
func (vc *VersionCheck) Check() bool {
	if vc.MajorMismatch {
		return false
	}
	if vc.MinorDiff > 10 {
		// Warning only, still compatible
		return true
	}
	return true
}

// Banner returns a multi-line banner for version warnings
func (vc *VersionCheck) Banner() string {
	if vc.WarningMessage == "" {
		return ""
	}

	var buf bytes.Buffer
	buf.WriteString("\n")
	buf.WriteString("╔══════════════════════════════════════════════════════════════════╗\n")
	buf.WriteString("║          ⚠  VERSION MISMATCH WARNING  ⚠                          ║\n")
	buf.WriteString("╚══════════════════════════════════════════════════════════════════╝\n")
	buf.WriteString("\n")
	buf.WriteString(vc.WarningMessage)
	buf.WriteString("\n")
	buf.WriteString("═══════════════════════════════════════════════════════════════════\n")
	return buf.String()
}

// GetDocVersion reads and parses the version from server_docs/api.md
func GetDocVersion() (SemVer, error) {
	// Try both the symlinked file and a dated file
	paths := []string{
		"server_docs/api.md",
		"server_docs/api.20260205.md", // Fallback to specific date
	}

	var lastErr error
	for _, path := range paths {
		version, err := extractVersionFromFile(path)
		if err == nil {
			return version, nil
		}
		lastErr = err
	}

	return SemVer{}, fmt.Errorf("failed to read version from api.md: %w", lastErr)
}

// extractVersionFromFile reads the first few lines of a file and extracts the version
func extractVersionFromFile(path string) (SemVer, error) {
	file, err := os.Open(path)
	if err != nil {
		return SemVer{}, err
	}
	defer func() { _ = file.Close() }()

	// Read first 10 lines looking for version
	scanner := bufio.NewScanner(file)
	lineNum := 0
	for scanner.Scan() && lineNum < 10 {
		line := scanner.Text()
		lineNum++

		// Look for pattern like: "This document is accurate for gameserver v0.43.6"
		if strings.Contains(line, "gameserver") && strings.Contains(line, "v") {
			// Extract version using regex
			re := regexp.MustCompile(`v(\d+\.\d+\.\d+)`)
			matches := re.FindStringSubmatch(line)
			if len(matches) >= 2 {
				return ParseSemVer(matches[1])
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return SemVer{}, err
	}

	return SemVer{}, errors.New("version not found in file")
}

// CheckVersion compares the server version against the version this client was built for.
// Uses the BuiltForAPIVersion constant, not the server_docs files (which may be newer).
func CheckVersion(serverVersionStr string) (*VersionCheck, error) {
	serverVer, err := ParseSemVer(serverVersionStr)
	if err != nil {
		return nil, fmt.Errorf("invalid server version: %w", err)
	}

	builtVer, err := ParseSemVer(BuiltForAPIVersion)
	if err != nil {
		return nil, fmt.Errorf("invalid built-for version constant: %w", err)
	}
	docVer := builtVer

	vc := &VersionCheck{
		ServerVersion:   serverVer,
		ExpectedVersion: docVer,
	}

	// Check major version mismatch
	if serverVer.Major != docVer.Major {
		vc.MajorMismatch = true
		vc.ErrorMessage = fmt.Sprintf(
			"CRITICAL: Major version mismatch!\n"+
				"  Server version: %s\n"+
				"  Expected version: %s\n"+
				"  Your client was built for a different major version of the API.\n"+
				"  Please update your client by running: ./update-server-docs\n"+
				"  Then rebuild and restart.",
			serverVer, docVer,
		)
		return vc, nil
	}

	// Check minor version difference
	minorDiff := serverVer.MinorDiff(docVer)
	vc.MinorDiff = minorDiff
	if minorDiff < 0 {
		minorDiff = -minorDiff // Make positive for comparison
	}

	if minorDiff > 10 {
		vc.WarningMessage = fmt.Sprintf(
			"WARNING: Your client is significantly behind the server API!\n"+
				"  Server version: %s\n"+
				"  Expected version: %s\n"+
				"  Minor versions behind: %d\n"+
				"\n"+
				"  Your client may still function but is likely missing new\n"+
				"  functionality or message fields that could be important.\n"+
				"\n"+
				"  Recommended: Run './update-server-docs' and rebuild your client.",
			serverVer, docVer, minorDiff,
		)
	}

	return vc, nil
}
