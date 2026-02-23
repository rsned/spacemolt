package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
	"github.com/rsned/spacemolt/pkg/game"
)

type TraderAgent struct {
	logger *log.Logger
}

func (t *TraderAgent) OnConnected(state *game.State) {
	t.logger.Printf("Connected! Credits: %.2f", state.Credits)
}

func (t *TraderAgent) OnMessage(resp protocol.Response) {
	switch resp.Type {
	case protocol.TypeOK:
		if msg, ok := resp.Payload["message"].(string); ok {
			t.logger.Printf("OK: %s", msg)
		}
	case protocol.TypeError:
		if msg, ok := resp.Payload["message"].(string); ok {
			t.logger.Printf("ERROR: %s", msg)
		}
	}
}

func (t *TraderAgent) OnDisconnected(err error) {
	t.logger.Printf("Disconnected: %v", err)
}

func updateCaptainsLog(agentID string, client *game.Client, tradingRuns int, creditsEarned float64, strategy string, tradeInfo ...string) {
	state := client.GetState()

	// Extract optional trade phase and route name.
	var tradePhaseStr, tradeRouteName string
	if len(tradeInfo) >= 1 {
		tradePhaseStr = tradeInfo[0]
	}
	if len(tradeInfo) >= 2 {
		tradeRouteName = tradeInfo[1]
	}

	var notes []string
	notes = append(notes, fmt.Sprintf("Trading runs completed: %d", tradingRuns))
	notes = append(notes, fmt.Sprintf("Credits earned this run: %.2f", creditsEarned))
	notes = append(notes, fmt.Sprintf("Current credits: %.2f", state.Credits))
	notes = append(notes, fmt.Sprintf("Ship: %s (%d modules)", state.Ship.Name, len(state.Ship.Modules)))
	notes = append(notes, fmt.Sprintf("Hull: %.0f/%.0f (%.0f%%)", state.Hull, state.MaxHull, (state.Hull/state.MaxHull)*100))
	notes = append(notes, fmt.Sprintf("Fuel: %.0f/%.0f", state.Fuel, state.MaxFuel))
	if len(state.Ship.Cargo) > 0 {
		notes = append(notes, fmt.Sprintf("Cargo: %d items (%.0f/%.0f)", len(state.Ship.Cargo), state.Ship.CargoUsed, state.Ship.CargoCapacity))
	}
	if strategy == "trade" && tradePhaseStr != "" {
		notes = append(notes, fmt.Sprintf("Trade phase: %s", tradePhaseStr))
	}
	if strategy == "trade" && tradeRouteName != "" {
		notes = append(notes, fmt.Sprintf("Route: %s", tradeRouteName))
	}

	currentGoal := "Autonomous trading operations - finding profitable trade routes"
	if state.Doc {
		switch strategy {
		case "sell":
			currentGoal = "Docked at station - selling cargo and monitoring markets"
		case "craft-sell":
			currentGoal = "Docked at station - crafting items, selling cargo, and monitoring markets"
		case "craft-deposit":
			currentGoal = "Docked at station - crafting items and depositing to storage"
		case "trade":
			currentGoal = fmt.Sprintf("Docked at station - executing trade route: %s (phase: %s)", tradeRouteName, tradePhaseStr)
		}
	} else if state.Traveling && state.TravelProgress != nil {
		currentGoal = fmt.Sprintf("Traveling to %s", state.TravelProgress.Destination)
	} else if state.Traveling {
		currentGoal = "Traveling (destination unknown)"
	} else if !state.Doc && len(state.Ship.Cargo) > 0 {
		currentGoal = "In space - returning to station for trading operations"
	}

	entry := &game.AgentLog{
		AgentName:   state.Player.Username,
		CurrentGoal: currentGoal,
		Location:    fmt.Sprintf("System: %s, POI: %s", state.CurrentSystem, state.CurrentPOI),
		Notes:       notes,
	}

	_ = game.WriteCaptainsLog(agentID, entry)
}

