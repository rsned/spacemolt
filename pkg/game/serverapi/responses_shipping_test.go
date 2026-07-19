package serverapi

import (
	"encoding/json"
	"testing"
)

func TestShippingContractResponseDecodes(t *testing.T) {
	// Shape from openapi.json ShippingContractResponse (action=accept).
	raw := []byte(`{
	  "action":"accept",
	  "contract":{
	    "id":"shp_1","package_id":"pkg_1",
	    "shipper":{"kind":"station","id":"sol_station"},
	    "recipient":{"kind":"station","id":"procyon_station"},
	    "contractor":{"kind":"player","id":"engineer-2"},
	    "origin_base_id":"sol_station","destination_base_id":"procyon_station",
	    "shipping_house_id":"house_1","visibility":"public","service_level":"standard",
	    "status":"in_transit","posted_at":"2026-07-19T20:00:00Z",
	    "listing_expires_at":"2026-07-19T22:00:00Z","accepted_tick":1000,
	    "target_tick":1200,"deadline_tick":1400,
	    "base_reward":5000,"max_speed_bonus":1000,"service_fee":50,
	    "reward_escrow":5000,"speed_bonus_escrow":1000,"failure_debt":2000,
	    "appraised_value":12000,"covered_value":0,"premium":0,"reserved_exposure":2000,
	    "policy_status":"none","insurable":true,"risk_band":"probationary",
	    "route_hops":3,"reputation_eligible":true
	  }
	}`)
	var resp ShippingContractResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Action != "accept" {
		t.Fatalf("action = %q, want accept", resp.Action)
	}
	c := resp.Contract
	if c.ID != "shp_1" || c.PackageID != "pkg_1" {
		t.Fatalf("ids: %q / %q", c.ID, c.PackageID)
	}
	if c.DestinationBaseID != "procyon_station" || c.Status != "in_transit" {
		t.Fatalf("dest/status: %q / %q", c.DestinationBaseID, c.Status)
	}
	if c.DeadlineTick != 1400 || c.TargetTick != 1200 {
		t.Fatalf("ticks: %d / %d", c.DeadlineTick, c.TargetTick)
	}
	if c.BaseReward != 5000 || c.MaxSpeedBonus != 1000 {
		t.Fatalf("reward: %d / %d", c.BaseReward, c.MaxSpeedBonus)
	}
	if c.Shipper == nil || c.Shipper.Kind != "station" || c.Shipper.ID != "sol_station" {
		t.Fatalf("shipper: %+v", c.Shipper)
	}
	if c.Contractor == nil || c.Contractor.ID != "engineer-2" {
		t.Fatalf("contractor: %+v", c.Contractor)
	}
}

func TestShippingListResponseDecodes(t *testing.T) {
	raw := []byte(`{
	  "action":"list","page":1,"per_page":20,"total":1,
	  "empty_reason":"","empty_reason_code":"",
	  "shipments":[{
	    "eligible":true,"reason":"",
	    "contract":{"id":"shp_9","package_id":"pkg_9",
	      "shipper":{"kind":"station","id":"a"},"recipient":{"kind":"station","id":"b"},
	      "origin_base_id":"a","destination_base_id":"b","shipping_house_id":"h",
	      "visibility":"public","service_level":"standard","status":"posted",
	      "posted_at":"2026-07-19T20:00:00Z","listing_expires_at":"2026-07-19T22:00:00Z",
	      "deadline_tick":1400,"base_reward":3000,"max_speed_bonus":0,"service_fee":0,
	      "reward_escrow":3000,"speed_bonus_escrow":0,"failure_debt":1000,
	      "reserved_exposure":1000,"policy_status":"none","insurable":false,
	      "risk_band":"probationary","route_hops":2}
	  }]
	}`)
	var resp ShippingListResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Total != 1 || len(resp.Shipments) != 1 {
		t.Fatalf("total/len: %d / %d", resp.Total, len(resp.Shipments))
	}
	l := resp.Shipments[0]
	if !l.Eligible || l.Contract.ID != "shp_9" || l.Contract.BaseReward != 3000 {
		t.Fatalf("listing: %+v", l)
	}
}

func TestShippingProfileResponseDecodes(t *testing.T) {
	raw := []byte(`{
	  "action":"profile","debt_blocks_acceptance":false,"debt_block_reason":"",
	  "profile":{"actor":{"kind":"player","id":"engineer-2"},"tier":"probationary",
	    "successful_deliveries":3,"delivered_value":15000,"priority_deliveries":0,
	    "returns":0,"breaches":0,"defaults":0,"active_contracts":1,
	    "active_liability":2000,"outstanding_debt":0,"updated_at":"2026-07-19T20:00:00Z"},
	  "capacity":{"active_contracts":1,"active_contracts_unlimited":false,
	    "active_liability":2000,"liability_unlimited":false,"active_contract_limit":3,
	    "aggregate_liability_limit":50000,"remaining_aggregate_liability":48000,
	    "single_package_liability_limit":20000},
	  "progression":{"current_tier":"probationary","next_tier":"licensed",
	    "at_maximum_tier":false,"successful_deliveries":3,"required_successful_deliveries":10,
	    "remaining_successful_deliveries":7,"delivered_value":15000,
	    "required_delivered_value":50000,"remaining_delivered_value":35000},
	  "debts":[]
	}`)
	var resp ShippingProfileResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.DebtBlocksAcceptance {
		t.Fatalf("want not debt-blocked")
	}
	if resp.Profile.Tier != "probationary" || resp.Profile.ActiveContracts != 1 {
		t.Fatalf("profile: %+v", resp.Profile)
	}
	if resp.Capacity.SinglePackageLiabilityLimit != 20000 {
		t.Fatalf("single-package limit: %d", resp.Capacity.SinglePackageLiabilityLimit)
	}
	if resp.Progression.RemainingSuccessfulDeliveries != 7 {
		t.Fatalf("progression: %+v", resp.Progression)
	}
}

func TestShippingSettlementResponseDecodes(t *testing.T) {
	raw := []byte(`{"action":"deliver","carrier_payout":5200,"claim_paid":0,
	  "debt_created":0,"shipper_refund":0,
	  "contract":{"id":"shp_1","package_id":"pkg_1",
	    "shipper":{"kind":"station","id":"a"},"recipient":{"kind":"station","id":"b"},
	    "origin_base_id":"a","destination_base_id":"b","shipping_house_id":"h",
	    "visibility":"public","service_level":"standard","status":"delivered",
	    "posted_at":"2026-07-19T20:00:00Z","listing_expires_at":"2026-07-19T22:00:00Z",
	    "deadline_tick":1400,"base_reward":5000,"max_speed_bonus":1000,"service_fee":50,
	    "reward_escrow":0,"speed_bonus_escrow":0,"failure_debt":0,"reserved_exposure":0,
	    "policy_status":"released","insurable":true,"risk_band":"probationary","route_hops":3}}`)
	var resp ShippingSettlementResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.CarrierPayout != 5200 || resp.Contract.Status != "delivered" {
		t.Fatalf("settlement: payout=%d status=%q", resp.CarrierPayout, resp.Contract.Status)
	}
}
