package assets

import (
	"context"
	"testing"
	"time"
)

// realListReply is a `citizenship list` reply captured from the live server on
// 2026-08-19 for explorer-7 (crimson origin, application to Outer Rim pending).
const realListReply = `{
  "origin": "crimson",
  "citizenships": [
    {"empire_id":"crimson","granted_at":"2026-02-08T08:51:32.482569229Z","granted_by":"origin"}
  ],
  "empires": [
    {"auto_approve":false,"eligible":false,"empire_id":"solarian","empire_name":"Solarian Confederacy",
     "exclusive":false,"fee":5000,"has_pending":false,
     "ineligible_reason":"balance below minimum (6474 < 25000)","is_citizen":false,
     "min_balance":25000,"min_reputation":40,"open":true,"your_reputation":10},
    {"auto_approve":true,"eligible":true,"empire_id":"voidborn","empire_name":"Voidborn Collective",
     "exclusive":false,"fee":0,"has_pending":false,"is_citizen":false,
     "min_balance":0,"min_reputation":0,"open":true,"your_reputation":10},
    {"auto_approve":false,"eligible":false,"empire_id":"crimson","empire_name":"Crimson Pact",
     "exclusive":true,"fee":10000,"has_pending":false,"is_citizen":true,
     "min_balance":50000,"min_reputation":50,"open":true,"your_reputation":10},
    {"auto_approve":false,"eligible":true,"empire_id":"outerrim","empire_name":"Outer Rim Explorers",
     "exclusive":false,"fee":0,"has_pending":true,"is_citizen":false,
     "min_balance":0,"min_reputation":0,"open":true,"your_reputation":10}
  ],
  "pending_petitions": [
    {"created_at":"2026-08-19T00:38:00.888442Z","credits":6474,"empire_id":"outerrim",
     "fee_paid":0,"held_citizenships":["crimson"],
     "id":"afe037f4ca853808bea8bfcb86ff988f","player_home_empire":"crimson",
     "player_id":"daf039b680b03d28155d797feaf64837","player_name":"Nova 'Navigator' Nash",
     "reputation":10,"status":"pending"}
  ]
}`

// TestCitizenshipFrom_SeparatesOriginFromCitizenship pins the distinction the
// whole table exists for: explorer-7's ORIGIN is crimson and it also HOLDS
// crimson, but those are different facts and only the second one is taxable.
func TestCitizenshipFrom_SeparatesOriginFromCitizenship(t *testing.T) {
	snap, ok, err := CitizenshipFrom([]byte(realListReply))
	if err != nil || !ok {
		t.Fatalf("CitizenshipFrom: ok=%v err=%v", ok, err)
	}
	if snap.Origin != "crimson" {
		t.Errorf("origin = %q", snap.Origin)
	}
	if len(snap.Held) != 1 || snap.Held[0].EmpireID != "crimson" {
		t.Fatalf("held = %+v", snap.Held)
	}
	if snap.Held[0].GrantedBy != "origin" {
		t.Errorf("granted_by = %q", snap.Held[0].GrantedBy)
	}
}

// TestCitizenshipFrom_CapturesTheExclusiveFlag: whether the destination empire
// is exclusive decides if a migration needs a follow-up renounce, so it must
// survive the round trip rather than being re-derived from memory.
func TestCitizenshipFrom_CapturesTheExclusiveFlag(t *testing.T) {
	snap, _, err := CitizenshipFrom([]byte(realListReply))
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]CitizenshipPolicy{}
	for _, p := range snap.Policies {
		byID[p.EmpireID] = p
	}
	if !byID["crimson"].Exclusive {
		t.Error("crimson must be exclusive")
	}
	if byID["outerrim"].Exclusive {
		t.Error("outerrim is NOT exclusive; recording it as exclusive would hide the renounce step")
	}
	if !byID["voidborn"].AutoApprove {
		t.Error("voidborn must be auto_approve")
	}
	// Outer Rim gates on nothing, which is what makes a broke agent able to apply.
	if o := byID["outerrim"]; o.Fee != 0 || o.MinBalance != 0 || o.MinReputation != 0 {
		t.Errorf("outerrim gates = %+v, want all zero", o)
	}
}

// TestCitizenshipFrom_RejectsNonListReplies guards the destructive path: apply,
// renounce and withdraw all return the same response type with only a couple of
// fields set. Accepting one as a snapshot would replace the policy table with
// nothing and report the agent as holding no citizenship.
func TestCitizenshipFrom_RejectsNonListReplies(t *testing.T) {
	for _, reply := range []string{
		`{"status":"pending","petition_id":"pet_1","fee_paid":0}`,
		`{"citizenship":{"empire_id":"outerrim","granted_at":"2026-08-19T00:00:00Z"},"renounced":["crimson"]}`,
		`{}`,
		``,
	} {
		if _, ok, err := CitizenshipFrom([]byte(reply)); ok || err != nil {
			t.Errorf("reply %q accepted as a snapshot (ok=%v err=%v)", reply, ok, err)
		}
	}
}

