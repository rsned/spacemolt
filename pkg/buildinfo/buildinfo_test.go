package buildinfo

import (
	"runtime/debug"
	"testing"
	"time"
)

func settings(kv ...string) *debug.BuildInfo {
	bi := &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}
	for i := 0; i+1 < len(kv); i += 2 {
		bi.Settings = append(bi.Settings, debug.BuildSetting{Key: kv[i], Value: kv[i+1]})
	}
	return bi
}

func TestResolveLdflagsVersionWins(t *testing.T) {
	got := resolve("v0.3.0-2-g8016cd8", "true",
		settings("vcs.revision", "8016cd8abcdef0123456", "vcs.time", "2026-07-23T10:00:00Z", "vcs.modified", "true"), true)
	if got.Version != "v0.3.0-2-g8016cd8" {
		t.Fatalf("Version = %q, want stamped ldflags value", got.Version)
	}
	if !got.CodeDirty {
		t.Fatalf("CodeDirty = false, want true from codeDirty=\"true\"")
	}
	if got.Commit != "8016cd8abcde" {
		t.Fatalf("Commit = %q, want 12-char short revision", got.Commit)
	}
	if !got.BuiltAt.Equal(time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("BuiltAt = %v, want parsed vcs.time", got.BuiltAt)
	}
	if !got.Modified {
		t.Fatalf("Modified = false, want raw vcs.modified=true")
	}
}

func TestResolveFallsBackToPseudoVersion(t *testing.T) {
	bi := settings("vcs.revision", "deadbeef")
	bi.Main.Version = "v0.0.0-20260723100000-8016cd8abcde"
	got := resolve("", "", bi, true)
	if got.Version != "v0.0.0-20260723100000-8016cd8abcde" {
		t.Fatalf("Version = %q, want module pseudo-version fallback", got.Version)
	}
	if got.CodeDirty {
		t.Fatalf("CodeDirty = true, want false when codeDirty unstamped")
	}
}

func TestResolveFallsBackToDev(t *testing.T) {
	if got := resolve("", "", settings(), true); got.Version != "dev" {
		t.Fatalf("Version = %q, want \"dev\" when nothing stamped and Main is (devel)", got.Version)
	}
	if got := resolve("", "false", nil, false); got.Version != "dev" || got.Commit != "" || !got.BuiltAt.IsZero() {
		t.Fatalf("ReadBuildInfo ok=false must yield dev/empty/zero without panic, got %+v", got)
	}
}

func TestGetIsStableAcrossCalls(t *testing.T) {
	first := Get()
	second := Get()
	if first != second {
		t.Fatalf("Get() must be memoized and return a stable value")
	}
}
