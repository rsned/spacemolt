package assets

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// ActionLogEvent is one get_action_log entry, flattened for storage.
//
// Summary is deliberately absent. The server composes it from the same data it
// already sends ("Facility rent: 9 credits for Signal Relay"), so storing it
// would duplicate every field in prose and make the table several times larger
// for nothing a query can use.
type ActionLogEvent struct {
	EventID   int64
	EventType string
	// Category is the event_type's prefix ("combat" from
	// "combat.ship_destroyed"), which is what the server's own category filter
	// matches. A dotless event_type falls back to the category field the reply
	// carries, and to "" if that is empty too.
	Category  string
	CreatedAt string
	Data      map[string]string
}

// ActionLogPage is one reply's worth of the since_id walk.
type ActionLogPage struct {
	Events []ActionLogEvent
	// NextSinceID is the server's own cursor. Zero means the reply carried none,
	// in which case the caller must fall back to the highest id it saw — an
	// older server, or a newest-first page reply, omits it.
	NextSinceID int64
	HasMore     bool
}

// ActionLogFrom decodes a raw get_action_log body (cache key "action_log").
//
// ok is false for an empty body, matching HullsFrom: a missing cache entry
// means "nothing captured this pass" and must never fail the pass.
//
// The decode uses UseNumber, which is not optional here. Every entry's data is
// a discriminated union of arbitrary scalars, so it can only be received as
// map[string]any — and with the default decoder every number in it becomes a
// float64. Stringifying those with %v turns a 1,200,000-credit price into
// "1.2e+06" and silently rounds any id past 2^53. json.Number keeps the exact
// digits the server sent.
func ActionLogFrom(raw []byte) (ActionLogPage, bool, error) {
	if len(raw) == 0 {
		return ActionLogPage{}, false, nil
	}

	var resp struct {
		Entries []struct {
			ID        json.Number    `json:"id"`
			EventType string         `json:"event_type"`
			Category  string         `json:"category"`
			CreatedAt string         `json:"created_at"`
			Data      map[string]any `json:"data"`
		} `json:"entries"`
		NextSinceID json.Number `json:"next_since_id"`
		HasMore     bool        `json:"has_more"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&resp); err != nil {
		return ActionLogPage{}, false, fmt.Errorf("assets: decode action_log: %w", err)
	}

	page := ActionLogPage{
		Events:  make([]ActionLogEvent, 0, len(resp.Entries)),
		HasMore: resp.HasMore,
	}
	if n, err := resp.NextSinceID.Int64(); err == nil {
		page.NextSinceID = n
	}
	for _, e := range resp.Entries {
		id, err := e.ID.Int64()
		if err != nil || id <= 0 {
			// An entry with no usable id cannot be stored or resumed from
			// without risking a duplicate or a skipped range: drop it rather
			// than invent a key.
			continue
		}
		page.Events = append(page.Events, ActionLogEvent{
			EventID:   id,
			EventType: e.EventType,
			Category:  actionLogCategory(e.EventType, e.Category),
			CreatedAt: e.CreatedAt,
			Data:      flattenActionLogData(e.Data),
		})
	}

	return page, true, nil
}

// actionLogCategory prefers the event_type's prefix over the reply's category
// field. The two agree on every event measured, but the prefix is present on
// every entry while category is only documented as required — and the prefix is
// what a query written against event_type can reproduce without a join.
func actionLogCategory(eventType, replyCategory string) string {
	if prefix, _, ok := strings.Cut(eventType, "."); ok && prefix != "" {
		return prefix
	}

	return strings.TrimSpace(replyCategory)
}

// flattenActionLogData turns one event's data object into string->string.
//
// The shape differs per event_type across 63 known types, so a typed column set
// is impossible; a flat string map keeps every field queryable via json_extract
// without a schema change each time the server adds an event.
func flattenActionLogData(in map[string]any) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = actionLogValueString(v)
	}

	return out
}

// actionLogValueString renders one data value as a string without losing
// digits. Nested objects and arrays keep their JSON form rather than being
// flattened further, so a value stays reconstructible.
func actionLogValueString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case json.Number:
		return t.String()
	case bool:
		return strconv.FormatBool(t)
	case float64:
		// Only reachable if a caller decoded without UseNumber. 'f' with
		// precision -1 at least avoids the exponent form that %v produces;
		// digits past 2^53 are already gone by this point.
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return ""
		}

		return string(b)
	}
}

// unmarshalActionLogData decodes a stored data_json column. A row that somehow
// holds unparseable JSON yields nil rather than failing the read: the event's
// identity (id, type, timestamp) is still worth returning.
func unmarshalActionLogData(raw string) map[string]string {
	if raw == "" || raw == "{}" {
		return nil
	}
	var out map[string]string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}

	return out
}

// boolToInt renders a bool for SQLite's integer booleans.
func boolToInt(b bool) int {
	if b {
		return 1
	}

	return 0
}

// MarshalActionLogData encodes a flattened data map for the data_json column.
// An empty map stores "{}" so the column is always valid JSON for json_extract.
func MarshalActionLogData(data map[string]string) (string, error) {
	if len(data) == 0 {
		return "{}", nil
	}
	b, err := json.Marshal(data)
	if err != nil {
		return "{}", fmt.Errorf("assets: encode action_log data: %w", err)
	}

	return string(b), nil
}
