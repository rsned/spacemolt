package market

import (
	"encoding/json"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// ReplyIsLocal reports whether a view_market reply is the book of the base the
// agent is docked at. Since v0.728.0 a docked agent can read any charted
// station's book with base_id, and every capture files the book under the
// docked station, so a remote read must be kept out of it. It answers true
// whenever it cannot tell (no base_id in the reply, docked base unknown, or a
// reply that does not parse): those are the cases capture always handled.
func ReplyIsLocal(raw []byte, dockedBase string) bool {
	if dockedBase == "" {
		return true
	}
	var resp serverapi.ViewMarketResponse
	if err := json.Unmarshal(raw, &resp); err != nil || resp.BaseID == "" {
		return true
	}

	return resp.BaseID == dockedBase
}
