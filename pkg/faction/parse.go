// Package faction collects comprehensive faction data from a connected game
// client and persists it to the knowledge base for the faction dashboard.
package faction

import (
	"time"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// parseFuelBunkers converts the faction_info galaxy-wide fuel-bunker summary
// (gameserver v0.346.0+) into KB rows. Empty when the response carries none
// (e.g. faction_info for a faction we are not a member of).
func parseFuelBunkers(info serverapi.FactionInfoResponse) []knowledge.FactionFuelBunkerRow {
	if len(info.FuelBunkers) == 0 {
		return nil
	}
	now := time.Now()
	out := make([]knowledge.FactionFuelBunkerRow, 0, len(info.FuelBunkers))
	for _, b := range info.FuelBunkers {
		out = append(out, knowledge.FactionFuelBunkerRow{
			FactionID:    info.ID,
			BaseID:       b.BaseID,
			BaseName:     b.BaseName,
			FuelReserve:  b.FuelReserve,
			FuelCapacity: b.FuelCapacity,
			CapturedAt:   now,
		})
	}
	return out
}

// parseFactionInfo converts a faction_info response into the KB header record,
// members, and relation edges (allies, enemies, wars, peace proposals).
func parseFactionInfo(info serverapi.FactionInfoResponse) (knowledge.FactionRecord, []knowledge.FactionMember, []knowledge.FactionRelation) {
	now := time.Now()
	rec := knowledge.FactionRecord{
		FactionID:      info.ID,
		Name:           info.Name,
		Tag:            info.Tag,
		LeaderID:       info.LeaderID,
		LeaderUsername: info.LeaderUsername,
		Treasury:       info.Treasury,
		MemberCount:    info.MemberCount,
		OwnedBases:     info.OwnedBases,
		Description:    info.Description,
		Charter:        info.Charter,
		Emblem:         info.Emblem,
		PrimaryColor:   info.PrimaryColor,
		SecondaryColor: info.SecondaryColor,
		FoundedUTC:     info.CreatedAt,
		CapturedAt:     now,
	}

	members := make([]knowledge.FactionMember, 0, len(info.Members))
	for _, m := range info.Members {
		members = append(members, knowledge.FactionMember{
			FactionID:   info.ID,
			PlayerID:    m.PlayerID,
			Username:    m.Username,
			Role:        m.Role,
			JoinedUTC:   m.JoinedAt,
			LastSeenUTC: m.LastSeen,
			IsOnline:    m.IsOnline,
			CapturedAt:  now,
		})
	}

	var rels []knowledge.FactionRelation
	for _, a := range info.Allies {
		rels = append(rels, knowledge.FactionRelation{
			FactionID:       info.ID,
			TargetFactionID: a.ID,
			TargetName:      a.Name,
			TargetTag:       a.Tag,
			Kind:            "ally",
			CapturedAt:      now,
		})
	}
	for _, e := range info.Enemies {
		rels = append(rels, knowledge.FactionRelation{
			FactionID:       info.ID,
			TargetFactionID: e.ID,
			TargetName:      e.Name,
			TargetTag:       e.Tag,
			Kind:            "enemy",
			CapturedAt:      now,
		})
	}
	for _, w := range info.Wars {
		rels = append(rels, knowledge.FactionRelation{
			FactionID:       info.ID,
			TargetFactionID: w.TargetFactionID,
			TargetName:      w.TargetFactionName,
			TargetTag:       w.TargetFactionTag,
			Kind:            "war",
			Reason:          w.Reason,
			OurKills:        w.OurKills,
			TheirKills:      w.TheirKills,
			StartedUTC:      w.StartedAt,
			CapturedAt:      now,
		})
	}
	// PeaceProposal uses FromFactionID/FromFactionName (not TargetFactionID/TargetName).
	// We store the proposing faction as TargetFactionID so the relation is from our
	// faction's perspective: "faction info.ID has a peace_proposal from p.FromFactionID".
	for _, p := range info.PeaceProposals {
		rels = append(rels, knowledge.FactionRelation{
			FactionID:       info.ID,
			TargetFactionID: p.FromFactionID,
			TargetName:      p.FromFactionName,
			Kind:            "peace_proposal",
			Terms:           p.Terms,
			CapturedAt:      now,
		})
	}
	return rec, members, rels
}
