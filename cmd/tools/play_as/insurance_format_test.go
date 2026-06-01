package main

import (
	"strings"
	"testing"
)

// TestFormatGetInsuranceQuote_RendersQuote guards that the styled formatter
// surfaces the quote's premium / coverage / fitted value and the per-factor
// breakdown, using the real get_insurance_quote response shape.
func TestFormatGetInsuranceQuote_RendersQuote(t *testing.T) {
	// Real server shape: expires_in is a human string ("7 days"), and some
	// factors carry only a detail (no name).
	raw := []byte(`{"message":"Insurance quote for your Census (fitted value: 33739 credits)",` +
		`"quote":{"coverage":33739,"premium":1687,"fitted_value":33739,"risk_score":0.42,"expires_in":"7 days",` +
		`"factors":[` +
		`{"detail":"Base premium: 5% of fitted value (33739 credits)","multiplier":1.0},` +
		`{"name":"Ship Condition","detail":"Hull at 92%","multiplier":1.05}` +
		`]}}`)

	out := formatGetInsuranceQuote(raw)

	for _, want := range []string{
		"Insurance Quote",
		"33739",   // fitted value / coverage
		"1687",    // premium
		"7 days",  // expires_in as string
		"Ship Condition",
		"Hull at 92%",
		"Base premium: 5% of fitted value", // detail-only factor still renders
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

// TestFormatGetInsuranceQuote_UnwrapsActionResult guards that a quote nested in
// an action_result frame ({"command","result","tick"}) still renders.
func TestFormatGetInsuranceQuote_UnwrapsActionResult(t *testing.T) {
	raw := []byte(`{"command":"get_insurance_quote","tick":42,"result":{` +
		`"message":"Insurance quote","quote":{"coverage":30000,"premium":4200,"fitted_value":33535}}}`)

	out := formatGetInsuranceQuote(raw)
	if !strings.Contains(out, "33535") || !strings.Contains(out, "4200") {
		t.Errorf("action_result frame not unwrapped:\n%s", out)
	}
}

// TestFormatGetInsuranceQuote_Refused guards a refused quote renders a clear
// REFUSED line rather than blank zero fields.
func TestFormatGetInsuranceQuote_Refused(t *testing.T) {
	raw := []byte(`{"message":"no deal","quote":{"refused":true,"risk_score":0.99}}`)
	out := formatGetInsuranceQuote(raw)
	if !strings.Contains(strings.ToUpper(out), "REFUSED") {
		t.Errorf("refused quote should say REFUSED:\n%s", out)
	}
}
