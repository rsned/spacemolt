package main

import "fmt"

// fixerAccount is the shared fixer-network account (OpenAPI FixerAccountInfo)
// that get_missions, get_active_missions and the accept/abandon/complete
// replies carry at the top level as "fixer". All values are credits.
//
//   - allowance: max unsecured courier-cargo exposure; starts 2,500, +250 per
//     successful courier delivery, caps at 10,000.
//   - exposure: debt + base value of provided cargo on active courier missions.
//   - available: max(0, allowance - exposure); 0 = no more courier cargo.
//   - debt: unpaid missing-courier-cargo liability; pay via
//     send_gift recipient="fixer:tab" or restitution missions.
type fixerAccount struct {
	Allowance int64 `json:"allowance"`
	Available int64 `json:"available"`
	Debt      int64 `json:"debt"`
	Exposure  int64 `json:"exposure"`
}

// formatFixer renders the account as one line, or "" when the reply had none.
func formatFixer(f *fixerAccount) string {
	if f == nil {
		return ""
	}
	line := fmt.Sprintf("Fixer: available %s of %s allowance · exposure %s · debt %s",
		commaInt(f.Available), commaInt(f.Allowance), commaInt(f.Exposure), commaInt(f.Debt))
	switch {
	case f.Debt > 0:
		line += fmt.Sprintf("  ⚠ DEBT — pay with send_gift recipient=\"fixer:tab\" credits=%d, or run restitution missions", f.Debt)
	case f.Available <= 0:
		line += "  ⚠ no allowance left — courier cargo missions blocked"
	}
	return line + "\n"
}
