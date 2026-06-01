package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// formatGetInsuranceQuote renders a get_insurance_quote response: the headline
// premium / coverage / fitted value plus the per-factor risk breakdown. Returns
// "" if the payload can't be parsed (caller falls back to JSON). Handles both
// the flat OK payload and an action_result-wrapped frame.
func formatGetInsuranceQuote(raw []byte) string {
	var resp serverapi.GetInsuranceQuoteResponse
	if err := json.Unmarshal(unwrapActionResult(raw), &resp); err != nil {
		return ""
	}
	q := resp.Quote

	var b strings.Builder
	b.WriteString("=== Insurance Quote ===\n")
	if resp.Message != "" {
		fmt.Fprintf(&b, "%s\n", resp.Message)
	}

	if q.Refused {
		fmt.Fprintf(&b, "Status: REFUSED (risk score %.2f)\n", q.RiskScore)
		if resp.Notice != "" {
			fmt.Fprintf(&b, "Notice: %s\n", resp.Notice)
		}
		return b.String()
	}

	if q.Coverage > 0 {
		fmt.Fprintf(&b, "Coverage:     %d cr\n", q.Coverage)
	}
	if q.Premium > 0 {
		fmt.Fprintf(&b, "Premium:      %d cr\n", q.Premium)
	}
	if q.FittedValue > 0 {
		fmt.Fprintf(&b, "Fitted value: %d cr\n", q.FittedValue)
	}
	if q.RiskScore > 0 {
		fmt.Fprintf(&b, "Risk score:   %.2f\n", q.RiskScore)
	}
	if q.ExpiresIn != "" {
		fmt.Fprintf(&b, "Valid for:    %s\n", q.ExpiresIn)
	}

	if len(q.Factors) > 0 {
		b.WriteString("\nRisk factors:\n")
		for _, f := range q.Factors {
			// Factors may carry a name, a detail, or both — render whichever
			// are present rather than a rigid table with blank columns.
			label := f.Name
			switch {
			case f.Name != "" && f.Detail != "":
				label = f.Name + " — " + f.Detail
			case f.Name == "":
				label = f.Detail
			}
			fmt.Fprintf(&b, "  %5.2fx  %s\n", f.Multiplier, label)
		}
	}

	if resp.Notice != "" {
		fmt.Fprintf(&b, "\nNotice: %s\n", resp.Notice)
	}

	return b.String()
}