func tradingLoop(agentID string, client *game.Client, logger *log.Logger, ctx context.Context, stationAction game.StationActionStrategy, strategy string) error {
	// For now, the auto-trader will operate in a simple loop:
	// 1. If docked with cargo, execute station action (craft/sell/deposit)
	// 2. If not docked, travel to nearest station
	// 3. Refuel and repair as needed
	// TODO: Add market analysis and profitable trade route finding

	runNum := 0
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	logTicker := time.NewTicker(2 * time.Minute)
	defer logTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			runNum++
			state := client.GetState()

			logger.Printf("═══ Trading Run #%d ═══", runNum)
			logger.Printf("Credits: %.2f | Fuel: %.0f/%.0f | Hull: %.0f/%.0f | Cargo: %.1f/%.1f",
				state.Credits, state.Fuel, state.MaxFuel, state.Hull, state.MaxHull,
				state.Ship.CargoUsed, state.Ship.CargoCapacity)

			// Get system data if needed
			if len(state.System.POIs) == 0 {
				logger.Printf("Fetching system data...")
				if err := client.GetSystem(ctx); err != nil {
					logger.Printf("Failed to get system: %v", err)
				}
				time.Sleep(2 * time.Second)
				state = client.GetState()
			}

			// Find nearest station
			var stationPOI string
			for _, poi := range state.System.POIs {
				if poi.Type == "station" {
					stationPOI = poi.ID
					break
				}
			}

			if stationPOI == "" {
				logger.Printf("No station found in system %s", state.System.Name)
				time.Sleep(30 * time.Second)
				continue
			}

			// Travel to station if not docked
			if !state.Doc {
				if state.CurrentPOI != stationPOI && !state.Traveling {
					logger.Printf("Traveling to station %s...", stationPOI)
					if err := client.Travel(ctx, stationPOI); err != nil {
						logger.Printf("Travel error: %v", err)
					}
					time.Sleep(20 * time.Second)
				}

				// Dock at station
				state = client.GetState()
				if !state.Doc && !state.Traveling {
					logger.Printf("Docking at station...")
					if err := client.Dock(ctx); err != nil {
						if err.Error() != "Already docked (success)" {
							logger.Printf("Dock error: %v", err)
						}
					}
					time.Sleep(15 * time.Second)
				}
			}

			// Execute station action if docked with cargo
			state = client.GetState()
			creditsBefore := state.Credits
			if state.Doc && len(state.Ship.Cargo) > 0 {
				logger.Printf("Executing station action strategy: %s", strategy)
				if err := stationAction(client, logger, ctx); err != nil {
					logger.Printf("Station action error: %v", err)
				} else {
					state = client.GetState()
					creditsEarned := state.Credits - creditsBefore
					if creditsEarned > 0 {
						logger.Printf("Credits earned: %.2f", creditsEarned)
					}
				}
			} else if !state.Doc {
				logger.Printf("Not docked, waiting...")
			} else if len(state.Ship.Cargo) == 0 {
				logger.Printf("Cargo is empty, monitoring market conditions...")
			}

			// Refuel if needed
			state = client.GetState()
			if state.Doc && state.Fuel < state.MaxFuel*0.8 {
				logger.Printf("Refueling...")
				if err := client.Refuel(ctx); err != nil {
					logger.Printf("Refuel error: %v", err)
				}
				time.Sleep(3 * time.Second)
			}

			// Repair if needed
			state = client.GetState()
			if state.Doc && state.Hull < state.MaxHull*0.9 {
				logger.Printf("Repairing hull...")
				if err := client.Repair(ctx); err != nil {
					logger.Printf("Repair error: %v", err)
				}
				time.Sleep(3 * time.Second)
			}

			// Update captain's log
			state = client.GetState()
			runCreditsEarned := state.Credits - creditsBefore
			updateCaptainsLog(agentID, client, runNum, runCreditsEarned, strategy)

			logger.Printf("═══ Run #%d Complete ═══", runNum)
			logger.Printf("Current Credits: %.2f\n", state.Credits)

		case <-logTicker.C:
			state := client.GetState()
			logger.Printf("Status: Credits: %.2f | Fuel: %.0f/%.0f | Hull: %.0f/%.0f | Docked: %v | Location: %s",
				state.Credits, state.Fuel, state.MaxFuel, state.Hull,
				state.MaxHull, state.Doc, state.System.Name)
			updateCaptainsLog(agentID, client, runNum, 0, strategy)
		}
	}
}

