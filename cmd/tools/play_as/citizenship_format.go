package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// citizenshipPolicy mirrors CitizenshipPolicySummary from the server schema.
// The response carries it as a raw message because the same field is absent
// from every sub-action except list.
type citizenshipPolicy struct {
	EmpireID         string `json:"empire_id"`
	EmpireName       string `json:"empire_name"`
	IsCitizen        bool   `json:"is_citizen"`
	HasPending       bool   `json:"has_pending"`
	Open             bool   `json:"open"`
	Exclusive        bool   `json:"exclusive"`
	AutoApprove      bool   `json:"auto_approve"`
	Fee              int64  `json:"fee"`
	MinBalance       int64  `json:"min_balance"`
	MinReputation    int64  `json:"min_reputation"`
	YourReputation   int64  `json:"your_reputation"`
	Eligible         bool   `json:"eligible"`
	IneligibleReason string `json:"ineligible_reason"`
}

// citizenshipGrant mirrors the Citizenship schema.
type citizenshipGrant struct {
	EmpireID  string `json:"empire_id"`
	GrantedAt string `json:"granted_at"`
	GrantedBy string `json:"granted_by"`
}

// citizenshipPetitionView mirrors CitizenshipPetition.
type citizenshipPetitionView struct {
	ID        string `json:"id"`
	EmpireID  string `json:"empire_id"`
	Status    string `json:"status"`
	Decision  string `json:"decision"`
	FeePaid   int64  `json:"fee_paid"`
	CreatedAt string `json:"created_at"`
	DecidedAt string `json:"decided_at"`
	DecidedBy string `json:"decided_by"`
}

// formatCitizenship renders every citizenship sub-action from one reply.
//
// The distinction the table exists to make visible is origin versus
// citizenship: origin is the immutable birthright that gates empire-restricted
// skills and hulls, while citizenship is the mutable membership that decides
// who taxes you. Reading player.empire as "which empire taxes me" is the
// mistake this output is built to prevent.
func formatCitizenship(raw []byte) string {
	var resp serverapi.CitizenshipResponse
	if err := json.Unmarshal(unwrapActionResult(raw), &resp); err != nil {
		return ""
	}

	var b strings.Builder
	b.WriteString("=== Citizenship ===\n")
	if resp.Origin != "" {
		fmt.Fprintf(&b, "Origin (immutable): %s\n", resp.Origin)
	}
	if s := formatCitizenshipHeld(resp.Citizenships, resp.Citizenship); s != "" {
		b.WriteString(s)
	}
	if s := formatRenounced(resp.Renounced); s != "" {
		b.WriteString(s)
	}
	if resp.FeePaid > 0 {
		fmt.Fprintf(&b, "Fee held in escrow: %d cr\n", resp.FeePaid)
	}
	if resp.FeeRefunded > 0 {
		fmt.Fprintf(&b, "Fee refunded: %d cr\n", resp.FeeRefunded)
	}
	if resp.Status != "" {
		fmt.Fprintf(&b, "Status: %s\n", resp.Status)
	}
	if resp.PetitionID != "" {
		fmt.Fprintf(&b, "Petition id: %s\n", resp.PetitionID)
	}
	if s := formatPetitionList("Pending applications", resp.PendingPetitions); s != "" {
		b.WriteString(s)
	}
	if s := formatPetitionList("Recent decisions", resp.RecentDecisions); s != "" {
		b.WriteString(s)
	}
	if s := formatCitizenshipPolicies(resp.Empires); s != "" {
		b.WriteString(s)
	}
	if s := formatCitizenshipRules(resp.Rules); s != "" {
		b.WriteString(s)
	}
	if resp.Message != "" {
		fmt.Fprintf(&b, "\n%s\n", resp.Message)
	}

	return b.String()
}

// formatCitizenshipHeld lists the memberships currently held. A single
// citizenship (the grant returned by apply) is reported alongside the list so
// an auto-approved grant is not silently dropped.
func formatCitizenshipHeld(rawList, rawOne json.RawMessage) string {
	var held []citizenshipGrant
	if len(rawList) > 0 {
		_ = json.Unmarshal(rawList, &held)
	}
	var one citizenshipGrant
	if len(rawOne) > 0 && json.Unmarshal(rawOne, &one) == nil && one.EmpireID != "" {
		held = append(held, one)
	}
	if len(held) == 0 {
		if len(rawList) == 0 {
			return ""
		}
		return "Citizenships: NONE (stateless — check get_tax_estimate before assuming this is free)\n"
	}
	var b strings.Builder
	b.WriteString("Citizenships held:\n")
	for _, c := range held {
		fmt.Fprintf(&b, "  %-10s granted %s", c.EmpireID, citizenshipStamp(c.GrantedAt))
		if c.GrantedBy != "" {
			fmt.Fprintf(&b, " by %s", c.GrantedBy)
		}
		b.WriteString("\n")
	}

	return b.String()
}

