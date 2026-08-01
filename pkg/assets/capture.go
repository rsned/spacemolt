package assets

import (
	"context"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// CaptureProfile refreshes one agent's identity, profile, skills, standings,
// carrier standing and hulls, then materialises its capabilities.
//
// Every source degrades independently: a failed call means that table keeps
// its previous captured_at and is simply not refreshed. Nothing here returns
// an error for a source failure, because asset capture must never become a new
// way for a worker pass to fail (the same rule pkg/worker/mission.go states for
// freight). Only a store-write failure propagates.
func CaptureProfile(ctx context.Context, client game.GameClient, st *Store, agentID string, now time.Time) error {
	if st == nil || client == nil {
		return nil
	}

	// GetStatus must be called explicitly rather than trusting ambient state:
	// Standings ride only on a FULL player payload, and the client preserves
	// the previous map when a partial one arrives. Skipping this call would
	// persist arbitrarily stale standings under a fresh timestamp. GetStatus
	// is synchronous, so a failure here means GetState() would still return
	// the previously cached Player (ID intact from login): recording that
	// under captured_at=now would be indistinguishable from a fresh capture,
	// which is the exact failure this ledger exists to catch. Bail out like
	// every other source failure and leave every table at its previous
	// captured_at.
	if err := client.GetStatus(ctx); err != nil {
		return nil //nolint:nilerr // a source failure must never fail the capture pass; every table keeps its previous captured_at
	}

	state := client.GetState()
	if state == nil || state.Player.ID == "" {
		return nil // nothing identifiable to record
	}
	p := state.Player
	playerID := p.ID

	if err := st.UpsertIdentity(ctx, Identity{
		PlayerID: playerID, AgentID: agentID, Username: p.Username,
	}, now); err != nil {
		return err
	}

	if err := st.UpsertProfile(ctx, Profile{
		PlayerID: playerID, Username: p.Username, Empire: p.Empire,
		Credits: p.Credits, HomeBase: p.HomeBase, DockedAtBase: p.DockedAtBase,
		CurrentSystem: state.CurrentSystem, CurrentPOI: state.CurrentPOI,
		ActiveShipID: p.CurrentShipID, FactionID: p.FactionID,
		FactionRank: p.FactionRank, Experience: p.Experience, CapturedAt: now,
	}); err != nil {
		return err
	}

	skills := make([]SkillRow, 0, len(p.Skills))
	skillMap := make(map[string]SkillRow, len(p.Skills))
	for name, sk := range p.Skills {
		row := SkillRow{Skill: name, Level: sk.Level, XP: sk.XP}
		skills = append(skills, row)
		skillMap[name] = row
	}
	if err := st.ReplaceSkills(ctx, playerID, skills, now); err != nil {
		return err
	}

	standings := make([]StandingRow, 0, len(p.Standings))
	standingMap := make(map[string]StandingRow, len(p.Standings))
	for name, sd := range p.Standings {
		row := StandingRow{
			Faction: name, Reputation: sd.Reputation, Baseline: sd.Baseline,
			OutstandingBounty: sd.OutstandingBounty, JailedUntil: sd.JailedUntil,
		}
		standings = append(standings, row)
		standingMap[name] = row
	}
	if err := st.ReplaceStandings(ctx, playerID, standings, now); err != nil {
		return err
	}

	// Carrier: a failed call or an undecodable body leaves agent_carrier
	// untouched. Writing a zero row instead would read as a debt-free
	// probationary carrier, which is worse than no data.
	var (
		carrier      Carrier
		carrierKnown bool
	)
	if err := client.ShippingProfile(ctx); err == nil {
		if c, ok, derr := CarrierFrom(client.GetRawJSON("shipping_profile")); derr == nil && ok {
			carrier, carrierKnown = c, true
			if err := st.UpsertCarrier(ctx, playerID, c, now); err != nil {
				return err
			}
		}
	}

	// Hulls: gate the write on whether the call and decode succeeded, NOT on
	// how many hulls came back. A ListShips error or an undecodable body
	// leaves agent_hulls untouched, same as carrier above. But a clean call
	// that legitimately reports zero owned ships must still replace the set
	// - otherwise a sold last ship leaves a stale, phantom hull (including a
	// stale is_active row) that Task 6's activeHull() would keep reading as
	// capability forever.
	var hulls []Hull
	if err := client.ListShips(ctx); err == nil {
		if hs, derr := HullsFrom(client.GetRawJSON("owned_ships")); derr == nil {
			hulls = hs
			if err := st.ReplaceHulls(ctx, playerID, hs, now); err != nil {
				return err
			}
		}
	}

	return st.ReplaceCapabilities(ctx, playerID, Evaluate(AgentSnapshot{
		Profile:      Profile{PlayerID: playerID, Credits: p.Credits},
		Skills:       skillMap,
		Standings:    standingMap,
		Carrier:      carrier,
		CarrierKnown: carrierKnown,
		Hulls:        hulls,
	}), now)
}
