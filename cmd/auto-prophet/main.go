package main

import (
	"cmp"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// Prophet behavior phases.
const (
	phaseSeekCongregation = "seek_congregation"
	phaseArriveAndPreach  = "arrive_and_preach"
	phaseMinister         = "minister"
	phaseMoveOn           = "move_on"
)

// prophetIdentity holds the sermon pools and rival info for a specific prophet.
type prophetIdentity struct {
	Name           string
	Organization   string
	RivalName      string
	Sermons        []string
	CounterSermons []string
}

// prophetMeta holds the static metadata for each known prophet.
// Sermons and counter-sermons are loaded from JSON files at runtime.
var prophetMeta = map[string]struct {
	Name         string
	Organization string
	RivalName    string
}{
	"prophet-1": {
		Name:         "The Prophet",
		Organization: "The Covenant of the Eternal Spark",
		RivalName:    "Hugh Mann",
	},
	"prophet-2": {
		Name:         "Hugh Mann",
		Organization: "The Order of the Grand Architects",
		RivalName:    "The Prophet",
	},
}

// loadSermonFile reads a JSON array of strings from a file.
func loadSermonFile(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var sermons []string
	if err := json.Unmarshal(data, &sermons); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if len(sermons) == 0 {
		return nil, fmt.Errorf("%s is empty", path)
	}
	return sermons, nil
}

// loadProphetIdentity loads a prophet's identity from metadata and sermon files on disk.
func loadProphetIdentity(agentID string) (prophetIdentity, error) {
	agentDir := fmt.Sprintf("data/agents/%s", agentID)

	// Try known metadata first, fall back to personality.json.
	meta, ok := prophetMeta[agentID]
	if !ok {
		m, found := loadMetaFromPersonality(agentID)
		if !found {
			return prophetIdentity{}, fmt.Errorf("unknown prophet agent %q — expected prophet-1 or prophet-2", agentID)
		}
		meta = m
	}

	sermons, err := loadSermonFile(fmt.Sprintf("%s/sermons.json", agentDir))
	if err != nil {
		return prophetIdentity{}, fmt.Errorf("load sermons for %s: %w", agentID, err)
	}

	counterSermons, err := loadSermonFile(fmt.Sprintf("%s/counter_sermons.json", agentDir))
	if err != nil {
		return prophetIdentity{}, fmt.Errorf("load counter-sermons for %s: %w", agentID, err)
	}

	return prophetIdentity{
		Name:           meta.Name,
		Organization:   meta.Organization,
		RivalName:      meta.RivalName,
		Sermons:        sermons,
		CounterSermons: counterSermons,
	}, nil
}

// mapSystemInfo is a minimal struct for parsing systems from GetRawJSON.
type mapSystemInfo struct {
	SystemID string `json:"system_id"`
	Name     string `json:"name"`
	Online   int    `json:"online"`
}

func main() {
	debug := flag.Bool("debug", false, "Enable debug logging")
	transport := flag.String("transport", "ws", "Transport: ws (WebSocket) or mcp (MCP HTTP)")
	flag.Parse()

	if len(flag.Args()) < 1 {
		fmt.Println("Usage: auto-prophet [flags] <agent-id>")
		fmt.Println("")
		fmt.Println("Arguments:")
		fmt.Println("  agent-id    Prophet agent identifier (prophet-1 or prophet-2)")
		fmt.Println("")
		fmt.Println("Flags:")
		flag.PrintDefaults()
		fmt.Println("")
		fmt.Println("Examples:")
		fmt.Println("  auto-prophet prophet-1    # The Prophet (Covenant of the Eternal Spark)")
		fmt.Println("  auto-prophet prophet-2    # Hugh Mann (Order of the Grand Architects)")
		fmt.Println("  auto-prophet -debug prophet-1  # With debug logging")
		fmt.Println("  auto-prophet -transport=mcp prophet-1 # Use MCP transport")
		os.Exit(1)
	}

	agentID := flag.Args()[0]
	logger := log.New(os.Stdout, fmt.Sprintf("[%s] ", agentID), log.LstdFlags)

	// Load prophet identity (metadata + sermons from disk).
	identity, err := loadProphetIdentity(agentID)
	if err != nil {
		log.Fatalf("Failed to load prophet identity: %v", err)
	}

	logger.Printf("Prophet: %s | Organization: %s", identity.Name, identity.Organization)
	logger.Printf("Rival: %s | Sermons: %d | Counter-sermons: %d",
		identity.RivalName, len(identity.Sermons), len(identity.CounterSermons))

	// Check captain's log for previous mission.
	if previousLog, err := game.ReadLatestCaptainsLog(agentID); err != nil {
		logger.Printf("Failed to read captain's log: %v", err)
	} else if previousLog != nil {
		logger.Printf("Captain's Log - Last Entry:")
		logger.Printf("  Mission: %s", previousLog.CurrentGoal)
		logger.Printf("  Location: %s", previousLog.Location)
		logger.Printf("  Time: %s", previousLog.Timestamp.Format("2006-01-02 15:04"))
	}

	ctx := context.Background()

	// Initialize game client based on transport selection.
	var client game.GameClient

	switch *transport {
	case "mcp":
		logger.Printf("Using MCP transport")
		client, _, err = game.InitializeMCPAgent(agentID, logger, ctx, *debug, false)
		if err != nil {
			log.Fatalf("Failed to initialize MCP agent: %v", err)
		}
	case "ws":
		logger.Printf("Using WebSocket transport")
		var wsClient *game.Client
		wsClient, _, err = game.InitializeAgent(agentID, logger, ctx, *debug)
		if err != nil {
			log.Fatalf("Failed to initialize agent: %v", err)
		}
		client = wsClient
	default:
		log.Fatalf("Unknown transport: %s (must be: ws, mcp)", *transport)
	}

	defer func() {
		if err := client.Close(); err != nil {
			log.Printf("Warning: Failed to close client: %v", err)
		}
	}()

	time.Sleep(game.SleepQuick)

	state := client.GetState()
	logger.Printf("Starting prophet agent %s...", identity.Name)
	logger.Printf("Location: %s | Credits: %.2f | Hull: %.0f/%.0f | Fuel: %.0f/%.0f",
		state.System.Name, state.Credits, state.Hull, state.MaxHull, state.Fuel, state.MaxFuel)

	if err := prophetLoop(agentID, client, logger, ctx, &identity); err != nil {
		log.Fatalf("Prophet loop error: %v", err)
	}
}

// loadMetaFromPersonality attempts to determine prophet metadata from the personality.json file.
func loadMetaFromPersonality(agentID string) (struct {
	Name         string
	Organization string
	RivalName    string
}, bool) {
	type result = struct {
		Name         string
		Organization string
		RivalName    string
	}

	data, err := os.ReadFile(fmt.Sprintf("data/agents/%s/personality.json", agentID))
	if err != nil {
		return result{}, false
	}

	var personality struct {
		Name         string `json:"name"`
		Organization string `json:"organization"`
	}
	if err := json.Unmarshal(data, &personality); err != nil {
		return result{}, false
	}

	// Match by name to one of the known prophets.
	for _, meta := range prophetMeta {
		if strings.EqualFold(meta.Name, personality.Name) ||
			strings.EqualFold(meta.Organization, personality.Organization) {
			return meta, true
		}
	}
	return result{}, false
}

// prophetLoop is the main behavior loop for the prophet agent.
func prophetLoop(agentID string, client game.GameClient, logger *log.Logger, ctx context.Context, identity *prophetIdentity) error {
	phase := phaseSeekCongregation
	var (
		targetSystem   string
		sermonsGiven   int
		systemsVisited int
		rivalsSeen     int
		ministerEnd    time.Time
		lastSermon     time.Time
		nextSermonAt   time.Time // When the next periodic sermon should be delivered.
	)

	ticker := time.NewTicker(game.SleepTick)
	defer ticker.Stop()

	logTicker := time.NewTicker(2 * time.Minute)
	defer logTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil

		case <-logTicker.C:
			state := client.GetState()
			nearby := state.GetNearbyPlayers()
			entry := &game.AgentLog{
				AgentName:   identity.Name,
				CurrentGoal: fmt.Sprintf("Spreading the word of %s (phase: %s)", identity.Organization, phase),
				Location:    fmt.Sprintf("System: %s, POI: %s", state.CurrentSystem, state.CurrentPOI),
				Notes: []string{
					fmt.Sprintf("Systems visited: %d", systemsVisited),
					fmt.Sprintf("Sermons delivered: %d", sermonsGiven),
					fmt.Sprintf("Rival encounters: %d", rivalsSeen),
					fmt.Sprintf("Nearby players: %d", len(nearby)),
				},
			}
			_ = game.WriteCaptainsLog(agentID, entry)

		case <-ticker.C:
			state := client.GetState()

			// Skip if traveling.
			if state.Traveling {
				logger.Printf("In transit... (phase: %s)", phase)
				continue
			}

			// Survival checks.
			if err := checkSurvival(client, ctx, logger, state); err != nil {
				logger.Printf("Survival check error: %v", err)
			}

			switch phase {
			case phaseSeekCongregation:
				logger.Printf("═══ Seeking congregation... ═══")

				// Get the galaxy map to find populated systems.
				if err := client.GetMap(ctx); err != nil {
					logger.Printf("Failed to get map: %v", err)
					continue
				}
				time.Sleep(game.SleepQuick)

				target, err := findPopulatedSystem(client, state.System.ID, logger)
				if err != nil {
					logger.Printf("Failed to find populated system: %v", err)
					continue
				}
				// For now, don't bother jumping and traveling across the galaxy.
				// targetSystem = target.SystemID
				targetSystem = state.System.ID
				logger.Printf("Target: %s (%s) — %d players online", target.Name, target.SystemID, target.Online)

				// Navigate to the target system (NavigateToSystem handles undocking internally).
				if !strings.EqualFold(state.System.ID, targetSystem) {
					logger.Printf("Navigating to %s...", targetSystem)
					if err := game.NavigateToSystem(client, ctx, targetSystem, logger); err != nil {
						// If navigation fails due to a stuck pending action, wait and retry.
						if strings.Contains(err.Error(), "pending") {
							logger.Printf("Pending action blocking navigation, waiting...")
							time.Sleep(game.SleepJump)
							continue
						}
						logger.Printf("Navigation error: %v", err)
						continue
					}
				}

				systemsVisited++
				phase = phaseArriveAndPreach

			case phaseArriveAndPreach:
				logger.Printf("═══ Arriving in system — preparing to preach ═══")

				// Dock at the station so we can be seen and refuel.
				if err := game.NavigateAndDock(client, ctx, logger); err != nil {
					logger.Printf("Dock error: %v (preaching from space instead)", err)
				}
				time.Sleep(game.SleepQuick)

				// Refuel and repair while docked.
				state = client.GetState()
				if state.Doc {
					if err := refuelAndRepair(client, ctx, logger); err != nil {
						logger.Printf("Refuel/repair error: %v", err)
					}
				}

				// Deliver arrival sermon.
				if err := pickAndSendSermon(client, ctx, "system", identity.Sermons, logger); err != nil {
					logger.Printf("Chat error: %v", err)
				} else {
					sermonsGiven++
					lastSermon = time.Now()
					logger.Printf("Delivered arrival sermon (#%d)", sermonsGiven)
				}
				time.Sleep(game.SleepQuick)

				// Set up minister phase duration: 15-45 minutes.
				ministerDuration := 15*time.Minute + time.Duration(rand.IntN(1800))*time.Second
				ministerEnd = time.Now().Add(ministerDuration)
				logger.Printf("Ministering for %s...", ministerDuration.Round(time.Second))

				phase = phaseMinister

			case phaseMinister:
				// Check if ministry time is up.
				if time.Now().After(ministerEnd) {
					logger.Printf("Ministry complete in this system. Moving on...")
					phase = phaseMoveOn
					continue
				}

				// Check for rival prophet.
				nearby := state.GetNearbyPlayers()
				rivalDetected := false
				for _, p := range nearby {
					if strings.EqualFold(p.Username, identity.RivalName) {
						rivalDetected = true
						break
					}
				}

				if rivalDetected {
					rivalsSeen++
					logger.Printf("RIVAL DETECTED: %s is in this system!", identity.RivalName)

					// Fire off a counter-sermon.
					if err := pickAndSendSermon(client, ctx, "system", identity.CounterSermons, logger); err != nil {
						logger.Printf("Counter-sermon chat error: %v", err)
					} else {
						sermonsGiven++
						lastSermon = time.Now()
						nextSermonAt = time.Time{} // Reset so next periodic sermon gets a fresh interval.
						logger.Printf("Delivered counter-sermon against %s (#%d)", identity.RivalName, sermonsGiven)
					}
					time.Sleep(game.SleepQuick)
					continue
				}

				// Periodic sermons: preach every 2-10 minutes.
				// Schedule next sermon time once, after delivering a sermon.
				if nextSermonAt.IsZero() {
					sermonInterval := 2*time.Minute + time.Duration(rand.IntN(480))*time.Second
					nextSermonAt = lastSermon.Add(sermonInterval)
					logger.Printf("Next sermon in %v", time.Until(nextSermonAt).Round(time.Second))
				}
				if time.Now().After(nextSermonAt) {
					if err := pickAndSendSermon(client, ctx, "system", identity.Sermons, logger); err != nil {
						logger.Printf("Sermon chat error: %v", err)
					} else {
						sermonsGiven++
						lastSermon = time.Now()
						nextSermonAt = time.Time{} // Reset so a new interval is picked next tick.
						logger.Printf("Delivered sermon (#%d) — %d players nearby", sermonsGiven, len(nearby))
					}
					time.Sleep(game.SleepQuick)
				}

			case phaseMoveOn:
				logger.Printf("═══ Moving on to next system ═══")

				// Undock if docked.
				state = client.GetState()
				if state.Doc {
					logger.Printf("Undocking...")
					if err := client.Undock(ctx); err != nil {
						logger.Printf("Undock error: %v", err)
					}
					time.Sleep(game.SleepUndock)
				}

				phase = phaseSeekCongregation
			}
		}
	}
}

