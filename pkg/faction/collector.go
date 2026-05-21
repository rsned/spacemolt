package faction

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// Store is the subset of *knowledge.SQLiteKB the Collector persists through.
// Satisfied by *knowledge.SQLiteKB.
type Store interface {
	StoreFaction(ctx context.Context, r knowledge.FactionRecord) error
	ReplaceFactionMembers(ctx context.Context, factionID string, members []knowledge.FactionMember) error
	ReplaceFactionRelations(ctx context.Context, factionID string, rels []knowledge.FactionRelation) error
	StoreFactionBase(ctx context.Context, b knowledge.FactionBaseRow) error
	ReplaceFactionFacilities(ctx context.Context, factionID, baseID string, fs []knowledge.FactionFacilityRow) error
	ReplaceFactionStorage(ctx context.Context, s knowledge.FactionStorageRow) error
	ReplaceFactionOrders(ctx context.Context, factionID, baseID string, orders []knowledge.FactionOrderRow) error
	ReplaceFactionMissions(ctx context.Context, factionID, baseID string, ms []knowledge.FactionMissionRow) error
	ReplaceFactionRooms(ctx context.Context, factionID, baseID string, rooms []knowledge.FactionRoomRow) error
}

// Collector gathers faction data from a connected game client and persists it.
type Collector struct {
	kb     Store
	logger *log.Logger
}

// NewCollector returns a Collector that writes to kb.
func NewCollector(kb Store, logger *log.Logger) *Collector {
	if logger == nil {
		logger = log.Default()
	}
	return &Collector{kb: kb, logger: logger}
}

// Collect gathers faction data from the connected client's vantage point.
// When includeFactionWide is true, it also collects faction_info-derived data
// (header, members, relations, intel). Station-scoped data (facilities, storage,
// orders, missions, rooms, bases) is always collected for the current station
// and known bases. Best-effort: sub-query failures are logged, not fatal.
func (c *Collector) Collect(ctx context.Context, client game.GameClient, includeFactionWide bool) error {
	state := client.GetState()
	factionID := state.Player.FactionID
	if factionID == "" {
		return fmt.Errorf("agent is not in a faction")
	}
	wsClient, ok := client.(*game.Client)
	if !ok {
		return fmt.Errorf("faction collection requires the WebSocket client (*game.Client)")
	}

	if includeFactionWide {
		c.collectFactionInfo(ctx, wsClient, factionID)
	}
	c.collectStation(ctx, wsClient, factionID, state)
	return nil
}

// submitAndRead sends a command and returns its response payload, mirroring the
// daily-summary storage-capture pattern. Returns nil payload on server error.
func submitAndRead(ctx context.Context, c *game.Client, msgType string, payload map[string]any) (map[string]any, error) {
	h, err := c.Submit(ctx, protocol.Message{
		Type:      msgType,
		Timestamp: time.Now().UnixMilli(),
		Payload:   payload,
	}, game.WithAckOnly(), game.WithTimeout(10*time.Second))
	if err != nil {
		return nil, err
	}
	resp, err := h.Result(ctx)
	if err != nil {
		return nil, err
	}
	if resp.Type == protocol.TypeError || resp.Type == protocol.TypeActionError {
		if msg, ok := resp.Payload["message"].(string); ok {
			return nil, fmt.Errorf("server error: %s", msg)
		}
		return nil, fmt.Errorf("server returned error response")
	}
	return resp.Payload, nil
}

