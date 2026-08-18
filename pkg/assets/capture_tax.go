package assets

import (
	"context"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// CaptureTaxEstimate records what the next weekly levy will demand of one agent.
//
// This is the forward-looking counterpart to the tax.* rows in the action log,
// which only say what already happened. It matters because an unpayable levy
// does not simply fail: the shortfall becomes an outstanding debt with that
// empire, the agent is detained in their territory until it clears, and the debt
// then skims income — so an agent at zero credits cannot earn its way out. Seen
// a week ahead, the same agent just needs a transfer.
//
// Like every capture here, a source failure is not an error: the tables keep
// their previous captured_at rather than recording a zero assessment, which
// would be indistinguishable from genuinely owing nothing.
func CaptureTaxEstimate(ctx context.Context, client game.GameClient, st *Store, agentID string, now time.Time) error {
	if st == nil || client == nil {
		return nil
	}

	state := client.GetState()
	if state == nil || state.Player.ID == "" {
		return nil
	}
	playerID := state.Player.ID

	if err := client.GetTaxEstimate(ctx); err != nil {
		return nil //nolint:nilerr // a source failure must never fail the capture pass
	}

	t, ok, derr := TaxEstimateFrom(client.GetRawJSON("tax_estimate"))
	if derr != nil || !ok {
		return nil //nolint:nilerr // an unparseable or absent reply leaves the previous capture standing
	}

	if err := st.UpsertIdentity(ctx, Identity{PlayerID: playerID, AgentID: agentID,
		Username: state.Player.Username}, now); err != nil {
		return err
	}
	if err := st.UpsertTax(ctx, playerID, t, now); err != nil {
		return err
	}

	return st.ReplaceTaxShips(ctx, playerID, t.Ships, now)
}
