package agent

import (
	"context"
	"log"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// WireStorageCapture configures a game client to automatically save storage
// snapshots to the knowledge base whenever a view_storage response is received.
func WireStorageCapture(client *game.Client, kb knowledge.Base, agentID string, logger *log.Logger) {
	if client == nil || kb == nil {
		return
	}
	client.SetOnStorageUpdate(func(evt game.StorageUpdateEvent) {
		items := make([]knowledge.StorageSnapshotItem, len(evt.Items))
		for i, item := range evt.Items {
			items[i] = knowledge.StorageSnapshotItem{
				ItemID:   item.ItemID,
				Name:     item.Name,
				Quantity: item.Quantity,
				Size:     item.Size,
			}
		}
		ships := make([]knowledge.StorageSnapshotShip, len(evt.Ships))
		for i, ship := range evt.Ships {
			ships[i] = knowledge.StorageSnapshotShip{
				ShipID:    ship.ShipID,
				ClassID:   ship.ClassID,
				ClassName: ship.ClassName,
				CargoUsed: ship.CargoUsed,
			}
		}
		snapshot := knowledge.StorageSnapshot{
			AgentID: agentID,
			BaseID:  evt.BaseID,
			Credits: evt.Credits,
			Items:   items,
			Ships:   ships,
		}
		if err := kb.StoreStorageSnapshot(context.Background(), snapshot); err != nil {
			logger.Printf("Warning: failed to save storage snapshot: %v", err)
		}
	})
}
