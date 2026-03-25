package actionspace

import "testing"

func TestPreconditionsDocked(t *testing.T) {
	docked := &GameContext{Docked: true}
	undocked := &GameContext{}
	transit := &GameContext{InTransit: true}

	if !RequiresDocked.Check(docked) {
		t.Error("RequiresDocked should pass when docked")
	}
	if RequiresDocked.Check(undocked) {
		t.Error("RequiresDocked should fail when undocked")
	}
	if !RequiresUndocked.Check(undocked) {
		t.Error("RequiresUndocked should pass when undocked")
	}
	if RequiresUndocked.Check(transit) {
		t.Error("RequiresUndocked should fail when in transit")
	}
	if RequiresNotInTransit.Check(transit) {
		t.Error("RequiresNotInTransit should fail when in transit")
	}
}

func TestPreconditionsResources(t *testing.T) {
	gc := &GameContext{
		Fuel: 50, MaxFuel: 100, Hull: 80, MaxHull: 100,
		CargoUsed: 10, CargoCapacity: 50, Credits: 1000, CargoItemCount: 3,
	}
	if !RequiresFuel.Check(gc) {
		t.Error("RequiresFuel should pass")
	}
	if !RequiresCargoSpace.Check(gc) {
		t.Error("RequiresCargoSpace should pass")
	}
	if !RequiresCredits.Check(gc) {
		t.Error("RequiresCredits should pass")
	}
	if !RequiresDamaged.Check(gc) {
		t.Error("RequiresDamaged should pass")
	}
	if !RequiresHasCargo.Check(gc) {
		t.Error("RequiresHasCargo should pass")
	}

	if RequiresCargoSpace.Check(&GameContext{CargoUsed: 50, CargoCapacity: 50}) {
		t.Error("RequiresCargoSpace should fail when full")
	}
	if RequiresCredits.Check(&GameContext{}) {
		t.Error("RequiresCredits should fail with 0")
	}
	if RequiresDamaged.Check(&GameContext{Hull: 100, MaxHull: 100}) {
		t.Error("RequiresDamaged should fail at full hull")
	}
}

func TestPreconditionsCombat(t *testing.T) {
	if !RequiresInCombat.Check(&GameContext{InCombat: true}) {
		t.Error("should pass in combat")
	}
	if !RequiresInCombat.Check(&GameContext{InBattle: true}) {
		t.Error("should pass in battle")
	}
	if RequiresInCombat.Check(&GameContext{}) {
		t.Error("should fail when peaceful")
	}
	if !RequiresNotInCombat.Check(&GameContext{}) {
		t.Error("should pass when peaceful")
	}
}

func TestPreconditionsMiningPOI(t *testing.T) {
	for _, poiType := range []string{"asteroid_belt", "asteroid_field", "asteroid", "ice_field", "gas_cloud"} {
		if !RequiresMiningPOI.Check(&GameContext{CurrentPOIType: poiType}) {
			t.Errorf("should pass at %s", poiType)
		}
	}
	for _, poiType := range []string{"station", "planet", ""} {
		if RequiresMiningPOI.Check(&GameContext{CurrentPOIType: poiType}) {
			t.Errorf("should fail at %q", poiType)
		}
	}
}

func TestPreconditionsPlayerStatus(t *testing.T) {
	if !RequiresFaction.Check(&GameContext{HasFaction: true}) {
		t.Error("RequiresFaction should pass")
	}
	if RequiresFaction.Check(&GameContext{}) {
		t.Error("RequiresFaction should fail")
	}
	if !RequiresNearbyPlayers.Check(&GameContext{NearbyPlayerCount: 1}) {
		t.Error("RequiresNearbyPlayers should pass")
	}
	if !RequiresWrecks.Check(&GameContext{WreckCount: 1}) {
		t.Error("RequiresWrecks should pass")
	}
	if !RequiresTowingWreck.Check(&GameContext{TowingWreck: true}) {
		t.Error("RequiresTowingWreck should pass")
	}
	if !RequiresStoredShips.Check(&GameContext{StoredShipCount: 1}) {
		t.Error("RequiresStoredShips should pass")
	}
}
