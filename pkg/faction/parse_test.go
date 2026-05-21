package faction

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

func TestParseFactionInfo(t *testing.T) {
	info := serverapi.FactionInfoResponse{
		ID: "f1", Name: "Crafters Union", Tag: "CRFT",
		LeaderID: "p1", LeaderUsername: "boss", Treasury: 1000,
		MemberCount: 2, OwnedBases: 1, Description: "lore", Charter: "rules",
		CreatedAt: "2026-01-01T00:00:00Z",
		Members: []serverapi.FactionMemberDetail{
			{PlayerID: "p1", Username: "boss", Role: "Leader", IsOnline: true},
			{PlayerID: "p2", Username: "grunt", Role: "Member"},
		},
		Allies:  []serverapi.FactionSummary{{ID: "f2", Name: "Allies Inc", Tag: "ALLY"}},
		Enemies: []serverapi.FactionSummary{{ID: "f3", Name: "Bad Guys", Tag: "EVIL"}},
		Wars: []serverapi.FactionWarDetail{
			{TargetFactionID: "f3", TargetFactionName: "Bad Guys", TargetFactionTag: "EVIL", OurKills: 3, TheirKills: 1, Reason: "honor"},
		},
	}

	rec, members, rels := parseFactionInfo(info)
	if rec.Tag != "CRFT" || rec.Treasury != 1000 || rec.FoundedUTC != "2026-01-01T00:00:00Z" {
		t.Errorf("record wrong: %+v", rec)
	}
	if len(members) != 2 || members[0].Role != "Leader" || !members[0].IsOnline {
		t.Errorf("members wrong: %+v", members)
	}
	if len(rels) != 3 {
		t.Fatalf("want 3 relations, got %d: %+v", len(rels), rels)
	}
	var kinds = map[string]int{}
	for _, r := range rels {
		kinds[r.Kind]++
	}
	if kinds["ally"] != 1 || kinds["enemy"] != 1 || kinds["war"] != 1 {
		t.Errorf("relation kinds wrong: %v", kinds)
	}
}
