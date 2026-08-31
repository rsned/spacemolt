package worker

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// TestDangerRefsFor covers the goldcrest incident: three haulers died to
// wildlife there in one week (2026-08-22..24) because nothing in haul routing
// consulted danger_zones. The refs must dual-key id+name (opportunity rows
// carry system NAMES, FindBestPrices rows carry ids) exactly like stronghold
// refs.
func TestDangerRefsFor(t *testing.T) {
	kb := &fakeKB{dangers: []knowledge.DangerZone{
		{SystemID: "goldcrest", SystemName: "Goldcrest", DangerLevel: 8},
		{SystemID: "nashira", SystemName: "Nashira", DangerLevel: 6},
	}}
	refs := dangerRefsFor(context.Background(), kb, io.Discard)
	for _, want := range []string{"goldcrest", "Goldcrest", "nashira", "Nashira"} {
		if !refs[want] {
			t.Errorf("refs missing %q; got %v", want, refs)
		}
	}
	if len(refs) != 4 {
		t.Errorf("refs = %v, want exactly 4 keys", refs)
	}
}

// A KB that errors (MemoryKB does; a broken SQLite read would) must degrade to
// an empty set — hauling is never blocked by a danger-zone read failure.
func TestDangerRefsForKBErrorIsEmpty(t *testing.T) {
	var buf strings.Builder
	refs := dangerRefsFor(context.Background(), &erroringDangerKB{}, &buf)
	if len(refs) != 0 {
		t.Errorf("refs = %v, want empty on KB error", refs)
	}
	if !strings.Contains(buf.String(), "danger") {
		t.Errorf("expected a logged warning, got %q", buf.String())
	}
}

func TestDangerRefsForNilKB(t *testing.T) {
	if refs := dangerRefsFor(context.Background(), nil, io.Discard); len(refs) != 0 {
		t.Errorf("nil KB should yield empty refs, got %v", refs)
	}
}

// unionRefs must not mutate its inputs (the stronghold refs are reused later
// in the pass) and must treat nil sides as empty.
func TestUnionRefs(t *testing.T) {
	strongholds := map[string]bool{"zaniah": true}
	dangers := map[string]bool{"goldcrest": true}
	u := unionRefs(strongholds, dangers)
	if !u["zaniah"] || !u["goldcrest"] || len(u) != 2 {
		t.Errorf("union = %v", u)
	}
	if len(strongholds) != 1 || len(dangers) != 1 {
		t.Errorf("inputs mutated: %v %v", strongholds, dangers)
	}
	if u2 := unionRefs(nil, dangers); !u2["goldcrest"] || len(u2) != 1 {
		t.Errorf("nil-left union = %v", u2)
	}
	if u3 := unionRefs(strongholds, nil); !u3["zaniah"] || len(u3) != 1 {
		t.Errorf("nil-right union = %v", u3)
	}
}

// The wildlife gate applies to EVERYONE — unlike strongholds, holding the
// pirate unlock is no defense against a Leviathan. An unlocked agent (empty
// stronghold set) must still see the danger refs via the union.
func TestDangerGateAppliesToUnlockedAgents(t *testing.T) {
	kb := &fakeKB{dangers: []knowledge.DangerZone{
		{SystemID: "goldcrest", SystemName: "Goldcrest", DangerLevel: 8},
	}}
	var strongholds map[string]bool // pirate unlock held -> empty set
	hazards := unionRefs(strongholds, dangerRefsFor(context.Background(), kb, io.Discard))
	if !hazards["Goldcrest"] || !hazards["goldcrest"] {
		t.Errorf("unlocked agent lost the wildlife gate: %v", hazards)
	}
}

type erroringDangerKB struct{ knowledge.Base }

func (e *erroringDangerKB) GetDangerZones(context.Context, int) ([]knowledge.DangerZone, error) {
	return nil, context.DeadlineExceeded
}
