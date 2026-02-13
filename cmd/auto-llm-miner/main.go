package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/llm"
	"github.com/rsned/spacemolt/pkg/prompts"
)

const (
	// Use decision.v3 template (simplified, working version)
	TEMPLATE_VERSION = 3
	ROLE = "miner"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: auto-llm-miner <agent-id>")
		fmt.Println("Example: auto-llm-miner miner-1")
		fmt.Println()
		os.Exit(1)
	}

	agentID := os.Args[1]
	logger := log.New(os.Stdout, fmt.Sprintf("[%s] ", agentID), log.LstdFlags)
	ctx := context.Background()

	// Initialize game client (uses shared library functions)
	client, creds, err := game.InitializeAgent(agentID, logger, ctx)
	if err != nil {
		log.Fatalf("Failed to initialize agent: %v", err)
	}
	defer client.Close()

	// Initialize prompt manager
	cfg := prompts.Config{
		TemplatesDir: "../../data/prompts/templates",
		ConfigPath:  "../../agents_config.yaml",
	}
	promptMgr, err := prompts.NewManager(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize prompt manager: %v", err)
	}

	logger.Printf("🤖 LLM-Powered autonomous miner starting...")
	logger.Printf("Agent: %s | Empire: %s | Credits: %.2f | Ship: %s | Cargo: %.0f/%.0f",
		creds.Username, creds.Empire, creds.Credits,
		creds.Ship.Name, creds.Ship.CargoUsed, creds.Ship.CargoCapacity)

	// Main LLM decision loop
	runCount := 0
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		runCount++
		logger.Printf("═══ Run #%d ═══", runCount)

		// Get current game state
		state := client.GetState()

		// Build template context with playbook content for miner role
		playbookContent := loadPlaybookForRole(ROLE, logger)
		ctx := prompts.NewTemplateContext(
			agentID,
			creds.Username,
			ROLE,
			map[string]interface{}{
				"name": creds.Username,
				"role": ROLE,
				"background": "An autonomous miner enhanced with LLM decision-making. Uses comprehensive strategy from playbook to optimize mining operations, equipment upgrades, and credit accumulation.",
			},
			state,
			nil, // knowledge - TODO: implement persistent knowledge base
			nil, // history - TODO: implement action history
			nil, // lastFeedback - will add after action
			nil, // system - TODO: add system info
			nil, // goal - TODO: implement goal tracking
		)

		// Render prompt with template
		prompt, err := promptMgr.RenderPrompt("decision", TEMPLATE_VERSION, ctx)
		if err != nil {
			logger.Printf("⚠️  Failed to render prompt: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}

		logger.Printf("📋 LLM Decision Prompt:")
		logger.Printf("%s", prompt)

		// Query LLM for decision
		response, err := llm.Query(ctx, llm.Config{
			BaseURL: "http://localhost:11434",
			Model:   "llama3.2",
		}, llm.DecisionRequest{
			Prompt:      prompt,
			Temperature: 0.7, // Balance creativity with consistency
			MaxTokens:    2048,
		})
		if err != nil {
			logger.Printf("❌ LLM query failed: %v", err)
			time.Sleep(10 * time.Second)
			continue
		}

		// Parse LLM response for structured command
		action, args, err := parseAction(response.Content)
		if err != nil {
			logger.Printf("❌ Failed to parse LLM response: %v", err)
			logger.Printf("Raw LLM output: %s", response.Content)
			time.Sleep(5 * time.Second)
			continue
		}

		logger.Printf("🎯 Executing: %s %s", action, args)

		// Execute action
		var result protocol.Response
		var message string

		err := executeAction(client, ctx, action, args, &result)
		if err != nil {
			logger.Printf("❌ Action execution failed: %v", err)
		} else {
			message = result.Payload["message"].(string)
		}

		// Update feedback for next iteration
		ctx = prompts.NewTemplateContext(
			agentID,
			creds.Username,
			ROLE,
			map[string]interface{}{
				"name": creds.Username,
				"role": ROLE,
				"background": "An autonomous miner enhanced with LLM decision-making. Uses comprehensive strategy from playbook to maximize mining efficiency and profit.",
			},
			state,
			nil, // knowledge
			prompts.HistoryContext{
				RecentActions: []prompts.ActionRecord{
					{
						Action:   action,
						Success: err == nil,
						Message: message,
					},
				},
			},
			prompts.FeedbackContext{
				Success: err == nil,
				Action:   action,
				Message: message,
			},
			nil, // system
			nil, // goal
		)

		// Wait for rate limiting (10s per game action)
		logger.Printf("⏱ Waiting 10 seconds for next action...")
		time.Sleep(10 * time.Second)
	}
}