// tradePhase tracks the current phase of a trade route execution.
type tradePhase int

const (
	phaseBuyAtHome    tradePhase = iota // Buy outbound ore at home station
	phaseTravelToSell                   // Travel to destination empire
	phaseSellAtDest                     // Sell outbound ore at destination
	phaseBuyReturn                      // Buy return ore at destination
	phaseTravelHome                     // Travel back to home system
	phaseSellAtHome                     // Sell return ore at home station
)

func (p tradePhase) String() string {
	switch p {
	case phaseBuyAtHome:
		return "BUY_AT_HOME"
	case phaseTravelToSell:
		return "TRAVEL_TO_SELL"
	case phaseSellAtDest:
		return "SELL_AT_DEST"
	case phaseBuyReturn:
		return "BUY_RETURN"
	case phaseTravelHome:
		return "TRAVEL_HOME"
	case phaseSellAtHome:
		return "SELL_AT_HOME"
	default:
		return "UNKNOWN"
	}
}

// tradeLoopState tracks persistent state across the trade loop iterations.
type tradeLoopState struct {
	phase         tradePhase
	outbound      game.TradeRoute
	returnLeg     game.TradeRoute
	hasReturnLeg  bool
	homeSystem    string
	destSystem    string
	tripCount     int
	totalProfit   float64
	tripStartCred float64
}

// persistedTradeState is the JSON-serializable form of tradeLoopState for crash recovery.
type persistedTradeState struct {
	Phase         string          `json:"phase"`
	Outbound      game.TradeRoute `json:"outbound_route"`
	ReturnLeg     game.TradeRoute `json:"return_route"`
	HasReturnLeg  bool            `json:"has_return_leg"`
	HomeSystem    string          `json:"home_system"`
	DestSystem    string          `json:"dest_system"`
	TripCount     int             `json:"trip_count"`
	TotalProfit   float64         `json:"total_profit"`
	TripStartCred float64         `json:"trip_start_credits"`
	LastUpdated   time.Time       `json:"last_updated"`
}

func tradeStateFilePath(agentID string) string {
	return filepath.Join("data", "agents", agentID, "trade_state.json")
}

func saveTradeState(agentID string, ts *tradeLoopState) error {
	ps := &persistedTradeState{
		Phase:         ts.phase.String(),
		Outbound:      ts.outbound,
		ReturnLeg:     ts.returnLeg,
		HasReturnLeg:  ts.hasReturnLeg,
		HomeSystem:    ts.homeSystem,
		DestSystem:    ts.destSystem,
		TripCount:     ts.tripCount,
		TotalProfit:   ts.totalProfit,
		TripStartCred: ts.tripStartCred,
		LastUpdated:   time.Now(),
	}

	data, err := json.MarshalIndent(ps, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal trade state: %w", err)
	}

	filePath := tradeStateFilePath(agentID)
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return fmt.Errorf("create trade state dir: %w", err)
	}

	tmpPath := filePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("write trade state tmp: %w", err)
	}
	if err := os.Rename(tmpPath, filePath); err != nil {
		return fmt.Errorf("rename trade state: %w", err)
	}
	return nil
}

