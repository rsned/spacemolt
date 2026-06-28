package game

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// decodePassengerPayload re-encodes a response payload (map[string]any) into the
// typed struct v via a JSON round-trip — the same shape the server sent, so field
// tags line up exactly.
func decodePassengerPayload(payload map[string]any, v any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("decode payload: %w", err)
	}
	return nil
}

// ListStationPassengers returns the passengers waiting for transport at station
// (its id or name). An empty station queries the currently docked station. This
// is a query (no tick cost).
func (c *Client) ListStationPassengers(ctx context.Context, station string) (*serverapi.ListStationPassengersResponse, error) {
	payload := map[string]any{}
	if station != "" {
		payload["station"] = station
	}
	msg := protocol.Message{
		Type:      "list_station_passengers",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTerminator(terminateOnActionOrOK), WithTimeout(SleepMedium))
	if err != nil {
		return nil, err
	}
	resp, err := c.await(ctx, h)
	if err != nil {
		return nil, err
	}
	var out serverapi.ListStationPassengersResponse
	if err := decodePassengerPayload(resp.Payload, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// LoadPassenger boards every waiting passenger bound for the given destination
// station that fits the ship's berths, and returns who actually boarded plus the
// booked total fare. Fewer may board than were waiting (see LoadPassengerResponse).
func (c *Client) LoadPassenger(ctx context.Context, destination string) (*serverapi.LoadPassengerResponse, error) {
	if destination == "" {
		return nil, fmt.Errorf("load_passenger: missing destination station")
	}
	msg := protocol.Message{
		Type:      "load_passenger",
		Payload:   map[string]any{"destination": destination},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTerminator(terminateOnActionOrOK), WithTimeout(SleepMedium))
	if err != nil {
		return nil, err
	}
	resp, err := c.await(ctx, h)
	if err != nil {
		return nil, err
	}
	var out serverapi.LoadPassengerResponse
	if err := decodePassengerPayload(resp.Payload, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
