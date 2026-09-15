package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/internal/protocol"
)

// push builds a protocol.Response from a raw payload literal, the way the
// read loop hands one to a push subscriber.
func push(t *testing.T, typ, payload string) protocol.Response {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(payload), &m); err != nil {
		t.Fatalf("bad payload fixture: %v", err)
	}
	return protocol.Response{Type: typ, Payload: m}
}

// The payloads below are verbatim captures from explorer-8's fight with
// Pressblister at wasat_cryobelt (2026-09-14), which produced no terminal
// output at all until debug logging was switched on.
func TestPushEventLines(t *testing.T) {
	const self = "b4795c96c934b5ec967606bf39a51c88"

	tests := []struct {
		name    string
		resp    protocol.Response
		want    []string
		wantNil bool
	}{
		{
			name: "battle_damage taken names the attacker and the split",
			resp: push(t, protocol.TypeBattleDamage, `{
				"attacker_id":"crt_91dabdeab9562d95905e51b839a13149",
				"attacker_name":"Pressblister",
				"damage_type":"thermal","hit_success":true,
				"hull_hit":0,"shield_hit":2,"total_damage":2,
				"target_id":"b4795c96c934b5ec967606bf39a51c88",
				"target_name":"Zenith 'Zone' Zimmer"}`),
			want: []string{"⚔ Pressblister → you: 2 thermal (shield 2, hull 0)"},
		},
		{
			name: "battle_damage dealt reads from our side",
			resp: push(t, protocol.TypeBattleDamage, `{
				"attacker_id":"b4795c96c934b5ec967606bf39a51c88",
				"attacker_name":"Zenith 'Zone' Zimmer",
				"damage_type":"kinetic","hit_success":true,
				"hull_hit":10,"shield_hit":4,"total_damage":14,
				"target_id":"crt_91dabdeab9562d95905e51b839a13149",
				"target_name":"Pressblister"}`),
			want: []string{"⚔ you → Pressblister: 14 kinetic (shield 4, hull 10)"},
		},
		{
			name: "battle_damage miss says so instead of reporting 0",
			resp: push(t, protocol.TypeBattleDamage, `{
				"attacker_id":"crt_91dabdeab9562d95905e51b839a13149",
				"attacker_name":"Pressblister","hit_success":false,
				"target_id":"b4795c96c934b5ec967606bf39a51c88"}`),
			want: []string{"⚔ Pressblister → you: MISS"},
		},
		{
			name: "battle_update shows our stance and every combatant's health",
			resp: push(t, protocol.TypeBattleUpdate, `{
				"auto_pilot":true,
				"battle_id":"00b1ab865111678e86c9fbb1977d83a2",
				"tick":1885634,
				"your_side_id":2,"your_stance":"fire","your_zone":"engaged",
				"your_target_id":"crt_91dabdeab9562d95905e51b839a13149",
				"participants":[
				  {"is_npc":true,"kind":"creature","player_id":"crt_91dabdeab9562d95905e51b839a13149",
				   "side_id":1,"stance":"fire","username":"Pressblister","hull_pct":100,"shield_pct":88},
				  {"player_id":"b4795c96c934b5ec967606bf39a51c88","side_id":2,"stance":"fire",
				   "username":"Zenith 'Zone' Zimmer","hull_pct":100,"shield_pct":60}]}`),
			want: []string{
				"⚔ tick 1885634 — you: side 2, stance fire, zone engaged, target Pressblister [autopilot]",
				"   side 1: Pressblister hull 100% shield 88%",
				"   side 2: Zenith 'Zone' Zimmer hull 100% shield 60%  ← you",
			},
		},
		{
			name: "battle_started names the battle so get_battle_log is reachable",
			resp: push(t, protocol.TypeBattleStarted, `{
				"battle_id":"00b1ab865111678e86c9fbb1977d83a2","system_id":"wasat",
				"participants":[{"player_id":"a"},{"player_id":"b"}]}`),
			want: []string{"⚔ Battle started battle=00b1ab865111678e86c9fbb1977d83a2 system=wasat combatants=2"},
		},
		{
			name: "battle_ended reports the outcome",
			resp: push(t, protocol.TypeBattleEnded, `{
				"battle_id":"00b1ab865111678e86c9fbb1977d83a2","reason":"all_hostiles_destroyed",
				"winning_side":2,"duration":30,"ships_destroyed":1,"total_damage":214}`),
			want: []string{"⚔ Battle ended battle=00b1ab865111678e86c9fbb1977d83a2 reason=all_hostiles_destroyed winner=side 2 ticks=30 destroyed=1 damage=214"},
		},
		{
			name: "battle_ended stalemate is spelled out, not -1",
			resp: push(t, protocol.TypeBattleEnded, `{
				"battle_id":"b1","reason":"timeout","winning_side":-1}`),
			want: []string{"⚔ Battle ended battle=b1 reason=timeout winner=stalemate"},
		},
		{
			name: "battle_left falls back to the id when a creature has no username",
			resp: push(t, protocol.TypeBattleLeft, `{
				"player_id":"crt_91dabdeab9562d95905e51b839a13149","reason":"fled"}`),
			want: []string{"⚔ crt_91dabdeab9562d95905e51b839a13149 left (fled)"},
		},
		{
			name: "battle_joined names the side",
			resp: push(t, protocol.TypeBattleJoined, `{
				"player_id":"p1","username":"MoltenOne","side_id":1}`),
			want: []string{"⚔ MoltenOne joined side 1"},
		},
		{
			name: "player_died carries the wreck location and the respawn",
			resp: push(t, protocol.TypePlayerDied, `{
				"killer_name":"Pressblister","cause":"creature",
				"ship_lost":"crimson_stiletto","new_ship_class":"escape_pod",
				"respawn_base":"war_citadel","clone_cost":5000,"insurance_payout":12000,
				"wreck_id":"wr_1","wreck_system_name":"Wasat","wreck_poi_name":"Wasat Cryobelt"}`),
			want: []string{
				"💀 Destroyed by Pressblister (creature) — lost crimson_stiletto, now in escape_pod",
				"   wreck wr_1 at Wasat Cryobelt, Wasat · respawn war_citadel · clone 5000 · insurance 12000",
			},
		},
		{
			name: "skill_level_up is a one-liner",
			resp: push(t, protocol.TypeSkillLevelUp, `{"skill_id":"gunnery","new_level":2,"xp_gained":150}`),
			want: []string{"⭐ gunnery reached level 2"},
		},
		{
			name: "pirate_warning surfaces the countdown",
			resp: push(t, protocol.TypePirateWarning, `{
				"pirate_name":"Rust Wolf","tier":"veteran","attack_in_ticks":3,
				"message":"A pirate is closing in!"}`),
			want: []string{"🏴 A pirate is closing in! (Rust Wolf, veteran, attacks in 3 ticks)"},
		},
		{
			name: "server_restart_warning gives the window",
			resp: push(t, protocol.TypeServerRestartWarning, `{
				"message":"Server restarting","seconds_until_restart":120,"target_version":"v0.605.0"}`),
			want: []string{"⚠ Server restarting — restart in 120s (target v0.605.0)"},
		},
		{
			name: "combat_update is the non-battle damage channel",
			resp: push(t, protocol.TypeCombatUpdate, `{
				"tick":1885634,"attacker":"Rust Wolf","target":"Zenith 'Zone' Zimmer",
				"damage":37,"damage_type":"kinetic","shield_hit":12,"hull_hit":25}`),
			want: []string{"⚔ Rust Wolf → Zenith 'Zone' Zimmer: 37 kinetic (shield 12, hull 25)"},
		},
		{
			name: "combat_update flags a destruction",
			resp: push(t, protocol.TypeCombatUpdate, `{
				"attacker":"Rust Wolf","target":"Zenith 'Zone' Zimmer",
				"damage":37,"damage_type":"kinetic","destroyed":true}`),
			want: []string{"⚔ Rust Wolf → Zenith 'Zone' Zimmer: 37 kinetic (shield 0, hull 0) DESTROYED"},
		},
		{
			name: "pirate_destroyed reports the reward and the wreck",
			resp: push(t, protocol.TypePirateDestroyed, `{
				"pirate_name":"Rust Wolf","tier":"veteran","credits_reward":4200,"xp_gained":80,
				"wreck_id":"wr_9","wreck_poi_name":"Wasat Cryobelt","wreck_has_cargo":true}`),
			want: []string{
				"🏴 Destroyed Rust Wolf (veteran) — 4200 credits, 80 xp",
				"   wreck wr_9 at Wasat Cryobelt · has cargo",
			},
		},
		{
			name: "player_kill points at the wreck we just made",
			resp: push(t, protocol.TypePlayerKill, `{
				"victim":"MoltenOne","wreck_id":"cc2128e8","wreck_has_cargo":true,
				"wreck_has_modules":true,"wreck_system_name":"Krynn","wreck_poi_name":"War Citadel"}`),
			want: []string{
				"💥 Destroyed MoltenOne",
				"   wreck cc2128e8 at War Citadel, Krynn · has cargo · has modules",
			},
		},
		{
			name: "police_warning gives the response window",
			resp: push(t, protocol.TypePoliceWarning, `{
				"message":"Police dispatched","police_level":3,"response_ticks":5,"system":"sol"}`),
			want: []string{"🚓 Police dispatched (level 3, sol, responding in 5 ticks)"},
		},
		{
			name: "ship_captured names both sides of the boarding",
			resp: push(t, protocol.TypeShipCaptured, `{
				"battle_id":"b1","captor_username":"MoltenOne","former_owner_username":"Zenith 'Zone' Zimmer",
				"ship_id":"s1","ship_class":"crimson_stiletto"}`),
			want: []string{"🏴 MoltenOne captured crimson_stiletto from Zenith 'Zone' Zimmer"},
		},
		{
			name:    "chat_message is already surfaced by the chat handler",
			resp:    push(t, protocol.TypeChatMessage, `{"message":"hi"}`),
			wantNil: true,
		},
		{
			name:    "crafting_update is already surfaced by the craft handler",
			resp:    push(t, protocol.TypeCraftingUpdate, `{"jobs":[]}`),
			wantNil: true,
		},
		{
			name:    "state_update is per-tick noise",
			resp:    push(t, protocol.TypeStateUpdate, `{"credits":1}`),
			wantNil: true,
		},
		{
			name:    "command replies are printed by the command path",
			resp:    push(t, protocol.TypeOK, `{"message":"Advancing toward the enemy."}`),
			wantNil: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := pushEventLines(tc.resp, self)
			if tc.wantNil {
				if got != nil {
					t.Fatalf("want no output, got %q", strings.Join(got, " | "))
				}
				return
			}
			if len(got) != len(tc.want) {
				t.Fatalf("want %d lines, got %d:\n%s", len(tc.want), len(got), strings.Join(got, "\n"))
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("line %d:\n want %q\n  got %q", i, tc.want[i], got[i])
				}
			}
		})
	}
}