func loadTradeState(agentID string) (*persistedTradeState, error) {
	data, err := os.ReadFile(tradeStateFilePath(agentID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read trade state: %w", err)
	}

	var ps persistedTradeState
	if err := json.Unmarshal(data, &ps); err != nil {
		return nil, fmt.Errorf("unmarshal trade state: %w", err)
	}
	return &ps, nil
}

func parsePhaseString(s string) tradePhase {
	switch s {
	case "BUY_AT_HOME":
		return phaseBuyAtHome
	case "TRAVEL_TO_SELL":
		return phaseTravelToSell
	case "SELL_AT_DEST":
		return phaseSellAtDest
	case "BUY_RETURN":
		return phaseBuyReturn
	case "TRAVEL_HOME":
		return phaseTravelHome
	case "SELL_AT_HOME":
		return phaseSellAtHome
	default:
		return phaseBuyAtHome
	}
}

func tradeLoop(agentID string, client *game.Client, logger *log.Logger, ctx context.Context,
	outbound game.TradeRoute, returnLeg game.TradeRoute, hasReturn bool, homeEmpire string) error {

	homeSystem := game.EmpireHomeSystem(homeEmpire)
	destSystem := game.EmpireHomeSystem(outbound.SellEmpire)

	if homeSystem == "" {
		return fmt.Errorf("unknown home system for empire %q", homeEmpire)
	}
	if destSystem == "" {
		return fmt.Errorf("unknown destination system for empire %q", outbound.SellEmpire)
	}

	ts := &tradeLoopState{
		outbound:     outbound,
		returnLeg:    returnLeg,
		hasReturnLeg: hasReturn,
		homeSystem:   homeSystem,
		destSystem:   destSystem,
	}

	// Try to restore persisted state from a previous run.
	state := client.GetState()
	restored := false
	if ps, err := loadTradeState(agentID); err != nil {
		logger.Printf("Warning: failed to load trade state: %v (starting fresh)", err)
	} else if ps != nil {
		// Validate the persisted route matches the current route.
		if ps.Outbound.OreID == outbound.OreID &&
			ps.Outbound.BuyEmpire == outbound.BuyEmpire &&
			ps.Outbound.SellEmpire == outbound.SellEmpire {
			ts.phase = parsePhaseString(ps.Phase)
			ts.tripCount = ps.TripCount
			ts.totalProfit = ps.TotalProfit
			ts.tripStartCred = ps.TripStartCred
			restored = true
			logger.Printf("Restored trade state: phase=%s, trips=%d, profit=%.2f (saved %s)",
				ts.phase, ts.tripCount, ts.totalProfit, ps.LastUpdated.Format("15:04:05"))
		} else {
			logger.Printf("Warning: persisted route (%s %s→%s) doesn't match current (%s %s→%s), starting fresh",
				ps.Outbound.OreID, ps.Outbound.BuyEmpire, ps.Outbound.SellEmpire,
				outbound.OreID, outbound.BuyEmpire, outbound.SellEmpire)
		}
	}

	if !restored {
		ts.phase = determineInitialPhase(state, ts)
		ts.tripStartCred = state.Credits
	}

	logger.Printf("═══ Trade Route: %s ═══", outbound.Name)
	logger.Printf("  Outbound: Buy %s at %s, sell at %s (est. margin: %.0f cr/unit)",
		outbound.OreName, outbound.BuyEmpire, outbound.SellEmpire, outbound.Margin())
	if hasReturn {
		logger.Printf("  Return:   Buy %s at %s, sell at %s (est. margin: %.0f cr/unit)",
			returnLeg.OreName, returnLeg.BuyEmpire, returnLeg.SellEmpire, returnLeg.Margin())
	}
	logger.Printf("  Home: %s | Dest: %s | Starting phase: %s", homeSystem, destSystem, ts.phase)

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	logTicker := time.NewTicker(2 * time.Minute)
	defer logTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-logTicker.C:
			updateCaptainsLog(agentID, client, ts.tripCount, ts.totalProfit, "trade", ts.phase.String(), ts.outbound.Name)
case <-ticker.C:
			if err := executeTradePhase(client, logger, ctx, ts); err != nil {
				logger.Printf("Trade phase %s error: %v", ts.phase, err)
				logger.Printf("Retrying in next tick...")
			}
			if err := saveTradeState(agentID, ts); err != nil {
				logger.Printf("Warning: failed to save trade state: %v", err)
			}
			updateCaptainsLog(agentID, client, ts.tripCount, ts.totalProfit, "trade", ts.phase.String(), ts.outbound.Name)
		}
	}
}