// loadPlaybookForRole loads playbook markdown content for a role
func loadPlaybookForRole(role string, logger *log.Logger) string {
	switch role {
	case "miner":
		content, err := loadPlaybookContent(logger, "../../playbook/miner.md")
		if err != nil {
			logger.Printf("⚠️  Failed to load miner playbook: %v", err)
		}
		return content
	default:
		return ""
	}
}

// parseAction extracts structured command from LLM response
// Expected format: "action_name arg1=value1 arg2=value2"
func parseAction(response string) (string, map[string]string, error) {
	trimmed := strings.TrimSpace(response)

	if trimmed == "" {
		return "", nil, fmt.Errorf("empty LLM response")
	}

	// Find action name (first word before space)
	spaceIdx := strings.Index(trimmed, " ")
	if spaceIdx == -1 {
		return "", nil, fmt.Errorf("no action found")
	}

	actionName := trimmed[:spaceIdx]
	args := make(map[string]string)

	// Parse arguments (key=value pairs after action name)
	parts := strings.Fields(trimmed[spaceIdx+1:])
	for _, part := range parts {
		if strings.Contains(part, "=") {
			kv := strings.SplitN(part, "=", 2)
			if len(kv) == 2 {
				args[kv[0]] = kv[1]
			}
		}
	}

	return actionName, args, nil
}

// executeAction performs the game action corresponding to parsed command
func executeAction(client *game.Client, ctx context.Context, action string, args map[string]string, result *protocol.Response) error {
	logger.Printf("Processing action: %s", action)

	switch action {
	case "undock":
		err := client.Undock(ctx)
		if err == nil && result != nil {
			result.Type = protocol.TypeOK
			result.Payload = map[string]interface{}{
				"message": "Undocked successfully",
			}
		}
	case "travel":
		if len(args) == 0 {
			err = fmt.Errorf("travel requires poi_id")
		} else {
			poiID := args["poi_id"]
			err = client.Travel(ctx, poiID)
		}
	case "mine":
		err := client.Mine(ctx)
	case "dock":
		err := client.Dock(ctx)
	case "sell_all":
		err = client.SellAllBulk(ctx, nil)
	case "sell":
		if len(args) < 2 {
			err = fmt.Errorf("sell requires item_id and quantity")
		} else {
			itemID := args["item_id"]
			var quantity float64
			if q, ok := args["quantity"]; ok {
				quantity, _ = strconv.ParseFloat(q)
			} else {
				quantity = 1
			}
			err = client.Sell(ctx, itemID, quantity)
		}
	case "buy":
		if len(args) < 2 {
			err = fmt.Errorf("buy requires item_id and quantity")
		} else {
			itemID := args["item_id"]
			var quantity float64
			if q, ok := args["quantity"]; ok {
				quantity, _ = strconv.ParseFloat(q)
			} else {
				quantity = 1
			}
			err = client.Buy(ctx, itemID, quantity)
		}
	case "install":
		if len(args) == 0 {
			err = fmt.Errorf("install requires module_id")
		} else {
			moduleID := args["module_id"]
			err = client.Install(ctx, moduleID)
		}
	case "refuel":
		err = client.Refuel(ctx)
	case "repair":
		err = client.Repair(ctx)
	case "get_status":
		_, err = client.GetStatus(ctx)
	case "get_system":
		_, err = client.GetSystem(ctx)
	default:
		err = fmt.Errorf("unknown action: %s", action)
	}

	if err != nil && result == nil {
		result.Type = protocol.TypeOK
		result.Payload = map[string]interface{}{
			"message": "Action completed",
		}
	}

	return err
}
