package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/battlereplay"
)

// The battle id is required. The worker's no-arg form drains a kill-derived
// queue, which is the wrong shape here -- silently doing nothing would be worse
// than an error, because the operator only gets one chance to capture the id.
func TestCaptureWildlifeAttacksRequiresBattleID(t *testing.T) {
	var out bytes.Buffer
	for _, args := range [][]string{nil, {}, {""}} {
		err := captureWildlifeAttacksCmd(context.Background(), &out, nil, nil, args)
		if err == nil {
			t.Fatalf("args %q: expected a usage error", args)
		}
		if !strings.Contains(err.Error(), "capture_wildlife_attacks <battle_id>") {
			t.Errorf("args %q: error %q should show the usage", args, err)
		}
	}
}

// Without --db-path there is nowhere to file rows; say so rather than failing,
// matching how capture_profile handles a missing assets DB.
func TestCaptureWildlifeAttacksWithoutKBExplainsItself(t *testing.T) {
	var out bytes.Buffer
	if err := captureWildlifeAttacksCmd(context.Background(), &out, nil, nil, []string{"abc123"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "--db-path") {
		t.Errorf("output should name the missing flag, got %q", out.String())
	}
}

func TestGetBattleLogRequiresBattleID(t *testing.T) {
	var out bytes.Buffer
	if err := getBattleLogCmd(context.Background(), &out, nil, nil, formatRaw); err == nil {
		t.Fatal("expected a usage error with no battle id")
	}
}

// The summary has to name creatures from the battle's own participant list, not
// from the field guide. That is the whole point of it: when the guide cannot
// name a species the capture files zero rows, and this is how the damage is
// still readable.
func TestBattleDamageSummaryNamesCreaturesWithoutTheFieldGuide(t *testing.T) {
	m := &battlereplay.ReplayModel{
		BattleID: "b1",
		Participants: []battlereplay.Participant{
			{PlayerID: "crt_1", Username: "Rainbow Leviathan", Kind: "creature"},
			{PlayerID: "ply_1", Username: "Arthur", Kind: "player"},
		},
		Frames: []battlereplay.Frame{{
			Shots: []battlereplay.Shot{
				{FromID: "crt_1", WeaponName: "prismatic_lance", DamageType: "energy", Hit: true, Damage: 130},
				{FromID: "crt_1", WeaponName: "prismatic_lance", DamageType: "energy", Hit: true, Damage: 90},
				{FromID: "crt_1", WeaponName: "prismatic_lance", DamageType: "energy", Hit: false, Damage: 0},
			},
		}},
	}
	var out bytes.Buffer
	if err := writeBattleDamageSummary(&out, m); err != nil {
		t.Fatalf("summary: %v", err)
	}
	got := out.String()
	for _, want := range []string{"Rainbow Leviathan", "creature", "prismatic_lance", "energy"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing %q:\n%s", want, got)
		}
	}
	// 3 shots, 2 hits, 220 total, min 90 max 130 -- a miss must not drag the
	// minimum to a value the weapon never deals.
	for _, want := range []string{"3", "2", "220", "90", "130"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing figure %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "     0 ") {
		t.Errorf("a missed shot must not report 0 as the damage minimum:\n%s", got)
	}
}

func TestBattleDamageSummaryHandlesNoShots(t *testing.T) {
	var out bytes.Buffer
	m := &battlereplay.ReplayModel{BattleID: "b2", Participants: []battlereplay.Participant{{PlayerID: "p", Kind: "player"}}}
	if err := writeBattleDamageSummary(&out, m); err != nil {
		t.Fatalf("summary: %v", err)
	}
	if !strings.Contains(out.String(), "no shots recorded") {
		t.Errorf("expected an explicit empty-log line, got %q", out.String())
	}
}