func determineInitialPhase(state *game.State, ts *tradeLoopState) tradePhase {
	atHome := strings.EqualFold(state.System.ID, ts.homeSystem)
	atDest := strings.EqualFold(state.System.ID, ts.destSystem)
	hasCargo := len(state.Ship.Cargo) > 0

	// Check if carrying outbound ore (should sell at dest).
	hasOutboundOre := false
	hasReturnOre := false
	for _, item := range state.Ship.Cargo {
		if item.ItemID == ts.outbound.OreID {
			hasOutboundOre = true
		}
		if ts.hasReturnLeg && item.ItemID == ts.returnLeg.OreID {
			hasReturnOre = true
		}
	}

	switch {
	case atHome && hasReturnOre:
		return phaseSellAtHome
	case atHome && hasOutboundOre:
		return phaseTravelToSell
	case atHome && !hasCargo:
		return phaseBuyAtHome
	case atDest && hasOutboundOre:
		return phaseSellAtDest
	case atDest && hasReturnOre:
		return phaseTravelHome
	case atDest && !hasCargo:
		return phaseBuyReturn
	case !atHome && !atDest && hasOutboundOre:
		return phaseTravelToSell
	case !atHome && !atDest && hasReturnOre:
		return phaseTravelHome
	default:
		// Unknown state - go home.
		return phaseTravelHome
	}
}

func executeTradePhase(client *game.Client, logger *log.Logger, ctx context.Context, ts *tradeLoopState) error {
	state := client.GetState()

	// If traveling, wait for arrival.
	if state.Traveling {
		logger.Printf("In transit... (phase: %s)", ts.phase)
		return nil
	}

	switch ts.phase {
	case phaseBuyAtHome:
		return executeBuyPhase(client, logger, ctx, ts, ts.outbound, ts.homeSystem, phaseTravelToSell)

	case phaseTravelToSell:
		return executeTravelPhase(client, logger, ctx, ts, ts.destSystem, phaseSellAtDest)

	case phaseSellAtDest:
		return executeSellPhase(client, logger, ctx, ts, ts.outbound.OreID, ts.outbound.OreName)

	case phaseBuyReturn:
		if !ts.hasReturnLeg {
			// No return cargo - just head home empty.
			logger.Printf("No return leg defined, heading home empty...")
			ts.phase = phaseTravelHome
			return nil
		}
		return executeBuyPhase(client, logger, ctx, ts, ts.returnLeg, ts.destSystem, phaseTravelHome)

	case phaseTravelHome:
		return executeTravelPhase(client, logger, ctx, ts, ts.homeSystem, phaseSellAtHome)

	case phaseSellAtHome:
		return executeSellPhase(client, logger, ctx, ts, "", "")
	}

	return nil
}

