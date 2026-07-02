package main

import (
	"strings"
	"testing"
)

// realFactionInfoPayload is a trimmed real faction_info response (2026-07-02).
const realFactionInfoPayload = `{"allies":[{"id":"a1","name":"Explorers Guild","member_count":2,"primary_color":"#FFFFFF","secondary_color":"#000000","tag":"XPLR"},{"id":"a2","name":"Data Bots","member_count":45,"primary_color":"#FFFFFF","secondary_color":"#000000","tag":" DB "}],"at_war":false,"id":"e727","is_member":true,"leader_username":"Arthur 'Artificer' Artis","member_count":2,"members":[{"is_online":true,"player_id":"p1","role":"Leader","username":"Arthur 'Artificer' Artis"},{"is_online":false,"player_id":"p2","role":"Officer","username":"Harrison 'Handiwork' Hay"},{"is_online":true,"player_id":"p3","role":"Officer","username":"Amy 'Anvil' Ash"}],"members_limit":50,"name":"Crafting Collective","owned_bases":0,"primary_color":"#FFFFFF","roles":[{"id":"leader","name":"Leader","permissions":{"manage_bases":true,"manage_facilities":true,"manage_roles":true,"manage_treasury":true},"priority":100},{"id":"officer","name":"Officer","permissions":{"manage_bases":true,"manage_facilities":true,"manage_roles":false,"manage_treasury":true},"priority":50},{"id":"member","name":"Member","permissions":{"manage_bases":false,"manage_facilities":false,"manage_roles":false,"manage_treasury":false},"priority":10},{"id":"recruit","name":"Recruit","permissions":{"manage_bases":false,"manage_facilities":false,"manage_roles":false,"manage_treasury":false},"priority":1}],"facilities":[{"active":true,"base_id":"grand_exchange_station","facility_id":"f1","faction_service":"faction_fuel","name":"Faction Fuel Bunker","type":"faction_fuel_bunker"},{"active":true,"base_id":"grand_exchange_station","facility_id":"f2","faction_service":"faction_storage","name":"Faction Warehouse","type":"faction_warehouse"},{"active":true,"base_id":"grand_exchange_station","facility_id":"f3","faction_service":"","name":"Iron Refinery","type":"iron_refinery"}],"secondary_color":"#000000","tag":"CRFT","treasury":329220}`

func lineWith(out, needle string) string {
	for _, ln := range strings.Split(out, "\n") {
		if strings.Contains(ln, needle) {
			return ln
		}
	}
	return ""
}

func TestFormatFactionInfoEnhancements(t *testing.T) {
	out := formatFactionInfo([]byte(realFactionInfoPayload))
	if out == "" {
		t.Fatal("formatter returned empty")
	}

	// 1. Member count shows count/limit.
	if !strings.Contains(out, "Members: 2/50") {
		t.Errorf("expected 'Members: 2/50' in header; got:\n%s", out)
	}
	if !strings.Contains(out, "Members (2/50):") {
		t.Errorf("expected 'Members (2/50):' section header")
	}

	// 2. Members grouped by role, priority desc (Leader before Officer).
	iLeader := strings.Index(out, "  Leader:")
	iOfficer := strings.Index(out, "  Officer:")
	if iLeader < 0 || iOfficer < 0 || iLeader > iOfficer {
		t.Errorf("member groups not ordered Leader before Officer (iLeader=%d iOfficer=%d)", iLeader, iOfficer)
	}
	// Alphabetical within Officer group: Amy before Harrison.
	iAmy := strings.Index(out, "Amy 'Anvil' Ash")
	iHarrison := strings.Index(out, "Harrison 'Handiwork' Hay")
	if iAmy < 0 || iHarrison < 0 || iAmy > iHarrison {
		t.Errorf("Officer members not alphabetical (Amy=%d Harrison=%d)", iAmy, iHarrison)
	}

	// 3. Roles table present, ordered by priority desc, with checkboxes.
	rolesIdx := strings.Index(out, "\nRoles:\n")
	if rolesIdx < 0 {
		t.Fatal("no Roles table section")
	}
	rolesSection := out[rolesIdx:]
	for _, h := range []string{"Prio", "Bases", "Facilities", "Treasury"} {
		if !strings.Contains(rolesSection, h) {
			t.Errorf("roles table missing %q header", h)
		}
	}
	// Priority order in the table: Leader, Officer, Member, Recruit.
	order := []string{"Leader", "Officer", "Member", "Recruit"}
	last := -1
	for _, r := range order {
		i := strings.Index(rolesSection, "\n  "+r+" ")
		if i < 0 {
			t.Errorf("roles table missing row %q", r)
			continue
		}
		if i < last {
			t.Errorf("roles table row %q out of priority order", r)
		}
		last = i
	}
	// Leader row: all four permissions checked; Recruit row: none.
	leaderLine := lineWith(rolesSection, "  Leader ")
	if strings.Count(leaderLine, "[x]") != 4 {
		t.Errorf("Leader row should have 4 checked perms; got: %q", leaderLine)
	}
	officerLine := lineWith(rolesSection, "  Officer ")
	if strings.Count(officerLine, "[x]") != 3 || strings.Count(officerLine, "[ ]") != 1 {
		t.Errorf("Officer row should have 3 checked / 1 unchecked; got: %q", officerLine)
	}
	recruitLine := lineWith(rolesSection, "  Recruit ")
	if strings.Count(recruitLine, "[ ]") != 4 {
		t.Errorf("Recruit row should have 4 unchecked perms; got: %q", recruitLine)
	}

	// 4. Allies section present with both allies.
	if !strings.Contains(out, "Explorers Guild") || !strings.Contains(out, "Data Bots") {
		t.Errorf("allies section missing expected factions")
	}

	// 5. Facilities section: grouped by base, alphabetical, service + status.
	facIdx := strings.Index(out, "\nFacilities (3):\n")
	if facIdx < 0 {
		t.Fatal("no Facilities section (expected 3 facilities)")
	}
	facSection := out[facIdx:]
	if !strings.Contains(facSection, "  grand_exchange_station:") {
		t.Errorf("facilities not grouped under base header")
	}
	// Alphabetical within the base: Faction Fuel Bunker < Faction Warehouse < Iron Refinery.
	iFuel := strings.Index(facSection, "Faction Fuel Bunker")
	iWarehouse := strings.Index(facSection, "Faction Warehouse")
	iRefinery := strings.Index(facSection, "Iron Refinery")
	if iFuel < 0 || iWarehouse < 0 || iRefinery < 0 || iFuel >= iWarehouse || iWarehouse >= iRefinery {
		t.Errorf("facilities not alphabetical (fuel=%d warehouse=%d refinery=%d)", iFuel, iWarehouse, iRefinery)
	}
	// Refinery has no faction_service → rendered as "-".
	refineryLine := lineWith(facSection, "Iron Refinery")
	if !strings.Contains(refineryLine, "| - ") && !strings.Contains(refineryLine, "| -|") {
		t.Errorf("refinery service should render '-'; got: %q", refineryLine)
	}
	if !strings.Contains(refineryLine, "active") {
		t.Errorf("refinery should show active status; got: %q", refineryLine)
	}
	if !strings.Contains(facSection, "faction_storage") {
		t.Errorf("warehouse faction_service missing from facilities section")
	}
}
