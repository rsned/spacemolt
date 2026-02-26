package knowledge

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// RecordResourceState tracks resource levels over time
func (kb *SQLiteKB) RecordResourceState(ctx context.Context, poiID, resourceID string, richness, remaining float64, gameTick int64, agentID string) error {
	_, err := kb.db.ExecContext(ctx, `
		INSERT INTO resource_history (poi_id, resource_id, richness, remaining, game_tick, recorded_at, agent_id, last_updated_tick)
		VALUES (?, ?, ?, ?, ?, datetime('now'), ?, ?)
	`, poiID, resourceID, richness, remaining, gameTick, agentID, gameTick)

	if err != nil {
		return fmt.Errorf("failed to record resource state: %w", err)
	}
	return nil
}

// GetResourceHistory retrieves historical resource data
func (kb *SQLiteKB) GetResourceHistory(ctx context.Context, poiID, resourceID string, limit int) ([]ResourceHistory, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT poi_id, resource_id, richness, remaining, game_tick, recorded_at, agent_id
		FROM resource_history
		WHERE poi_id = ? AND resource_id = ?
		ORDER BY game_tick DESC
		LIMIT ?
	`, poiID, resourceID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query resource history: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var history []ResourceHistory
	for rows.Next() {
		var h ResourceHistory
		var recordedAt string
		err := rows.Scan(&h.POIID, &h.ResourceID, &h.Richness, &h.Remaining, &h.GameTick, &recordedAt, &h.AgentID)
		if err != nil {
			return nil, fmt.Errorf("failed to scan resource history: %w", err)
		}
		h.RecordedAt, _ = time.Parse("2006-01-02 15:04:05", recordedAt)
		history = append(history, h)
	}

	return history, rows.Err()
}

// GetDepletingResources finds resources that are depleting rapidly
func (kb *SQLiteKB) GetDepletingResources(ctx context.Context, threshold float64) ([]DepletingResource, error) {
	// Find resources where remaining has dropped significantly
	rows, err := kb.db.QueryContext(ctx, `
		WITH latest AS (
			SELECT poi_id, resource_id, remaining, game_tick,
				   ROW_NUMBER() OVER (PARTITION BY poi_id, resource_id ORDER BY game_tick DESC) as rn
			FROM resource_history
		),
		previous AS (
			SELECT poi_id, resource_id, remaining, game_tick,
				   ROW_NUMBER() OVER (PARTITION BY poi_id, resource_id ORDER BY game_tick DESC) as rn
			FROM resource_history
		)
		SELECT
			p.id as poi_id,
			p.name as poi_name,
			s.id as system_id,
			s.name as system_name,
			l.resource_id,
			l.remaining as current_remaining,
			COALESCE(prev.remaining, l.remaining) as previous_remaining,
			CASE
				WHEN l.game_tick > COALESCE(prev.game_tick, l.game_tick)
				THEN (COALESCE(prev.remaining, l.remaining) - l.remaining) / (l.game_tick - COALESCE(prev.game_tick, l.game_tick))
				ELSE 0
			END as depletion_rate,
			CASE
				WHEN l.game_tick > COALESCE(prev.game_tick, l.game_tick) AND (COALESCE(prev.remaining, l.remaining) - l.remaining) > 0
				THEN CAST(l.remaining / ((COALESCE(prev.remaining, l.remaining) - l.remaining) / (l.game_tick - COALESCE(prev.game_tick, l.game_tick))) AS INTEGER)
				ELSE 0
			END as estimated_life
		FROM latest l
		JOIN pois p ON l.poi_id = p.id
		JOIN systems s ON p.system_id = s.id
		LEFT JOIN previous prev ON l.poi_id = prev.poi_id AND l.resource_id = prev.resource_id AND prev.rn = 2
		WHERE l.rn = 1
		  AND l.remaining > 0
		  AND l.remaining < ?
		ORDER BY estimated_life ASC, current_remaining ASC
		LIMIT 50
	`, threshold)

	if err != nil {
		return nil, fmt.Errorf("failed to query depleting resources: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var resources []DepletingResource
	for rows.Next() {
		var r DepletingResource
		err := rows.Scan(&r.POIID, &r.POIName, &r.SystemID, &r.SystemName, &r.ResourceID,
			&r.CurrentRem, &r.PreviousRem, &r.DepletionRate, &r.EstimatedLife)
		if err != nil {
			return nil, fmt.Errorf("failed to scan depleting resource: %w", err)
		}
		resources = append(resources, r)
	}

	return resources, rows.Err()
}

// RecordJourney logs a journey between systems for route optimization
func (kb *SQLiteKB) RecordJourney(ctx context.Context, fromSystem, toSystem string, fuelCost, travelTime float64, agentID string) error {
	_, err := kb.db.ExecContext(ctx, `
		INSERT INTO connection_metrics (from_system, to_system, travel_count, avg_fuel_cost, avg_travel_time, last_traveled, traveled_by, last_updated_tick)
		VALUES (?, ?, 1, ?, ?, datetime('now'), ?, 0)
		ON CONFLICT(from_system, to_system) DO UPDATE SET
			travel_count = travel_count + 1,
			avg_fuel_cost = (avg_fuel_cost * travel_count + ?) / (travel_count + 1),
			avg_travel_time = (avg_travel_time * travel_count + ?) / (travel_count + 1),
			last_traveled = datetime('now'),
			traveled_by = ?
	`, fromSystem, toSystem, fuelCost, travelTime, agentID, fuelCost, travelTime, agentID)

	if err != nil {
		return fmt.Errorf("failed to record journey: %w", err)
	}
	return nil
}

// GetOptimalRoute finds the best known route between two systems
func (kb *SQLiteKB) GetOptimalRoute(ctx context.Context, fromSystem, toSystem string) (*ConnectionMetrics, error) {
	var cm ConnectionMetrics
	var lastTraveled string

	err := kb.db.QueryRowContext(ctx, `
		SELECT from_system, to_system, travel_count, avg_fuel_cost, avg_travel_time, last_traveled, traveled_by
		FROM connection_metrics
		WHERE from_system = ? AND to_system = ?
	`, fromSystem, toSystem).Scan(&cm.FromSystem, &cm.ToSystem, &cm.TravelCount, &cm.AvgFuelCost, &cm.AvgTravelTime, &lastTraveled, &cm.TraveledBy)

	if err == sql.ErrNoRows {
		return nil, nil // No route data available
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get optimal route: %w", err)
	}

	cm.LastTraveled, _ = time.Parse("2006-01-02 15:04:05", lastTraveled)
	return &cm, nil
}

// FindCheapestRoute finds the most fuel-efficient multi-hop route
func (kb *SQLiteKB) FindCheapestRoute(ctx context.Context, fromSystem, toSystem string, maxHops int) ([]string, float64, error) {
	// Simple implementation: direct route only for now
	// TODO: Implement Dijkstra's algorithm for multi-hop pathfinding
	metrics, err := kb.GetOptimalRoute(ctx, fromSystem, toSystem)
	if err != nil {
		return nil, 0, err
	}
	if metrics == nil {
		return nil, 0, fmt.Errorf("no known route from %s to %s", fromSystem, toSystem)
	}

	return []string{fromSystem, toSystem}, metrics.AvgFuelCost, nil
}

// RecordAnomaly logs an unusual discovery or event
func (kb *SQLiteKB) RecordAnomaly(ctx context.Context, anomaly Anomaly) error {
	result, err := kb.db.ExecContext(ctx, `
		INSERT INTO anomalies (type, severity, system_id, poi_id, description, details, detected_at, detected_by, status, last_updated_tick)
		VALUES (?, ?, ?, ?, ?, ?, datetime('now'), ?, 'active', ?)
	`, anomaly.Type, anomaly.Severity, anomaly.SystemID, anomaly.POIID, anomaly.Description, anomaly.Details, anomaly.DetectedBy, anomaly.LastUpdatedTick)

	if err != nil {
		return fmt.Errorf("failed to record anomaly: %w", err)
	}

	id, _ := result.LastInsertId()
	anomaly.ID = id
	return nil
}

// GetActiveAnomalies retrieves active anomalies in a system
func (kb *SQLiteKB) GetActiveAnomalies(ctx context.Context, systemID string) ([]Anomaly, error) {
	query := `
		SELECT id, type, severity, system_id, COALESCE(poi_id, ''), description, COALESCE(details, ''), detected_at, detected_by, status
		FROM anomalies
		WHERE status = 'active'
	`
	args := []interface{}{}

	if systemID != "" {
		query += " AND system_id = ?"
		args = append(args, systemID)
	}

	query += " ORDER BY detected_at DESC LIMIT 100"

	rows, err := kb.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query active anomalies: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var anomalies []Anomaly
	for rows.Next() {
		var a Anomaly
		var detectedAt string
		err := rows.Scan(&a.ID, &a.Type, &a.Severity, &a.SystemID, &a.POIID, &a.Description, &a.Details, &detectedAt, &a.DetectedBy, &a.Status)
		if err != nil {
			return nil, fmt.Errorf("failed to scan anomaly: %w", err)
		}
		a.DetectedAt, _ = time.Parse("2006-01-02 15:04:05", detectedAt)
		anomalies = append(anomalies, a)
	}

	return anomalies, rows.Err()
}

// GetAnomaliesByType retrieves anomalies filtered by type and severity
func (kb *SQLiteKB) GetAnomaliesByType(ctx context.Context, anomalyType, severity string, limit int) ([]Anomaly, error) {
	query := `
		SELECT id, type, severity, system_id, COALESCE(poi_id, ''), description, COALESCE(details, ''), detected_at, detected_by, status
		FROM anomalies
		WHERE 1=1
	`
	args := []interface{}{}

	if anomalyType != "" {
		query += " AND type = ?"
		args = append(args, anomalyType)
	}
	if severity != "" {
		query += " AND severity = ?"
		args = append(args, severity)
	}

	query += " ORDER BY detected_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := kb.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query anomalies by type: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var anomalies []Anomaly
	for rows.Next() {
		var a Anomaly
		var detectedAt string
		err := rows.Scan(&a.ID, &a.Type, &a.Severity, &a.SystemID, &a.POIID, &a.Description, &a.Details, &detectedAt, &a.DetectedBy, &a.Status)
		if err != nil {
			return nil, fmt.Errorf("failed to scan anomaly: %w", err)
		}
		a.DetectedAt, _ = time.Parse("2006-01-02 15:04:05", detectedAt)
		anomalies = append(anomalies, a)
	}

	return anomalies, rows.Err()
}

// ResolveAnomaly marks an anomaly as resolved or obsolete
func (kb *SQLiteKB) ResolveAnomaly(ctx context.Context, anomalyID int64, status string) error {
	_, err := kb.db.ExecContext(ctx, `
		UPDATE anomalies
		SET status = ?, last_updated_tick = 0
		WHERE id = ?
	`, status, anomalyID)

	if err != nil {
		return fmt.Errorf("failed to resolve anomaly: %w", err)
	}
	return nil
}

// AnalyzePriceTrends calculates price trends for an item at a station
func (kb *SQLiteKB) AnalyzePriceTrends(ctx context.Context, itemID, stationID string, windowHours int) (*PriceTrend, error) {
	cutoff := time.Now().Add(-time.Duration(windowHours) * time.Hour)

	var trend PriceTrend
	var windowStart, windowEnd string

	err := kb.db.QueryRowContext(ctx, `
		SELECT
			ml.item_id,
			ml.item_type,
			ms.station_id,
			ms.system_id,
			ml.listing_type,
			AVG(ml.price_per_unit) as avg_price,
			MIN(ml.price_per_unit) as min_price,
			MAX(ml.price_per_unit) as max_price,
			COUNT(*) as sample_count,
			MIN(ms.captured_at) as window_start,
			MAX(ms.captured_at) as window_end
		FROM market_listings ml
		JOIN market_snapshots ms ON ml.snapshot_id = ms.id
		WHERE ml.item_id = ?
		  AND ms.station_id = ?
		  AND ms.captured_at >= ?
		GROUP BY ml.item_id, ml.item_type, ms.station_id, ms.system_id, ml.listing_type
	`, itemID, stationID, cutoff.Format("2006-01-02 15:04:05")).Scan(
		&trend.ItemID, &trend.ItemType, &trend.StationID, &trend.SystemID, &trend.ListingType,
		&trend.AvgPrice, &trend.MinPrice, &trend.MaxPrice, &trend.SampleCount,
		&windowStart, &windowEnd,
	)

	if err == sql.ErrNoRows {
		return nil, nil // No data available
	}
	if err != nil {
		return nil, fmt.Errorf("failed to analyze price trends: %w", err)
	}

	trend.WindowStart, _ = time.Parse("2006-01-02 15:04:05", windowStart)
	trend.WindowEnd, _ = time.Parse("2006-01-02 15:04:05", windowEnd)

	// Get current price (most recent)
	var capturedAt string
	err = kb.db.QueryRowContext(ctx, `
		SELECT ml.price_per_unit, ms.captured_at
		FROM market_listings ml
		JOIN market_snapshots ms ON ml.snapshot_id = ms.id
		WHERE ml.item_id = ? AND ms.station_id = ? AND ml.listing_type = ?
		ORDER BY ms.captured_at DESC
		LIMIT 1
	`, itemID, stationID, trend.ListingType).Scan(&trend.CurrentPrice, &capturedAt)

	if err == nil {
		// Calculate price change percentage
		if trend.AvgPrice > 0 {
			trend.PriceChange = ((trend.CurrentPrice - trend.AvgPrice) / trend.AvgPrice) * 100
		}
	}

	return &trend, nil
}

// FindBestPrices finds the best buy/sell prices for an item across all stations
func (kb *SQLiteKB) FindBestPrices(ctx context.Context, itemID string, listingType string, limit int) ([]BestPrice, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT
			ml.item_id,
			ms.station_id,
			ms.station_name,
			ms.system_id,
			ms.system_name,
			ml.price_per_unit,
			ml.quantity,
			ml.listing_type,
			ms.captured_at
		FROM (
			SELECT snapshot_id, item_id, price_per_unit, quantity, listing_type,
				   ROW_NUMBER() OVER (PARTITION BY item_id, listing_type ORDER BY captured_at DESC) as rn
			FROM market_listings ml2
			JOIN market_snapshots ms2 ON ml2.snapshot_id = ms2.id
			WHERE ml2.item_id = ? AND ml2.listing_type = ?
		) ml
		JOIN market_snapshots ms ON ml.snapshot_id = ms.id
		WHERE ml.rn = 1
		ORDER BY
			CASE WHEN ? = 'buy' THEN ml.price_per_unit END DESC,
			CASE WHEN ? = 'sell' THEN ml.price_per_unit END ASC
		LIMIT ?
	`, itemID, listingType, listingType, listingType, limit)

	if err != nil {
		return nil, fmt.Errorf("failed to find best prices: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var prices []BestPrice
	for rows.Next() {
		var p BestPrice
		var capturedAt string
		err := rows.Scan(&p.ItemID, &p.StationID, &p.StationName, &p.SystemID, &p.SystemName,
			&p.Price, &p.Quantity, &p.ListingType, &capturedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan best price: %w", err)
		}
		p.CapturedAt, _ = time.Parse("2006-01-02 15:04:05", capturedAt)
		prices = append(prices, p)
	}

	return prices, rows.Err()
}

// GetPriceHistory retrieves historical price points for an item
func (kb *SQLiteKB) GetPriceHistory(ctx context.Context, itemID, stationID string, limit int) ([]PricePoint, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT ml.price_per_unit, ml.quantity, ms.game_tick, ms.captured_at
		FROM market_listings ml
		JOIN market_snapshots ms ON ml.snapshot_id = ms.id
		WHERE ml.item_id = ? AND ms.station_id = ?
		ORDER BY ms.captured_at DESC
		LIMIT ?
	`, itemID, stationID, limit)

	if err != nil {
		return nil, fmt.Errorf("failed to get price history: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var points []PricePoint
	for rows.Next() {
		var p PricePoint
		var capturedAt string
		err := rows.Scan(&p.Price, &p.Quantity, &p.GameTick, &capturedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan price point: %w", err)
		}
		p.CapturedAt, _ = time.Parse("2006-01-02 15:04:05", capturedAt)
		points = append(points, p)
	}

	return points, rows.Err()
}

// RecordHostileEncounter logs a hostile encounter in a system
func (kb *SQLiteKB) RecordHostileEncounter(ctx context.Context, systemID string, encounterType string, details string) error {
	_, err := kb.db.ExecContext(ctx, `
		INSERT INTO danger_zones (system_id, danger_level, hostile_encounters, player_kills, npc_kills, last_incident, last_updated)
		VALUES (?, 1, 1,
			CASE WHEN ? = 'player_kill' THEN 1 ELSE 0 END,
			CASE WHEN ? = 'npc_kill' THEN 1 ELSE 0 END,
			datetime('now'), datetime('now'))
		ON CONFLICT(system_id) DO UPDATE SET
			hostile_encounters = hostile_encounters + 1,
			player_kills = player_kills + CASE WHEN ? = 'player_kill' THEN 1 ELSE 0 END,
			npc_kills = npc_kills + CASE WHEN ? = 'npc_kill' THEN 1 ELSE 0 END,
			danger_level = MIN(10, 1 + (hostile_encounters + 1) / 10),
			last_incident = datetime('now'),
			last_updated = datetime('now')
	`, systemID, encounterType, encounterType, encounterType, encounterType)

	if err != nil {
		return fmt.Errorf("failed to record hostile encounter: %w", err)
	}
	return nil
}

// GetDangerZones retrieves systems with high danger levels
func (kb *SQLiteKB) GetDangerZones(ctx context.Context, minDangerLevel int) ([]DangerZone, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT dz.system_id, s.name, dz.danger_level, dz.hostile_encounters, dz.player_kills, dz.npc_kills, dz.last_incident, dz.last_updated
		FROM danger_zones dz
		JOIN systems s ON dz.system_id = s.id
		WHERE dz.danger_level >= ?
		ORDER BY dz.danger_level DESC, dz.hostile_encounters DESC
	`, minDangerLevel)

	if err != nil {
		return nil, fmt.Errorf("failed to get danger zones: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var zones []DangerZone
	for rows.Next() {
		var z DangerZone
		var lastIncident, lastUpdated string
		err := rows.Scan(&z.SystemID, &z.SystemName, &z.DangerLevel, &z.HostileEncounters, &z.PlayerKills, &z.NPCKills, &lastIncident, &lastUpdated)
		if err != nil {
			return nil, fmt.Errorf("failed to scan danger zone: %w", err)
		}
		z.LastIncident, _ = time.Parse("2006-01-02 15:04:05", lastIncident)
		z.LastUpdated, _ = time.Parse("2006-01-02 15:04:05", lastUpdated)
		zones = append(zones, z)
	}

	return zones, rows.Err()
}

// GetSystemDanger retrieves danger information for a specific system
func (kb *SQLiteKB) GetSystemDanger(ctx context.Context, systemID string) (*DangerZone, error) {
	var z DangerZone
	var lastIncident, lastUpdated string

	err := kb.db.QueryRowContext(ctx, `
		SELECT dz.system_id, s.name, dz.danger_level, dz.hostile_encounters, dz.player_kills, dz.npc_kills, dz.last_incident, dz.last_updated
		FROM danger_zones dz
		JOIN systems s ON dz.system_id = s.id
		WHERE dz.system_id = ?
	`, systemID).Scan(&z.SystemID, &z.SystemName, &z.DangerLevel, &z.HostileEncounters, &z.PlayerKills, &z.NPCKills, &lastIncident, &lastUpdated)

	if err == sql.ErrNoRows {
		// Return a zone with zero danger if not found
		sys, _ := kb.GetSystem(ctx, systemID)
		if sys != nil {
			return &DangerZone{
				SystemID:    systemID,
				SystemName:  sys.Name,
				DangerLevel: 0,
			}, nil
		}
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get system danger: %w", err)
	}

	z.LastIncident, _ = time.Parse("2006-01-02 15:04:05", lastIncident)
	z.LastUpdated, _ = time.Parse("2006-01-02 15:04:05", lastUpdated)
	return &z, nil
}

// ExportKnowledge creates a JSON export of the knowledge base
func (kb *SQLiteKB) ExportKnowledge(ctx context.Context, description string, agentID string) (*KnowledgeExport, error) {
	export := &KnowledgeExport{
		ID:          fmt.Sprintf("export-%d", time.Now().Unix()),
		CreatedAt:   time.Now(),
		CreatedBy:   agentID,
		Description: description,
	}

	// Collect systems
	systems := kb.GetSystems()
	export.SystemsCount = len(systems)

	// Collect POIs
	var allPOIs []POI
	for _, sys := range systems {
		pois, err := kb.GetPOIs(ctx, sys.ID)
		if err == nil {
			allPOIs = append(allPOIs, pois...)
		}
	}
	export.POIsCount = len(allPOIs)

	// Collect experiences (limited to most recent 1000)
	var allExperiences []Experience
	rows, err := kb.db.QueryContext(ctx, `
		SELECT agent_id, time, type, description, outcome, location
		FROM experiences
		ORDER BY id DESC
		LIMIT 1000
	`)
	if err == nil {
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var exp Experience
			if err := rows.Scan(&exp.Time, &exp.Type, &exp.Description, &exp.Outcome, &exp.Location); err != nil {
				// Skip this record if scan fails
				continue
			}
			allExperiences = append(allExperiences, exp)
		}
	}
	export.ExperiencesCount = len(allExperiences)

	// Create JSON export
	exportData := map[string]interface{}{
		"systems":     systems,
		"pois":        allPOIs,
		"experiences": allExperiences,
		"exported_at": export.CreatedAt,
	}

	jsonData, err := json.Marshal(exportData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal export data: %w", err)
	}
	export.ExportData = string(jsonData)

	// Store export metadata
	_, err = kb.db.ExecContext(ctx, `
		INSERT INTO knowledge_exports (id, created_at, created_by, description, systems_count, pois_count, experiences_count, export_data, last_updated_tick)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)
	`, export.ID, export.CreatedAt.Format("2006-01-02 15:04:05"), export.CreatedBy, export.Description,
		export.SystemsCount, export.POIsCount, export.ExperiencesCount, export.ExportData)

	if err != nil {
		return nil, fmt.Errorf("failed to store export metadata: %w", err)
	}

	return export, nil
}

// ImportKnowledge imports data from a JSON export
func (kb *SQLiteKB) ImportKnowledge(ctx context.Context, exportData string) error {
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(exportData), &data); err != nil {
		return fmt.Errorf("failed to unmarshal export data: %w", err)
	}

	// Import systems
	if systemsData, ok := data["systems"].([]interface{}); ok {
		for range systemsData {
			// TODO: Properly unmarshal and import system data
			// For now, skip to avoid complexity
		}
	}

	// Import POIs
	if poisData, ok := data["pois"].([]interface{}); ok {
		for range poisData {
			// TODO: Properly unmarshal and import POI data
		}
	}

	return nil
}

// ListExports retrieves metadata for all knowledge exports
func (kb *SQLiteKB) ListExports(ctx context.Context) ([]KnowledgeExportMeta, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT id, created_at, created_by, description, systems_count, pois_count, experiences_count
		FROM knowledge_exports
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to list exports: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var exports []KnowledgeExportMeta
	for rows.Next() {
		var exp KnowledgeExportMeta
		var createdAt string
		err := rows.Scan(&exp.ID, &createdAt, &exp.CreatedBy, &exp.Description, &exp.SystemsCount, &exp.POIsCount, &exp.ExperiencesCount)
		if err != nil {
			return nil, fmt.Errorf("failed to scan export meta: %w", err)
		}
		exp.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
		exports = append(exports, exp)
	}

	return exports, rows.Err()
}
