package assets

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// PirateUnlockBaseline is the pirate standing BASELINE an agent holds once it has
// completed `an_introduction`, the mission that grants free docking at pirate
// strongholds.
//
// The value is exact, not a threshold guess: across all 151 captured agents the
// pirate baseline takes exactly two values — -30 for the locked and 10 for the
// unlocked, with nothing in between. Reputation is NOT the signal; it reads 10-11
// either way, so sampling only unlocked agents makes the difference vanish.
const PirateUnlockBaseline = 10

// pirateFactionPrefix keys the nine per-stronghold pirate standings
// (pirate_voss, pirate_thane, ...). The old single `pirates` key is retired.
const pirateFactionPrefix = "pirate_"

// HoldsPirateUnlock reports whether agentID has the pirate stronghold unlock,
// according to the most recent standings capture.
//
// An agent with no captured standings at all returns false with no error: it is
// not evidence of being locked so much as absence of evidence, and the safe
// reading for a caller deciding whether to end a secondment is "not yet".
func (s *Store) HoldsPirateUnlock(ctx context.Context, agentID string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		  FROM agent_standings st
		  JOIN agents a ON a.player_id = st.player_id
		 WHERE a.agent_id = ?
		   AND st.faction LIKE ? || '%'
		   AND st.baseline >= ?`,
		agentID, pirateFactionPrefix, PirateUnlockBaseline).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("assets: read pirate standings for %s: %w", agentID, err)
	}
	return n > 0, nil
}
