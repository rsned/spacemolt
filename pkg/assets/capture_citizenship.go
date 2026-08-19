package assets

import (
	"context"
	"encoding/json"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
	"github.com/rsned/spacemolt/pkg/game"
)

// CaptureCitizenship records which empires an agent belongs to, what each empire
// demands of applicants, and the state of any application in flight.
//
// It answers a question `agent_profile.empire` cannot: that column is the
// immutable ORIGIN, while tax is assessed by every empire whose citizenship the
// agent holds. The two diverge the moment anyone migrates.
//
// The reply is read from the request-correlated result sink rather than the raw
// JSON cache. The cache is keyed off the reply's "action" field, and the
// citizenship reply carries none — so GetRawJSON would silently return another
// command's payload or nothing at all.
//
// Like every capture here, a source failure is not an error: a missed pass
// leaves the previous snapshot standing rather than recording an agent as
// stateless, which is a real and very different state.
func CaptureCitizenship(ctx context.Context, client game.GameClient, st *Store, agentID string, now time.Time) error {
	if st == nil || client == nil {
		return nil
	}

	state := client.GetState()
	if state == nil || state.Player.ID == "" {
		return nil
	}
	playerID := state.Player.ID

	var sink protocol.Response
	if err := client.Citizenship(game.WithResultSink(ctx, &sink), "list", ""); err != nil {
		return nil //nolint:nilerr // a source failure must never fail the capture pass
	}
	if len(sink.Payload) == 0 {
		return nil
	}
	raw, err := json.Marshal(sink.Payload)
	if err != nil {
		return nil //nolint:nilerr // an unmarshalable reply leaves the previous capture standing
	}

	snap, ok, derr := CitizenshipFrom(raw)
	if derr != nil || !ok {
		return nil //nolint:nilerr // an unparseable or partial reply leaves the previous capture standing
	}

	if err := st.UpsertIdentity(ctx, Identity{PlayerID: playerID, AgentID: agentID,
		Username: state.Player.Username}, now); err != nil {
		return err
	}
	if err := st.ReplaceCitizenships(ctx, playerID, snap.Held, now); err != nil {
		return err
	}
	if err := st.UpsertCitizenshipPetitions(ctx, playerID, snap.Petitions, now); err != nil {
		return err
	}

	return st.ReplaceCitizenshipPolicies(ctx, playerID, snap.Policies, now)
}
