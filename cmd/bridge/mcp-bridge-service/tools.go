package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
	"github.com/rsned/spacemolt/pkg/game"
)

// Tool definitions with agent_id parameter
var toolDefinitions = []map[string]any{
	{
		"name":        "get_status",
		"description": "Get current status for an agent (location, credits, fuel, cargo, etc.)",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"agent_id": map[string]any{
					"type":        "string",
					"description": "Agent ID (e.g., 'pirate-4', 'explorer-1')",
				},
			},
			"required": []string{"agent_id"},
		},
	},
	{
		"name":        "get_active_missions",
		"description": "Get list of active missions for an agent",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"agent_id": map[string]any{
					"type":        "string",
					"description": "Agent ID",
				},
			},
			"required": []string{"agent_id"},
		},
	},
	{
		"name":        "get_ships",
		"description": "Get list of ships owned by an agent",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"agent_id": map[string]any{
					"type":        "string",
					"description": "Agent ID",
				},
			},
			"required": []string{"agent_id"},
		},
	},
	{
		"name":        "switch_ship",
		"description": "Switch to a different ship",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"agent_id": map[string]any{
					"type":        "string",
					"description": "Agent ID",
				},
				"ship_name": map[string]any{
					"type":        "string",
					"description": "Name of ship to switch to",
				},
			},
			"required": []string{"agent_id", "ship_name"},
		},
	},
	{
		"name":        "get_system",
		"description": "Get information about current system (POIs, stations, etc.)",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"agent_id": map[string]any{
					"type":        "string",
					"description": "Agent ID",
				},
			},
			"required": []string{"agent_id"},
		},
	},
	{
		"name":        "travel",
		"description": "Travel to a POI in the current system",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"agent_id": map[string]any{
					"type":        "string",
					"description": "Agent ID",
				},
				"poi_id": map[string]any{
					"type":        "string",
					"description": "Point of interest ID to travel to",
				},
			},
			"required": []string{"agent_id", "poi_id"},
		},
	},
	{
		"name":        "jump",
		"description": "Jump to another star system",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"agent_id": map[string]any{
					"type":        "string",
					"description": "Agent ID",
				},
				"system_name": map[string]any{
					"type":        "string",
					"description": "Name of system to jump to",
				},
			},
			"required": []string{"agent_id", "system_name"},
		},
	},
	{
		"name":        "dock",
		"description": "Dock at current station",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"agent_id": map[string]any{
					"type":        "string",
					"description": "Agent ID",
				},
			},
			"required": []string{"agent_id"},
		},
	},
	{
		"name":        "undock",
		"description": "Undock from current station",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"agent_id": map[string]any{
					"type":        "string",
					"description": "Agent ID",
				},
			},
			"required": []string{"agent_id"},
		},
	},
	{
		"name":        "get_missions",
		"description": "Get available missions at current station",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"agent_id": map[string]any{
					"type":        "string",
					"description": "Agent ID",
				},
			},
			"required": []string{"agent_id"},
		},
	},
	{
		"name":        "accept_mission",
		"description": "Accept a mission by ID",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"agent_id": map[string]any{
					"type":        "string",
					"description": "Agent ID",
				},
				"mission_id": map[string]any{
					"type":        "string",
					"description": "Mission ID to accept",
				},
			},
			"required": []string{"agent_id", "mission_id"},
		},
	},
	{
		"name":        "captains_log_add",
		"description": "Add entry to captain's log",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"agent_id": map[string]any{
					"type":        "string",
					"description": "Agent ID",
				},
				"entry": map[string]any{
					"type":        "string",
					"description": "Log entry text",
				},
				"mood": map[string]any{
					"type":        "string",
					"description": "Current mood (optional)",
				},
			},
			"required": []string{"agent_id", "entry"},
		},
	},
	{
		"name":        "captains_log_get",
		"description": "Get recent captain's log entries",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"agent_id": map[string]any{
					"type":        "string",
					"description": "Agent ID",
				},
				"limit": map[string]any{
					"type":        "number",
					"description": "Number of entries to retrieve (default: 10)",
				},
			},
			"required": []string{"agent_id"},
		},
	},
	{
		"name":        "refuel",
		"description": "Refuel ship at current station",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"agent_id": map[string]any{
					"type":        "string",
					"description": "Agent ID",
				},
			},
			"required": []string{"agent_id"},
		},
	},
	{
		"name":        "repair",
		"description": "Repair ship hull at current station",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"agent_id": map[string]any{
					"type":        "string",
					"description": "Agent ID",
				},
			},
			"required": []string{"agent_id"},
		},
	},
	{
		"name":        "find_route",
		"description": "Find route between star systems",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"agent_id": map[string]any{
					"type":        "string",
					"description": "Agent ID",
				},
				"from": map[string]any{
					"type":        "string",
					"description": "Starting system name",
				},
				"to": map[string]any{
					"type":        "string",
					"description": "Destination system name",
				},
			},
			"required": []string{"agent_id", "from", "to"},
		},
	},
}

