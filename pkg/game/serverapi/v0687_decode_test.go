package serverapi

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestV0687Decode(t *testing.T) {
	tests := []struct {
		name string
		js   string
		into func() any
		get  func(any) any
		want any
	}{
		{"accept no_expiry", `{"no_expiry":true}`, func() any { return &AcceptMissionResponse{} }, func(v any) any { return v.(*AcceptMissionResponse).NoExpiry }, true},
		{"battle private", `{"private":true}`, func() any { return &BattleLogEntry{} }, func(v any) any { return v.(*BattleLogEntry).Private }, true},
		{"cancel refunded_to", `{"refunded_to":"bob"}`, func() any { return &CancelCommissionResponse{} }, func(v any) any { return v.(*CancelCommissionResponse).RefundedTo }, "bob"},
		{"faction licenses", `{"ship_licenses":[{"ship_class":"x","ship_name":"X","empire":"solarian","acquired_at":"t"}]}`, func() any { return &FactionInfoResponse{} }, func(v any) any { return v.(*FactionInfoResponse).ShipLicenses[0].Empire }, "solarian"},
		{"nearby repeaters", `{"repeater_count":1,"repeaters":[{"repeater_id":"r1","name":"Repeater Pane","ship_class":"pane","hull":5,"max_hull":9,"shield":1,"max_shield":2,"in_battle":true}]}`, func() any { return &GetNearbyResponse{} }, func(v any) any {
			r := v.(*GetNearbyResponse)
			return []any{r.RepeaterCount, r.Repeaters[0].MaxHull, r.Repeaters[0].InBattle}
		}, []any{1, 9, true}},
		{"poi interaction", `{"interaction":{"message":"m","command":"interact_poi"}}`, func() any { return &GetPOIResponse{} }, func(v any) any { return v.(*GetPOIResponse).Interaction.Command }, "interact_poi"},
		{"ship workshop", `{"ship":{},"field_workshop":{"state":"paused","pause_reason":"not_active","queued_jobs":2,"reserved_cargo":7}}`, func() any { return &GetShipResponse{} }, func(v any) any { return v.(*GetShipResponse).FieldWorkshop.ReservedCargo }, 7},
		{"map tier", `{"frontier_tier":2}`, func() any { return &MapSystem{} }, func(v any) any { return v.(*MapSystem).FrontierTier }, 2},
		{"system tier", `{"frontier_tier":1}`, func() any { return &SystemData{} }, func(v any) any { return v.(*SystemData).FrontierTier }, 1},
		{"player refuel_xp", `{"refuel_xp":{"day":"2026-11-10","targets":{"p1":true}}}`, func() any { return &Player{} }, func(v any) any { return v.(*Player).RefuelXP.Targets["p1"] }, true},
		{"stats", `{"archives_copied":1,"cloaked_creatures_killed":2,"frontier_systems_explored":3,"gate_crossings":4,"gate_sites_founded":5,"monoliths_destroyed":6,"repeaters_destroyed":7}`, func() any { return &PlayerStats{} }, func(v any) any {
			s := v.(*PlayerStats)
			return []int{s.ArchivesCopied, s.CloakedCreaturesKilled, s.FrontierSystemsExplored, s.GateCrossings, s.GateSitesFounded, s.MonolithsDestroyed, s.RepeatersDestroyed}
		}, []int{1, 2, 3, 4, 5, 6, 7}},
		{"recipe unlock", `{"requires_unlock":true}`, func() any { return &Recipe{} }, func(v any) any { return v.(*Recipe).RequiresUnlock }, true},
		{"refuel xp", `{"piloting_xp":25}`, func() any { return &RefuelResponse{} }, func(v any) any { return v.(*RefuelResponse).PilotingXP }, 25},
		{"repair xp", `{"piloting_xp":3}`, func() any { return &RepairResponse{} }, func(v any) any { return v.(*RepairResponse).PilotingXP }, 3},
		{"settlement xp", `{"piloting_xp":140}`, func() any { return &ShippingSettlementResponse{} }, func(v any) any { return v.(*ShippingSettlementResponse).PilotingXP }, 140},
		{"contract hazmat", `{"requires_hazmat":true}`, func() any { return &ShipmentContract{} }, func(v any) any { return v.(*ShipmentContract).RequiresHazmat }, true},
		{"listing warning", `{"warning":"w"}`, func() any { return &ShippingListing{} }, func(v any) any { return v.(*ShippingListing).Warning }, "w"},
		{"storage deposits", `{"deposits_blocked":true,"deposits_blocked_reason":"dismantling"}`, func() any { return &ViewFactionStorageResponse{} }, func(v any) any {
			r := v.(*ViewFactionStorageResponse)
			return []any{r.DepositsBlocked, r.DepositsBlockedReason}
		}, []any{true, "dismantling"}},
		{"help", `{"help":"x"}`, func() any { return &HelpResponse{} }, func(v any) any { return string(v.(*HelpResponse).Help) }, `"x"`},
		{"dock gifts", `{"gifts_truncated":true}`, func() any { return &DockResponse{} }, func(v any) any { return v.(*DockResponse).GiftsTruncated }, true},
		{"base cost", `{"outpost_founding_fee":500}`, func() any { return &GetBaseCostResponse{} }, func(v any) any { return v.(*GetBaseCostResponse).OutpostFoundingFee }, int64(500)},
		{"mission rewards", `{"unlock_recipes":["r1"]}`, func() any { return &MissionRewards{} }, func(v any) any { return v.(*MissionRewards).UnlockRecipes }, []string{"r1"}},
		{"active mission", `{"no_expiry":true}`, func() any { return &ActiveMission{} }, func(v any) any { return v.(*ActiveMission).NoExpiry }, true},
		{"base gate", `{"gate":{"far_system_id":"s2","online":true,"phase":4,"phase_cycles":10,"toll":5,"low_supply_warned":true}}`, func() any { return &Base{} }, func(v any) any {
			g := v.(*Base).Gate
			return []any{g.FarSystemID, g.Online, g.Phase, g.PhaseCycles, g.Toll, g.LowSupplyWarned}
		}, []any{"s2", true, 4, 10, 5, true}},
		{"station config gate", `{"gate":{"far_system_id":"s2","far_system_name":"Two","partner_built":false,"built":false,"online":false,"toll":0,"faction_only":true,"phases_total":4,"phase":2,"phase_materials_left":{"iron":3},"upkeep_cycles_covered":9,"entangler_due_in_cycles":1}}`, func() any { return &StationConfigResponse{} }, func(v any) any {
			g := v.(*StationConfigResponse).Gate
			return []any{g.FarSystemName, g.FactionOnly, g.PhasesTotal, g.PhaseMaterialsLeft["iron"], g.UpkeepCyclesCovered, g.EntanglerDueInCycles}
		}, []any{"Two", true, 4, 3, 9, 1}},
		{"interact_poi", `{"poi_id":"p","message":"m","data_copied":true,"items_recovered":{"x":2}}`, func() any { return &InteractPOIResponse{} }, func(v any) any {
			r := v.(*InteractPOIResponse)
			return []any{r.POIID, r.DataCopied, r.ItemsRecovered["x"]}
		}, []any{"p", true, 2}},
		{"player_died hazard cause", `{"cause":"hazard","killer_name":"Radiation"}`, func() any { return &PlayerDied{} }, func(v any) any { return v.(*PlayerDied).Cause }, DeathCauseHazard},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := tt.into()
			if err := json.Unmarshal([]byte(tt.js), v); err != nil {
				t.Fatal(err)
			}
			if got := tt.get(v); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %#v, want %#v", got, tt.want)
			}
		})
	}
}
