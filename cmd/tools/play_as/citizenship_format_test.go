package main

import (
	"strings"
	"testing"
)

// listReply is shaped from the openapi CitizenshipResponse/CitizenshipPolicySummary
// schemas: the empires summary is a list, and every policy field is required.
// listReply is a verbatim `citizenship list` reply captured from the live
// server on 2026-08-18 (origin solarian, 73,409 cr), trimmed to the fields the
// formatter reads. Every policy number below is real, not invented.
const listReply = `{
  "origin": "solarian",
  "citizenships": [{"empire_id": "solarian", "granted_at": "2026-08-08T02:17:22Z", "granted_by": "origin"}],
  "empires": [
    {"auto_approve": false, "eligible": false, "empire_id": "solarian", "empire_name": "Solarian Confederacy",
     "exclusive": false, "fee": 5000, "has_pending": false, "ineligible_reason": "already a citizen",
     "is_citizen": true, "min_balance": 25000, "min_reputation": 40, "open": true, "your_reputation": 20},
    {"auto_approve": true, "eligible": true, "empire_id": "voidborn", "empire_name": "Voidborn Collective",
     "exclusive": false, "fee": 0, "has_pending": false, "is_citizen": false,
     "min_balance": 0, "min_reputation": 0, "open": true, "your_reputation": 10},
    {"auto_approve": false, "eligible": false, "empire_id": "crimson", "empire_name": "Crimson Pact",
     "exclusive": true, "fee": 10000, "has_pending": false,
     "ineligible_reason": "reputation below minimum (10 < 50)",
     "is_citizen": false, "min_balance": 50000, "min_reputation": 50, "open": true, "your_reputation": 10},
    {"auto_approve": false, "eligible": false, "empire_id": "nebula", "empire_name": "Nebula Trade Federation",
     "exclusive": false, "fee": 25000, "has_pending": false,
     "ineligible_reason": "balance below minimum (73409 < 1000000)",
     "is_citizen": false, "min_balance": 1000000, "min_reputation": 0, "open": true, "your_reputation": 10},
    {"auto_approve": false, "eligible": true, "empire_id": "outerrim", "empire_name": "Outer Rim Explorers",
     "exclusive": false, "fee": 0, "has_pending": false, "is_citizen": false,
     "min_balance": 0, "min_reputation": 0, "open": true, "your_reputation": 10}
  ]
}`

// TestFormatCitizenship_ShowsOriginAndCitizenshipSeparately pins the one
// distinction the whole command exists for: origin is the immutable birthright,
// citizenship is the mutable membership that decides who taxes you. Collapsing
// them is how player.empire gets misread as the tax authority.
func TestFormatCitizenship_ShowsOriginAndCitizenshipSeparately(t *testing.T) {
	out := formatCitizenship([]byte(listReply))

	if !strings.Contains(out, "Origin (immutable): solarian") {
		t.Errorf("origin missing:\n%s", out)
	}
	if !strings.Contains(out, "Citizenships held:") || !strings.Contains(out, "solarian   granted 2026-08-08 02:17 UTC") {
		t.Errorf("held citizenship missing or unstamped:\n%s", out)
	}
}

// TestFormatCitizenship_FlagsExclusivity guards the trap that decides whether a
// migration needs a separate renounce step: an exclusive grant silently drops
// every other membership, and a non-exclusive one leaves you taxed by BOTH.
func TestFormatCitizenship_FlagsExclusivity(t *testing.T) {
	out := formatCitizenship([]byte(listReply))

	// Crimson is the exclusive one; outerrim is NOT. That asymmetry is the whole
	// migration plan: leaving crimson for outerrim needs an explicit renounce,
	// because the grant alone will not drop the old citizenship.
	crimson := policyRow(out, "crimson")
	if !strings.Contains(crimson, "[x]") {
		t.Errorf("crimson row has no checked box, want EXCL set:\n%s", crimson)
	}
	outerrim := policyRow(out, "outerrim")
	if strings.HasSuffix(strings.TrimSpace(exclCell(outerrim)), "[x]") {
		t.Errorf("outerrim marked exclusive; the live policy says it is not:\n%s", outerrim)
	}
	if !strings.Contains(out, "EXCL=a grant here auto-renounces every other citizenship you hold") {
		t.Errorf("exclusivity legend missing:\n%s", out)
	}
}