// handleToolsList returns the list of available tools
func (b *BridgeService) handleToolsList(req JSONRPCRequest) *JSONRPCResponse {
	result := map[string]any{
		"tools": toolDefinitions,
	}

	resultJSON, _ := json.Marshal(result)

	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  resultJSON,
	}
}

// handleToolsCall executes a tool call
func (b *BridgeService) handleToolsCall(req JSONRPCRequest) *JSONRPCResponse {
	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}

	if err := json.Unmarshal(req.Params, &params); err != nil {
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &JSONRPCError{
				Code:    -32602,
				Message: "Invalid params",
				Data:    err.Error(),
			},
		}
	}

	// Extract agent_id
	agentID, ok := params.Arguments["agent_id"].(string)
	if !ok || agentID == "" {
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &JSONRPCError{
				Code:    -32602,
				Message: "Missing required parameter: agent_id",
			},
		}
	}

	// Get connection for this agent
	conn, err := b.pool.GetConnection(agentID)
	if err != nil {
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &JSONRPCError{
				Code:    -32603,
				Message: fmt.Sprintf("Failed to get connection for agent %s", agentID),
				Data:    err.Error(),
			},
		}
	}

	// Execute the tool
	result, err := b.executeTool(conn, params.Name, params.Arguments)
	if err != nil {
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &JSONRPCError{
				Code:    -32603,
				Message: fmt.Sprintf("Tool execution failed: %v", err),
			},
		}
	}

	// Format result as MCP tool response
	toolResult := map[string]any{
		"content": []map[string]any{
			{
				"type": "text",
				"text": result,
			},
		},
	}

	resultJSON, _ := json.Marshal(toolResult)

	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  resultJSON,
	}
}

