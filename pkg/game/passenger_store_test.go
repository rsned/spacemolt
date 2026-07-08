package game

import (
	"io"
	"log"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/internal/protocol"
)

// TestStoreRawJSON_PassengerCommands verifies that the passenger feature's
// action-less `ok` responses are stored under their command name (not only
// under the shared "_last" slot), so play_as's lookupRawJSON(command) finds
// them and the styled formatters render instead of falling back to raw JSON.
//
// Regression: list_station_passengers returned type=ok with a payload that has
// no "action" field, so storeRawJSON never keyed it by command and the REPL
// printed nothing.
func TestStoreRawJSON_PassengerCommands(t *testing.T) {
	cases := []struct {
		name    string
		key     string // distinctive payload field
		payload map[string]any
		marker  string // substring expected in the stored JSON
	}{
		{
			name: "list_station_passengers",
			key:  "list_station_passengers",
			payload: map[string]any{
				"count":   1,
				"station": "Grand Exchange Station",
				"waiting": []any{map[string]any{"name": "Auberon Demme", "class": "first"}},
			},
			marker: "Grand Exchange Station",
		},
		{
			name: "list_passengers",
			key:  "list_passengers",
			payload: map[string]any{
				"count":          1,
				"economy_berths": "1/12",
				"passengers":     []any{map[string]any{"name": "Zed Aulin", "class": "economy"}},
			},
			marker: "Zed Aulin",
		},
		{
			name: "load_passenger",
			key:  "load_passenger",
			payload: map[string]any{
				"message":    "Boarded.",
				"count":      1,
				"total_fare": 420,
				"loaded":     []any{map[string]any{"name": "Zed Aulin"}},
			},
			marker: "Boarded.",
		},
		{
			name: "unload_passenger",
			key:  "unload_passenger",
			payload: map[string]any{
				"name":           "Zed Aulin",
				"delivered":      true,
				"fare_collected": 420,
			},
			marker: "Zed Aulin",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &Client{
				latestRawJSON: make(map[string][]byte),
				debugLogger:   log.New(io.Discard, "", 0),
			}
			c.storeRawJSON(protocol.Response{Type: protocol.TypeOK, Payload: tc.payload})

			got := string(c.GetRawJSON(tc.key))
			if got == "" {
				t.Fatalf("nothing stored under %q", tc.key)
			}
			if !strings.Contains(got, tc.marker) {
				t.Errorf("stored payload under %q = %q, want it to contain %q", tc.key, got, tc.marker)
			}
		})
	}
}
