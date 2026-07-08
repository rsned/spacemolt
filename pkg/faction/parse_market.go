package faction

import (
	"time"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// parseFactionOrders converts the orders of a view_orders response (called
// with scope=faction by the collector) into KB rows. There is no separate
// faction_orders array on the wire — a single scoped `orders` field carries
// either personal or faction orders depending on the request's scope param.
// Side falls back to order_type when side is empty.
func parseFactionOrders(factionID, baseID string, resp serverapi.ViewOrdersResponse) []knowledge.FactionOrderRow {
	now := time.Now()
	base := resp.Base
	if base == "" {
		base = baseID
	}
	out := make([]knowledge.FactionOrderRow, 0, len(resp.Orders))
	for _, o := range resp.Orders {
		side := o.Side
		if side == "" {
			side = o.OrderType
		}
		out = append(out, knowledge.FactionOrderRow{
			FactionID: factionID, BaseID: base, OrderID: o.OrderID, Side: side,
			ItemID: o.ItemID, ItemName: o.ItemName,
			PriceEach: float64(o.PriceEach), Quantity: float64(o.Quantity), CapturedAt: now,
		})
	}
	return out
}

// parseFactionMissions converts a faction_list_missions response into KB rows.
func parseFactionMissions(factionID, baseID string, resp serverapi.FactionListMissionsResponse) []knowledge.FactionMissionRow {
	now := time.Now()
	base := resp.BaseID
	if base == "" {
		base = baseID
	}
	out := make([]knowledge.FactionMissionRow, 0, len(resp.Missions))
	for _, m := range resp.Missions {
		id := m.MissionID
		if id == "" {
			id = m.TemplateID
		}
		out = append(out, knowledge.FactionMissionRow{
			FactionID: factionID, BaseID: base, MissionID: id, Title: m.Title, Type: m.Type,
			Description: m.Description, GiverName: m.GiverName,
			RewardsJSON: string(m.Rewards), ObjectivesJSON: string(m.Objectives),
			AssignedPlayerID: m.AssignedPlayerID, ExpirationUTC: m.ExpiresAt, CapturedAt: now,
		})
	}
	return out
}

// parseFactionRooms converts a faction_rooms response into KB rows.
func parseFactionRooms(factionID, baseID string, resp serverapi.FactionRoomsResponse) []knowledge.FactionRoomRow {
	now := time.Now()
	base := resp.BaseID
	if base == "" {
		base = baseID
	}
	out := make([]knowledge.FactionRoomRow, 0, len(resp.Rooms))
	for _, r := range resp.Rooms {
		out = append(out, knowledge.FactionRoomRow{
			FactionID: factionID, BaseID: base, RoomID: r.RoomID, Name: r.Name,
			Access: r.Access, Description: r.Description, CapturedAt: now,
		})
	}
	return out
}
