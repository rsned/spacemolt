package serverapi

import (
	"encoding/json"
	"testing"
)

// The live 2026-11-12 retaliation hit decodes fully: effect_kind and the
// player-keyed xp_gained map.
func TestBattleDamageRetaliationDecodes(t *testing.T) {
	raw := `{"attacker_id":"repeater_383b6aeca7f9224a7eb52dfca1da288b","attacker_name":"Repeater Pane","damage_type":"em","effect_kind":"retaliation","hit_success":true,"hull_hit":0,"shield_hit":0,"target_id":"a50924913cef881c5e4d14257589d9ba","target_name":"Arthur 'Artificer' Artis","tick":2388274,"total_damage":0,"weapons_fired":[],"xp_gained":{"a50924913cef881c5e4d14257589d9ba":{"piloting":4,"tactics":2}}}`
	var ev BattleDamage
	if err := json.Unmarshal([]byte(raw), &ev); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if ev.EffectKind != "retaliation" {
		t.Errorf("EffectKind = %q", ev.EffectKind)
	}
	if got := ev.XPGained["a50924913cef881c5e4d14257589d9ba"]["piloting"]; got != 4 {
		t.Errorf("piloting xp = %d, want 4", got)
	}
}