// executeTool executes a game command for an agent
func (b *BridgeService) executeTool(conn *AgentConnection, toolName string, args map[string]any) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn.logger.Printf("Executing tool: %s", toolName)

	switch toolName {
	case "get_status":
		conn.logger.Printf("Getting state...")
		conn.logger.Printf("About to call GetState()...")

		// Try with a timeout channel to detect if GetState blocks
		stateChan := make(chan *game.State, 1)
		go func() {
			stateChan <- conn.client.GetState()
		}()

		var state *game.State
		select {
		case state = <-stateChan:
			conn.logger.Printf("Got state successfully")
		case <-time.After(5 * time.Second):
			return "", fmt.Errorf("GetState() timed out after 5 seconds - possible deadlock")
		}

		if state == nil {
			return "", fmt.Errorf("state is nil")
		}
		conn.logger.Printf("Got state, marshaling...")
		data, _ := json.MarshalIndent(map[string]any{
			"agent":          conn.agentID,
			"username":       state.Player.Username,
			"credits":        state.Credits,
			"system":         state.CurrentSystem,
			"poi":            state.CurrentPOI,
			"ship":           state.Ship.Name,
			"fuel":           fmt.Sprintf("%.0f/%.0f", state.Fuel, state.MaxFuel),
			"hull":           fmt.Sprintf("%.0f/%.0f", state.Hull, state.MaxHull),
			"cargo":          fmt.Sprintf("%.0f/%.0f", state.Ship.CargoUsed, state.Ship.CargoCapacity),
			"docked":         state.Doc,
			"traveling":      state.Traveling,
			"in_combat":      state.InCombat,
		}, "", "  ")
		return string(data), nil

	case "get_active_missions":
		if err := conn.client.Send(ctx, protocol.Message{Type: "get_active_missions"}); err != nil {
			return "", err
		}
		time.Sleep(2 * time.Second)
		return "Active missions requested - response sent to game client", nil

	case "get_ships":
		if err := conn.client.Send(ctx, protocol.Message{Type: "get_ships"}); err != nil {
			return "", err
		}
		time.Sleep(1 * time.Second)
		// Response will be in last message
		return "Ships list requested - check game response", nil

	case "switch_ship":
		shipName, _ := args["ship_name"].(string)
		if err := conn.client.Send(ctx, protocol.Message{
			Type:    "switch_ship",
			Payload: map[string]any{"ship_name": shipName},
		}); err != nil {
			return "", err
		}
		time.Sleep(2 * time.Second)
		return fmt.Sprintf("Switched to ship: %s", shipName), nil

	case "get_system":
		if err := conn.client.GetSystem(ctx); err != nil {
			return "", err
		}
		time.Sleep(1 * time.Second)
		state := conn.client.GetState()
		data, _ := json.MarshalIndent(state.System, "", "  ")
		return string(data), nil

	case "travel":
		poiID, _ := args["poi_id"].(string)
		if _, err := conn.client.Travel(ctx, poiID); err != nil {
			return "", err
		}
		return fmt.Sprintf("Traveling to %s", poiID), nil

	case "jump":
		systemName, _ := args["system_name"].(string)
		if err := conn.client.Send(ctx, protocol.Message{
			Type:    "jump",
			Payload: map[string]any{"system": systemName},
		}); err != nil {
			return "", err
		}
		return fmt.Sprintf("Jumping to %s", systemName), nil

	case "dock":
		if err := conn.client.Dock(ctx); err != nil {
			return "", err
		}
		return "Docking at station", nil

	case "undock":
		if err := conn.client.Undock(ctx); err != nil {
			return "", err
		}
		return "Undocking from station", nil

	case "get_missions":
		if err := conn.client.Send(ctx, protocol.Message{Type: "get_missions"}); err != nil {
			return "", err
		}
		time.Sleep(1 * time.Second)
		return "Missions list requested - check response", nil

	case "accept_mission":
		missionID, _ := args["mission_id"].(string)
		if err := conn.client.Send(ctx, protocol.Message{
			Type:    "accept_mission",
			Payload: map[string]any{"mission_id": missionID},
		}); err != nil {
			return "", err
		}
		return fmt.Sprintf("Accepted mission: %s", missionID), nil

	case "captains_log_add":
		entry, _ := args["entry"].(string)
		mood, _ := args["mood"].(string)
		if err := conn.client.Send(ctx, protocol.Message{
			Type: "captains_log_add",
			Payload: map[string]any{
				"entry": entry,
				"mood":  mood,
			},
		}); err != nil {
			return "", err
		}
		return "Captain's log entry added", nil

	case "captains_log_get":
		limit := 10
		if l, ok := args["limit"].(float64); ok {
			limit = int(l)
		}
		if err := conn.client.Send(ctx, protocol.Message{
			Type:    "captains_log_get",
			Payload: map[string]any{"limit": limit},
		}); err != nil {
			return "", err
		}
		time.Sleep(1 * time.Second)
		return "Captain's log requested", nil

	case "refuel":
		if err := conn.client.Refuel(ctx); err != nil {
			return "", err
		}
		return "Refueling ship", nil

	case "repair":
		if err := conn.client.Repair(ctx); err != nil {
			return "", err
		}
		return "Repairing hull", nil

	case "find_route":
		from, _ := args["from"].(string)
		to, _ := args["to"].(string)
		if err := conn.client.Send(ctx, protocol.Message{
			Type: "find_route",
			Payload: map[string]any{
				"from": from,
				"to":   to,
			},
		}); err != nil {
			return "", err
		}
		time.Sleep(1 * time.Second)
		return fmt.Sprintf("Route from %s to %s calculated", from, to), nil

	default:
		return "", fmt.Errorf("unknown tool: %s", toolName)
	}
}