// A tagged response is a reply to one of our own commands; the command path
// already prints it, so the push renderer must not double-print.
func TestPushEventLinesSkipsTaggedReplies(t *testing.T) {
	resp := push(t, protocol.TypeBattleDamage, `{"attacker_name":"X","target_name":"Y","hit_success":true,"total_damage":1}`)
	resp.RequestID = "req-1"
	if got := pushEventLines(resp, "self"); got != nil {
		t.Fatalf("tagged reply should not render, got %q", strings.Join(got, " | "))
	}
}

// Colour is by severity, so a death does not scroll past looking like another
// damage tick.
func TestPushEventColor(t *testing.T) {
	tests := []struct {
		typ  string
		want string
	}{
		{protocol.TypeBattleDamage, ansiYellow},
		{protocol.TypeBattleUpdate, ansiYellow},
		{protocol.TypeCombatUpdate, ansiYellow},
		{protocol.TypePlayerDied, ansiBrightRed},
		{protocol.TypePlayerKill, ansiBrightRed},
		{protocol.TypeShipCaptured, ansiBrightRed},
		{protocol.TypePirateWarning, ansiMagenta},
		{protocol.TypePoliceWarning, ansiMagenta},
		{protocol.TypeSkillLevelUp, ansiGreen},
		{protocol.TypeServerRestartWarning, ansiBrightYellow},
		{protocol.TypeTick, ansiYellow}, // default
	}
	for _, tc := range tests {
		if got := pushEventColor(tc.typ); got != tc.want {
			t.Errorf("%s: want %q, got %q", tc.typ, tc.want, got)
		}
	}
}

func TestParseOnOff(t *testing.T) {
	for _, in := range []string{"on", "ON", "true", "True", "1", "t"} {
		got, err := parseOnOff(in)
		if err != nil || !got {
			t.Errorf("%q: want true/nil, got %v/%v", in, got, err)
		}
	}
	for _, in := range []string{"off", "OFF", "false", "0", "f"} {
		got, err := parseOnOff(in)
		if err != nil || got {
			t.Errorf("%q: want false/nil, got %v/%v", in, got, err)
		}
	}
	for _, in := range []string{"", "yes", "maybe"} {
		if _, err := parseOnOff(in); err == nil {
			t.Errorf("%q: want an error", in)
		}
	}
}

func TestEnabledWord(t *testing.T) {
	if got := enabledWord(true); got != "enabled" {
		t.Errorf("want enabled, got %q", got)
	}
	if got := enabledWord(false); got != "disabled" {
		t.Errorf("want disabled, got %q", got)
	}
}
