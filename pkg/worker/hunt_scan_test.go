package worker

import (
	"context"
	"io"
	"slices"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// scanFakeClient records ScanTarget calls and replays a canned "scan" reply.
type scanFakeClient struct {
	game.GameClient
	scanned []string
	reply   []byte
}

func (c *scanFakeClient) ScanTarget(_ context.Context, id string) error {
	c.scanned = append(c.scanned, id)
	return nil
}

func (c *scanFakeClient) GetRawJSON(key string) []byte {
	if key == "scan" {
		return c.reply
	}
	return nil
}

// scanFakeKB reports a fixed unscanned set and records what is filed.
type scanFakeKB struct {
	knowledge.Base
	unscanned []string
	filed     []string
}

func (k *scanFakeKB) UnscannedWildlifeSpecies(_ context.Context, species []string) ([]string, error) {
	var out []string
	for _, s := range species {
		if slices.Contains(k.unscanned, s) {
			out = append(out, s)
		}
	}
	return out, nil
}

func (k *scanFakeKB) UpsertWildlifeSpecies(_ context.Context, rows []knowledge.WildlifeSpecies) error {
	for _, r := range rows {
		k.filed = append(k.filed, r.Species)
	}
	return nil
}

func (k *scanFakeKB) RecordWildlifeSightings(context.Context, []knowledge.WildlifeSighting) error {
	return nil
}
func (k *scanFakeKB) RecordWildlifeKill(context.Context, knowledge.WildlifeKill) error { return nil }
func (k *scanFakeKB) RecordWildlifeCoverage(context.Context, []knowledge.WildlifeCoverage) error {
	return nil
}

func TestHuntScanUnknownScansOnlyUnscannedSpecies(t *testing.T) {
	c := &scanFakeClient{reply: []byte(`{"command":"scan","result":{"target_id":"crt_moth1","success":true,` +
		`"username":"Frost-Moth [grazer — harmless prey, skittish]","revealed_info":["danger"],"hull":40}}`)}
	kb := &scanFakeKB{unscanned: []string{"frost_moth"}}
	deps := HuntDeps{Client: c, KB: kb}

	got := huntScanUnknown(context.Background(), deps, io.Discard, []serverapi.NearbyCreature{
		{CreatureID: "crt_g1", Species: "belt_grazer"},
		{CreatureID: "crt_g2", Species: "belt_grazer"},
		{CreatureID: "crt_moth1", Species: "frost_moth"},
	})

	if got != 1 {
		t.Errorf("scans = %d, want 1", got)
	}
	if !slices.Equal(c.scanned, []string{"crt_moth1"}) {
		t.Errorf("ScanTarget calls = %v, want [crt_moth1]", c.scanned)
	}
	if !slices.Equal(kb.filed, []string{"frost_moth"}) {
		t.Errorf("filed = %v, want [frost_moth]", kb.filed)
	}
}

// A cached scan reply for a DIFFERENT creature (a stale frame) is never filed:
// it would put one species' name and danger under another's id, and display
// names feed "Dropped by:" resolution.
func TestHuntScanUnknownIgnoresAStaleReply(t *testing.T) {
	c := &scanFakeClient{reply: []byte(`{"command":"scan","result":{"target_id":"crt_eel9","success":true,` +
		`"username":"Tempest-Eel [predator — deadly, aggressive]","revealed_info":["danger"],"hull":280}}`)}
	kb := &scanFakeKB{unscanned: []string{"frost_moth"}}
	deps := HuntDeps{Client: c, KB: kb}

	huntScanUnknown(context.Background(), deps, io.Discard, []serverapi.NearbyCreature{
		{CreatureID: "crt_moth1", Species: "frost_moth"},
	})
	if len(kb.filed) != 0 {
		t.Errorf("filed %v from a reply for crt_eel9 while scanning crt_moth1", kb.filed)
	}
}
