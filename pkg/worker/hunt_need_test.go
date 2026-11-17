package worker

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

func loadNeedFixture(t *testing.T) serverapi.MissionBoardEntry {
	t.Helper()
	b, err := os.ReadFile("testdata/station_need_superfluid_vial.json")
	if err != nil {
		t.Fatal(err)
	}
	var e serverapi.MissionBoardEntry
	if err := json.Unmarshal(b, &e); err != nil {
		t.Fatal(err)
	}
	return e
}

func TestHuntParseNeedReadsDroppedBy(t *testing.T) {
	e := loadNeedFixture(t)
	n, ok := huntParseNeed(e, map[string]string{"frost-moth": "frost_moth"}, nil)
	if !ok {
		t.Fatal("want ok")
	}
	want := huntNeed{ItemID: "superfluid_vial", Quantity: 10, TargetBaseID: "confederacy_central_command",
		SystemID: "sol", Species: []string{"frost_moth"}, Reward: 7118}
	if n.ItemID != want.ItemID || n.Quantity != want.Quantity || n.TargetBaseID != want.TargetBaseID ||
		n.SystemID != want.SystemID || n.Reward != want.Reward || !slices.Equal(n.Species, want.Species) {
		t.Errorf("need = %+v, want %+v", n, want)
	}

	// A guide that spells the name without the hyphen still resolves.
	n, ok = huntParseNeed(e, map[string]string{"frost moth": "frost_moth"}, nil)
	if !ok || !slices.Equal(n.Species, []string{"frost_moth"}) {
		t.Errorf("unhyphenated guide: %+v ok=%v", n, ok)
	}
}

func TestHuntParseNeedFallsBackToDropRecords(t *testing.T) {
	e := loadNeedFixture(t)
	e.Description = "The winder waits on its feed."
	n, ok := huntParseNeed(e, nil, func(id string) []string {
		if id != "superfluid_vial" {
			t.Errorf("dropsOf asked for %q", id)
		}
		return []string{"frost_moth"}
	})
	if !ok || !slices.Equal(n.Species, []string{"frost_moth"}) {
		t.Errorf("need = %+v ok=%v", n, ok)
	}
}

func TestHuntParseNeedRefusesUnknownSource(t *testing.T) {
	e := loadNeedFixture(t)
	if _, ok := huntParseNeed(e, map[string]string{}, func(string) []string { return nil }); ok {
		t.Error("unresolvable Dropped by with no drop records must be refused")
	}
	e.Type = "mining"
	if _, ok := huntParseNeed(e, map[string]string{"frost-moth": "frost_moth"}, nil); ok {
		t.Error("non-delivery mission must be refused")
	}
}

func TestHuntAdmissibleNeedRefusesPredator(t *testing.T) {
	n := huntNeed{Species: []string{"frost_moth", "razor_wyrm"}}
	roles := map[string]string{"frost_moth": "grazer", "razor_wyrm": "predator"}
	ok, reason := huntAdmissibleNeed(n, 1, func(s string) string { return roles[s] })
	if ok || !strings.Contains(reason, "razor_wyrm") {
		t.Errorf("ok=%v reason=%q, want refusal naming razor_wyrm", ok, reason)
	}
	ok, _ = huntAdmissibleNeed(huntNeed{Species: []string{"frost_moth"}}, 1, func(s string) string { return roles[s] })
	if !ok {
		t.Error("grazer should be admissible")
	}
}

func TestHuntAdmissibleNeedRefusesUnknownRole(t *testing.T) {
	ok, reason := huntAdmissibleNeed(huntNeed{Species: []string{"mystery"}}, 1, func(string) string { return "" })
	if ok || !strings.Contains(reason, "mystery") {
		t.Errorf("ok=%v reason=%q", ok, reason)
	}
}

func TestHuntAdmissibleNeedCapsDifficultyAt2(t *testing.T) {
	n := huntNeed{Species: []string{"frost_moth"}}
	grazer := func(string) string { return "grazer" }
	if ok, _ := huntAdmissibleNeed(n, 2, grazer); !ok {
		t.Error("difficulty 2 should pass")
	}
	if ok, _ := huntAdmissibleNeed(n, 3, grazer); ok {
		t.Error("difficulty 3 should be refused")
	}
}

func TestHuntParseNeedKeepsEveryNameDespiteParens(t *testing.T) {
	e := loadNeedFixture(t)
	e.Description = "Dropped by: Belt-Grazer (in belts), Crusher-Mantis (anywhere)."
	byName := map[string]string{"belt-grazer": "belt_grazer", "crusher-mantis": "crusher_mantis"}
	n, ok := huntParseNeed(e, byName, nil)
	if !ok || !slices.Equal(n.Species, []string{"belt_grazer", "crusher_mantis"}) {
		t.Fatalf("need = %+v ok=%v", n, ok)
	}
	roles := map[string]string{"belt_grazer": "grazer", "crusher_mantis": "predator"}
	if adm, reason := huntAdmissibleNeed(n, 1, func(s string) string { return roles[s] }); adm {
		t.Errorf("predator on the line must be refused, reason=%q", reason)
	}
}

func TestHuntParseNeedRefusesPartiallyResolvedList(t *testing.T) {
	e := loadNeedFixture(t)
	e.Description = "Dropped by: Belt-Grazer or Unknown-Thing."
	dropsOf := func(string) []string { return []string{"belt_grazer"} }
	if _, ok := huntParseNeed(e, map[string]string{"belt-grazer": "belt_grazer"}, dropsOf); ok {
		t.Error("an unresolved name must refuse the need, not fall back or admit a partial list")
	}
}