func executeBuyPhase(client *game.Client, logger *log.Logger, ctx context.Context,
	ts *tradeLoopState, route game.TradeRoute, expectedSystem string, nextPhase tradePhase) error {

	state := client.GetState()

	// Ensure we're in the right system.
	if !strings.EqualFold(state.System.ID, expectedSystem) {
		logger.Printf("Not at expected system %s (at %s, id=%s), navigating...", expectedSystem, state.CurrentSystem, state.System.ID)
		if err := game.NavigateToSystem(client, ctx, expectedSystem, logger); err != nil {
			return fmt.Errorf("navigate to %s: %w", expectedSystem, err)
		}
	}

	// Dock if not docked.
	if err := ensureDocked(client, ctx, logger); err != nil {
		return err
	}

	// Refuel before buying (so we have enough fuel for the trip).
	if err := refuelAndRepair(client, ctx, logger); err != nil {
		logger.Printf("Warning: refuel/repair issue: %v", err)
	}

	state = client.GetState()

	// Calculate how much we can buy based on available cargo space and credits.
	availableSpace := state.Ship.CargoCapacity - state.Ship.CargoUsed
	if availableSpace <= 0 {
		logger.Printf("Cargo is full, skipping buy - advancing to next phase")
		ts.phase = nextPhase
		return nil
	}

	// Reserve some credits for fuel/repairs.
	const reserveCredits = 50.0
	spendable := state.Credits - reserveCredits
	if spendable <= 0 {
		logger.Printf("Not enough credits (%.2f, reserve %.2f) - advancing anyway", state.Credits, reserveCredits)
		ts.phase = nextPhase
		return nil
	}

	// Check actual market price by fetching listings.
	if err := client.GetListings(ctx); err != nil {
		logger.Printf("Warning: failed to fetch listings: %v", err)
	}
	time.Sleep(game.SleepQuick)

	listings := client.GetMarketListings()
	buyPrice := route.ExpectedBuy
	for _, l := range listings {
		if l.ItemID == route.OreID && l.Type == "sell" {
			buyPrice = l.PricePerUnit
			break
		}
	}

	maxByPrice := math.Floor(spendable / buyPrice)
	quantity := math.Min(availableSpace, maxByPrice)
	if quantity < 1 {
		logger.Printf("Cannot afford %s at %.2f cr/unit (budget: %.2f) - advancing", route.OreName, buyPrice, spendable)
		ts.phase = nextPhase
		return nil
	}

	logger.Printf("Buying %.0f x %s at %.2f cr/unit (total: %.2f)...",
		quantity, route.OreName, buyPrice, quantity*buyPrice)

	if err := client.Buy(ctx, route.OreID, quantity); err != nil {
		return fmt.Errorf("buy %s: %w", route.OreID, err)
	}
	time.Sleep(game.SleepShort)

	state = client.GetState()
	logger.Printf("Buy complete. Credits: %.2f, Cargo: %.0f/%.0f",
		state.Credits, state.Ship.CargoUsed, state.Ship.CargoCapacity)

	// Insure high-value cargo before departure.
	cargoValue := quantity * buyPrice
	const insuranceThreshold = 10_000.0
	const insuranceTicks = 100
	if cargoValue >= insuranceThreshold {
		logger.Printf("Cargo value %.0f cr exceeds threshold, purchasing insurance (%d ticks)...", cargoValue, insuranceTicks)
		if err := client.BuyInsurance(ctx, insuranceTicks); err != nil {
			logger.Printf("Warning: insurance purchase failed: %v (continuing without insurance)", err)
		} else {
			logger.Printf("Insurance purchased for %d ticks", insuranceTicks)
		}
		time.Sleep(game.SleepShort)
	}

	ts.phase = nextPhase
	return nil
}

func executeTravelPhase(client *game.Client, logger *log.Logger, ctx context.Context,
	ts *tradeLoopState, targetSystem string, nextPhase tradePhase) error {

	state := client.GetState()

	if strings.EqualFold(state.System.ID, targetSystem) {
		logger.Printf("Already at target system %s", targetSystem)
		ts.phase = nextPhase
		return nil
	}

	logger.Printf("Navigating to %s (phase: %s -> %s)...", targetSystem, ts.phase, nextPhase)
	if err := game.NavigateToSystem(client, ctx, targetSystem, logger); err != nil {
		return fmt.Errorf("navigate to %s: %w", targetSystem, err)
	}

	ts.phase = nextPhase
	return nil
}

func executeSellPhase(client *game.Client, logger *log.Logger, ctx context.Context,
	ts *tradeLoopState, specificOreID string, oreName string) error {

	// Dock if not docked.
	if err := ensureDocked(client, ctx, logger); err != nil {
		return err
	}

	state := client.GetState()
	if len(state.Ship.Cargo) == 0 {
		logger.Printf("No cargo to sell, advancing...")
		advanceSellPhase(ts)
		return nil
	}

	// Sell cargo.
	creditsBefore := state.Credits
	if specificOreID != "" {
		// Sell just the specific ore.
		for _, item := range state.Ship.Cargo {
			if item.ItemID == specificOreID {
				logger.Printf("Selling %.0f x %s...", item.Quantity, oreName)
				if err := client.Sell(ctx, item.ItemID, item.Quantity); err != nil {
					return fmt.Errorf("sell %s: %w", item.ItemID, err)
				}
				time.Sleep(game.SleepTick)
				break
			}
		}
	} else {
		// Sell all cargo.
		logger.Printf("Selling all cargo (%d items)...", len(state.Ship.Cargo))
		for _, item := range state.Ship.Cargo {
			logger.Printf("  Selling %.0f x %s", item.Quantity, item.ItemID)
			if err := client.Sell(ctx, item.ItemID, item.Quantity); err != nil {
				logger.Printf("  Warning: sell %s failed: %v", item.ItemID, err)
			}
			time.Sleep(game.SleepTick)
		}
	}

	state = client.GetState()
	profit := state.Credits - creditsBefore
	logger.Printf("Sold! Profit: %.2f cr | Total credits: %.2f", profit, state.Credits)

	// Refuel/repair after selling.
	if err := refuelAndRepair(client, ctx, logger); err != nil {
		logger.Printf("Warning: refuel/repair issue: %v", err)
	}

	advanceSellPhase(ts)
	return nil
}