// formatRenounced surfaces the citizenships an exclusive empire dropped on your
// behalf. Exclusivity is applied at grant time by the server, so this is the
// only notice that a membership disappeared.
func formatRenounced(raw json.RawMessage) string {
	var ids []string
	if len(raw) == 0 || json.Unmarshal(raw, &ids) != nil || len(ids) == 0 {
		return ""
	}

	return fmt.Sprintf("Renounced: %s\n", strings.Join(ids, ", "))
}

// formatCitizenshipPolicies renders the per-empire policy table: what each
// empire charges, what it demands, and whether you can apply right now.
//
// The checkbox columns are the policy switches (open / auto-approve /
// exclusive) and the numeric columns are the gates apply is checked against.
// EXCL earns its own column rather than a footnote because it is the field
// that decides whether a migration needs a follow-up renounce: a non-exclusive
// grant leaves the old citizenship in place, and both empires then tax you.
func formatCitizenshipPolicies(raw json.RawMessage) string {
	var policies []citizenshipPolicy
	if len(raw) == 0 || json.Unmarshal(raw, &policies) != nil || len(policies) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("\nEmpire policies:\n")
	fmt.Fprintf(&b, "  %-10s %-3s %-4s %-4s %-4s %-4s %10s %11s %8s %9s  %s\n",
		"EMPIRE", "CIT", "PEND", "OPEN", "AUTO", "EXCL", "FEE", "MIN BAL", "MIN REP", "YOUR REP", "ELIGIBLE")
	for _, p := range policies {
		eligible := "yes"
		if !p.Eligible {
			eligible = "no"
			if p.IneligibleReason != "" {
				eligible += " - " + p.IneligibleReason
			}
		}
		fmt.Fprintf(&b, "  %-10s %-3s %-4s %-4s %-4s %-4s %10s %11s %8d %9d  %s\n",
			p.EmpireID, checkbox(p.IsCitizen), checkbox(p.HasPending),
			checkbox(p.Open), checkbox(p.AutoApprove), checkbox(p.Exclusive),
			groupDigits(p.Fee), groupDigits(p.MinBalance),
			p.MinReputation, p.YourReputation, eligible)
	}
	b.WriteString("  CIT=citizen PEND=application filed AUTO=granted on meeting the gates\n")
	b.WriteString("  EXCL=a grant here auto-renounces every other citizenship you hold\n")

	return b.String()
}

// checkbox renders a policy flag as a box so a row reads at a glance.
func checkbox(on bool) string {
	if on {
		return "[x]"
	}

	return "[ ]"
}

// groupDigits renders a credit figure with thousands separators.
func groupDigits(n int64) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	off := len(s) % 3
	if off > 0 {
		b.WriteString(s[:off])
	}
	for i := off; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s[i : i+3])
	}

	return b.String()
}

// formatPetitionList renders an application queue under the given heading.
func formatPetitionList(heading string, raw json.RawMessage) string {
	var petitions []citizenshipPetitionView
	if len(raw) == 0 || json.Unmarshal(raw, &petitions) != nil || len(petitions) == 0 {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "\n%s:\n", heading)
	for _, p := range petitions {
		state := p.Status
		if p.Decision != "" {
			state = p.Decision
		}
		fmt.Fprintf(&b, "  %-10s %-9s fee %d cr, filed %s",
			p.EmpireID, state, p.FeePaid, citizenshipStamp(p.CreatedAt))
		if p.DecidedAt != "" {
			fmt.Fprintf(&b, ", decided %s", citizenshipStamp(p.DecidedAt))
		}
		if p.DecidedBy != "" {
			fmt.Fprintf(&b, " by %s", p.DecidedBy)
		}
		if p.ID != "" {
			fmt.Fprintf(&b, "\n    id %s", p.ID)
		}
		b.WriteString("\n")
	}

	return b.String()
}

// formatCitizenshipRules prints the server's own policy prose when it sends it.
func formatCitizenshipRules(raw json.RawMessage) string {
	var rules []string
	if len(raw) == 0 || json.Unmarshal(raw, &rules) != nil || len(rules) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("\nRules:\n")
	for _, r := range rules {
		fmt.Fprintf(&b, "  - %s\n", r)
	}

	return b.String()
}

// formatPetition renders the reply to a petition message (empire mail, which is
// unrelated to a citizenship application).
func formatPetition(raw []byte) string {
	var resp serverapi.PetitionResponse
	if err := json.Unmarshal(unwrapActionResult(raw), &resp); err != nil {
		return ""
	}
	name := resp.EmpireName
	if name == "" {
		name = resp.EmpireID
	}
	if name == "" {
		return ""
	}

	return fmt.Sprintf("Petition sent to %s\n%s\n", name, resp.Message)
}

// citizenshipStamp renders an RFC3339 timestamp in the same UTC form the tax
// output uses, passing anything unparseable through untouched.
func citizenshipStamp(s string) string {
	if s == "" {
		return "-"
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return s
	}

	return t.UTC().Format("2006-01-02 15:04 UTC")
}
