package serverapi

import (
	"encoding/json"
	"testing"
)

// Live 2026-11-13 at Conjunction Junction: the dock reply lists ships still
// being built for us at this station (commissions_active), alongside the
// existing commissions_ready.
func TestDockResponseDecodesActiveCommissions(t *testing.T) {
	const payload = `{"action":"dock","commissions_active":[
		{"class_id":"congregation","commission_id":"3ea9c80ab7035b5ceba28f71888d5182","eta_seconds":350,"ship_class":"Congregation","status":"building","ticks_remaining":35},
		{"class_id":"congregation","commission_id":"78322a909b5b5cf85e427cc440338384","eta_seconds":410,"ship_class":"Congregation","status":"building","ticks_remaining":41}]}`

	var got DockResponse
	if err := json.Unmarshal([]byte(payload), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.CommissionsActive) != 2 {
		t.Fatalf("len(CommissionsActive) = %d, want 2", len(got.CommissionsActive))
	}
	c := got.CommissionsActive[1]
	if c.ClassID != "congregation" || c.CommissionID != "78322a909b5b5cf85e427cc440338384" || c.ETASeconds != 410 ||
		c.ShipClass != "Congregation" || c.Status != "building" || c.TicksRemaining != 41 {
		t.Errorf("commission = %+v", c)
	}
}
