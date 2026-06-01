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

	fmt.Fprintf(&b, "Premium:      %d cr   |   Coverage: %d cr\n", q.Premium, q.Coverage)
	if q.FittedValue > 0 {
		fmt.Fprintf(&b, "Fitted value: %d cr", q.FittedValue)
		if q.RiskScore > 0 {
			fmt.Fprintf(&b, "   |   Risk score: %.2f", q.RiskScore)
		}
		b.WriteString("\n")
	} else if q.RiskScore > 0 {
		fmt.Fprintf(&b, "Risk score:   %.2f\n", q.RiskScore)
	}
	if q.ExpiresIn > 0 {
		fmt.Fprintf(&b, "Quote valid for: %d ticks\n", q.ExpiresIn)
	}

	if len(q.Factors) > 0 {
		b.WriteString("\nRisk factors:\n")
		fmt.Fprintf(&b, "  %-18s  %-7s  %s\n", "Factor", "Mult", "Detail")
		fmt.Fprintf(&b, "  %-18s  %-7s  %s\n", "------------------", "-------", "------")
		for _, f := range q.Factors {
			fmt.Fprintf(&b, "  %-18s  %5.2fx  %s\n", f.Name, f.Multiplier, f.Detail)
		}
	}

	if resp.Notice != "" {
		fmt.Fprintf(&b, "\nNotice: %s\n", resp.Notice)
	}

	return b.String()
}
