package buildinfo_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot walks up from the test's working directory (pkg/buildinfo) to the
// module root so `go build` and file reads use stable absolute paths.
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

// TestLdflagsStampWiresThrough proves the ldflags target names the real symbol:
// a build stamping buildinfo.version must surface verbatim via Get().Version.
func TestLdflagsStampWiresThrough(t *testing.T) {
	root := repoRoot(t)
	bin := filepath.Join(t.TempDir(), "probe")
	cmd := exec.Command("go", "build",
		"-ldflags", "-X github.com/rsned/spacemolt/pkg/buildinfo.version=v9.9.9-stamp-test",
		"-o", bin, "./scripts/buildinfo-probe")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("stamped build failed: %v\n%s", err, out)
	}
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run probe: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != "v9.9.9-stamp-test" {
		t.Fatalf("stamped version = %q, want v9.9.9-stamp-test — ldflags target is wrong", got)
	}
}

// TestBuildScriptTargetsBuildinfo guards that the release script stamps the
// exact ldflags symbols and builds all three fleet binaries.
func TestBuildScriptTargetsBuildinfo(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "scripts", "build.sh"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"github.com/rsned/spacemolt/pkg/buildinfo.version=",
		"github.com/rsned/spacemolt/pkg/buildinfo.codeDirty=",
		"git status --porcelain -- ':!data/'",
		"-o bin/overmind ",
		"-o bin/worker ",
		"-o bin/overmind-dashboard ",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("scripts/build.sh missing %q", want)
		}
	}
}