// TestFormatCitizenship_ShowsTheEligibilityGate: apply fails on credits and
// reputation, so the numbers that gate it have to be on screen before the
// mutation is spent.
func TestFormatCitizenship_ShowsTheEligibilityGate(t *testing.T) {
	out := formatCitizenship([]byte(listReply))

	if !strings.Contains(out, "reputation below minimum (10 < 50)") {
		t.Errorf("ineligible reason not surfaced:\n%s", out)
	}
	// Nebula's million-credit floor must render grouped, not as a raw run of
	// digits that no one can read at a glance.
	if !strings.Contains(out, "1,000,000") {
		t.Errorf("min_balance not digit-grouped:\n%s", out)
	}
	// Outer Rim gates on nothing at all: a broke agent can still apply. This is
	// what makes the migration affordable, so it is worth pinning.
	if row := policyRow(out, "outerrim"); !strings.Contains(row, "yes") {
		t.Errorf("outerrim should be eligible with zero credits:\n%s", row)
	}
}

// TestFormatCitizenship_ReportsRenouncedOnGrant: when an exclusive grant lands,
// the renounced list is the ONLY notice that memberships disappeared.
func TestFormatCitizenship_ReportsRenouncedOnGrant(t *testing.T) {
	out := formatCitizenship([]byte(`{
	  "citizenship": {"empire_id": "outerrim", "granted_at": "2026-08-18T20:00:00Z", "granted_by": "auto"},
	  "renounced": ["crimson"],
	  "status": "granted",
	  "fee_paid": 500
	}`))

	if !strings.Contains(out, "Renounced: crimson") {
		t.Errorf("renounced list missing:\n%s", out)
	}
	if !strings.Contains(out, "outerrim   granted 2026-08-18 20:00 UTC by auto") {
		t.Errorf("granted citizenship missing:\n%s", out)
	}
	if !strings.Contains(out, "Status: granted") {
		t.Errorf("status missing:\n%s", out)
	}
}

// TestFormatCitizenship_ReportsPendingReview covers the manual-review path: the
// fee is gone into escrow and nothing is granted, which must not read as success.
func TestFormatCitizenship_ReportsPendingReview(t *testing.T) {
	out := formatCitizenship([]byte(`{
	  "status": "pending",
	  "petition_id": "pet_123",
	  "fee_paid": 500,
	  "pending_petitions": [
	    {"id": "pet_123", "empire_id": "outerrim", "status": "pending", "fee_paid": 500,
	     "created_at": "2026-08-18T20:00:00Z"}
	  ]
	}`))

	if !strings.Contains(out, "Fee held in escrow: 500 cr") {
		t.Errorf("escrow not reported:\n%s", out)
	}
	if !strings.Contains(out, "Pending applications:") || !strings.Contains(out, "outerrim   pending") {
		t.Errorf("pending queue missing:\n%s", out)
	}
}

// TestFormatCitizenship_StatelessIsNotFree: holding zero citizenships is a real
// state the server allows, and the docs warn it does not mean paying nothing.
func TestFormatCitizenship_StatelessIsNotFree(t *testing.T) {
	out := formatCitizenship([]byte(`{"origin": "crimson", "citizenships": []}`))

	if !strings.Contains(out, "NONE (stateless") {
		t.Errorf("stateless state not reported:\n%s", out)
	}
}

// TestFormatCitizenship_BadPayloadFallsBackToJSON: an unparseable reply must
// return "" so the caller prints raw JSON rather than an empty screen.
func TestFormatCitizenship_BadPayloadFallsBackToJSON(t *testing.T) {
	if out := formatCitizenship([]byte(`not json`)); out != "" {
		t.Errorf("bad payload rendered %q, want empty so the caller falls back", out)
	}
}

// TestFormatPetition_IsNotCitizenship guards against conflating the two: the
// petition command sends mail to an empire and grants nothing.
func TestFormatPetition_IsNotCitizenship(t *testing.T) {
	out := formatPetition([]byte(`{"empire_id":"outerrim","empire_name":"Outer Rim Coalition","message":"Received."}`))

	if !strings.Contains(out, "Petition sent to Outer Rim Coalition") {
		t.Errorf("petition ack missing:\n%s", out)
	}
	if strings.Contains(out, "Citizenship") {
		t.Errorf("petition output implies citizenship:\n%s", out)
	}
}

// policyRow returns the rendered table line for one empire.
func policyRow(out, empire string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), empire+" ") {
			return line
		}
	}

	return ""
}

// exclCell returns the EXCL checkbox from a policy row: it is the fifth and
// last box on the line.
func exclCell(row string) string {
	i := strings.LastIndex(row, "[")
	if i < 0 {
		return ""
	}

	return row[i:]
}