// readInto submits a command and unmarshals the payload into out.
func readInto(ctx context.Context, c *game.Client, msgType string, payload map[string]any, out any) error {
	p, err := submitAndRead(ctx, c, msgType, payload)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

func (c *Collector) collectFactionInfo(ctx context.Context, client *game.Client, factionID string) {
	var info serverapi.FactionInfoResponse
	if err := readInto(ctx, client, "faction_info", map[string]any{"limit": 200}, &info); err != nil {
		c.logger.Printf("  faction_info failed: %v", err)
		return
	}
	rec, members, rels := parseFactionInfo(info)
	if rec.FactionID == "" {
		rec.FactionID = factionID
	}
	rec.IntelSystems, rec.IntelTrade = c.collectIntel(ctx, client)

	if err := c.kb.StoreFaction(ctx, rec); err != nil {
		c.logger.Printf("  StoreFaction failed: %v", err)
	}
	if err := c.kb.ReplaceFactionMembers(ctx, rec.FactionID, members); err != nil {
		c.logger.Printf("  ReplaceFactionMembers failed: %v", err)
	}
	if err := c.kb.ReplaceFactionRelations(ctx, rec.FactionID, rels); err != nil {
		c.logger.Printf("  ReplaceFactionRelations failed: %v", err)
	}
	c.logger.Printf("  Faction %s: treasury=%d members=%d relations=%d", rec.Tag, rec.Treasury, len(members), len(rels))
}

// collectIntel reads intel coverage counts; returns (systems, trade), 0 on error.
func (c *Collector) collectIntel(ctx context.Context, client *game.Client) (int, int) {
	var systems, trade int
	if p, err := submitAndRead(ctx, client, "faction_intel_status", nil); err == nil {
		// Server fields per openapi: unique_systems is the coverage count;
		// total_reports is a reasonable fallback.
		systems = intFromAny(p["unique_systems"], p["total_reports"])
	}
	if p, err := submitAndRead(ctx, client, "faction_trade_intel_status", nil); err == nil {
		trade = intFromAny(p["unique_stations"], p["total_reports"])
	}
	return systems, trade
}

// intFromAny returns the first numeric value found among candidates as an int.
func intFromAny(candidates ...any) int {
	for _, v := range candidates {
		if f, ok := v.(float64); ok {
			return int(f)
		}
	}
	return 0
}

// collectStation collects all station-scoped data from the agent's current
// vantage: facilities at the current station, faction storage at the current
// station and every base discovered via facilities, plus orders/missions/rooms
// (Task 8). Best-effort throughout.
func (c *Collector) collectStation(ctx context.Context, client *game.Client, factionID string, state *game.State) {
	currentBase := state.CurrentPOI
	knownBases := map[string]bool{}
	if currentBase != "" {
		knownBases[currentBase] = true
	}

	// Facilities at the current station.
	var facResp serverapi.FacilityListResponse
	if err := readInto(ctx, client, "facility", map[string]any{"action": "faction_list"}, &facResp); err != nil {
		c.logger.Printf("  facility faction_list failed: %v", err)
	} else {
		base := facResp.BaseID
		if base == "" {
			base = currentBase
		}
		rows := parseFacilities(factionID, base, facResp.FactionFacilities)
		if base != "" {
			if err := c.kb.ReplaceFactionFacilities(ctx, factionID, base, rows); err != nil {
				c.logger.Printf("  ReplaceFactionFacilities failed: %v", err)
			}
		}
		for _, r := range rows {
			if r.BaseID != "" {
				knownBases[r.BaseID] = true
			}
		}
	}

	// Persist known bases + collect faction storage at each (remote query supported).
	for baseID := range knownBases {
		c.persistBase(ctx, factionID, baseID, currentBase, state)
		c.collectStorage(ctx, client, factionID, baseID)
	}

	// Orders / missions / rooms at the current station (Task 8).
	c.collectOrders(ctx, client, factionID, currentBase)
	c.collectMissions(ctx, client, factionID, currentBase)
	c.collectRooms(ctx, client, factionID, currentBase)
}

// persistBase records a faction base. The current station is enriched with
// system info from state; remote bases are recorded by ID only (the renderer
// falls back to the base ID when the name is empty).
func (c *Collector) persistBase(ctx context.Context, factionID, baseID, currentBase string, state *game.State) {
	row := knowledge.FactionBaseRow{FactionID: factionID, BaseID: baseID, CapturedAt: time.Now()}
	if baseID == currentBase {
		row.SystemID = state.System.ID
		row.SystemName = state.System.Name
		row.POIID = currentBase
	}
	if err := c.kb.StoreFactionBase(ctx, row); err != nil {
		c.logger.Printf("  StoreFactionBase failed: %v", err)
	}
}

func (c *Collector) collectStorage(ctx context.Context, client *game.Client, factionID, baseID string) {
	var resp serverapi.ViewFactionStorageResponse
	if err := readInto(ctx, client, "view_faction_storage", map[string]any{"station_id": baseID}, &resp); err != nil {
		c.logger.Printf("  faction storage at %s failed: %v", baseID, err)
		return
	}
	row := knowledge.FactionStorageRow{
		FactionID: factionID, BaseID: baseID, Credits: resp.Credits, CapturedAt: time.Now(),
	}
	for _, it := range resp.Items {
		row.Items = append(row.Items, knowledge.FactionStorageItem{
			ItemID: it.ItemID, Name: it.Name, Quantity: it.Quantity, Size: it.Size,
		})
	}
	row.ItemCount = len(row.Items)
	if err := c.kb.ReplaceFactionStorage(ctx, row); err != nil {
		c.logger.Printf("  ReplaceFactionStorage failed: %v", err)
	}
}

func (c *Collector) collectOrders(ctx context.Context, client *game.Client, factionID, baseID string) {
	if baseID == "" {
		return
	}
	var resp serverapi.ViewOrdersResponse
	if err := readInto(ctx, client, "view_orders", nil, &resp); err != nil {
		c.logger.Printf("  view_orders failed: %v", err)
		return
	}
	rows := parseFactionOrders(factionID, baseID, resp)
	if err := c.kb.ReplaceFactionOrders(ctx, factionID, baseID, rows); err != nil {
		c.logger.Printf("  ReplaceFactionOrders failed: %v", err)
	}
}

func (c *Collector) collectMissions(ctx context.Context, client *game.Client, factionID, baseID string) {
	if baseID == "" {
		return
	}
	var resp serverapi.FactionListMissionsResponse
	if err := readInto(ctx, client, "faction_list_missions", nil, &resp); err != nil {
		c.logger.Printf("  faction_list_missions failed: %v", err)
		return
	}
	rows := parseFactionMissions(factionID, baseID, resp)
	if err := c.kb.ReplaceFactionMissions(ctx, factionID, baseID, rows); err != nil {
		c.logger.Printf("  ReplaceFactionMissions failed: %v", err)
	}
}

func (c *Collector) collectRooms(ctx context.Context, client *game.Client, factionID, baseID string) {
	if baseID == "" {
		return
	}
	var resp serverapi.FactionRoomsResponse
	if err := readInto(ctx, client, "faction_rooms", nil, &resp); err != nil {
		c.logger.Printf("  faction_rooms failed: %v", err)
		return
	}
	rows := parseFactionRooms(factionID, baseID, resp)
	if err := c.kb.ReplaceFactionRooms(ctx, factionID, baseID, rows); err != nil {
		c.logger.Printf("  ReplaceFactionRooms failed: %v", err)
	}
}
