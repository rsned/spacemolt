package assets

import (
	"encoding/json"
	"fmt"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// CitizenshipSnapshot is one agent's citizenship position at a moment.
type CitizenshipSnapshot struct {
	// Origin is the immutable birthright. It is recorded alongside the
	// citizenships precisely because the two are constantly conflated: origin
	// gates skills and hulls, citizenship decides who taxes you.
	Origin    string
	Held      []CitizenshipGrant
	Petitions []CitizenshipPetition
	Policies  []CitizenshipPolicy
}

// CitizenshipGrant is a membership currently held.
type CitizenshipGrant struct {
	EmpireID  string
	GrantedAt string
	GrantedBy string
}

// CitizenshipPetition is an application and, once decided, its outcome.
type CitizenshipPetition struct {
	ID         string
	EmpireID   string
	Status     string
	Decision   string
	FeePaid    int64
	Reputation int64
	Credits    int64
	CreatedAt  string
	DecidedAt  string
	DecidedBy  string
}

// CitizenshipPolicy is one empire's advertised citizenship policy.
type CitizenshipPolicy struct {
	EmpireID         string
	EmpireName       string
	IsCitizen        bool
	HasPending       bool
	Open             bool
	Exclusive        bool
	AutoApprove      bool
	Fee              int64
	MinBalance       int64
	MinReputation    int64
	YourReputation   int64
	Eligible         bool
	IneligibleReason string
}

// CitizenshipFrom decodes a `citizenship list` reply.
//
// Returns ok=false for an empty body or a reply carrying no empires summary.
// That second guard matters: every other citizenship sub-action returns the same
// response type with only a couple of fields set, so accepting one of those as a
// snapshot would wipe the policy table and report the agent as holding nothing.
func CitizenshipFrom(raw []byte) (CitizenshipSnapshot, bool, error) {
	if len(raw) == 0 {
		return CitizenshipSnapshot{}, false, nil
	}
	var resp serverapi.CitizenshipResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return CitizenshipSnapshot{}, false, fmt.Errorf("assets: decode citizenship: %w", err)
	}
	if len(resp.Empires) == 0 {
		return CitizenshipSnapshot{}, false, nil
	}

	snap := CitizenshipSnapshot{Origin: resp.Origin}

	var policies []CitizenshipPolicy
	if err := json.Unmarshal(resp.Empires, &policies); err != nil {
		return CitizenshipSnapshot{}, false, fmt.Errorf("assets: decode citizenship empires: %w", err)
	}
	snap.Policies = policies

	if len(resp.Citizenships) > 0 {
		if err := json.Unmarshal(resp.Citizenships, &snap.Held); err != nil {
			return CitizenshipSnapshot{}, false, fmt.Errorf("assets: decode citizenships: %w", err)
		}
	}

	// Pending and recent applications share a shape and a table; what separates
	// them is status, and merging here is what lets a single row be watched
	// across the transition from pending to decided.
	for _, raw := range []json.RawMessage{resp.PendingPetitions, resp.RecentDecisions} {
		if len(raw) == 0 {
			continue
		}
		var batch []CitizenshipPetition
		if err := json.Unmarshal(raw, &batch); err != nil {
			return CitizenshipSnapshot{}, false, fmt.Errorf("assets: decode citizenship petitions: %w", err)
		}
		snap.Petitions = append(snap.Petitions, batch...)
	}

	return snap, true, nil
}

// UnmarshalJSON maps the server's snake_case petition fields.
func (p *CitizenshipPetition) UnmarshalJSON(b []byte) error {
	var w struct {
		ID         string `json:"id"`
		EmpireID   string `json:"empire_id"`
		Status     string `json:"status"`
		Decision   string `json:"decision"`
		FeePaid    int64  `json:"fee_paid"`
		Reputation int64  `json:"reputation"`
		Credits    int64  `json:"credits"`
		CreatedAt  string `json:"created_at"`
		DecidedAt  string `json:"decided_at"`
		DecidedBy  string `json:"decided_by"`
	}
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	*p = CitizenshipPetition(w)

	return nil
}

// UnmarshalJSON maps the server's snake_case grant fields.
func (g *CitizenshipGrant) UnmarshalJSON(b []byte) error {
	var w struct {
		EmpireID  string `json:"empire_id"`
		GrantedAt string `json:"granted_at"`
		GrantedBy string `json:"granted_by"`
	}
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	*g = CitizenshipGrant(w)

	return nil
}

// UnmarshalJSON maps the server's snake_case policy fields.
func (c *CitizenshipPolicy) UnmarshalJSON(b []byte) error {
	var w struct {
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
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	*c = CitizenshipPolicy(w)

	return nil
}