// TestReplaceCitizenships_DropsRenounced: a renounced citizenship must stop
// appearing, or the table keeps reporting a tax liability the agent no longer
// has. An exclusive grant renounces others server-side without saying which.
func TestReplaceCitizenships_DropsRenounced(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 19, 6, 0, 0, 0, time.UTC)

	if err := st.ReplaceCitizenships(ctx, "p1", []CitizenshipGrant{
		{EmpireID: "crimson", GrantedAt: "2026-02-08T08:51:32Z", GrantedBy: "origin"},
		{EmpireID: "voidborn", GrantedAt: "2026-08-19T05:00:00Z"},
	}, now); err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceCitizenships(ctx, "p1", []CitizenshipGrant{
		{EmpireID: "outerrim", GrantedAt: "2026-08-19T06:00:00Z"},
	}, now); err != nil {
		t.Fatal(err)
	}

	var n int
	if err := st.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM agent_citizenship WHERE player_id='p1'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("rows = %d, want 1 — the old citizenships must be gone", n)
	}
	var empire string
	if err := st.db.QueryRowContext(ctx,
		`SELECT empire_id FROM agent_citizenship WHERE player_id='p1'`).Scan(&empire); err != nil {
		t.Fatal(err)
	}
	if empire != "outerrim" {
		t.Errorf("empire = %q", empire)
	}
}

// TestUpsertCitizenshipPetitions_KeepsDecidedRows: a decided petition eventually
// falls out of the server's recent-decisions window, and losing the row destroys
// the only record of how long the review took. first_seen must not move either.
func TestUpsertCitizenshipPetitions_KeepsDecidedRows(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	filed := time.Date(2026, 8, 19, 0, 38, 0, 0, time.UTC)
	decided := time.Date(2026, 8, 21, 9, 0, 0, 0, time.UTC)

	p := CitizenshipPetition{
		ID: "pet_1", EmpireID: "outerrim", Status: "pending",
		CreatedAt: "2026-08-19T00:38:00Z",
	}
	if err := st.UpsertCitizenshipPetitions(ctx, "p1", []CitizenshipPetition{p}, filed); err != nil {
		t.Fatal(err)
	}

	p.Status = "granted"
	p.Decision = "granted"
	p.DecidedAt = "2026-08-21T09:00:00Z"
	p.DecidedBy = "empire"
	if err := st.UpsertCitizenshipPetitions(ctx, "p1", []CitizenshipPetition{p}, decided); err != nil {
		t.Fatal(err)
	}

	var status, decidedAt, firstSeen string
	if err := st.db.QueryRowContext(ctx,
		`SELECT status, decided_at, first_seen FROM agent_citizenship_petitions WHERE petition_id='pet_1'`).
		Scan(&status, &decidedAt, &firstSeen); err != nil {
		t.Fatal(err)
	}
	if status != "granted" || decidedAt == "" {
		t.Errorf("status=%q decided_at=%q, want the decision recorded", status, decidedAt)
	}
	if firstSeen != rfc3339(filed) {
		t.Errorf("first_seen = %q, want it pinned to %q so latency stays measurable",
			firstSeen, rfc3339(filed))
	}

	// And the decided row must still be findable.
	var n int
	if err := st.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM agent_citizenship_petitions`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("rows = %d, want the decided petition kept", n)
	}
}

// TestPendingPetitions_ListsOnlyUndecided is the operator query: an application
// that has sat at pending for days is the signal that a manual review queue is
// unattended, and a decided one is noise.
func TestPendingPetitions_ListsOnlyUndecided(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 19, 6, 0, 0, 0, time.UTC)

	if err := st.UpsertIdentity(ctx, Identity{
		PlayerID: "p1", AgentID: "explorer-7", Username: "Nova"}, now); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertCitizenshipPetitions(ctx, "p1", []CitizenshipPetition{
		{ID: "pet_open", EmpireID: "outerrim", Status: "pending", CreatedAt: "2026-08-19T00:38:00Z"},
		{ID: "pet_done", EmpireID: "nebula", Status: "rejected", Decision: "rejected",
			CreatedAt: "2026-08-10T00:00:00Z", DecidedAt: "2026-08-11T00:00:00Z"},
	}, now); err != nil {
		t.Fatal(err)
	}

	got, err := st.PendingPetitions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].PetitionID != "pet_open" {
		t.Fatalf("pending = %+v, want only the undecided one", got)
	}
	if got[0].AgentID != "explorer-7" {
		t.Errorf("agent_id = %q, want the join to resolve it", got[0].AgentID)
	}
}
