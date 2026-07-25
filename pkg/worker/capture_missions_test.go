package worker

import (
	"context"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// recordingMissionKB counts template upserts (MemoryKB has no template getter).
type recordingMissionKB struct {
	knowledge.Base
	upserts []serverapi.MissionBoardEntry
}

func (r *recordingMissionKB) UpsertMissionTemplate(ctx context.Context, entry serverapi.MissionBoardEntry, baseID, systemID string, tick int64) (*knowledge.MissionUpsertResult, error) {
	r.upserts = append(r.upserts, entry)
	return &knowledge.MissionUpsertResult{Inserted: true}, nil
}

const missionsRaw = `{"base_id":"nyx_nexus_station","base_name":"Nyx Nexus Station","missions":[
 {"mission_id":"m1","template_id":"tpl_courier_1","title":"Courier Run"},
 {"mission_id":"smuggling_courier_nyx_haven_red_mist~deadbeef","template_id":"","title":"Procedural Haul","type":"smuggling"}
]}`

func TestKBUpdateMissionsCapturesProceduralToo(t *testing.T) {
	st := &game.State{Doc: true}
	st.System.ID = "nyx"
	st.CurrentPOI = "nyx_nexus_station"
	client := &fakeClient{state: st, raw: map[string][]byte{"missions": []byte(missionsRaw)}}
	kb := &recordingMissionKB{Base: knowledge.NewMemoryKB()}

	if err := KBUpdateMissions(context.Background(), client, kb); err != nil {
		t.Fatal(err)
	}
	if len(kb.upserts) != 2 {
		t.Fatalf("want both hand-authored + procedural upserted, got %d: %+v", len(kb.upserts), kb.upserts)
	}
}

func TestKBUpdateMissionsRequiresDock(t *testing.T) {
	client := &fakeClient{state: &game.State{}}
	if err := KBUpdateMissions(context.Background(), client, knowledge.NewMemoryKB()); err == nil {
		t.Fatal("undocked KBUpdateMissions must error")
	}
}