// advanceSellPhase moves to the next phase after selling.
func advanceSellPhase(ts *tradeLoopState) {
	switch ts.phase {
	case phaseSellAtDest:
		if ts.hasReturnLeg {
			ts.phase = phaseBuyReturn
		} else {
			ts.phase = phaseTravelHome
		}
	case phaseSellAtHome:
		// Round trip complete!
		ts.tripCount++
		tripProfit := 0.0 // Will be tracked across phases in future
		ts.totalProfit += tripProfit
		ts.phase = phaseBuyAtHome // Start next trip
	}
}

func ensureDocked(client *game.Client, ctx context.Context, logger *log.Logger) error {
	state := client.GetState()
	if state.Doc {
		return nil
	}
	return game.NavigateAndDock(client, ctx, logger)
}

func refuelAndRepair(client *game.Client, ctx context.Context, logger *log.Logger) error {
	state := client.GetState()
	if !state.Doc {
		return nil
	}

	if state.Fuel < state.MaxFuel*0.8 {
		logger.Printf("Refueling... (%.0f/%.0f)", state.Fuel, state.MaxFuel)
		if err := client.Refuel(ctx); err != nil {
			return fmt.Errorf("refuel: %w", err)
		}
		time.Sleep(game.SleepTick)
	}

	state = client.GetState()
	if state.Hull < state.MaxHull*0.9 {
		logger.Printf("Repairing hull... (%.0f/%.0f)", state.Hull, state.MaxHull)
		if err := client.Repair(ctx); err != nil {
			return fmt.Errorf("repair: %w", err)
		}
		time.Sleep(game.SleepTick)
	}

	return nil
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: auto-trader <agent-id> [strategy] [ore] [target-empire]")
		fmt.Println("")
		fmt.Println("Arguments:")
		fmt.Println("  agent-id       Agent identifier (e.g., trader-1, trader-2)")
		fmt.Println("  strategy       Action strategy (optional, default: sell)")
		fmt.Println("  ore            Ore ID for trade strategy (e.g., ore_silicon)")
		fmt.Println("  target-empire  Target empire for trade strategy (e.g., crimson)")
		fmt.Println("")
		fmt.Println("Strategies:")
		fmt.Println("  sell           Sell all cargo immediately (default)")
		fmt.Println("  craft-sell     Craft items from resources, then sell all")
		fmt.Println("  craft-deposit  Craft items from resources, then deposit to storage")
		fmt.Println("  trade          Inter-empire trade routes (buy, travel, sell)")
		fmt.Println("")
		fmt.Println("Examples:")
		fmt.Println("  auto-trader trader-1                          # Sell everything (default)")
		fmt.Println("  auto-trader trader-1 sell                     # Sell everything (explicit)")
		fmt.Println("  auto-trader trader-1 craft-sell               # Craft then sell")
		fmt.Println("  auto-trader trader-1 trade                    # Auto-select best trade route")
		fmt.Println("  auto-trader trader-1 trade ore_silicon crimson # Explicit route")
		os.Exit(1)
	}

	agentID := os.Args[1]

	// Parse station action strategy
	strategy := "sell"
	if len(os.Args) >= 3 {
		strategy = os.Args[2]
	}

	logger := log.New(os.Stdout, fmt.Sprintf("[%s] ", agentID), log.LstdFlags)

	// Check captain's log for previous mission
	previousLog, err := game.ReadLatestCaptainsLog(agentID)
	if err != nil {
		logger.Printf("Failed to read captain's log: %v", err)
	} else if previousLog != nil {
		logger.Printf("Captain's Log - Last Entry:")
		logger.Printf("   Mission: %s", previousLog.CurrentGoal)
		logger.Printf("   Location: %s", previousLog.Location)
		logger.Printf("   Time: %s", previousLog.Timestamp.Format("2006-01-02 15:04"))
	}

	ctx := context.Background()

	client, creds, err := game.InitializeAgent(agentID, logger, ctx)
	if err != nil {
		log.Fatalf("Failed to initialize agent: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			log.Printf("Warning: Failed to close client: %v", err)
		}
	}()

	time.Sleep(1 * time.Second)

	// Get initial state
	state := client.GetState()
	logger.Printf("Starting autonomous trading agent...")
	logger.Printf("Agent: %s | Empire: %s | Credits: %.2f | Ship: %s | Cargo: %.0f/%.0f",
		creds.Username, creds.Empire, state.Credits, state.Ship.Name,
		state.Ship.CargoUsed, state.Ship.CargoCapacity)

	// Handle "trade" strategy separately.
	if strategy == "trade" {
		runTradeStrategy(agentID, client, logger, ctx, creds)
		return
	}

	// Validate non-trade strategy.
	var stationAction game.StationActionStrategy
	switch strategy {
	case "sell":
		stationAction = game.StationActionSellAll
	case "craft-sell":
		stationAction = game.StationActionCraftAndSell
	case "craft-deposit":
		stationAction = game.StationActionCraftAndDeposit
	default:
		log.Fatalf("Unknown strategy: %s (must be: sell, craft-sell, craft-deposit, trade)", strategy)
	}

	// Initialize crafting configuration if using a crafting strategy
	if strategy == "craft-sell" || strategy == "craft-deposit" {
		client.CraftingConfig = &game.CraftingConfig{
			CraftingServerPath: "", // Empty string uses "crafting-server" from PATH
		}
		logger.Printf("Crafting configured: using MCP server from PATH")
	}

	// Start autonomous trading loop
	logger.Printf("Starting autonomous trading loop...")
	logger.Printf("Station action strategy: %s", strategy)

	if err := tradingLoop(agentID, client, logger, ctx, stationAction, strategy); err != nil {
		log.Fatalf("Trading loop error: %v", err)
	}
}

