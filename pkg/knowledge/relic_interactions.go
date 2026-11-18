package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Outcomes of one interact_poi attempt at a relic.
const (
	RelicDataCopied     = "data_copied"             // the reply set data_copied
	RelicItems          = "items"                   // the reply listed items_recovered
	RelicNothing        = "nothing"                 // a reply with a message only
	RelicRefused        = "refused"                 // the server refused the command
	RelicMissionSkipped = "mission_offered_skipped" // the prompt offered a mission; not interacted with
	RelicNoPrompt       = "no_prompt"               // no interaction block, and the attempt was refused
)

// RelicInteraction is one agent's attempt to interact with a relic POI.
type RelicInteraction struct {
	POIID       string
	SystemID    string
	AgentID     string
	Tick        int64
	Outcome     string
	Prompt      string // get_poi interaction.message
	Message     string // interact_poi message
	DataCopied  bool
	Items       map[string]int
	MissionJSON string
	Error       string
	ObservedUTC time.Time
}

// Responded reports whether the relic gave something back.
func (r RelicInteraction) Responded() bool {
	return r.Outcome == RelicDataCopied || r.Outcome == RelicItems
}

// RecordRelicInteraction stores one attempt. A repeat by the same agent at the
// same tick replaces the earlier row.
func (kb *SQLiteKB) RecordRelicInteraction(ctx context.Context, r RelicInteraction) error {
	items := ""
	if len(r.Items) > 0 {
		b, err := json.Marshal(r.Items)
		if err != nil {
			return fmt.Errorf("encode relic items: %w", err)
		}
		items = string(b)
	}
	_, err := kb.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO relic_interactions
			(poi_id, system_id, agent_id, tick, outcome, prompt, message, data_copied, items_json, mission_json, error, observed_utc)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.POIID, r.SystemID, r.AgentID, r.Tick, r.Outcome, r.Prompt, r.Message, r.DataCopied,
		items, r.MissionJSON, r.Error, r.ObservedUTC.UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("record relic interaction: %w", err)
	}
	return nil
}

// RelicInteractions returns every recorded attempt, oldest first.
func (kb *SQLiteKB) RelicInteractions(ctx context.Context) ([]RelicInteraction, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT poi_id, system_id, agent_id, tick, outcome, prompt, message, data_copied, items_json, mission_json, error, observed_utc
		FROM relic_interactions ORDER BY observed_utc, poi_id, agent_id`)
	if err != nil {
		return nil, fmt.Errorf("query relic interactions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []RelicInteraction
	for rows.Next() {
		var r RelicInteraction
		var items, observed string
		if err := rows.Scan(&r.POIID, &r.SystemID, &r.AgentID, &r.Tick, &r.Outcome, &r.Prompt, &r.Message,
			&r.DataCopied, &items, &r.MissionJSON, &r.Error, &observed); err != nil {
			return nil, fmt.Errorf("scan relic interaction: %w", err)
		}
		if items != "" {
			if err := json.Unmarshal([]byte(items), &r.Items); err != nil {
				return nil, fmt.Errorf("decode relic items for %s: %w", r.POIID, err)
			}
		}
		r.ObservedUTC, _ = time.Parse(time.RFC3339, observed)
		out = append(out, r)
	}
	return out, rows.Err()
}

// RecordRelicInteraction stores one attempt in memory.
func (kb *MemoryKB) RecordRelicInteraction(_ context.Context, r RelicInteraction) error {
	kb.mu.Lock()
	defer kb.mu.Unlock()
	kb.relicInteractions = append(kb.relicInteractions, r)
	return nil
}

// RelicInteractions returns every attempt recorded in memory, in record order.
func (kb *MemoryKB) RelicInteractions(_ context.Context) ([]RelicInteraction, error) {
	kb.mu.RLock()
	defer kb.mu.RUnlock()
	return append([]RelicInteraction(nil), kb.relicInteractions...), nil
}