// findPopulatedSystem parses the map data to find the most populated system
// that isn't the current one.
func findPopulatedSystem(client game.GameClient, currentSystemID string, logger *log.Logger) (mapSystemInfo, error) {
	raw := client.GetRawJSON("systems")
	if raw == nil {
		return mapSystemInfo{}, fmt.Errorf("no map data available (call GetMap first)")
	}

	// The raw JSON is the full payload: {"systems": [...], "total_count": N}
	var payload struct {
		Systems []mapSystemInfo `json:"systems"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return mapSystemInfo{}, fmt.Errorf("parse map data: %w", err)
	}

	if len(payload.Systems) == 0 {
		return mapSystemInfo{}, fmt.Errorf("no systems in map data")
	}

	// Sort by online count descending.
	slices.SortFunc(payload.Systems, func(a, b mapSystemInfo) int {
		return cmp.Compare(b.Online, a.Online)
	})

	// Pick the most populated system that isn't where we are.
	for _, sys := range payload.Systems {
		if !strings.EqualFold(sys.SystemID, currentSystemID) && sys.Online > 0 {
			return sys, nil
		}
	}

	// If no populated systems found, pick any system that isn't current.
	for _, sys := range payload.Systems {
		if !strings.EqualFold(sys.SystemID, currentSystemID) {
			logger.Printf("No populated systems found, picking %s anyway", sys.Name)
			return sys, nil
		}
	}

	return mapSystemInfo{}, fmt.Errorf("no other systems available")
}

// checkSurvival handles refueling and repairs when docked.
func checkSurvival(client game.GameClient, ctx context.Context, logger *log.Logger, state *game.State) error {
	if !state.Doc {
		// Check if we need emergency docking.
		fuelPct := state.Fuel / state.MaxFuel
		hullPct := (state.Hull / state.MaxHull) * 100
		if fuelPct < game.FuelCriticalThreshold || hullPct < game.HullCriticalThreshold {
			logger.Printf("EMERGENCY: Fuel %.0f%% Hull %.0f%% — seeking station!", fuelPct*100, hullPct)
			if err := game.NavigateAndDock(client, ctx, logger); err != nil {
				return fmt.Errorf("emergency dock: %w", err)
			}
		} else {
			return nil
		}
	}

	return refuelAndRepair(client, ctx, logger)
}

// maxChatLen is the server's maximum chat message length.
const maxChatLen = 500

// sendSermon sends a sermon via chat, splitting into multiple messages if it
// exceeds the server's 500-character limit. Splits on sentence boundaries.
func sendSermon(client game.GameClient, ctx context.Context, channel, sermon string) error {
	if len(sermon) <= maxChatLen {
		return client.Chat(ctx, channel, sermon, "")
	}

	// Split into chunks at sentence boundaries (". ") that fit within the limit.
	remaining := sermon
	for remaining != "" {
		chunk := remaining
		if len(chunk) > maxChatLen {
			// Find the last sentence boundary within the limit.
			cut := strings.LastIndex(chunk[:maxChatLen], ". ")
			if cut <= 0 {
				// No sentence boundary — fall back to last space.
				cut = strings.LastIndex(chunk[:maxChatLen], " ")
			}
			if cut <= 0 {
				// No space at all — hard cut.
				cut = maxChatLen - 1
			}
			chunk = remaining[:cut+1]
		}
		if err := client.Chat(ctx, channel, strings.TrimSpace(chunk), ""); err != nil {
			return err
		}
		remaining = strings.TrimSpace(remaining[len(chunk):])
		if remaining != "" {
			time.Sleep(game.SleepQuick)
		}
	}
	return nil
}

// pickAndSendSermon picks a random sermon from the pool and sends it.
// On duplicate_message errors, it retries with a different sermon up to 3 times.
func pickAndSendSermon(client game.GameClient, ctx context.Context, channel string, pool []string, logger *log.Logger) error {
	tried := make(map[int]bool)
	for range 3 {
		idx := rand.IntN(len(pool))
		// Avoid retrying the same sermon.
		for tried[idx] && len(tried) < len(pool) {
			idx = rand.IntN(len(pool))
		}
		tried[idx] = true

		err := sendSermon(client, ctx, channel, pool[idx])
		if err == nil {
			return nil
		}
		if strings.Contains(err.Error(), "duplicate_message") {
			logger.Printf("Duplicate sermon, picking another...")
			time.Sleep(game.SleepQuick)
			continue
		}
		return err
	}
	return fmt.Errorf("all sermon attempts were duplicates")
}

// refuelAndRepair handles refueling and repairs when docked.
func refuelAndRepair(client game.GameClient, ctx context.Context, logger *log.Logger) error {
	state := client.GetState()
	if !state.Doc {
		return nil
	}

	if state.Fuel < state.MaxFuel*game.FuelLowThreshold {
		logger.Printf("Refueling... (%.0f/%.0f)", state.Fuel, state.MaxFuel)
		if err := client.Refuel(ctx); err != nil {
			logger.Printf("Refuel error: %v", err)
		}
		time.Sleep(game.SleepShort)
	}

	state = client.GetState()
	hullPct := (state.Hull / state.MaxHull) * 100
	if hullPct < game.HullDamagedThreshold {
		logger.Printf("Repairing hull... (%.0f/%.0f, %.0f%%)", state.Hull, state.MaxHull, hullPct)
		if err := client.Repair(ctx); err != nil {
			logger.Printf("Repair error: %v", err)
		}
		time.Sleep(game.SleepShort)
	}

	return nil
}