func runTradeStrategy(agentID string, client *game.Client, logger *log.Logger, ctx context.Context, creds *game.Credentials) {
	empire := strings.ToLower(creds.Empire)

	var outbound game.TradeRoute
	var returnLeg game.TradeRoute
	var hasReturn bool

	if len(os.Args) >= 5 {
		// Explicit route: auto-trader trader-1 trade ore_silicon crimson
		oreID := os.Args[3]
		targetEmpire := os.Args[4]
		var found bool
		outbound, returnLeg, found = game.FindRouteByOreAndTarget(empire, oreID, targetEmpire)
		if !found {
			log.Fatalf("No known route: buy %s at %s, sell at %s", oreID, empire, targetEmpire)
		}
		hasReturn = returnLeg.OreID != ""
	} else {
		// Auto-select best route for this empire.
		routes := game.FindTradeRoutes(empire)
		if len(routes) == 0 {
			log.Fatalf("No trade routes available for empire %q", empire)
		}

		outbound = routes[0]
		returnLeg, hasReturn = game.GetReturnLeg(outbound)

		logger.Printf("Available routes for %s:", empire)
		for i, r := range routes {
			logger.Printf("  %d. %s (priority: %d, margin: %.0f cr/unit)", i+1, r.Name, r.Priority, r.Margin())
		}
		logger.Printf("Selected: %s", outbound.Name)
	}

	if err := tradeLoop(agentID, client, logger, ctx, outbound, returnLeg, hasReturn, empire); err != nil {
		log.Fatalf("Trade loop error: %v", err)
	}
}
