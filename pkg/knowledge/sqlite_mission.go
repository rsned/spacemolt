package knowledge

import (
	"context"
	"fmt"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// UpsertMissionTemplate is a placeholder implemented fully in Task 5.
func (kb *SQLiteKB) UpsertMissionTemplate(
	ctx context.Context,
	entry serverapi.MissionBoardEntry,
	baseID, systemID string,
	tick int64,
) (*MissionUpsertResult, error) {
	return nil, fmt.Errorf("UpsertMissionTemplate not yet implemented")
}
