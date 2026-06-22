// Command: play_as
// Usage: play_as <agent-id>
//
// Interactive game terminal for playing as an agent using MCP transport.
// Provides a shell-like prompt for sending game commands and viewing responses.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"maps"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"cmp"

	"github.com/mattn/go-runewidth"
	"github.com/peterh/liner"
	"github.com/rsned/spacemolt/internal/protocol"
	"github.com/rsned/spacemolt/pkg/agent"
	"github.com/rsned/spacemolt/pkg/faction"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/market"
	"github.com/rsned/spacemolt/pkg/mbox"
	"github.com/rsned/spacemolt/pkg/registry"
	"github.com/rsned/spacemolt/pkg/respfmt"
	"github.com/rsned/spacemolt/pkg/worker"
)

// Package-level knowledge base, initialized if --db-path is provided.
var globalKB knowledge.Base

// globalGraphCache provides lazy-loaded galaxy graph for pathfinding.
// Used by nearest command (see Task 7-8).
var globalGraphCache *graphCache //nolint:unused // Will be used in nearest command

// globalClock tracks game ticks, initialized after client connects.
var globalClock *game.GameClock

// processStartTime records when this play_as session started, used to filter
// old chat messages (show at most 1 per sender for messages before this time).
var processStartTime = time.Now()

// globalClient is set during initialization so formatters can access game state.
var globalClient game.GameClient

// globalAgentID is this session's agent id (args[0]), set during init. Used as
// the detected_by attribution when formatters persist observations to the KB.
var globalAgentID string

// globalFactionBackfiller is the background faction_info backfiller, set during
// initialization when a SQLite KB and WS client are available. Nil otherwise.
// The seen_factions command uses it to seed the factions table on demand.
var globalFactionBackfiller *faction.FactionBackfiller

// globalMarketCollector is the market DB connection used by the update_market
// command. Constructed once at startup from the --market-db-path flag; nil if
// the flag was empty or the open failed.
var globalMarketCollector *market.Collector

// Output format for server responses.
type outputFormat string

const (
	formatRaw    outputFormat = "raw"
	formatStyled outputFormat = "styled"
)

func main() {
	debug := flag.Bool("debug", false, "Enable debug logging (show sent/received JSON)")
	debugFullPayload := flag.Bool("debug-full-payload", false, "When --debug is on, log full response payloads instead of truncating at 200 chars")
	quietEvents := flag.String("quiet-events", "", "Comma-separated server event types to suppress from --debug receive logs (e.g. mining_yield to silence drone mining pushes). poi_arrival/poi_departure are always silenced.")
	configPath := flag.String("config", defaultConfigPath(), "Path to config file")
	registryURL := flag.String("registry-url", "", "Status registry URL (e.g., http://localhost:8081)")
	dbPath := flag.String("db-path", "data/spacemolt-knowledge.db", "Path to SQLite knowledge base (enables update_* commands)")
	marketDBPath := flag.String("market-db-path", "data/market.db", "Path to the separate market database")
	intelDir := flag.String("intel-dir", "data/intel", "Base directory for per-POI get_poi intel dumps (<intel-dir>/<system_id>/<system_id>___<poi_id>.json); empty to disable")
	xpTracking := flag.Bool("xp-tracking", true, "Enable XP observation tracking to the knowledge base")
	transport := flag.String("transport", "ws", "Game transport: 'ws' (WebSocket, default) or 'mcp' (MCP over HTTP)")
	flag.Parse()

	globalIntelDir = *intelDir

	args := flag.Args()
	if len(args) < 1 {
		printUsage()
		os.Exit(1)
	}

	agentID := args[0]
	globalAgentID = agentID
	logger := log.New(os.Stdout, fmt.Sprintf("[PLAY_AS-%s] ", agentID), log.LstdFlags)

	ctx := context.Background()

	logger.Printf("Initializing agent %s via %s transport...", agentID, *transport)
	var client game.GameClient
	var creds *game.Credentials
	var err error
	switch strings.ToLower(*transport) {
	case "ws", "websocket":
		var wsClient *game.Client
		wsClient, creds, err = game.InitializeAgent(agentID, logger, ctx, *debug)
		if wsClient != nil && *debugFullPayload {
			wsClient.SetDebugPayloadMaxLen(0)
		}
		if wsClient != nil && strings.TrimSpace(*quietEvents) != "" {
			// Split and pass the raw list; SetQuietEventTypes trims and
			// drops empties. Wired only for the WS client because that's
			// where the noisy "=== Game Client Receive Debug ===" log lives.
			wsClient.SetQuietEventTypes(strings.Split(*quietEvents, ","))
		}
		client = wsClient
	case "mcp":
		client, creds, err = game.InitializeMCPAgent(agentID, logger, ctx, *debug, true) // disablePolling=true
		// Note: --debug-full-payload only affects the WS client's response
		// payload logging; the MCP client uses its own MCP-protocol dump.
	default:
		log.Fatalf("unknown --transport %q (expected 'ws' or 'mcp')", *transport)
	}
	if err != nil {
		log.Fatalf("Failed to initialize agent: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			logger.Printf("Error closing client: %v", err)
		}
	}()

	logger.Printf("Connected as: %s (Empire: %s)", creds.Username, creds.Empire)
	globalClient = client

	// Register with status registry if configured
	if *registryURL != "" {
		toolID := fmt.Sprintf("play-as-%s", agentID)
		regClient := registry.NewClient(*registryURL, toolID)

		reg := registry.ToolRegistration{
			ToolID:    toolID,
			ToolType:  registry.ToolTypePlayAs,
			PID:       os.Getpid(),
			AgentID:   agentID,
			AgentName: creds.Username,
			AgentRole: "Interactive",
			Status:    "active",
			Capabilities: map[string]any{
				"interactive": true,
			},
			Metadata: map[string]any{
				"empire": creds.Empire,
			},
		}

		if err := regClient.Register(reg); err != nil {
			logger.Printf("⚠ Warning: Failed to register with status registry: %v", err)
		} else {
			logger.Printf("✓ Registered with status registry")
			regClient.StartHeartbeat(ctx, 5*time.Second, func() (status, action string) {
				state := client.GetState()
				if state == nil {
					return "active", "Interactive session"
				}
				return "active", fmt.Sprintf("In %s (%.0f credits)", state.System.Name, state.Credits)
			})
			defer func() {
				if err := regClient.Deregister(); err != nil {
					logger.Printf("Warning: Failed to deregister: %v", err)
				}
			}()
		}
	}

	// Initialize knowledge base for update_* commands.
	if *dbPath != "" {
		sqliteKB, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: *dbPath})
		if err != nil {
			logger.Printf("Warning: Failed to open knowledge base at %s: %v", *dbPath, err)
			logger.Printf("  update_* commands will be unavailable")
		} else {
			globalKB = sqliteKB
			globalGraphCache = newGraphCache(sqliteKB)
			logger.Printf("Knowledge base loaded: %s", *dbPath)
			defer func() { _ = sqliteKB.Close() }()

			// Wire XP observation tracking. Every successful mutation command
			// that changes skill XP will be recorded to xp_observations, so
			// interactive play also contributes datapoints to the analytics
			// database. Works for any client implementing XPCallbackSetter
			// (both WS *game.Client and *game.MCPGameClient do).
			if *xpTracking {
				if setter, ok := client.(game.XPCallbackSetter); ok {
					knowledge.NewXPTracker(setter, sqliteKB, agentID, logger)
					logger.Printf("XP observation tracking enabled")
				}
			} else {
				logger.Printf("XP observation tracking disabled (--xp-tracking=false)")
			}

			// Persist every encountered player to the shared KB so REPL
			// queries and agents can mine sighting history. See spec
			// docs/superpowers/specs/2026-05-17-player-sightings-design.md.
			// MCP transport isn't supported because notifyPlayers wiring
			// lives on *game.Client (the WS client); MCPGameClient routes
			// responses differently and would need a parallel hook.
			if c, ok := client.(*game.Client); ok {
				// Backfill full faction details for factions seen on observed
				// agents: a new/stale faction_id triggers a background
				// faction_info fetch into the factions tables.
				collector := faction.NewCollector(sqliteKB, logger)
				backfiller := faction.NewFactionBackfiller(c, collector, sqliteKB, game.FreshnessFaction, logger)
				backfiller.Start(ctx)
				globalFactionBackfiller = backfiller
				agent.WirePlayerObserver(c, sqliteKB, backfiller)
				// Build the galaxy-wide passenger catalog from any
				// passenger-bearing response (station lists, manifests, dock
				// arrivals) that flows through the client.
				agent.WirePassengerObserver(c, sqliteKB)
				logger.Printf("Player-sightings recording + faction backfill + passenger catalog enabled")
			}
		}
	}

	// Initialize market collector once from --market-db-path flag.
	if *marketDBPath != "" {
		mc, err := market.Open(market.Config{DBPath: *marketDBPath, WAL: true})
		if err != nil {
			logger.Printf("Warning: failed to open market db at %s: %v", *marketDBPath, err)
		} else {
			globalMarketCollector = mc
			logger.Printf("Market database loaded: %s", *marketDBPath)
		}
	}

	// Cache ship and system data on startup for travel estimation and statusline.
	_ = client.GetShip(ctx)
	_ = client.GetSystem(ctx)

	// Initialize game clock for tick tracking.
	if gc, err := game.NewGameClock(ctx, client, logger); err != nil {
		logger.Printf("Warning: Failed to initialize game clock: %v", err)
	} else {
		globalClock = gc
		defer gc.Stop()
		// Hand the clock to the WS client so Travel/Jump can capture an
		// authoritative StartTick instead of the possibly-stale state value.
		if wsClient, ok := client.(*game.Client); ok {
			wsClient.SetTickProvider(gc.Tick)
		}
	}

	// Show initial status
	fmt.Println("\n╔════════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                    SPACE MOLT GAME TERMINAL                        ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════════╝")
	fmt.Printf("\nLogged in as: %s\n", creds.Username)
	fmt.Printf("Empire: %s\n", creds.Empire)
	fmt.Println("\nType 'help' for available commands, 'exit' or 'quit' to leave.")

	// Load config
	cfg := loadConfig(*configPath)

	// Run REPL loop
	runREPL(client, ctx, cfg, agentID, creds.Username)
}

func printUsage() {
	fmt.Println("Usage: play_as [flags] <agent-id>")
	fmt.Println("Example: play_as explorer-1")
	fmt.Println("  play_as --debug explorer-1")
	fmt.Println("\nFlags:")
	fmt.Println("  --debug                Enable debug logging (show sent/received JSON)")
	fmt.Println("  --debug-full-payload   Log full response payloads (default truncates at 200 chars)")
	fmt.Println("  --quiet-events <list>  Comma-separated event types to suppress from --debug receive logs (e.g. mining_yield)")
	fmt.Println("  --config <path>        Path to config file (default: ~/.config/spacemolt/play_as.yaml)")
	fmt.Println("  --registry-url <url>   Status registry URL (e.g., http://localhost:8081)")
	fmt.Println("  --xp-tracking=false    Disable XP observation tracking (default: true)")
	fmt.Println("  --transport <ws|mcp>   Game transport (default: ws)")
	fmt.Println("\nThis tool provides an interactive terminal for playing Spacemolt.")
	fmt.Println("All commands are case-insensitive. Use 'help' to see available commands.")
}

const maxHistoryLines = 25

func runREPL(client game.GameClient, ctx context.Context, cfg PlayAsConfig, agentID, username string) {
	line := liner.NewLiner()
	defer func() { _ = line.Close() }()

	line.SetCtrlCAborts(true)

	// Ctrl+C handling has two regimes. At the idle prompt liner reads in raw
	// mode (ISIG disabled) and turns Ctrl+C into an aborted Prompt itself
	// (SetCtrlCAborts above). While a foreground command runs, the terminal is
	// back in signal-generating mode, so Ctrl+C arrives as SIGINT — without a
	// handler the Go runtime would kill the whole process. We catch it and
	// cancel just the running command's context, so a long loop or a blocking
	// command aborts back to the prompt. The interrupter is only armed around
	// foreground execution, so an interrupt at the idle prompt is a no-op here
	// and falls through to liner's own handling.
	intr := &interrupter{}
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)
	go intr.watch(sigCh, func() { fmt.Print("\n^C — interrupting…\n") })

	completionCommands := loadCompletionCommands(filepath.Join("server_docs", "openapi.json"))
	line.SetCompleter(makeCompleter(completionCommands, client))
	line.SetTabCompletionStyle(liner.TabPrints)

	// Load persistent command history from agent directory.
	historyPath := filepath.Join("data", "agents", agentID, "play_as_history.txt")
	if f, err := os.Open(historyPath); err == nil {
		_, _ = line.ReadHistory(f)
		_ = f.Close()
	}
	saveHistory := func() {
		if f, err := os.Create(historyPath); err == nil {
			_, _ = line.WriteHistory(f)
			_ = f.Close()
		}
	}
	defer saveHistory()

	// Initialize mbox store for persistent message storage.
	var mboxStore *mbox.Store
	var mboxIng *mbox.Ingester

	// Spam blocklist: senders the user has muted. Loaded regardless of mbox
	// availability so console suppression still works; persisted as a JSON
	// array of strings at data/agents/<agent>/spam_list.json.
	spamListPath := filepath.Join("data", "agents", agentID, "spam_list.json")
	blocklist, err := mbox.LoadBlocklist(spamListPath)
	if err != nil {
		log.Printf("[mbox] warning: could not load spam list: %v", err)
		blocklist, _ = mbox.LoadBlocklist(filepath.Join(os.TempDir(), "spam_list.json"))
	}

	mboxDBPath := filepath.Join("data", "agents", agentID, "mbox.db")
	if s, err := mbox.Open(mboxDBPath); err != nil {
		log.Printf("[mbox] warning: could not open mbox: %v", err)
	} else {
		mboxStore = s
		defer func() { _ = mboxStore.Close() }()
		mboxIng = mbox.NewIngester(mboxStore)
		mboxIng.SetBlocklist(blocklist)
		if state := client.GetState(); state != nil && state.Player.ID != "" {
			mboxIng.SetSelfID(state.Player.ID)
		}
	}

	// Start background chat poller — runs hourly (SleepChatPoll) in all modes.
	//
	// On MCP: the poller is the primary source of chat — it prints new
	// messages and ingests them into the mbox once an hour.
	//
	// On WS: chat is delivered via push (SetOnChatMessage). The poller is a
	// silent hourly reconciler, backfilling the mbox with anything missed
	// during a reconnect window without duplicating the push-driven terminal
	// output.
	poller := newChatPoller(client, ctx, username)
	poller.ingester = mboxIng // nil-safe: chatPoller checks before calling
	poller.blocklist = blocklist
	if _, isWS := client.(*game.Client); isWS {
		poller.silent = true
	}
	poller.start()
	defer poller.stop()

	// Wire the push handler (no-op on MCP, which has no push channel).
	// On WS the callback is responsible for both printing the message and
	// ingesting it into the mbox — the poller is silent in that mode.
	client.SetOnChatMessage(func(msg serverapi.ChatMessage) {
		if mboxIng != nil {
			mboxIng.HandlePush(msg)
		}
		// Blocked senders are still captured (in the spam folder, via the
		// ingester) but never printed to the console.
		if blocklist.IsBlocked(msg.SenderID, msg.Sender) {
			return
		}
		poller.displayMessage(msg.Channel, msg)
	})

	// Crafting progress is delivered via push (WS) as runs complete over the
	// ticks following a craft command. Surface each job's deposit + remaining
	// runs instead of leaving it to the debug log only. Push is WS-only, so this
	// is registered on the concrete *game.Client (no-op under MCP).
	if wsClient, isWS := client.(*game.Client); isWS {
		wsClient.SetOnCraftingUpdate(func(ev serverapi.CraftingUpdateEvent) {
			for _, line := range craftingUpdateLines(ev) {
				fmt.Printf("\r\033[36m🔨 %s\033[0m\n", line)
			}
		})
	}

	format := outputFormat(cfg.OutputFormat)

	// execMu serializes command execution so a background scheduled command
	// never interleaves with a foreground REPL command (or vice-versa).
	var execMu sync.Mutex

	// Scheduler: user-registered recurring commands (hourly/daily/weekly).
	scheduler, err := worker.LoadScheduler(filepath.Join("data", "agents", agentID, "scheduled_commands.json"))
	if err != nil {
		log.Printf("[scheduler] warning: could not load schedules: %v", err)
		scheduler, _ = worker.LoadScheduler(filepath.Join(os.TempDir(), "scheduled_commands.json"))
	}
	scheduler.StartLoop(ctx, game.SleepLong, &execMu, func(t worker.ScheduledTask) {
		fmt.Printf("\r⏰ [scheduled %s] %s\n", t.Frequency, t.Command)
		_ = executeLogicalCommand(client, ctx, t.Command, format, cfg, agentID)
	}, func() time.Time { return time.Now().UTC() })

	// lastCommand holds the most recent game/loop command, for `save <name>`.
	var lastCommand string

	for {
		// Read input with history support. A line ending with an
		// unbalanced '{' causes readLogicalCommand to continue with a
		// "... " prompt until braces balance.
		input, err := readLogicalCommand(line)
		if err != nil {
			if err == liner.ErrPromptAborted {
				if input == "" {
					// Ctrl-C at the main prompt exits.
					fmt.Println("Goodbye!")
					return
				}
				// Ctrl-C during a block continuation discards the block.
				fmt.Println("^C (block discarded)")
				continue
			}
			fmt.Printf("Error reading input: %v\n", err)
			continue
		}

		// Trim whitespace
		cmd := strings.TrimSpace(input)
		if cmd == "" {
			continue
		}

		// Collapse multi-line blocks into a single semicolon-joined
		// history entry so one up-arrow recalls the whole script.
		historyEntry := strings.ReplaceAll(cmd, "\n", "; ")
		line.AppendHistory(historyEntry)
		saveHistory()

		// Parts are derived from the first line only; block-form loop
		// handling below uses parseStatements on the full input.
		firstLine := cmd
		if nl := strings.IndexByte(firstLine, '\n'); nl >= 0 {
			firstLine = firstLine[:nl]
		}
		parts := worker.SplitArgs(firstLine)
		if len(parts) == 0 {
			continue
		}

		command := strings.ToLower(parts[0])

		// Handle exit/quit
		if command == "exit" || command == "quit" {
			fmt.Println("Goodbye!")
			return
		}

		// Handle help
		if command == "help" {
			printHelp()
			continue
		}

		// Handle history
		if command == "history" {
			// Read history file and show last N entries
			data, err := os.ReadFile(historyPath)
			if err != nil {
				fmt.Println("No command history yet.")
			} else {
				lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
				start := 0
				if len(lines) > maxHistoryLines {
					start = len(lines) - maxHistoryLines
				}
				for i, l := range lines[start:] {
					fmt.Printf("  %3d  %s\n", start+i+1, l)
				}
			}
			fmt.Println()
			continue
		}

		// Handle mbox
		if command == "mbox" {
			if mboxStore == nil {
				fmt.Println("mbox not available (database not initialized)")
				fmt.Println()
				continue
			}
			handleMboxCommand(mboxStore, mboxIng, blocklist, client, ctx, parts[1:])
			fmt.Println()
			continue
		}

		// Top-level spam aliases mirror the mbox subcommands.
		switch command {
		case "mark_spam":
			if mboxStore == nil {
				fmt.Println("mbox not available (database not initialized)")
				fmt.Println()
				continue
			}
			mboxMarkSpam(mboxStore, blocklist, parts[1:])
			fmt.Println()
			continue
		case "unmark_spam":
			if mboxStore == nil {
				fmt.Println("mbox not available (database not initialized)")
				fmt.Println()
				continue
			}
			mboxUnmarkSpam(mboxStore, blocklist, parts[1:])
			fmt.Println()
			continue
		case "spam_list":
			mboxSpamList(blocklist)
			fmt.Println()
			continue
		case "schedule_add":
			handleScheduleAdd(scheduler, func(cmd string) {
				execMu.Lock()
				defer execMu.Unlock()
				_ = executeLogicalCommand(client, ctx, cmd, format, cfg, agentID)
			}, time.Now().UTC(), parts[1:])
			fmt.Println()
			continue
		case "schedule_remove":
			handleScheduleRemove(scheduler, parts[1:])
			fmt.Println()
			continue
		case "view_scheduled":
			handleViewScheduled(scheduler, time.Now().UTC())
			fmt.Println()
			continue
		}

		// Handle set_format
		if command == "set_format" {
			if len(parts) < 2 {
				fmt.Printf("Current format: %s\n", format)
				fmt.Println("Usage: set_format <raw|json|styled>")
				continue
			}
			switch strings.ToLower(parts[1]) {
			case "raw", "json":
				format = outputFormat(strings.ToLower(parts[1]))
				fmt.Printf("Output format set to: %s\n", format)
			case "styled":
				format = formatStyled
				fmt.Printf("Output format set to: styled\n")
			default:
				fmt.Printf("Unknown format %q. Use: raw, json, or styled\n", parts[1])
			}
			fmt.Println()
			continue
		}

		// Handle set_debug (toggle game client debug logging at runtime).
		if command == "set_debug" {
			toggler, ok := client.(interface{ SetDebugLogging(bool) })
			if !ok {
				fmt.Println("set_debug: not supported by this client")
				fmt.Println()
				continue
			}
			if len(parts) < 2 {
				fmt.Println("Usage: set_debug <true|false|on|off>")
				fmt.Println()
				continue
			}
			var enabled bool
			switch strings.ToLower(parts[1]) {
			case "on":
				enabled = true
			case "off":
				enabled = false
			default:
				b, perr := strconv.ParseBool(parts[1])
				if perr != nil {
					fmt.Printf("set_debug: unrecognized value %q (use true/false/on/off)\n", parts[1])
					fmt.Println()
					continue
				}
				enabled = b
			}
			toggler.SetDebugLogging(enabled)
			if enabled {
				fmt.Println("Debug logging enabled")
			} else {
				fmt.Println("Debug logging disabled")
			}
			fmt.Println()
			continue
		}

		// Handle scripts (list saved scripts)
		if command == "scripts" {
			perAgent, shared := worker.ListScripts(agentID)
			if len(perAgent) == 0 && len(shared) == 0 {
				fmt.Println("No scripts found.")
			} else {
				overridden := make(map[string]bool, len(perAgent))
				for _, n := range perAgent {
					overridden[n] = true
				}
				fmt.Println("Scripts:")
				for _, n := range perAgent {
					fmt.Printf("  %s (agent)\n", n)
				}
				for _, n := range shared {
					if overridden[n] {
						fmt.Printf("  %s (shared, overridden)\n", n)
					} else {
						fmt.Printf("  %s (shared)\n", n)
					}
				}
			}
			fmt.Println()
			continue
		}

		// Handle save (persist the last command to the shared scripts dir)
		if command == "save" {
			switch {
			case len(parts) < 2:
				fmt.Println("Usage: save <name>")
			case lastCommand == "":
				fmt.Println("❌ save: no previous command to save")
			default:
				if err := worker.SaveScript(parts[1], lastCommand); err != nil {
					fmt.Printf("❌ %v\n", err)
				} else {
					fmt.Printf("✓ saved script %q\n", parts[1])
				}
			}
			fmt.Println()
			continue
		}

		// Handle run (load and execute a script)
		if command == "run" {
			if len(parts) < 2 {
				fmt.Println("Usage: run <name|path>")
			} else {
				runScript(client, ctx, parts[1], format, cfg, agentID)
			}
			fmt.Println()
			continue
		}

		// Game command or loop: dispatch through the shared helper (also used
		// by `run`). execMu keeps a background scheduled command from
		// interleaving with this foreground one.
		lastCommand = cmd
		execMu.Lock()
		// Run under a per-command cancellable context armed on the interrupter,
		// so a SIGINT (Ctrl+C) during this command cancels it — aborting a loop
		// or unblocking an in-flight await — instead of killing the process.
		cmdCtx, cancel := context.WithCancel(ctx)
		intr.arm(cancel)
		_ = executeLogicalCommand(client, cmdCtx, cmd, format, cfg, agentID)
		intr.disarm()
		cancel()
		execMu.Unlock()
	}
}

// printResponse formats and prints the server response based on the current format.
func printResponse(raw []byte, format outputFormat, command string) {
	if format == formatRaw {
		fmt.Printf("\n%s\n", string(raw))
		return
	}

	// For styled format, try command-specific formatters first
	if format == formatStyled {
		if styled := formatStyledResponse(raw, command); styled != "" {
			fmt.Printf("\n%s\n", styled)
			return
		}
		// Fall through to JSON if no styled formatter exists
	}

	// Default: pretty-printed JSON
	var pretty any
	if err := json.Unmarshal(raw, &pretty); err == nil {
		formatted, _ := json.MarshalIndent(pretty, "", "  ")
		fmt.Printf("\n%s\n", string(formatted))
	} else {
		fmt.Printf("\n%s\n", string(raw))
	}
}

// unwrapActionResult returns the inner result JSON if raw is an action_result
// frame ({"command":"...","result":{...},"tick":N}), or raw unchanged otherwise.
// Many formatters predate the response-router migration that switched these
// commands to action_result termination. The wrapper means those formatters
// see top-level keys like {"command","result","tick"} and miss the actual
// payload fields nested inside "result". Calling unwrapActionResult at the
// top of a formatter restores the pre-wrap shape so existing field bindings
// work unchanged.
func unwrapActionResult(raw []byte) []byte {
	var probe struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return raw
	}
	if len(probe.Result) == 0 {
		return raw
	}
	return probe.Result
}

// formatStyledResponse returns a styled string for the given command, or "" if no formatter exists.
func formatStyledResponse(raw []byte, command string) string {
	switch command {
	case "cloak":
		return formatCloak(raw)
	case "status", "get_status":
		return formatGetStatus(raw)
	case "get_state":
		return formatGetState(raw)
	case "storage", "view_storage":
		return formatStorage(raw)
	case "view_faction_storage":
		return formatFactionStorage(raw)
	case "cargo", "get_cargo":
		return formatCargo(raw)
	case "browse_ships":
		return formatBrowseShips(raw)
	case "nearby", "get_nearby":
		return formatNearby(raw)
	case "travel":
		return formatTravel(raw)
	case "mine":
		return formatMine(raw)
	case "jump":
		return formatJump(raw)
	case "dock":
		return formatDock(raw)
	case "wrecks", "get_wrecks":
		return formatWrecks(raw)
	case "loot", "loot_wreck":
		return formatLootWreck(raw)
	case "jettison":
		return formatJettison(raw)
	case "refuel":
		return formatRefuel(raw)
	case "undock":
		return "Undocked"
	case "system", "get_system":
		return formatSystem(raw)
	case "withdraw", "withdraw_items":
		return formatWithdraw(raw)
	case "create_faction":
		return formatCreateFaction(raw)
	case "faction_info":
		return formatFactionInfo(raw)
	case "faction_intel_status":
		return formatFactionIntelStatus(raw)
	case "faction_query_intel":
		return formatFactionQueryIntel(raw)
	case "deposit", "deposit_items":
		return formatDeposit(raw)
	case "skills", "get_skills":
		return formatSkills(raw)
	case "view_market":
		return formatMarket(raw)
	case "view_orders", "orders":
		return formatViewOrders(raw)
	case "faction_get_invites":
		return formatFactionInvites(raw)
	case "chat_history", "get_chat_history":
		return formatChatHistory(raw)
	case "craft":
		return formatCraft(raw)
	case "recycle":
		return formatCraft(raw)
	case "missions", "get_missions":
		return formatMissions(raw)
	case "active_missions", "get_active_missions":
		return formatActiveMissions(raw)
	case "complete_mission":
		return formatCompleteMission(raw)
	case "notes", "get_notes":
		return formatNotes(raw)
	case "list_ships":
		return formatListShips(raw)
	case "facility":
		return formatFacility(raw)
	case "get_system_agents":
		return formatGetSystemAgents(raw)
	case "get_drones", "drones":
		return formatGetDrones(raw)
	case "get_location", "location":
		return formatGetLocation(raw)
	case "get_drone":
		return formatGetDrone(raw)
	case "load_drone":
		return formatLoadDrone(raw)
	case "unload_drone":
		return formatUnloadDrone(raw)
	case "recall_drone":
		return formatRecallDrone(raw)
	case "upload_drone_script":
		return formatUploadDroneScript(raw)
	case "deploy_drone":
		return formatDeployDrone(raw)
	case "get_tax_estimate", "tax_estimate":
		return formatGetTaxEstimate(raw)
	case "get_insurance_quote", "insurance_quote":
		return formatGetInsuranceQuote(raw)
	case "get_achievements", "achievements":
		return formatGetAchievements(raw)
	case "commission_quote":
		return formatCommissionQuote(raw)
	case "commission_status":
		return formatCommissionStatus(raw)
	case "list_passengers", "passengers":
		return formatListPassengers(raw)
	case "list_station_passengers", "station_passengers":
		return formatStationPassengers(raw)
	case "load_passenger":
		return formatLoadPassenger(raw)
	case "unload_passenger":
		return formatUnloadPassenger(raw)
	default:
		return ""
	}
}

// formatGetSystemAgents renders a get_system_agents response using the same
// player-table layout as formatNearby for consistency. nearbyPlayer's fields
// are a superset of what get_system_agents returns, so the same writer works.
func formatGetSystemAgents(raw []byte) string {
	var resp struct {
		SystemID string         `json:"system_id"`
		Count    int            `json:"count"`
		Agents   []nearbyPlayer `json:"agents"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "System: %s\n", resp.SystemID)
	fmt.Fprintf(&b, "Count: %d\n\n", resp.Count)
	writePlayerTable(&b, resp.Agents)
	return b.String()
}

// formatGetDrones renders a get_drones response: a bay/bandwidth summary line
// followed by a roster table. Field names match the live payload (id, type,
// cargo_pct, has_script), which differs from serverapi.Drone — so the row
// shape is declared inline here, as elsewhere in this file. Full 32-char drone
// IDs are shown so they can be copy-pasted into deploy_drone/get_drone/recall.
func formatGetDrones(raw []byte) string {
	raw = unwrapActionResult(raw)
	type droneRow struct {
		ID        string  `json:"id"`
		Name      string  `json:"name"`
		Type      string  `json:"type"`
		Status    string  `json:"status"`
		Hull      int     `json:"hull"`
		MaxHull   int     `json:"max_hull"`
		CargoPct  float64 `json:"cargo_pct"`
		HasScript bool    `json:"has_script"`
		POIID     string  `json:"poi_id"`
	}
	var resp struct {
		BandwidthTotal int        `json:"bandwidth_total"`
		BandwidthUsed  int        `json:"bandwidth_used"`
		BayCapacity    int        `json:"bay_capacity"`
		BayCount       int        `json:"bay_count"`
		DeployedCount  int        `json:"deployed_count"`
		Drones         []droneRow `json:"drones"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Drones: %d in bay, %d deployed | Bay: %d/%d | Bandwidth: %d/%d\n",
		resp.BayCount, resp.DeployedCount, resp.BayCount, resp.BayCapacity,
		resp.BandwidthUsed, resp.BandwidthTotal)
	if len(resp.Drones) == 0 {
		b.WriteString("\n  (no drones)\n")
		return b.String()
	}

	slices.SortFunc(resp.Drones, func(a, c droneRow) int {
		return strings.Compare(a.ID, c.ID)
	})

	// Name column appears only if at least one drone has been named
	// (set_drone_name); otherwise it's wasted whitespace.
	hasName := false
	for _, d := range resp.Drones {
		if d.Name != "" {
			hasName = true
			break
		}
	}

	typeW, statusW, hullW, poiW, nameW := len("Type"), len("Status"), len("Hull"), len("POI"), len("Name")
	for _, d := range resp.Drones {
		typeW = max(typeW, len(d.Type))
		statusW = max(statusW, len(d.Status))
		hullW = max(hullW, len(fmt.Sprintf("%d/%d", d.Hull, d.MaxHull)))
		poiW = max(poiW, len(d.POIID))
		nameW = max(nameW, len(d.Name))
	}

	if hasName {
		fmt.Fprintf(&b, "\n  %-*s | %-*s | %-*s | %-*s | %5s | %-6s | %-*s | %s\n",
			nameW, "Name", typeW, "Type", statusW, "Status", hullW, "Hull", "Cargo", "Script", poiW, "POI", "Drone ID")
		fmt.Fprintf(&b, "  %s-+-%s-+-%s-+-%s-+-%s-+-%s-+-%s-+-%s\n",
			strings.Repeat("-", nameW), strings.Repeat("-", typeW), strings.Repeat("-", statusW),
			strings.Repeat("-", hullW), strings.Repeat("-", 5), strings.Repeat("-", 6),
			strings.Repeat("-", poiW), strings.Repeat("-", len("Drone ID")))
	} else {
		fmt.Fprintf(&b, "\n  %-*s | %-*s | %-*s | %5s | %-6s | %-*s | %s\n",
			typeW, "Type", statusW, "Status", hullW, "Hull", "Cargo", "Script", poiW, "POI", "Drone ID")
		fmt.Fprintf(&b, "  %s-+-%s-+-%s-+-%s-+-%s-+-%s-+-%s\n",
			strings.Repeat("-", typeW), strings.Repeat("-", statusW), strings.Repeat("-", hullW),
			strings.Repeat("-", 5), strings.Repeat("-", 6), strings.Repeat("-", poiW), strings.Repeat("-", len("Drone ID")))
	}
	for _, d := range resp.Drones {
		script := "no"
		if d.HasScript {
			script = "yes"
		}
		if hasName {
			fmt.Fprintf(&b, "  %-*s | %-*s | %-*s | %-*s | %4.0f%% | %-6s | %-*s | %s\n",
				nameW, d.Name, typeW, d.Type, statusW, d.Status,
				hullW, fmt.Sprintf("%d/%d", d.Hull, d.MaxHull),
				d.CargoPct, script, poiW, d.POIID, d.ID)
		} else {
			fmt.Fprintf(&b, "  %-*s | %-*s | %-*s | %4.0f%% | %-6s | %-*s | %s\n",
				typeW, d.Type, statusW, d.Status, hullW, fmt.Sprintf("%d/%d", d.Hull, d.MaxHull),
				d.CargoPct, script, poiW, d.POIID, d.ID)
		}
	}
	return b.String()
}

// formatGetDrone renders the detail view for a single drone (get_drone).
func formatGetDrone(raw []byte) string {
	raw = unwrapActionResult(raw)
	type droneCargoItem struct {
		ItemID   string  `json:"item_id"`
		Quantity float64 `json:"quantity"`
		Size     int     `json:"size"`
	}
	var resp struct {
		ID            string            `json:"id"`
		Name          string            `json:"name"`
		Type          string            `json:"type"`
		Status        string            `json:"status"`
		Hull          int               `json:"hull"`
		MaxHull       int               `json:"max_hull"`
		ItemID        string            `json:"item_id"`
		POIID         string            `json:"poi_id"`
		SystemID      string            `json:"system_id"`
		CargoUsed     int               `json:"cargo_used"`
		CargoCapacity int               `json:"cargo_capacity"`
		Cargo         []droneCargoItem  `json:"cargo"`
		Script        string            `json:"script"`
		Memory        map[string]string `json:"memory"`
		LoadedAt      string            `json:"loaded_at"`
		DeployedAt    string            `json:"deployed_at"`
		TravelTo      string            `json:"travel_to"`
		TravelTicks   int               `json:"travel_ticks"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}
	if resp.ID == "" {
		return ""
	}

	var b strings.Builder
	if resp.Name != "" {
		fmt.Fprintf(&b, "Drone %s — %s (%s)\n", resp.Name, resp.ID, resp.Type)
	} else {
		fmt.Fprintf(&b, "Drone %s (%s)\n", resp.ID, resp.Type)
	}
	fmt.Fprintf(&b, "  Status:   %s\n", resp.Status)
	fmt.Fprintf(&b, "  Hull:     %d/%d\n", resp.Hull, resp.MaxHull)
	fmt.Fprintf(&b, "  Cargo:    %d/%d", resp.CargoUsed, resp.CargoCapacity)
	if len(resp.Cargo) > 0 {
		fmt.Fprintf(&b, " (%d stack(s))", len(resp.Cargo))
	}
	b.WriteString("\n")
	if len(resp.Cargo) > 0 {
		slices.SortFunc(resp.Cargo, func(a, c droneCargoItem) int {
			return strings.Compare(a.ItemID, c.ItemID)
		})
		idW, qtyW, sizeW := len("ID"), len("Qty"), len("Size")
		for _, item := range resp.Cargo {
			idW = max(idW, len(item.ItemID))
			qtyW = max(qtyW, len(formatFloat(item.Quantity)))
			sizeW = max(sizeW, len(strconv.Itoa(item.Size)))
		}
		fmt.Fprintf(&b, "    %-*s | %*s | %*s\n", idW, "ID", qtyW, "Qty", sizeW, "Size")
		fmt.Fprintf(&b, "    %s-+-%s-+-%s\n",
			strings.Repeat("-", idW), strings.Repeat("-", qtyW), strings.Repeat("-", sizeW))
		for _, item := range resp.Cargo {
			fmt.Fprintf(&b, "    %-*s | %*s | %*d\n",
				idW, item.ItemID, qtyW, formatFloat(item.Quantity), sizeW, item.Size)
		}
	}
	if resp.POIID != "" {
		fmt.Fprintf(&b, "  POI:      %s\n", resp.POIID)
	}
	if resp.SystemID != "" {
		fmt.Fprintf(&b, "  System:   %s\n", resp.SystemID)
	}
	if resp.TravelTo != "" {
		fmt.Fprintf(&b, "  Travel:   → %s (%d tick(s))\n", resp.TravelTo, resp.TravelTicks)
	}
	if resp.ItemID != "" {
		fmt.Fprintf(&b, "  Item:     %s\n", resp.ItemID)
	}
	if resp.Script != "" {
		lines := strings.Split(resp.Script, "\n")
		fmt.Fprintf(&b, "  Script:   %d char(s), %d line(s)\n", len(resp.Script), len(lines))
		// Show the first three lines so the operator can recognise the
		// script at a glance; truncate the third line with '...' if more
		// content follows.
		const previewLines = 3
		for i := 0; i < previewLines && i < len(lines); i++ {
			line := lines[i]
			if i == previewLines-1 && len(lines) > previewLines {
				// Strip trailing whitespace before appending the ellipsis
				// so it sits flush with real content.
				line = strings.TrimRight(line, " \t") + " ..."
			}
			fmt.Fprintf(&b, "    %s\n", line)
		}
	} else {
		b.WriteString("  Script:   (none)\n")
	}
	if len(resp.Memory) > 0 {
		keys := make([]string, 0, len(resp.Memory))
		for k := range resp.Memory {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		keyW := 0
		for _, k := range keys {
			keyW = max(keyW, len(k))
		}
		fmt.Fprintf(&b, "  Memory:   %d key(s)\n", len(keys))
		for _, k := range keys {
			fmt.Fprintf(&b, "    %-*s = %s\n", keyW, k, resp.Memory[k])
		}
	}
	return b.String()
}

// formatGetLocation renders a get_location response: a compact header (POI,
// system, security, connections), nearby counts, and tables for the nearby
// players / empire NPCs (reusing writePlayerTable for the player table since
// the nearby_players objects match nearbyPlayer's shape). Field names follow
// the live payload (poi_id, system_id, nearby_empire_npc_count, …).
func formatGetLocation(raw []byte) string {
	type empireNPC struct {
		Name      string `json:"name"`
		ShipName  string `json:"ship_name,omitempty"`
		ShipClass string `json:"ship_class"`
		Empire    string `json:"empire"`
		FleetName string `json:"fleet_name,omitempty"`
		Role      string `json:"role,omitempty"`
		InCombat  bool   `json:"in_combat"`
		NPCID     string `json:"npc_id"`
	}
	type loc struct {
		POIID            string            `json:"poi_id"`
		POIName          string            `json:"poi_name"`
		POIType          string            `json:"poi_type"`
		DockedAt         string            `json:"docked_at"`
		SystemID         string            `json:"system_id"`
		SystemName       string            `json:"system_name"`
		Empire           string            `json:"empire"`
		Security         string            `json:"security_status"`
		Connections      []string          `json:"connections"`
		Resources        []json.RawMessage `json:"resources"`
		PlayerCount      int               `json:"nearby_player_count"`
		NPCCount         int               `json:"nearby_empire_npc_count"`
		PirateCount      int               `json:"nearby_pirate_count"`
		OfflineCollapsed int               `json:"offline_collapsed"`
		Players          []nearbyPlayer    `json:"nearby_players"`
		NPCs             []empireNPC       `json:"nearby_empire_npcs"`
		Pirates          []json.RawMessage `json:"nearby_pirates"`
	}
	var resp struct {
		Location loc    `json:"location"`
		Message  string `json:"message"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}
	l := resp.Location
	if l.POIID == "" && l.SystemID == "" {
		return ""
	}

	var b strings.Builder
	if l.POIName != "" {
		fmt.Fprintf(&b, "Location: %s", l.POIName)
		if l.POIType != "" {
			fmt.Fprintf(&b, " (%s)", l.POIType)
		}
	} else {
		fmt.Fprintf(&b, "Location: %s", l.POIID)
	}
	if l.DockedAt != "" {
		fmt.Fprintf(&b, "  [docked: %s]", l.DockedAt)
	}
	b.WriteString("\n")
	if l.SystemName != "" || l.SystemID != "" {
		fmt.Fprintf(&b, "System:   %s (%s)", l.SystemName, l.SystemID)
		if l.Empire != "" {
			fmt.Fprintf(&b, " — empire: %s", l.Empire)
		}
		b.WriteString("\n")
	}
	if l.Security != "" {
		fmt.Fprintf(&b, "Security: %s\n", l.Security)
	}
	if len(l.Connections) > 0 {
		fmt.Fprintf(&b, "Connect:  %s\n", strings.Join(l.Connections, ", "))
	}
	if len(l.Resources) > 0 {
		fmt.Fprintf(&b, "Resources: %d listed\n", len(l.Resources))
	}

	fmt.Fprintf(&b, "\nNearby: %d player(s), %d empire NPC(s), %d pirate(s)",
		l.PlayerCount, l.NPCCount, l.PirateCount)
	if l.OfflineCollapsed > 0 {
		fmt.Fprintf(&b, " (%d offline)", l.OfflineCollapsed)
	}
	b.WriteString("\n")

	if len(l.Players) > 0 {
		b.WriteString("\nPlayers:\n")
		writePlayerTable(&b, l.Players)
	}

	if len(l.NPCs) > 0 {
		slices.SortFunc(l.NPCs, func(a, c empireNPC) int {
			if d := strings.Compare(a.FleetName, c.FleetName); d != 0 {
				return d
			}
			return strings.Compare(a.Name, c.Name)
		})
		nameW, classW, fleetW, roleW := len("Name"), len("Class"), len("Fleet"), len("Role")
		for _, n := range l.NPCs {
			nameW = max(nameW, len(n.Name))
			classW = max(classW, len(n.ShipClass))
			fleetW = max(fleetW, len(n.FleetName))
			roleW = max(roleW, len(n.Role))
		}
		fmt.Fprintf(&b, "\nEmpire NPCs:\n  %-*s | %-*s | %-*s | %-*s\n",
			nameW, "Name", classW, "Class", fleetW, "Fleet", roleW, "Role")
		fmt.Fprintf(&b, "  %s-+-%s-+-%s-+-%s\n",
			strings.Repeat("-", nameW), strings.Repeat("-", classW),
			strings.Repeat("-", fleetW), strings.Repeat("-", roleW))
		for _, n := range l.NPCs {
			fmt.Fprintf(&b, "  %-*s | %-*s | %-*s | %-*s\n",
				nameW, n.Name, classW, n.ShipClass, fleetW, n.FleetName, roleW, n.Role)
		}
	}

	if len(l.Pirates) > 0 {
		fmt.Fprintf(&b, "\nPirates: %d\n", len(l.Pirates))
	}
	return b.String()
}

// formatLoadDrone renders a load_drone action_result.
func formatLoadDrone(raw []byte) string {
	raw = unwrapActionResult(raw)
	var resp struct {
		DroneID     string `json:"drone_id"`
		DroneType   string `json:"drone_type"`
		Hull        int    `json:"hull"`
		Status      string `json:"status"`
		BayCount    int    `json:"bay_count"`
		BayCapacity int    `json:"bay_capacity"`
		Message     string `json:"message"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}
	if resp.DroneID == "" && resp.Message == "" {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Loaded %s drone [%s] | Hull %d | Bay %d/%d\n",
		resp.DroneType, resp.Status, resp.Hull, resp.BayCount, resp.BayCapacity)
	if resp.DroneID != "" {
		fmt.Fprintf(&b, "  Drone ID: %s\n", resp.DroneID)
	}
	if resp.Message != "" {
		fmt.Fprintf(&b, "  %s\n", resp.Message)
	}
	return b.String()
}

// formatUnloadDrone renders an unload_drone action_result.
func formatUnloadDrone(raw []byte) string {
	raw = unwrapActionResult(raw)
	var resp struct {
		DroneID string `json:"drone_id"`
		ItemID  string `json:"item_id"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}
	if resp.DroneID == "" && resp.Message == "" {
		return ""
	}
	var b strings.Builder
	if resp.DroneID != "" {
		fmt.Fprintf(&b, "Unloaded drone %s → %s\n", resp.DroneID, resp.ItemID)
	}
	if resp.Message != "" {
		fmt.Fprintf(&b, "  %s\n", resp.Message)
	}
	return b.String()
}

// formatRecallDrone renders a recall_drone action_result.
func formatRecallDrone(raw []byte) string {
	raw = unwrapActionResult(raw)
	var resp struct {
		Recalled int    `json:"recalled"`
		Skipped  int    `json:"skipped"`
		Message  string `json:"message"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Recalled %d drone(s), skipped %d\n", resp.Recalled, resp.Skipped)
	if resp.Message != "" {
		fmt.Fprintf(&b, "  %s\n", resp.Message)
	}
	return b.String()
}

// formatUploadDroneScript renders an upload_drone_script action_result.
func formatUploadDroneScript(raw []byte) string {
	raw = unwrapActionResult(raw)
	var resp struct {
		DroneID   string `json:"drone_id"`
		ScriptLen int    `json:"script_len"`
		Message   string `json:"message"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}
	if resp.DroneID == "" && resp.Message == "" {
		return ""
	}
	var b strings.Builder
	if resp.ScriptLen == 0 {
		fmt.Fprintf(&b, "Cleared script on drone %s\n", resp.DroneID)
	} else {
		fmt.Fprintf(&b, "Uploaded script to drone %s (%d char(s))\n", resp.DroneID, resp.ScriptLen)
	}
	if resp.Message != "" {
		fmt.Fprintf(&b, "  %s\n", resp.Message)
	}
	return b.String()
}

// formatDeployDrone renders a deploy_drone action_result. The server uses
// two distinct payload shapes: single-drone deploys carry drone_id +
// drone_type + hull/max_hull/status (per-drone fields); the --all bulk
// deploy carries deployed/skipped counts instead. We detect the bulk shape
// via the presence of "deployed" and render accordingly.
func formatDeployDrone(raw []byte) string {
	raw = unwrapActionResult(raw)
	var probe struct {
		Deployed *int `json:"deployed"`
	}
	if err := json.Unmarshal(raw, &probe); err == nil && probe.Deployed != nil {
		var bulk struct {
			Deployed       int    `json:"deployed"`
			Skipped        int    `json:"skipped"`
			BandwidthUsed  int    `json:"bandwidth_used"`
			BandwidthTotal int    `json:"bandwidth_total"`
			Message        string `json:"message"`
		}
		if err := json.Unmarshal(raw, &bulk); err != nil {
			return ""
		}
		var b strings.Builder
		fmt.Fprintf(&b, "Deployed %d drone(s)", bulk.Deployed)
		if bulk.Skipped > 0 {
			fmt.Fprintf(&b, " (%d skipped — would exceed bandwidth)", bulk.Skipped)
		}
		fmt.Fprintf(&b, " | Bandwidth %d/%d\n", bulk.BandwidthUsed, bulk.BandwidthTotal)
		if bulk.Message != "" {
			fmt.Fprintf(&b, "  %s\n", bulk.Message)
		}
		return b.String()
	}

	var resp struct {
		DroneID        string `json:"drone_id"`
		DroneType      string `json:"drone_type"`
		Hull           int    `json:"hull"`
		MaxHull        int    `json:"max_hull"`
		Status         string `json:"status"`
		BandwidthUsed  int    `json:"bandwidth_used"`
		BandwidthTotal int    `json:"bandwidth_total"`
		Message        string `json:"message"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}
	if resp.DroneID == "" && resp.Message == "" {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Deployed %s drone [%s] | Hull %d/%d | Bandwidth %d/%d\n",
		resp.DroneType, resp.Status, resp.Hull, resp.MaxHull,
		resp.BandwidthUsed, resp.BandwidthTotal)
	if resp.DroneID != "" {
		fmt.Fprintf(&b, "  Drone ID: %s\n", resp.DroneID)
	}
	if resp.Message != "" {
		fmt.Fprintf(&b, "  %s\n", resp.Message)
	}
	return b.String()
}

// formatGetTaxEstimate renders the tax preview: a header noting whether
// taxes are live or simulated, a totals row, sales-tax rates broken out
// per empire (citizen rate flagged), taxable income by source, and the
// per-ship property assessment. Empty sections are elided.
func formatGetTaxEstimate(raw []byte) string {
	type incomeRow struct {
		Amount   int64  `json:"amount"`
		Category string `json:"category"`
	}
	type salesRate struct {
		Empire string `json:"empire"`
		RateBP int    `json:"rate_bps"`
		Reason string `json:"reason"`
	}
	type shipValue struct {
		ShipID string `json:"ship_id"`
		Value  int64  `json:"value"`
	}
	var resp struct {
		AssessedPropertyByShip      []shipValue `json:"assessed_property_by_ship"`
		AssessedPropertyValue       int64       `json:"assessed_property_value"`
		IncomeTaxTotal              int64       `json:"income_tax_total"`
		LastAssessedAt              int64       `json:"last_assessed_at"`
		LastPropertyAssessedAt      int64       `json:"last_property_assessed_at"`
		NextAssessmentApproxSeconds int64       `json:"next_assessment_approx_seconds"`
		Note                        string      `json:"note"`
		PropertyTaxTotal            int64       `json:"property_tax_total"`
		SalesTaxRates               []salesRate `json:"sales_tax_rates"`
		TaxCollectionActive         bool        `json:"tax_collection_active"`
		TaxableIncomeBySource       []incomeRow `json:"taxable_income_by_source"`
		TaxableIncomeToDate         int64       `json:"taxable_income_to_date"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}

	var b strings.Builder
	status := "PREVIEW (simulated, no credits deducted)"
	if resp.TaxCollectionActive {
		status = "ACTIVE"
	}
	fmt.Fprintf(&b, "=== Tax Estimate (%s) ===\n", status)
	fmt.Fprintf(&b, "Assessed property:  %d cr (income-to-date: %d cr)\n",
		resp.AssessedPropertyValue, resp.TaxableIncomeToDate)
	fmt.Fprintf(&b, "Property tax due:   %d cr   |   Income tax due: %d cr\n",
		resp.PropertyTaxTotal, resp.IncomeTaxTotal)
	if resp.NextAssessmentApproxSeconds > 0 {
		days := resp.NextAssessmentApproxSeconds / 86400
		hours := (resp.NextAssessmentApproxSeconds % 86400) / 3600
		fmt.Fprintf(&b, "Next assessment:    ~%dd %dh\n", days, hours)
	}

	if len(resp.SalesTaxRates) > 0 {
		fmt.Fprintf(&b, "\nSales tax rates (per empire):\n")
		fmt.Fprintf(&b, "  %-12s  %-7s  %s\n", "Empire", "Rate", "Reason")
		fmt.Fprintf(&b, "  %-12s  %-7s  %s\n", "------------", "-------", "------")
		for _, r := range resp.SalesTaxRates {
			tag := ""
			if r.Reason == "citizen" {
				tag = " *"
			}
			fmt.Fprintf(&b, "  %-12s  %5.2f%%  %s%s\n",
				r.Empire, float64(r.RateBP)/100, r.Reason, tag)
		}
	}

	if len(resp.TaxableIncomeBySource) > 0 {
		any := false
		for _, r := range resp.TaxableIncomeBySource {
			if r.Amount != 0 {
				any = true
				break
			}
		}
		fmt.Fprintf(&b, "\nTaxable income by source:\n")
		if !any {
			fmt.Fprintf(&b, "  (none recorded)\n")
		} else {
			for _, r := range resp.TaxableIncomeBySource {
				if r.Amount == 0 {
					continue
				}
				fmt.Fprintf(&b, "  %-14s  %d cr\n", r.Category, r.Amount)
			}
		}
	}

	if len(resp.AssessedPropertyByShip) > 0 {
		fmt.Fprintf(&b, "\nAssessed property by ship (%d ships):\n", len(resp.AssessedPropertyByShip))
		fmt.Fprintf(&b, "  %-34s  %s\n", "Ship ID", "Value (cr)")
		fmt.Fprintf(&b, "  %-34s  %s\n", strings.Repeat("-", 34), "----------")
		for _, s := range resp.AssessedPropertyByShip {
			fmt.Fprintf(&b, "  %-34s  %10d\n", s.ShipID, s.Value)
		}
	}

	if resp.Note != "" {
		fmt.Fprintf(&b, "\nNote: %s\n", resp.Note)
	}
	return b.String()
}

// formatCommissionQuote renders a commission_quote response: the per-class
// header (class, name, shipyard tier here vs required), both pricing
// options (credits-only and provide-materials), and the build-materials
// table when materials are an option. Build time is shown in ticks +
// hours since 1 tick ≈ 10 s.
func formatCommissionQuote(raw []byte) string {
	type material struct {
		ItemID   string `json:"item_id"`
		Name     string `json:"name"`
		Quantity int    `json:"quantity"`
		Size     int    `json:"size"`
	}
	var resp struct {
		Message                   string     `json:"message"`
		ShipClass                 string     `json:"ship_class"`
		ShipName                  string     `json:"ship_name"`
		CanCommission             bool       `json:"can_commission"`
		CreditsOnlyTotal          int        `json:"credits_only_total"`
		ProvideMaterialsTotal     int        `json:"provide_materials_total"`
		CreditsOnlyAvailable      bool       `json:"credits_only_available"`
		CanAffordCreditsOnly      bool       `json:"can_afford_credits_only"`
		CanAffordProvideMaterials bool       `json:"can_afford_provide_materials"`
		Blockers                  []string   `json:"blockers"`
		BuildMaterials            []material `json:"build_materials"`
		BuildTime                 int        `json:"build_time"`
		LaborCost                 int        `json:"labor_cost"`
		MaterialCost              int        `json:"material_cost"`
		PlayerCredits             int        `json:"player_credits"`
		ShipyardTierHere          int        `json:"shipyard_tier_here"`
		ShipyardTierRequired      int        `json:"shipyard_tier_required"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}
	if resp.ShipClass == "" {
		return ""
	}

	check := func(ok bool) string {
		if ok {
			return "✓"
		}
		return "✗"
	}

	var b strings.Builder
	name := resp.ShipName
	if name == "" {
		name = resp.ShipClass
	}
	fmt.Fprintf(&b, "=== Commission Quote: %s (%s) ===\n", name, resp.ShipClass)

	tierNote := ""
	if resp.ShipyardTierHere < resp.ShipyardTierRequired {
		tierNote = "  ✗ insufficient"
	}
	fmt.Fprintf(&b, "Shipyard tier:  here=%d  required=%d%s\n",
		resp.ShipyardTierHere, resp.ShipyardTierRequired, tierNote)
	if resp.BuildTime > 0 {
		hours := float64(resp.BuildTime) * 10 / 3600
		fmt.Fprintf(&b, "Build time:     %d ticks (~%.1f h)\n", resp.BuildTime, hours)
	}
	fmt.Fprintf(&b, "Player credits: %d cr\n", resp.PlayerCredits)
	fmt.Fprintf(&b, "Can commission: %s\n\n", check(resp.CanCommission))

	// Pricing options.
	fmt.Fprintln(&b, "Options:")
	if resp.CreditsOnlyAvailable {
		fmt.Fprintf(&b, "  %s credits-only:       %d cr   (afford: %s)\n",
			check(resp.CanAffordCreditsOnly), resp.CreditsOnlyTotal,
			check(resp.CanAffordCreditsOnly))
	} else {
		fmt.Fprintln(&b, "  ✗ credits-only:       not offered at this shipyard")
	}
	fmt.Fprintf(&b, "  %s provide-materials:  %d cr labor",
		check(resp.CanAffordProvideMaterials), resp.ProvideMaterialsTotal)
	if resp.MaterialCost > 0 {
		fmt.Fprintf(&b, " (materials worth ~%d cr)", resp.MaterialCost)
	}
	fmt.Fprintf(&b, "   (afford: %s)\n", check(resp.CanAffordProvideMaterials))

	if len(resp.Blockers) > 0 {
		fmt.Fprintf(&b, "\nBlockers:\n")
		for _, blk := range resp.Blockers {
			fmt.Fprintf(&b, "  - %s\n", blk)
		}
	}

	if len(resp.BuildMaterials) > 0 {
		fmt.Fprintf(&b, "\nBuild materials (provide-materials path, %d items):\n",
			len(resp.BuildMaterials))
		tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(tw, "  ITEM\tQTY\tNAME")
		for _, m := range resp.BuildMaterials {
			_, _ = fmt.Fprintf(tw, "  %s\t%d\t%s\n", m.ItemID, m.Quantity, m.Name)
		}
		_ = tw.Flush()
	}

	if resp.Message != "" {
		fmt.Fprintf(&b, "\n%s\n", resp.Message)
	}
	return b.String()
}

// facilityPositionalKeys returns the payload keys that bare positional
// arguments fill for a given facility action, in order. Most actions take a
// facility_type (build, types, personal_build, …), but a few take different
// primary arguments — e.g. `facility set_access public` means access=public,
// not facility_type=public. Anything not listed here falls back to
// facility_type so the historical `facility build <type>` form keeps working.
func facilityPositionalKeys(action string) []string {
	switch action {
	case "set_access":
		return []string{"access"}
	case "set_output_price":
		return []string{"item_id", "price"}
	case "buy_listing", "cancel_listing":
		return []string{"listing_id"}
	default:
		return []string{"facility_type"}
	}
}

// buildFacilityPayload parses a `facility ...` command's tokens (parts[0] is
// "facility") into the server payload, plus the client-side
// show_station_facilities display toggle. Both --flag=value and --flag value
// forms are accepted, as are bare key=value tokens; the first bare positional
// is the action (unless action=... is given), and remaining positionals map to
// the action's keys via facilityPositionalKeys.
func buildFacilityPayload(parts []string) (payload map[string]any, showStation bool, err error) {
	payload = map[string]any{}
	var positionals []string
	for i := 1; i < len(parts); i++ {
		arg := parts[i]
		if key, ok := strings.CutPrefix(arg, "--"); ok {
			// Support the --flag=value form first, so e.g. --deliver_to=faction
			// doesn't fold "=faction" into the key and swallow the next token.
			if k, v, found := strings.Cut(key, "="); found {
				key = k
				if key == "show_station_facilities" {
					showStation = true
					continue
				}
				payload[key] = v
				continue
			}
			// --show_station_facilities is a client-side display toggle for
			// `facility list`; it takes no value and is not sent to the server.
			if key == "show_station_facilities" {
				showStation = true
				continue
			}
			// --flag value form: consume the next token as the value, but only
			// when it isn't itself another --flag (a lone flag sends "").
			if i+1 < len(parts) && !strings.HasPrefix(parts[i+1], "--") {
				i++
				payload[key] = parts[i]
			} else {
				payload[key] = ""
			}
		} else if k, v, ok := strings.Cut(arg, "="); ok {
			payload[k] = v
		} else {
			positionals = append(positionals, arg)
		}
	}
	// Resolve the action: an explicit action=... / --action wins, otherwise
	// the first bare positional is the action.
	action, _ := payload["action"].(string)
	if action == "" && len(positionals) > 0 {
		action = positionals[0]
		positionals = positionals[1:]
		payload["action"] = action
	}
	if action == "" {
		return nil, false, fmt.Errorf("facility: missing action (e.g. `facility build` or `facility action=build`)")
	}
	// Map any remaining bare positionals onto this action's argument keys.
	// Flags already in the payload take precedence; extra positionals beyond
	// the action's arity are ignored.
	posKeys := facilityPositionalKeys(action)
	for idx, val := range positionals {
		if idx >= len(posKeys) {
			break
		}
		if _, exists := payload[posKeys[idx]]; !exists {
			payload[posKeys[idx]] = val
		}
	}
	// Convert numeric string fields.
	for _, numKey := range []string{"level", "page", "per_page", "quantity", "position", "price"} {
		if v, ok := payload[numKey].(string); ok {
			if n, convErr := strconv.Atoi(v); convErr == nil {
				payload[numKey] = n
			}
		}
	}
	return payload, showStation, nil
}

// formatFacility dispatches by the response's "action" field (or, for
// payloads that don't carry one, by the presence of distinctive keys).
// Actions without a styled formatter return "" so the caller falls through
// to pretty-printed JSON.
func formatFacility(raw []byte) string {
	// Mutations (e.g. faction_build) terminate as action_result frames that
	// nest the payload under "result"; queries (types, faction_list) carry it
	// at the top level. Unwrap so the action probe and sub-formatters see the
	// same shape either way (no-op when there is no "result" key).
	raw = unwrapActionResult(raw)
	var probe struct {
		Action            string          `json:"action"`
		TypeID            string          `json:"type_id"`
		FactionFacilities json.RawMessage `json:"faction_facilities"`
		PlayerFacilities  json.RawMessage `json:"player_facilities"`
		StationFacilities json.RawMessage `json:"station_facilities"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return ""
	}
	switch probe.Action {
	case "types":
		// When the caller passes facility_type=<id>, the server returns the
		// detail of a single facility (top-level type_id) instead of a
		// types[] list.
		if probe.TypeID != "" {
			return formatFacilityTypeDetail(raw)
		}
		return formatFacilityTypes(raw)
	case "faction_build":
		return formatFacilityFactionBuild(raw)
	case "list":
		return formatFacilityList(raw)
	case "owned":
		return formatFacilityOwned(raw)
	case "faction_owned":
		return formatFacilityFactionOwned(raw)
	case "browse_for_sale":
		return formatFacilityForSale(raw)
	case "job_list":
		return formatCraftQueue(unwrapActionResult(raw))
	case "job_add", "job_cancel", "job_reorder", "set_output_price", "set_access", "upgrade":
		return formatFacilityActionMessage(raw)
	}
	// Plain `facility list` lacks an action field but carries all three
	// section keys (player_facilities + station_facilities + faction_facilities,
	// any of which may be empty []). Detect it FIRST so we don't fall through
	// into formatFacilityFactionList just because faction_facilities happens
	// to be populated.
	//
	// json.RawMessage is nil-length when the JSON key is absent, length>0 when
	// the key is present (even as `[]`), so this is a key-presence test.
	if len(probe.PlayerFacilities) > 0 || len(probe.StationFacilities) > 0 {
		return formatFacilityList(raw)
	}
	// faction_list omits the action field and only carries faction_facilities.
	if len(probe.FactionFacilities) > 0 {
		return formatFacilityFactionList(raw)
	}
	return ""
}

// formatFacilityActionMessage renders the simple {action, message, ...} result
// of facility job/business mutations (job_add, job_cancel, job_reorder,
// set_output_price, set_access, upgrade). It shows the action and the server's
// human message, falling back to "" so the caller prints JSON when absent.
func formatFacilityActionMessage(raw []byte) string {
	var r struct {
		Action     string `json:"action"`
		Message    string `json:"message"`
		JobID      string `json:"job_id"`
		FacilityID string `json:"facility_id"`
	}
	if err := json.Unmarshal(unwrapActionResult(raw), &r); err != nil {
		return ""
	}
	if r.Message == "" {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "🏭 facility %s: %s", r.Action, r.Message)
	if r.JobID != "" {
		fmt.Fprintf(&b, " (job %s)", r.JobID)
	}
	b.WriteString("\n")
	return b.String()
}

// formatFacilityOwned renders a `facility owned` response (gameserver v0.347.0+):
// every facility the player owns across all stations, with the aggregate rent
// bill and any rent arrears.
func formatFacilityOwned(raw []byte) string {
	var resp struct {
		Facilities []struct {
			Name              string `json:"name"`
			Type              string `json:"type"`
			BaseName          string `json:"base_name"`
			SystemID          string `json:"system_id"`
			RentPerCycle      int    `json:"rent_per_cycle"`
			LaborPerRun       int    `json:"labor_per_run"`
			ArrearsOwed       int    `json:"arrears_owed"`
			MissedRentCycles  int    `json:"missed_rent_cycles"`
			Active            bool   `json:"active"`
			UnderConstruction bool   `json:"under_construction"`
		} `json:"facilities"`
		Rent struct {
			Facilities        int    `json:"facilities"`
			TotalRentPerCycle int    `json:"total_rent_per_cycle"`
			EstRentPerDay     int    `json:"est_rent_per_day"`
			ArrearsOwed       int    `json:"arrears_owed"`
			GraceCycles       int    `json:"grace_cycles"`
			Note              string `json:"note"`
		} `json:"rent"`
	}
	if err := json.Unmarshal(unwrapActionResult(raw), &resp); err != nil {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "🏭 Your Facilities — %d total | rent %d/cycle, ~%d/day\n",
		resp.Rent.Facilities, resp.Rent.TotalRentPerCycle, resp.Rent.EstRentPerDay)
	if resp.Rent.ArrearsOwed > 0 {
		fmt.Fprintf(&b, "⚠ Arrears owed: %d (grace: %d cycles)\n", resp.Rent.ArrearsOwed, resp.Rent.GraceCycles)
	}
	if len(resp.Facilities) == 0 {
		fmt.Fprintf(&b, "  (none)\n")
		return b.String()
	}

	nameW, typeW, baseW := len("Name"), len("Type"), len("Base")
	for _, f := range resp.Facilities {
		nameW = max(nameW, len(f.Name))
		typeW = max(typeW, len(f.Type))
		baseW = max(baseW, len(f.BaseName))
	}
	fmt.Fprintf(&b, "  %-*s | %-*s | %-*s | %9s | %5s | Status\n",
		nameW, "Name", typeW, "Type", baseW, "Base", "Rent/cyc", "Labor")
	fmt.Fprintf(&b, "  %s-+-%s-+-%s-+-%s-+-%s-+--------\n",
		strings.Repeat("-", nameW), strings.Repeat("-", typeW), strings.Repeat("-", baseW),
		strings.Repeat("-", 9), strings.Repeat("-", 5))
	for _, f := range resp.Facilities {
		status := "active"
		switch {
		case f.UnderConstruction:
			status = "building"
		case !f.Active:
			status = "idle"
		}
		if f.MissedRentCycles > 0 {
			status += fmt.Sprintf(" ⚠%d missed", f.MissedRentCycles)
		}
		fmt.Fprintf(&b, "  %-*s | %-*s | %-*s | %9d | %5d | %s\n",
			nameW, f.Name, typeW, f.Type, baseW, f.BaseName, f.RentPerCycle, f.LaborPerRun, status)
	}
	if resp.Rent.Note != "" {
		fmt.Fprintf(&b, "  %s\n", resp.Rent.Note)
	}
	return b.String()
}

// formatFacilityFactionOwned renders a `facility faction_owned` response
// (gameserver v0.347.0+): the faction's facilities everywhere, with per-run
// labor cost and any idle reason.
func formatFacilityFactionOwned(raw []byte) string {
	var resp struct {
		FactionID  string `json:"faction_id"`
		Note       string `json:"note"`
		Facilities []struct {
			Name              string `json:"name"`
			Type              string `json:"type"`
			BaseName          string `json:"base_name"`
			SystemID          string `json:"system_id"`
			LaborPerRun       int    `json:"labor_per_run"`
			IdleReason        string `json:"idle_reason"`
			Active            bool   `json:"active"`
			UnderConstruction bool   `json:"under_construction"`
		} `json:"facilities"`
	}
	if err := json.Unmarshal(unwrapActionResult(raw), &resp); err != nil {
		return ""
	}

	var b strings.Builder
	var totalLabor int
	for _, f := range resp.Facilities {
		totalLabor += f.LaborPerRun
	}
	fmt.Fprintf(&b, "🏭 Faction Facilities — %d total | labor %d/run\n", len(resp.Facilities), totalLabor)
	if len(resp.Facilities) == 0 {
		fmt.Fprintf(&b, "  (none)\n")
		return b.String()
	}

	nameW, typeW, baseW := len("Name"), len("Type"), len("Base")
	for _, f := range resp.Facilities {
		nameW = max(nameW, len(f.Name))
		typeW = max(typeW, len(f.Type))
		baseW = max(baseW, len(f.BaseName))
	}
	fmt.Fprintf(&b, "  %-*s | %-*s | %-*s | %5s | Status\n",
		nameW, "Name", typeW, "Type", baseW, "Base", "Labor")
	fmt.Fprintf(&b, "  %s-+-%s-+-%s-+-%s-+--------\n",
		strings.Repeat("-", nameW), strings.Repeat("-", typeW), strings.Repeat("-", baseW), strings.Repeat("-", 5))
	for _, f := range resp.Facilities {
		status := "active"
		switch {
		case f.UnderConstruction:
			status = "building"
		case !f.Active:
			status = "idle"
			if f.IdleReason != "" {
				status += " (" + f.IdleReason + ")"
			}
		}
		fmt.Fprintf(&b, "  %-*s | %-*s | %-*s | %5d | %s\n",
			nameW, f.Name, typeW, f.Type, baseW, f.BaseName, f.LaborPerRun, status)
	}
	if resp.Note != "" {
		fmt.Fprintf(&b, "  %s\n", resp.Note)
	}
	return b.String()
}

// formatFacilityForSale renders a `facility browse_for_sale` response — the
// facilities listed for sale at the current station. The per-listing item schema
// is not pinned here (the server may add fields), so each listing is rendered
// generically from whatever keys it carries, with facility_id surfaced first
// since it is the argument the buy_listing / cancel_listing actions need.
func formatFacilityForSale(raw []byte) string {
	raw = unwrapActionResult(raw)
	var resp struct {
		BaseID   string           `json:"base_id"`
		BaseName string           `json:"base_name"`
		Listings []map[string]any `json:"listings"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}
	base := resp.BaseName
	if base == "" {
		base = resp.BaseID
	}
	var b strings.Builder
	if len(resp.Listings) == 0 {
		fmt.Fprintf(&b, "  No facilities listed for sale at %s.\n", base)
		return b.String()
	}
	fmt.Fprintf(&b, "  Facilities for sale at %s (%d)\n", base, len(resp.Listings))
	for _, l := range resp.Listings {
		if id, ok := l["facility_id"]; ok {
			fmt.Fprintf(&b, "\n    facility_id: %v\n", id)
		}
		keys := make([]string, 0, len(l))
		for k := range l {
			if k == "facility_id" {
				continue
			}
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			fmt.Fprintf(&b, "      %s: %v\n", k, l[k])
		}
	}
	return b.String()
}

// formatFacilityFactionBuild renders a `facility faction_build` action_result:
// the facility identity, where it was built, its service and rent, construction
// status, and any XP awarded.
func formatFacilityFactionBuild(raw []byte) string {
	raw = unwrapActionResult(raw)
	var resp struct {
		BaseID            string         `json:"base_id"`
		FacilityID        string         `json:"facility_id"`
		FacilityName      string         `json:"facility_name"`
		FacilityType      string         `json:"facility_type"`
		FactionService    string         `json:"faction_service"`
		Hint              string         `json:"hint"`
		MembersAwardedXP  int            `json:"members_awarded_xp"`
		RentPerCycle      int64          `json:"rent_per_cycle"`
		SkillXP           map[string]int `json:"skill_xp"`
		UnderConstruction bool           `json:"under_construction"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}
	if resp.FacilityID == "" && resp.FacilityName == "" {
		return ""
	}

	var b strings.Builder
	status := "Active"
	if resp.UnderConstruction {
		status = "Under construction"
	}
	fmt.Fprintf(&b, "🏗  Built faction facility: %s (%s)\n", resp.FacilityName, resp.FacilityType)
	fmt.Fprintf(&b, "  Base:        %s\n", resp.BaseID)
	fmt.Fprintf(&b, "  Facility ID: %s\n", resp.FacilityID)
	if resp.FactionService != "" {
		fmt.Fprintf(&b, "  Service:     %s\n", resp.FactionService)
	}
	fmt.Fprintf(&b, "  Rent/Cycle:  %s\n", formatCredits(float64(resp.RentPerCycle)))
	fmt.Fprintf(&b, "  Status:      %s\n", status)

	if len(resp.SkillXP) > 0 {
		skills := make([]string, 0, len(resp.SkillXP))
		for skill := range resp.SkillXP {
			skills = append(skills, skill)
		}
		slices.Sort(skills)
		b.WriteString("\n")
		for _, skill := range skills {
			fmt.Fprintf(&b, " +%d xp %s\n", resp.SkillXP[skill], skill)
		}
		if resp.MembersAwardedXP > 0 {
			fmt.Fprintf(&b, " (awarded to %d member(s))\n", resp.MembersAwardedXP)
		}
	}

	if resp.Hint != "" {
		fmt.Fprintf(&b, "\nℹ %s\n", resp.Hint)
	}
	return b.String()
}

// formatFacilityTypes renders a `facility types` listing as an aligned
// table, sorted alphabetically by id.
func formatFacilityTypes(raw []byte) string {
	var resp struct {
		Page       int `json:"page"`
		TotalPages int `json:"total_pages"`
		Total      int `json:"total"`
		Types      []struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			Category  string `json:"category"`
			Level     int    `json:"level"`
			BuildCost int64  `json:"build_cost"`
		} `json:"types"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}
	if len(resp.Types) == 0 {
		return "  (no facility types)\n"
	}
	slices.SortFunc(resp.Types, func(a, b struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Category  string `json:"category"`
		Level     int    `json:"level"`
		BuildCost int64  `json:"build_cost"`
	}) int {
		return strings.Compare(a.ID, b.ID)
	})

	idW := len("ID")
	nameW := len("Name")
	costW := len("Cost")
	for _, t := range resp.Types {
		idW = max(idW, len(t.ID))
		nameW = max(nameW, len(t.Name))
		costW = max(costW, len(formatCredits(float64(t.BuildCost))))
	}

	var b strings.Builder
	category := resp.Types[0].Category
	header := "  Facilities"
	if category != "" {
		header = fmt.Sprintf("  Facilities available for '%s'", category)
	}
	if resp.TotalPages > 1 {
		fmt.Fprintf(&b, "%s (page %d/%d, %d total):\n\n", header, resp.Page, resp.TotalPages, resp.Total)
	} else {
		fmt.Fprintf(&b, "%s:\n\n", header)
	}
	fmt.Fprintf(&b, "  %-*s | %-*s | Level | %*s\n", idW, "ID", nameW, "Name", costW, "Cost")
	fmt.Fprintf(&b, "  %s-+-%s-+-------+-%s\n",
		strings.Repeat("-", idW), strings.Repeat("-", nameW), strings.Repeat("-", costW))
	for _, t := range resp.Types {
		fmt.Fprintf(&b, "  %-*s | %-*s | %5d | %*s\n",
			idW, t.ID, nameW, t.Name, t.Level, costW, formatCredits(float64(t.BuildCost)))
	}
	return b.String()
}

// facilityProduction is the per-facility production/throughput block the server
// attaches to production facilities (station- and faction-owned alike). Shared
// by stationFacility and factionFacilityRow so both render via one helper.
type facilityProduction struct {
	Recipe       string `json:"recipe"`
	RecipeID     string `json:"recipe_id"`
	ItemsPerHour int    `json:"items_per_hour"`
	OutputPerRun int    `json:"output_per_run"`
	// TicksPerRun is fractional on the live server (e.g. 0.0939), so it must
	// decode as a float — an int field makes json.Unmarshal fail and the whole
	// `facility list` fall back to raw JSON.
	TicksPerRun     float64 `json:"ticks_per_run"`
	QueuedRuns      int     `json:"queued_runs"`
	QueuedItems     int     `json:"queued_items"`
	BacklogTicks    int     `json:"backlog_ticks"`
	RentalFeePerRun int     `json:"rental_fee_per_run"`
	Public          bool    `json:"public"`
}

// productionFacility is the minimal shape renderProductionFacilityTable needs:
// a display name plus the production block. Station and faction production
// facilities both map onto it so they share one renderer.
type productionFacility struct {
	name string
	prod *facilityProduction
}

// renderProductionFacilityTable writes the wide production table (recipe +
// throughput / queue / rent columns under a two-line header) for the given
// facilities, prefixed with indent and introduced by heading. Shared by the
// Station Production and Faction Production sections so they stay identical.
func renderProductionFacilityTable(b *strings.Builder, facs []productionFacility, indent, heading string) {
	type prodRow struct {
		name, typ, feehr, outrun, cycle, runcost, queued, backlog, public string
	}
	rows := make([]prodRow, 0, len(facs))
	for _, f := range facs {
		p := f.prod
		public := "No"
		if p.Public {
			public = "Yes"
		}
		rows = append(rows, prodRow{
			name:   f.name,
			typ:    "⚙ " + p.Recipe,
			feehr:  strconv.Itoa(p.ItemsPerHour),
			outrun: strconv.Itoa(p.OutputPerRun),
			// Fractional ticks/run (e.g. 0.9259…) is rounded to 2 dp.
			cycle:   strconv.FormatFloat(p.TicksPerRun, 'f', 2, 64),
			runcost: strconv.Itoa(p.RentalFeePerRun),
			queued:  strconv.Itoa(p.QueuedRuns),
			backlog: strconv.Itoa(p.BacklogTicks),
			public:  public,
		})
	}
	// Column widths span both header lines and the values. The Type cell carries
	// a multi-byte ⚙ glyph, so measure it by rune count (display width) and pad
	// it manually rather than via %-*s (which pads bytes).
	nameW := len("Name")
	typeW := len("Type")
	feeW := max(len("Fee"), len("/hr"))
	outW := max(len("Output"), len("/run"))
	cycW := max(len("Cycle"), len("tick/run"))
	costW := max(len("Run"), len("cost"))
	queueW := max(len("Queued"), len("runs"))
	backW := max(len("Backlog"), len("ticks"))
	pubW := len("Public")
	for _, r := range rows {
		nameW = max(nameW, len(r.name))
		typeW = max(typeW, len([]rune(r.typ)))
		feeW = max(feeW, len(r.feehr))
		outW = max(outW, len(r.outrun))
		cycW = max(cycW, len(r.cycle))
		costW = max(costW, len(r.runcost))
		queueW = max(queueW, len(r.queued))
		backW = max(backW, len(r.backlog))
		pubW = max(pubW, len(r.public))
	}
	padRunes := func(s string, w int) string {
		if n := len([]rune(s)); n < w {
			return s + strings.Repeat(" ", w-n)
		}
		return s
	}
	fmt.Fprintf(b, "\n%s%s:\n", indent, heading)
	// Two-line header: units (/hr, /run, tick/run, ...) sit on row two.
	fmt.Fprintf(b, "%s  %-*s | %-*s | %*s | %*s | %*s | %*s | %*s | %*s | %-*s\n",
		indent, nameW, "Name", typeW, "Type", feeW, "Fee", outW, "Output", cycW, "Cycle",
		costW, "Run", queueW, "Queued", backW, "Backlog", pubW, "Public")
	fmt.Fprintf(b, "%s  %-*s | %-*s | %*s | %*s | %*s | %*s | %*s | %*s | %-*s\n",
		indent, nameW, "", typeW, "", feeW, "/hr", outW, "/run", cycW, "tick/run",
		costW, "cost", queueW, "runs", backW, "ticks", pubW, "")
	fmt.Fprintf(b, "%s  %s-+-%s-+-%s-+-%s-+-%s-+-%s-+-%s-+-%s-+-%s\n",
		indent,
		strings.Repeat("-", nameW), strings.Repeat("-", typeW), strings.Repeat("-", feeW),
		strings.Repeat("-", outW), strings.Repeat("-", cycW), strings.Repeat("-", costW),
		strings.Repeat("-", queueW), strings.Repeat("-", backW), strings.Repeat("-", pubW))
	for _, r := range rows {
		fmt.Fprintf(b, "%s  %-*s | %s | %*s | %*s | %*s | %*s | %*s | %*s | %-*s\n",
			indent, nameW, r.name, padRunes(r.typ, typeW), feeW, r.feehr, outW, r.outrun,
			cycW, r.cycle, costW, r.runcost, queueW, r.queued, backW, r.backlog, pubW, r.public)
	}
}

// factionFacilityRow is the shared decode target for the faction-facility
// table rendered by both formatFacilityFactionList and formatFacilityList.
// Lifted out of the formatters so the rendering helper can sit beside it
// without re-declaring the type per call site.
type factionFacilityRow struct {
	Active         bool   `json:"active"`
	Capacity       int64  `json:"capacity"`
	CustomName     string `json:"custom_name,omitempty"`
	FacilityID     string `json:"facility_id"`
	FactionService string `json:"faction_service"`
	Level          int    `json:"level"`
	Name           string `json:"name"`
	RentPerCycle   int64  `json:"rent_per_cycle"`
	Status         string `json:"status"`
	Type           string `json:"type"`
	// Production is set for faction production facilities (refineries, etc.);
	// nil for plain service facilities. Drives the production/service split.
	Production *facilityProduction `json:"production,omitempty"`
}

// displayName returns the operator-assigned custom name when set, else the
// facility's type name — so a renamed facility (e.g. "Bob's Iron Smeltery")
// shows that rather than the generic "Iron Refinery".
func (f factionFacilityRow) displayName() string {
	if f.CustomName != "" {
		return f.CustomName
	}
	return f.Name
}

// renderFactionFacilities splits faction facilities into service and production
// groups and renders each with its dedicated table (mirroring the station
// services/production split): plain services via renderFactionFacilityTable,
// production facilities via the shared renderProductionFacilityTable.
func renderFactionFacilities(b *strings.Builder, facilities []factionFacilityRow, indent string) {
	var services, production []factionFacilityRow
	for _, f := range facilities {
		if f.Production != nil {
			production = append(production, f)
		} else {
			services = append(services, f)
		}
	}
	if len(services) > 0 {
		renderFactionFacilityTable(b, services, indent)
	}
	if len(production) > 0 {
		pf := make([]productionFacility, 0, len(production))
		for _, f := range production {
			pf = append(pf, productionFacility{name: f.displayName(), prod: f.Production})
		}
		renderProductionFacilityTable(b, pf, indent, fmt.Sprintf("Faction Production (%d)", len(production)))
	}
}

// renderFactionFacilityTable writes the standard faction-facility table
// (Name | Type | Service | Lvl | Status | Capacity | Rent/cycle | Rent/day)
// into b, prefixed with the given indent on every line. Used by both
// `facility faction_list` and the Faction section of `facility list` so the
// two views stay visually consistent.
func renderFactionFacilityTable(b *strings.Builder, facilities []factionFacilityRow, indent string) {
	nameW := len("Name")
	typeW := len("Type")
	svcW := len("Service")
	statusW := len("Status")
	capW := len("Capacity")
	rentW := len("Rent/cycle")
	dailyW := len("Rent/day")
	idW := len("Facility ID")
	for _, f := range facilities {
		nameW = max(nameW, len(f.displayName()))
		typeW = max(typeW, len(f.Type))
		svcW = max(svcW, len(f.FactionService))
		statusW = max(statusW, len(f.Status))
		capW = max(capW, len(formatCredits(float64(f.Capacity))))
		rentW = max(rentW, len(formatCredits(float64(f.RentPerCycle))))
		dailyW = max(dailyW, len(formatCredits(float64(dailyRent(f.RentPerCycle)))))
		idW = max(idW, len(f.FacilityID))
	}

	fmt.Fprintf(b, "%s%-*s | %-*s | %-*s | Lvl | %-*s | %*s | %*s | %*s | %-*s\n",
		indent, nameW, "Name", typeW, "Type", svcW, "Service",
		statusW, "Status", capW, "Capacity", rentW, "Rent/cycle", dailyW, "Rent/day", idW, "Facility ID")
	fmt.Fprintf(b, "%s%s-+-%s-+-%s-+-----+-%s-+-%s-+-%s-+-%s-+-%s\n",
		indent,
		strings.Repeat("-", nameW), strings.Repeat("-", typeW),
		strings.Repeat("-", svcW), strings.Repeat("-", statusW),
		strings.Repeat("-", capW), strings.Repeat("-", rentW),
		strings.Repeat("-", dailyW), strings.Repeat("-", idW))
	for _, f := range facilities {
		fmt.Fprintf(b, "%s%-*s | %-*s | %-*s | %3d | %-*s | %*s | %*s | %*s | %-*s\n",
			indent,
			nameW, f.displayName(), typeW, f.Type, svcW, f.FactionService,
			facilityLevelOrDefault(f.Level), statusW, f.Status,
			capW, formatCredits(float64(f.Capacity)),
			rentW, formatCredits(float64(f.RentPerCycle)),
			dailyW, formatCredits(float64(dailyRent(f.RentPerCycle))),
			idW, f.FacilityID)
	}
}

// formatFacilityFactionList renders a `facility faction_list` response:
// a header with the base/faction context, the faction-storage summary,
// the table of built facilities, and the server's hint if present.
func formatFacilityFactionList(raw []byte) string {
	var resp struct {
		BaseID            string               `json:"base_id"`
		FactionID         string               `json:"faction_id"`
		FactionFacilities []factionFacilityRow `json:"faction_facilities"`
		FactionStorage    struct {
			Credits   int64 `json:"credits"`
			ItemTypes int   `json:"item_types"`
			Rooms     int   `json:"rooms"`
		} `json:"faction_storage"`
		Hint string `json:"hint"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "  Faction Facilities at %s\n", resp.BaseID)
	if resp.FactionID != "" {
		fmt.Fprintf(&b, "    Faction:  %s\n", resp.FactionID)
	}
	fmt.Fprintf(&b, "    Storage:  %s cr | %d item type(s) | %d room(s)\n\n",
		formatCredits(float64(resp.FactionStorage.Credits)),
		resp.FactionStorage.ItemTypes, resp.FactionStorage.Rooms)

	if len(resp.FactionFacilities) == 0 {
		fmt.Fprintln(&b, "  (no faction facilities built)")
		if resp.Hint != "" {
			fmt.Fprintf(&b, "\n  💡 %s\n", resp.Hint)
		}
		return b.String()
	}

	slices.SortFunc(resp.FactionFacilities, func(a, c factionFacilityRow) int {
		return strings.Compare(a.Name, c.Name)
	})
	renderFactionFacilities(&b, resp.FactionFacilities, "  ")

	if resp.Hint != "" {
		fmt.Fprintf(&b, "\n  💡 %s\n", resp.Hint)
	}
	return b.String()
}

// facilityLevelOrDefault renders an instance level. The server's facility
// list response currently omits `level` per-instance (it's in the catalog
// but not echoed back), defaulting our struct field to 0. Per the catalog,
// every facility type starts at level 1, so treat 0 as the unset sentinel
// and display 1. Drop this fallback once the server emits the field.
func facilityLevelOrDefault(level int) int {
	if level <= 0 {
		return 1
	}
	return level
}

// dailyRent converts a per-cycle rent value to a per-real-day amount. A
// rent cycle is 100 game ticks; with SleepTick=10s that's 1000s/cycle,
// or 86.4 cycles per real-time day. Integer-safe: perCycle * 86400 / 1000.
func dailyRent(perCycle int64) int64 {
	const secondsPerCycle = 100 * 10 // 100 ticks × 10s/tick
	const secondsPerDay = 24 * 60 * 60
	return perCycle * int64(secondsPerDay) / int64(secondsPerCycle)
}

// formatFacilityList renders a plain `facility list` response — the three
// facility groupings visible at the current station: station services,
// player-owned personal facilities (quarters, etc.), and faction-owned
// facilities. Each section renders only if non-empty, with column sets
// tuned to the fields the server actually carries for that scope.
// showStationFacilities toggles rendering of the station-owned facility list
// in `facility list` output. It is off by default (the station's own
// facilities are noise for the player's facility view) and enabled
// per-invocation by the `--show_station_facilities` flag. It is set under
// execMu (foreground and background commands are serialized) and read
// synchronously while the response is formatted, so a plain package-level
// flag is sufficient.
var showStationFacilities bool

func formatFacilityList(raw []byte) string {
	type personalFacility struct {
		Active               bool   `json:"active"`
		Category             string `json:"category"`
		FacilityID           string `json:"facility_id"`
		MaintenanceSatisfied bool   `json:"maintenance_satisfied"`
		Name                 string `json:"name"`
		PersonalService      string `json:"personal_service"`
		RentPaidUntilTick    int64  `json:"rent_paid_until_tick"`
		RentPerCycle         int64  `json:"rent_per_cycle"`
		Type                 string `json:"type"`
	}
	// stationFacility describes a facility the station itself owns/operates.
	// Only rendered when --show_station_facilities is set.
	type stationFacility struct {
		Active               bool   `json:"active"`
		Category             string `json:"category"`
		Description          string `json:"description"`
		FacilityID           string `json:"facility_id"`
		Level                int    `json:"level"`
		MaintenanceSatisfied bool   `json:"maintenance_satisfied"`
		Name                 string `json:"name"`
		Service              string `json:"service"`
		RecipeID             string `json:"recipe_id"`
		IdleReason           string `json:"idle_reason"`
		Type                 string `json:"type"`
		Production           *facilityProduction `json:"production"`
	}
	var resp struct {
		BaseID           string             `json:"base_id"`
		PlayerFacilities []personalFacility `json:"player_facilities"`
		// StationFacilities describes what the station itself offers (markets,
		// refuel, production, etc.). Hidden by default — it's not the player's
		// own facilities — and only rendered with --show_station_facilities.
		StationFacilities []stationFacility    `json:"station_facilities"`
		FactionFacilities []factionFacilityRow `json:"faction_facilities"`
		// PlayerRent is the aggregate rent bill across all the player's
		// facilities (gameserver v0.347.0+), surfaced as a station-level total.
		PlayerRent struct {
			Facilities        int    `json:"facilities"`
			TotalRentPerCycle int    `json:"total_rent_per_cycle"`
			EstRentPerDay     int    `json:"est_rent_per_day"`
			ArrearsOwed       int    `json:"arrears_owed"`
			GraceCycles       int    `json:"grace_cycles"`
			Note              string `json:"note"`
		} `json:"player_rent"`
		Hint string `json:"hint"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "  Facilities at %s\n", resp.BaseID)
	if resp.PlayerRent.Facilities > 0 {
		fmt.Fprintf(&b, "  Rent (all your facilities): %d/cycle, ~%d/day across %d facilities\n",
			resp.PlayerRent.TotalRentPerCycle, resp.PlayerRent.EstRentPerDay, resp.PlayerRent.Facilities)
		if resp.PlayerRent.ArrearsOwed > 0 {
			fmt.Fprintf(&b, "  ⚠ Arrears owed: %d (grace: %d cycles)\n", resp.PlayerRent.ArrearsOwed, resp.PlayerRent.GraceCycles)
		}
	}

	totalSections := 0
	if len(resp.PlayerFacilities) > 0 {
		totalSections++
		slices.SortFunc(resp.PlayerFacilities, func(a, c personalFacility) int {
			return strings.Compare(a.Name, c.Name)
		})
		nameW := len("Name")
		typeW := len("Type")
		svcW := len("Service")
		tickW := len("Paid until tick")
		idW := len("Facility ID")
		for _, f := range resp.PlayerFacilities {
			nameW = max(nameW, len(f.Name))
			typeW = max(typeW, len(f.Type))
			svcW = max(svcW, len(f.PersonalService))
			tickW = max(tickW, len(strconv.FormatInt(f.RentPaidUntilTick, 10)))
			idW = max(idW, len(f.FacilityID))
		}
		// One rent cycle = 100 ticks ≈ 17 min real time; 86.4 cycles per day.
		// Show both per-cycle and per-day so the operator can sanity-check
		// burn rate against credit balance.
		fmt.Fprintf(&b, "\n  Personal: (1 cycle = 100 ticks ≈ 17 min, 86.4 cycles/day)\n")
		fmt.Fprintf(&b, "    %-*s | %-*s | %-*s | Active | Maint | %10s | %10s | %-*s | %-*s\n",
			nameW, "Name", typeW, "Type", svcW, "Service", "Rent/cycle", "Rent/day", tickW, "Paid until tick", idW, "Facility ID")
		fmt.Fprintf(&b, "    %s-+-%s-+-%s-+--------+-------+-%s-+-%s-+-%s-+-%s\n",
			strings.Repeat("-", nameW), strings.Repeat("-", typeW),
			strings.Repeat("-", svcW), strings.Repeat("-", 10), strings.Repeat("-", 10),
			strings.Repeat("-", tickW), strings.Repeat("-", idW))
		for _, f := range resp.PlayerFacilities {
			active := "no"
			if f.Active {
				active = "yes"
			}
			maint := "ok"
			if !f.MaintenanceSatisfied {
				maint = "!"
			}
			fmt.Fprintf(&b, "    %-*s | %-*s | %-*s | %-6s | %-5s | %10s | %10s | %-*d | %-*s\n",
				nameW, f.Name, typeW, f.Type, svcW, f.PersonalService,
				active, maint,
				formatCredits(float64(f.RentPerCycle)),
				formatCredits(float64(dailyRent(f.RentPerCycle))),
				tickW, f.RentPaidUntilTick,
				idW, f.FacilityID)
		}
	}
	if len(resp.FactionFacilities) > 0 {
		totalSections++
		slices.SortFunc(resp.FactionFacilities, func(a, c factionFacilityRow) int {
			return strings.Compare(a.Name, c.Name)
		})
		fmt.Fprintf(&b, "\n  Faction:\n")
		renderFactionFacilities(&b, resp.FactionFacilities, "    ")
	}
	if showStationFacilities && len(resp.StationFacilities) > 0 {
		slices.SortFunc(resp.StationFacilities, func(a, c stationFacility) int {
			if a.Category != c.Category {
				return strings.Compare(a.Category, c.Category)
			}
			return strings.Compare(a.Name, c.Name)
		})
		// Production facilities carry extra throughput/rent/queue detail, so
		// split them into their own heading and a wider table; the rest stay
		// in the compact services table. Facilities are grouped by whether they
		// expose a production block (the source of those extra details).
		var services, production []stationFacility
		for _, f := range resp.StationFacilities {
			if f.Production != nil {
				production = append(production, f)
			} else {
				services = append(services, f)
			}
		}

		if len(services) > 0 {
			totalSections++
			nameW, typeW, catW, svcW := len("Name"), len("Type"), len("Category"), len("Service")
			statusW := len("Status")
			for _, f := range services {
				nameW = max(nameW, len(f.Name))
				typeW = max(typeW, len(f.Type))
				catW = max(catW, len(f.Category))
				svcW = max(svcW, len(f.Service))
				statusW = max(statusW, len(stationFacilityStatus(f.Active, f.IdleReason)))
			}
			fmt.Fprintf(&b, "\n  Station Services (%d):\n", len(services))
			fmt.Fprintf(&b, "    %-*s | %-*s | %-*s | Lvl | %-*s | Maint | %-*s\n",
				nameW, "Name", typeW, "Type", catW, "Category", svcW, "Service", statusW, "Status")
			fmt.Fprintf(&b, "    %s-+-%s-+-%s-+-----+-%s-+-------+-%s\n",
				strings.Repeat("-", nameW), strings.Repeat("-", typeW), strings.Repeat("-", catW),
				strings.Repeat("-", svcW), strings.Repeat("-", statusW))
			for _, f := range services {
				maint := "ok"
				if !f.MaintenanceSatisfied {
					maint = "!"
				}
				fmt.Fprintf(&b, "    %-*s | %-*s | %-*s | %3d | %-*s | %-5s | %-*s\n",
					nameW, f.Name, typeW, f.Type, catW, f.Category, f.Level,
					svcW, f.Service, maint, statusW, stationFacilityStatus(f.Active, f.IdleReason))
			}
		}

		if len(production) > 0 {
			totalSections++
			pf := make([]productionFacility, 0, len(production))
			for _, f := range production {
				pf = append(pf, productionFacility{name: f.Name, prod: f.Production})
			}
			renderProductionFacilityTable(&b, pf, "  ", fmt.Sprintf("Station Production (%d)", len(production)))
		}
	}

	if totalSections == 0 {
		fmt.Fprintln(&b, "  (no facilities)")
	}
	if resp.Hint != "" {
		fmt.Fprintf(&b, "\n  💡 %s\n", resp.Hint)
	}
	return b.String()
}

// stationFacilityStatus summarizes a station facility's operating state: an
// inactive facility reads "inactive"; an active-but-stalled one surfaces its
// idle_reason (e.g. "idle: no_inputs"); otherwise "active".
func stationFacilityStatus(active bool, idleReason string) string {
	if !active {
		return "inactive"
	}
	if idleReason != "" {
		return "idle: " + idleReason
	}
	return "active"
}

// formatFacilityTypeDetail renders the single-facility detail variant of a
// `facility types` response (returned when the request includes a
// facility_type filter).
func formatFacilityTypeDetail(raw []byte) string {
	type material struct {
		ItemID   string `json:"item_id"`
		Name     string `json:"name"`
		Quantity int    `json:"quantity"`
	}
	var resp struct {
		TypeID               string     `json:"type_id"`
		Name                 string     `json:"name"`
		Description          string     `json:"description"`
		Lore                 string     `json:"lore"`
		Category             string     `json:"category"`
		Level                int        `json:"level"`
		BuildCost            int64      `json:"build_cost"`
		BuildTime            int        `json:"build_time"`
		LaborCost            int        `json:"labor_cost"`
		RentPerCycle         int64      `json:"rent_per_cycle"`
		FactionCap           int        `json:"faction_cap"`
		FactionService       string     `json:"faction_service"`
		PersonalService      string     `json:"personal_service"`
		RecipeID             string     `json:"recipe_id"`
		RecipeMultiplier     float64    `json:"recipe_multiplier"`
		BonusType            string     `json:"bonus_type"`
		BonusValue           float64    `json:"bonus_value"`
		BuildMaterials       []material `json:"build_materials"`
		MaintenancePerCycle  []material `json:"maintenance_per_cycle"`
		UpgradesFrom         string     `json:"upgrades_from"`
		UpgradesFromName     string     `json:"upgrades_from_name"`
		UpgradesTo           string     `json:"upgrades_to"`
		UpgradesToName       string     `json:"upgrades_to_name"`
		Buildable            bool       `json:"buildable"`
		Hint                 string     `json:"hint"`
		SatisfiedDescription string     `json:"satisfied_description"`
		DegradedDescription  string     `json:"degraded_description"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}
	if resp.TypeID == "" {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "  Facility: %s (%s)\n", resp.Name, resp.TypeID)
	fmt.Fprintf(&b, "  Category: %s    Level: %d\n", resp.Category, resp.Level)
	if resp.Description != "" {
		fmt.Fprintf(&b, "\n  %s\n", resp.Description)
	}

	fmt.Fprintf(&b, "\n  Build Cost:   %s\n", formatCredits(float64(resp.BuildCost)))
	fmt.Fprintf(&b, "  Build Time:   %d\n", resp.BuildTime)
	fmt.Fprintf(&b, "  Labor Cost:   %d\n", resp.LaborCost)
	fmt.Fprintf(&b, "  Rent / Cycle: %s\n", formatCredits(float64(resp.RentPerCycle)))

	if len(resp.BuildMaterials) > 0 {
		b.WriteString("\n  Build Materials:\n")
		for _, m := range resp.BuildMaterials {
			fmt.Fprintf(&b, "    %dx %s (%s)\n", m.Quantity, m.Name, m.ItemID)
		}
	}
	if len(resp.MaintenancePerCycle) > 0 {
		b.WriteString("\n  Maintenance / Cycle:\n")
		for _, m := range resp.MaintenancePerCycle {
			fmt.Fprintf(&b, "    %dx %s (%s)\n", m.Quantity, m.Name, m.ItemID)
		}
	}

	if resp.RecipeID != "" {
		if resp.RecipeMultiplier != 0 {
			fmt.Fprintf(&b, "\n  Recipe: %s (x%.2f)\n", resp.RecipeID, resp.RecipeMultiplier)
		} else {
			fmt.Fprintf(&b, "\n  Recipe: %s\n", resp.RecipeID)
		}
	}
	if resp.BonusType != "" {
		fmt.Fprintf(&b, "  Bonus:  %s +%g\n", resp.BonusType, resp.BonusValue)
	}
	if resp.FactionService != "" {
		fmt.Fprintf(&b, "  Faction Service:  %s\n", resp.FactionService)
	}
	if resp.PersonalService != "" {
		fmt.Fprintf(&b, "  Personal Service: %s\n", resp.PersonalService)
	}
	if resp.FactionCap > 0 {
		fmt.Fprintf(&b, "  Faction Cap:      %d\n", resp.FactionCap)
	}

	if resp.UpgradesFrom != "" {
		fmt.Fprintf(&b, "\n  Upgrades from: %s (%s)\n", resp.UpgradesFromName, resp.UpgradesFrom)
	}
	if resp.UpgradesTo != "" {
		fmt.Fprintf(&b, "  Upgrades to:   %s (%s)\n", resp.UpgradesToName, resp.UpgradesTo)
	}

	fmt.Fprintf(&b, "\n  Buildable: %t\n", resp.Buildable)
	if resp.Hint != "" {
		fmt.Fprintf(&b, "  Hint: %s\n", resp.Hint)
	}
	if resp.Lore != "" {
		fmt.Fprintf(&b, "\n  Lore: %s\n", resp.Lore)
	}
	return b.String()
}

// formatListShips renders a list_ships response as a table.
func formatListShips(raw []byte) string {
	type shipRow struct {
		ShipID         string `json:"ship_id"`
		ClassID        string `json:"class_id"`
		ClassName      string `json:"class_name"`
		IsActive       bool   `json:"is_active"`
		Location       string `json:"location"`
		LocationBaseID string `json:"location_base_id,omitempty"`
		Hull           string `json:"hull"`
		Fuel           string `json:"fuel"`
		Modules        int    `json:"modules"`
		CargoUsed      int    `json:"cargo_used,omitempty"`
		ListingID      string `json:"listing_id,omitempty"`
		ListingPrice   int64  `json:"listing_price,omitempty"`
		ListingBaseID  string `json:"listing_base_id,omitempty"`
	}
	var resp struct {
		ActiveShipID    string    `json:"active_ship_id"`
		ActiveShipClass string    `json:"active_ship_class"`
		Count           int       `json:"count"`
		Ships           []shipRow `json:"ships"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}
	if len(resp.Ships) == 0 {
		return "Ships: (none)\n"
	}

	slices.SortFunc(resp.Ships, func(a, b shipRow) int {
		if a.IsActive != b.IsActive {
			if a.IsActive {
				return -1
			}
			return 1
		}
		return strings.Compare(strings.ToLower(a.ClassName), strings.ToLower(b.ClassName))
	})

	anyListed := false
	for _, s := range resp.Ships {
		if s.ListingID != "" {
			anyListed = true
			break
		}
	}

	activeW, classW, locW, hullW, fuelW, modW, cargoW, idW :=
		1, len("Class"), len("Location"), len("Hull"), len("Fuel"), len("Mods"), len("Cargo"), len("Ship ID")
	listedW := len("Listed")
	for _, s := range resp.Ships {
		classW = max(classW, len(s.ClassName))
		locW = max(locW, len(s.Location))
		hullW = max(hullW, len(s.Hull))
		fuelW = max(fuelW, len(s.Fuel))
		modW = max(modW, len(fmt.Sprintf("%d", s.Modules)))
		cargoW = max(cargoW, len(fmt.Sprintf("%d", s.CargoUsed)))
		idW = max(idW, len(s.ShipID))
		if s.ListingID != "" {
			listedW = max(listedW, len(formatCredits(float64(s.ListingPrice))))
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Fleet: %d ship(s), active=%s\n\n", resp.Count, resp.ActiveShipClass)
	if anyListed {
		fmt.Fprintf(&b, "%-*s | %-*s | %-*s | %-*s | %-*s | %*s | %*s | %*s | %-*s\n",
			activeW, "*", classW, "Class", locW, "Location", hullW, "Hull", fuelW, "Fuel",
			modW, "Mods", cargoW, "Cargo", listedW, "Listed", idW, "Ship ID")
		b.WriteString(strings.Repeat("-", activeW+classW+locW+hullW+fuelW+modW+cargoW+listedW+idW+24) + "\n")
	} else {
		fmt.Fprintf(&b, "%-*s | %-*s | %-*s | %-*s | %-*s | %*s | %*s | %-*s\n",
			activeW, "*", classW, "Class", locW, "Location", hullW, "Hull", fuelW, "Fuel",
			modW, "Mods", cargoW, "Cargo", idW, "Ship ID")
		b.WriteString(strings.Repeat("-", activeW+classW+locW+hullW+fuelW+modW+cargoW+idW+21) + "\n")
	}

	for _, s := range resp.Ships {
		marker := " "
		if s.IsActive {
			marker = "*"
		}
		cargoStr := ""
		if s.CargoUsed > 0 {
			cargoStr = fmt.Sprintf("%d", s.CargoUsed)
		}
		if anyListed {
			listedStr := ""
			if s.ListingID != "" {
				listedStr = formatCredits(float64(s.ListingPrice))
			}
			fmt.Fprintf(&b, "%-*s | %-*s | %-*s | %-*s | %-*s | %*d | %*s | %*s | %-*s\n",
				activeW, marker, classW, s.ClassName, locW, s.Location,
				hullW, s.Hull, fuelW, s.Fuel, modW, s.Modules, cargoW, cargoStr,
				listedW, listedStr, idW, s.ShipID)
		} else {
			fmt.Fprintf(&b, "%-*s | %-*s | %-*s | %-*s | %-*s | %*d | %*s | %-*s\n",
				activeW, marker, classW, s.ClassName, locW, s.Location,
				hullW, s.Hull, fuelW, s.Fuel, modW, s.Modules, cargoW, cargoStr, idW, s.ShipID)
		}
	}

	return b.String()
}

// formatNotes renders a get_notes response as a sorted table.
func formatNotes(raw []byte) string {
	var resp struct {
		Notes []struct {
			NoteID        string `json:"note_id"`
			Title         string `json:"title"`
			CreatedAt     string `json:"created_at"`
			CreatedBy     string `json:"created_by"`
			ContentLength int    `json:"content_length"`
			Value         int    `json:"value"`
		} `json:"notes"`
		TotalCount int `json:"total_count"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}
	if len(resp.Notes) == 0 {
		return "Notes: (none)\n"
	}
	slices.SortFunc(resp.Notes, func(a, b struct {
		NoteID        string `json:"note_id"`
		Title         string `json:"title"`
		CreatedAt     string `json:"created_at"`
		CreatedBy     string `json:"created_by"`
		ContentLength int    `json:"content_length"`
		Value         int    `json:"value"`
	}) int {
		return strings.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title))
	})
	// Note IDs are 32-char hex; render full so users can copy-paste into
	// `read_note <id>`. The server rejects a truncated prefix.
	titleW, idW, byW := len("Title"), len("Note ID"), len("Author")
	lenW, valW := len("Length"), len("Value")
	for _, n := range resp.Notes {
		titleW = max(titleW, len(n.Title))
		idW = max(idW, len(n.NoteID))
		byW = max(byW, len(n.CreatedBy))
		lenW = max(lenW, len(strconv.Itoa(n.ContentLength)))
		valW = max(valW, len(strconv.Itoa(n.Value)))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Notes: %d\n\n", resp.TotalCount)
	fmt.Fprintf(&b, "  %-*s | %-*s | %-*s | %*s | %*s | %s\n",
		titleW, "Title", idW, "Note ID", byW, "Author", lenW, "Length", valW, "Value", "Created")
	fmt.Fprintf(&b, "  %s-+-%s-+-%s-+-%s-+-%s-+-%s\n",
		strings.Repeat("-", titleW), strings.Repeat("-", idW),
		strings.Repeat("-", byW), strings.Repeat("-", lenW),
		strings.Repeat("-", valW), strings.Repeat("-", 19))
	for _, n := range resp.Notes {
		fmt.Fprintf(&b, "  %-*s | %-*s | %-*s | %*d | %*d | %s\n",
			titleW, n.Title, idW, n.NoteID, byW, n.CreatedBy,
			lenW, n.ContentLength, valW, n.Value, n.CreatedAt)
	}
	return b.String()
}

// storageItem is a parsed item from a view_storage response.
type storageItem struct {
	ItemID   string  `json:"item_id"`
	Name     string  `json:"name"`
	Quantity float64 `json:"quantity"`
	Size     int     `json:"size"`
}

// storageShip is a parsed ship from a view_storage response.
type storageShip struct {
	ShipID     string `json:"ship_id"`
	ClassID    string `json:"class_id"`
	ClassName  string `json:"class_name,omitempty"`
	CustomName string `json:"custom_name,omitempty"`
	CargoUsed  int    `json:"cargo_used"`
	Modules    int    `json:"modules"`
}

// displayName returns the ship's player-assigned custom name when set,
// otherwise its human-readable class name. Used for the SHIPS "Ship Name"
// column so a renamed ship shows its custom name rather than its class.
func (s storageShip) displayName() string {
	if s.CustomName != "" {
		return s.CustomName
	}
	return s.ClassName
}

// storageFmtOptions controls optional formatting behaviour for view_storage.
// It is set by the dispatch case immediately before the response is formatted
// and reset afterwards. Foreground and scheduled commands are serialized by
// execMu, so a plain package var needs no further synchronization.
type storageFmtOptions struct {
	group  bool   // group items by catalog category instead of a flat table
	filter string // case-insensitive substring matched against item_id or name
}

var storageFmtOpts storageFmtOptions

// formatStorage formats a view_storage response as sorted tables. With
// storageFmtOpts.filter set, items are limited to those whose item_id or name
// contains the substring (case-insensitive); with storageFmtOpts.group set,
// items are grouped into per-category sections (catalog-derived).
func formatStorage(raw []byte) string {
	var resp struct {
		BaseID string        `json:"base_id"`
		Items  []storageItem `json:"items"`
		Ships  []storageShip `json:"ships"`
		Hint   string        `json:"hint"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}

	// Guard against non-storage frames (e.g. an error like "You must be docked
	// to view storage" routed here, or a stale/empty slot). A real view_storage
	// response always carries a base_id; without one and with no items or ships
	// there is nothing to render — return "" so the caller falls through rather
	// than printing "Storage at  — 0 types".
	if resp.BaseID == "" && len(resp.Items) == 0 && len(resp.Ships) == 0 {
		return ""
	}

	opts := storageFmtOpts

	// Optional case-insensitive substring filter on item_id or name.
	totalTypes := len(resp.Items)
	if opts.filter != "" {
		needle := strings.ToLower(opts.filter)
		kept := make([]storageItem, 0, len(resp.Items))
		for _, item := range resp.Items {
			if strings.Contains(strings.ToLower(item.ItemID), needle) ||
				strings.Contains(strings.ToLower(item.Name), needle) {
				kept = append(kept, item)
			}
		}
		resp.Items = kept
	}

	var totalUnits float64
	var totalVolume float64
	for _, item := range resp.Items {
		totalUnits += item.Quantity
		totalVolume += item.Quantity * float64(item.Size)
	}

	var b strings.Builder
	if opts.filter != "" {
		fmt.Fprintf(&b, "Storage at %s — %d/%d types match %q, %s units, %s volume\n",
			resp.BaseID, len(resp.Items), totalTypes, opts.filter,
			formatFloat(totalUnits), formatFloat(totalVolume))
	} else {
		fmt.Fprintf(&b, "Storage at %s — %d types, %s units, %s volume\n",
			resp.BaseID, len(resp.Items), formatFloat(totalUnits), formatFloat(totalVolume))
	}

	// Items table
	switch {
	case len(resp.Items) == 0:
		if opts.filter != "" {
			b.WriteString("  (no items match filter)\n")
		} else {
			b.WriteString("  (no items)\n")
		}
	case opts.group:
		writeStorageGrouped(&b, resp.Items)
	default:
		writeStorageFlat(&b, resp.Items)
	}

	// Ships table
	if len(resp.Ships) > 0 {
		b.WriteString("\n  SHIPS\n")

		slices.SortFunc(resp.Ships, func(a, b storageShip) int {
			return strings.Compare(a.ShipID, b.ShipID)
		})

		idW, nameW, classW, cargoW, modW := len("ID"), len("Ship Name"), len("Class"), len("Cargo Used"), len("Modules")
		for _, ship := range resp.Ships {
			idW = max(idW, len(ship.ShipID))
			nameW = max(nameW, len(ship.displayName()))
			classW = max(classW, len(ship.ClassID))
			cargoW = max(cargoW, len(strconv.Itoa(ship.CargoUsed)))
			modW = max(modW, len(strconv.Itoa(ship.Modules)))
		}

		fmt.Fprintf(&b, "  %-*s | %-*s | %-*s | %*s | %*s\n",
			idW, "ID", nameW, "Ship Name", classW, "Class", cargoW, "Cargo Used", modW, "Modules")
		fmt.Fprintf(&b, "  %s-+-%s-+-%s-+-%s-+-%s\n",
			strings.Repeat("-", idW), strings.Repeat("-", nameW),
			strings.Repeat("-", classW), strings.Repeat("-", cargoW),
			strings.Repeat("-", modW))

		for _, ship := range resp.Ships {
			fmt.Fprintf(&b, "  %-*s | %-*s | %-*s | %*d | %*d\n",
				idW, ship.ShipID, nameW, ship.displayName(),
				classW, ship.ClassID, cargoW, ship.CargoUsed,
				modW, ship.Modules)
		}
		fmt.Fprintf(&b, "  (%d ships)\n", len(resp.Ships))
	}

	// Server hint: where else the agent has items stored (e.g. "6,632 items in
	// storage at frontier_station"). Surface it so the agent knows to check
	// other stations.
	if resp.Hint != "" {
		fmt.Fprintf(&b, "  ↪ %s\n", resp.Hint)
	}

	return b.String()
}

// storageColWidths computes the column widths for the storage items table over
// the given items, so grouped sections share one consistent layout.
func storageColWidths(items []storageItem) (nameW, qtyW, sizeW int) {
	nameW, qtyW, sizeW = len("Name (id)"), len("Qty"), len("Unit Size")
	for _, item := range items {
		nameW = max(nameW, len(item.Name)+len(item.ItemID)+3)
		qtyW = max(qtyW, len(formatFloat(item.Quantity)))
		sizeW = max(sizeW, len(strconv.Itoa(item.Size)))
	}
	return nameW, qtyW, sizeW
}

// writeStorageHeader writes the items table header + separator at the given
// column widths.
func writeStorageHeader(b *strings.Builder, nameW, qtyW, sizeW int) {
	fmt.Fprintf(b, "  %-*s | %*s | %*s\n", nameW, "Name (id)", qtyW, "Qty", sizeW, "Unit Size")
	fmt.Fprintf(b, "  %s-+-%s-+-%s\n",
		strings.Repeat("-", nameW), strings.Repeat("-", qtyW), strings.Repeat("-", sizeW))
}

// writeStorageRows writes one row per item at the given column widths.
func writeStorageRows(b *strings.Builder, items []storageItem, nameW, qtyW, sizeW int) {
	for _, item := range items {
		fmt.Fprintf(b, "  %-*s | %*s | %*d\n",
			nameW, fmt.Sprintf("%s (%s)", item.Name, item.ItemID),
			qtyW, formatFloat(item.Quantity), sizeW, item.Size)
	}
}

// writeStorageFlat renders items as a single id-sorted table.
func writeStorageFlat(b *strings.Builder, items []storageItem) {
	slices.SortFunc(items, func(a, c storageItem) int {
		return strings.Compare(a.ItemID, c.ItemID)
	})
	nameW, qtyW, sizeW := storageColWidths(items)
	writeStorageHeader(b, nameW, qtyW, sizeW)
	writeStorageRows(b, items, nameW, qtyW, sizeW)
	fmt.Fprintf(b, "  (%d items)\n", len(items))
}

// writeStorageGrouped renders items in per-category sections (catalog-derived),
// categories sorted alphabetically with "unknown" last. Column widths are
// uniform across sections so the tables line up.
func writeStorageGrouped(b *strings.Builder, items []storageItem) {
	groups := make(map[string][]storageItem)
	order := make([]string, 0)
	for _, item := range items {
		cat := itemCategory(item.ItemID)
		if _, ok := groups[cat]; !ok {
			order = append(order, cat)
		}
		groups[cat] = append(groups[cat], item)
	}

	slices.SortFunc(order, func(a, c string) int {
		if a == "unknown" && c != "unknown" {
			return 1
		}
		if a != "unknown" && c == "unknown" {
			return -1
		}
		return strings.Compare(a, c)
	})

	nameW, qtyW, sizeW := storageColWidths(items)
	for idx, cat := range order {
		if idx > 0 {
			b.WriteString("\n")
		}
		catItems := groups[cat]
		slices.SortFunc(catItems, func(a, c storageItem) int {
			return strings.Compare(a.ItemID, c.ItemID)
		})
		var catUnits float64
		for _, it := range catItems {
			catUnits += it.Quantity
		}
		fmt.Fprintf(b, "  %s (%d types, %s units)\n", cat, len(catItems), formatFloat(catUnits))
		writeStorageHeader(b, nameW, qtyW, sizeW)
		writeStorageRows(b, catItems, nameW, qtyW, sizeW)
	}
	fmt.Fprintf(b, "  (%d items in %d categories)\n", len(items), len(order))
}

// formatCommas formats an integer with thousands separators.
func formatCommas(n int) string {
	s := strconv.Itoa(n)
	neg := false
	if strings.HasPrefix(s, "-") {
		neg = true
		s = s[1:]
	}
	if len(s) <= 3 {
		if neg {
			return "-" + s
		}
		return s
	}
	first := len(s) % 3
	if first == 0 {
		first = 3
	}
	var b strings.Builder
	if neg {
		b.WriteByte('-')
	}
	b.WriteString(s[:first])
	for i := first; i < len(s); i += 3 {
		b.WriteByte(',')
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

// factionStorageActivity is one row from view_faction_storage's recent_activity.
type factionStorageActivity struct {
	Player    string `json:"player"`
	Action    string `json:"action"`
	Item      string `json:"item,omitempty"`
	Quantity  int    `json:"quantity,omitempty"`
	Credits   int    `json:"credits,omitempty"`
	Timestamp string `json:"timestamp"`
}

// formatFactionStorage formats a view_faction_storage response as sorted tables.
func formatFactionStorage(raw []byte) string {
	var resp struct {
		FactionID      string                   `json:"faction_id"`
		FactionName    string                   `json:"faction_name"`
		FactionTag     string                   `json:"faction_tag"`
		BaseID         string                   `json:"base_id"`
		Credits        int                      `json:"credits"`
		Items          []storageItem            `json:"items"`
		RecentActivity []factionStorageActivity `json:"recent_activity"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}

	// Guard against non-storage frames (e.g. an error routed here, or a stale/
	// empty slot). A real view_faction_storage response identifies the faction
	// and base; without those and with no items or activity there is nothing to
	// render — return "" so the caller falls through.
	if resp.FactionID == "" && resp.BaseID == "" && len(resp.Items) == 0 && len(resp.RecentActivity) == 0 {
		return ""
	}

	opts := storageFmtOpts

	// Optional case-insensitive substring filter on item_id or name.
	totalTypes := len(resp.Items)
	if opts.filter != "" {
		needle := strings.ToLower(opts.filter)
		kept := make([]storageItem, 0, len(resp.Items))
		for _, item := range resp.Items {
			if strings.Contains(strings.ToLower(item.ItemID), needle) ||
				strings.Contains(strings.ToLower(item.Name), needle) {
				kept = append(kept, item)
			}
		}
		resp.Items = kept
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Faction Storage: %s [%s] at %s\n", resp.FactionName, resp.FactionTag, resp.BaseID)
	fmt.Fprintf(&b, "  Credits: %s\n", formatCommas(resp.Credits))
	if opts.filter != "" {
		fmt.Fprintf(&b, "  Filter: %q — %d/%d types match\n", opts.filter, len(resp.Items), totalTypes)
	}

	// Items table
	switch {
	case len(resp.Items) == 0:
		if opts.filter != "" {
			b.WriteString("  (no items match filter)\n")
		} else {
			b.WriteString("  (no items)\n")
		}
	case opts.group:
		writeFactionStorageGrouped(&b, resp.Items)
	default:
		writeFactionStorageFlat(&b, resp.Items)
	}

	// Recent activity — sorted newest-first, capped at 10 rows.
	if len(resp.RecentActivity) > 0 {
		b.WriteString("\n  RECENT ACTIVITY\n")

		activity := slices.Clone(resp.RecentActivity)
		slices.SortFunc(activity, func(a, b factionStorageActivity) int {
			return strings.Compare(b.Timestamp, a.Timestamp)
		})
		const maxRows = 10
		extra := 0
		if len(activity) > maxRows {
			extra = len(activity) - maxRows
			activity = activity[:maxRows]
		}

		whenW, playerW, actionW, detailW := len("When"), len("Player"), len("Action"), len("Detail")
		rows := make([][4]string, 0, len(activity))
		for _, a := range activity {
			when := a.Timestamp
			if t, err := time.Parse(time.RFC3339Nano, a.Timestamp); err == nil {
				when = t.Format("2006-01-02 15:04:05")
			}
			detail := ""
			switch {
			case a.Credits > 0:
				detail = formatCommas(a.Credits) + " credits"
			case a.Item != "" && a.Quantity > 0:
				detail = fmt.Sprintf("%d × %s", a.Quantity, a.Item)
			case a.Item != "":
				detail = a.Item
			}
			whenW = max(whenW, len(when))
			playerW = max(playerW, len(a.Player))
			actionW = max(actionW, len(a.Action))
			detailW = max(detailW, len(detail))
			rows = append(rows, [4]string{when, a.Player, a.Action, detail})
		}

		fmt.Fprintf(&b, "  %-*s | %-*s | %-*s | %-*s\n",
			whenW, "When", playerW, "Player", actionW, "Action", detailW, "Detail")
		fmt.Fprintf(&b, "  %s-+-%s-+-%s-+-%s\n",
			strings.Repeat("-", whenW), strings.Repeat("-", playerW),
			strings.Repeat("-", actionW), strings.Repeat("-", detailW))

		for _, r := range rows {
			fmt.Fprintf(&b, "  %-*s | %-*s | %-*s | %-*s\n",
				whenW, r[0], playerW, r[1], actionW, r[2], detailW, r[3])
		}
		if extra > 0 {
			fmt.Fprintf(&b, "  ... %d more\n", extra)
		}
	}

	return b.String()
}

// factionStorageColWidths computes column widths for the faction-storage items
// table over the given items, so grouped sections share one layout.
func factionStorageColWidths(items []storageItem) (idW, nameW, qtyW, sizeW int) {
	idW, nameW, qtyW, sizeW = len("ID"), len("Name"), len("Qty"), len("Unit Size")
	for _, item := range items {
		idW = max(idW, len(item.ItemID))
		nameW = max(nameW, len(item.Name))
		qtyW = max(qtyW, len(formatFloat(item.Quantity)))
		sizeW = max(sizeW, len(strconv.Itoa(item.Size)))
	}
	return idW, nameW, qtyW, sizeW
}

func writeFactionStorageHeader(b *strings.Builder, idW, nameW, qtyW, sizeW int) {
	fmt.Fprintf(b, "  %-*s | %-*s | %*s | %*s\n", idW, "ID", nameW, "Name", qtyW, "Qty", sizeW, "Unit Size")
	fmt.Fprintf(b, "  %s-+-%s-+-%s-+-%s\n",
		strings.Repeat("-", idW), strings.Repeat("-", nameW),
		strings.Repeat("-", qtyW), strings.Repeat("-", sizeW))
}

func writeFactionStorageRows(b *strings.Builder, items []storageItem, idW, nameW, qtyW, sizeW int) {
	for _, item := range items {
		fmt.Fprintf(b, "  %-*s | %-*s | %*s | %*d\n",
			idW, item.ItemID, nameW, item.Name,
			qtyW, formatFloat(item.Quantity), sizeW, item.Size)
	}
}

// writeFactionStorageFlat renders items as a single id-sorted table.
func writeFactionStorageFlat(b *strings.Builder, items []storageItem) {
	slices.SortFunc(items, func(a, c storageItem) int {
		return strings.Compare(a.ItemID, c.ItemID)
	})
	idW, nameW, qtyW, sizeW := factionStorageColWidths(items)
	writeFactionStorageHeader(b, idW, nameW, qtyW, sizeW)
	writeFactionStorageRows(b, items, idW, nameW, qtyW, sizeW)
	fmt.Fprintf(b, "  (%d items)\n", len(items))
}

// writeFactionStorageGrouped renders items in per-category sections
// (catalog-derived), categories sorted alphabetically with "unknown" last and
// uniform column widths across sections.
func writeFactionStorageGrouped(b *strings.Builder, items []storageItem) {
	groups := make(map[string][]storageItem)
	order := make([]string, 0)
	for _, item := range items {
		cat := itemCategory(item.ItemID)
		if _, ok := groups[cat]; !ok {
			order = append(order, cat)
		}
		groups[cat] = append(groups[cat], item)
	}

	slices.SortFunc(order, func(a, c string) int {
		if a == "unknown" && c != "unknown" {
			return 1
		}
		if a != "unknown" && c == "unknown" {
			return -1
		}
		return strings.Compare(a, c)
	})

	idW, nameW, qtyW, sizeW := factionStorageColWidths(items)
	for idx, cat := range order {
		if idx > 0 {
			b.WriteString("\n")
		}
		catItems := groups[cat]
		slices.SortFunc(catItems, func(a, c storageItem) int {
			return strings.Compare(a.ItemID, c.ItemID)
		})
		var catUnits float64
		for _, it := range catItems {
			catUnits += it.Quantity
		}
		fmt.Fprintf(b, "  %s (%d types, %s units)\n", cat, len(catItems), formatFloat(catUnits))
		writeFactionStorageHeader(b, idW, nameW, qtyW, sizeW)
		writeFactionStorageRows(b, catItems, idW, nameW, qtyW, sizeW)
	}
	fmt.Fprintf(b, "  (%d items in %d categories)\n", len(items), len(order))
}

// marketOrder is one row from view_market's buy_orders / sell_orders array.
//
// MyQuantity is populated by the server when the order belongs to this player
// (the standalone "my open order" case). Source == "station" means the row
// is a station/NPC listing rather than a player order.
type marketOrder struct {
	PriceEach  float64 `json:"price_each"`
	Quantity   float64 `json:"quantity"`
	MyQuantity float64 `json:"my_quantity,omitempty"`
	Source     string  `json:"source,omitempty"`
}

// formattedOrder is one rendered row of the order book.
type formattedOrder struct {
	price string
	qty   string
}

// orderPrefix returns the visual marker for an order:
//
//	"✓ " — your own open order (my_quantity > 0)
//	"* " — station/NPC listing
//	""   — another player's order
//
// If a row somehow qualifies as both yours and a station entry (shouldn't
// happen in practice), "✓ " wins since it's the more actionable info.
func orderPrefix(o marketOrder) string {
	switch {
	case o.MyQuantity > 0:
		return "✓ "
	case o.Source == "station":
		return "* "
	default:
		return ""
	}
}

// formatBuyOrders formats every buy order with the appropriate prefix,
// sorted by price descending (highest bid first).
func formatBuyOrders(orders []marketOrder) []formattedOrder {
	// Sort by price descending (highest first). Buy orders are bids from
	// other players; the best one to accept is the one offering the most.
	sorted := make([]marketOrder, len(orders))
	copy(sorted, orders)
	slices.SortFunc(sorted, func(a, b marketOrder) int {
		return cmp.Compare(b.PriceEach, a.PriceEach)
	})

	result := make([]formattedOrder, 0, len(sorted))
	for _, o := range sorted {
		result = append(result, formattedOrder{
			price: orderPrefix(o) + fmt.Sprintf("%.0f", o.PriceEach),
			qty:   fmt.Sprintf("%.0f", o.Quantity),
		})
	}
	return result
}

// formatSellOrders formats every sell order with the appropriate prefix,
// sorted by price ascending (cheapest ask first).
func formatSellOrders(orders []marketOrder) []formattedOrder {
	// Sort by price ascending (lowest first). Sell orders are listings
	// from other players; the best one to buy from is the cheapest.
	sorted := make([]marketOrder, len(orders))
	copy(sorted, orders)
	slices.SortFunc(sorted, func(a, b marketOrder) int {
		return cmp.Compare(a.PriceEach, b.PriceEach)
	})

	result := make([]formattedOrder, 0, len(sorted))
	for _, o := range sorted {
		result = append(result, formattedOrder{
			price: orderPrefix(o) + fmt.Sprintf("%.0f", o.PriceEach),
			qty:   fmt.Sprintf("%.0f", o.Quantity),
		})
	}

	return result
}

// formatMarket formats a view_market response as a multi-row table grouped by category.
func formatChatHistory(raw []byte) string {
	type chatMsg struct {
		Channel        string `json:"channel"`
		Sender         string `json:"sender"`
		SenderID       string `json:"sender_id"`
		Content        string `json:"content"`
		TimestampUTC   string `json:"timestamp_utc"`
		Timestamp      string `json:"timestamp"`
		TargetID       string `json:"target_id,omitempty"`
		EmpireOfficial bool   `json:"empire_official,omitempty"`
	}
	var resp struct {
		Messages []chatMsg `json:"messages"`
		Channel  string    `json:"channel"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil || len(resp.Messages) == 0 {
		return ""
	}

	// Get current system ID for filtering system chat messages.
	currentSystemID := ""
	if globalClient != nil {
		if state := globalClient.GetState(); state != nil {
			currentSystemID = state.System.ID
		}
	}

	// Phase 1: Filter messages.
	// - System chat: only show messages targeting the current system.
	// - Old messages (before session start): keep only the most recent per sender.
	//   We do a reverse pass to find which old message per sender to keep.
	keepOldSender := make(map[string]int) // sender -> index of most recent old msg to keep
	for i := len(resp.Messages) - 1; i >= 0; i-- {
		msg := resp.Messages[i]
		msgTime, err := time.Parse(time.RFC3339Nano, msg.TimestampUTC)
		isOld := err == nil && msgTime.Before(processStartTime)
		if isOld {
			key := msg.SenderID
			if key == "" {
				key = msg.Sender
			}
			if _, seen := keepOldSender[key]; !seen {
				keepOldSender[key] = i
			}
		}
	}

	var filtered []chatMsg
	skipped := 0
	for i, msg := range resp.Messages {
		// Filter system chat by target system.
		if resp.Channel == "system" && msg.TargetID != "" && currentSystemID != "" {
			if !strings.EqualFold(msg.TargetID, currentSystemID) {
				skipped++
				continue
			}
		}

		msgTime, err := time.Parse(time.RFC3339Nano, msg.TimestampUTC)
		isOld := err == nil && msgTime.Before(processStartTime)

		if isOld {
			key := msg.SenderID
			if key == "" {
				key = msg.Sender
			}
			if keepIdx, ok := keepOldSender[key]; ok && keepIdx != i {
				skipped++
				continue
			}
		}
		filtered = append(filtered, msg)
	}

	// Phase 2: Collapse consecutive duplicate messages (same sender + content).
	type entry struct {
		sender         string
		content        string
		timestamp      string
		count          int
		empireofficial bool
	}
	var collapsed []entry
	for _, msg := range filtered {
		ts := msg.Timestamp
		if ts == "" {
			ts = msg.TimestampUTC
		}
		if len(collapsed) > 0 {
			last := &collapsed[len(collapsed)-1]
			if last.sender == msg.Sender && last.content == msg.Content {
				last.count++
				continue
			}
		}
		collapsed = append(collapsed, entry{
			sender:         msg.Sender,
			content:        msg.Content,
			timestamp:      ts,
			count:          1,
			empireofficial: msg.EmpireOfficial,
		})
	}

	var b strings.Builder
	fmt.Fprintf(&b, "\nChat history (%s) — %d messages", resp.Channel, len(resp.Messages))
	if skipped > 0 {
		fmt.Fprintf(&b, " (%d old duplicates hidden)", skipped)
	}
	b.WriteString(":\n\n")

	// Debug: known senders to dump full JSON for investigation.
	debugSenders := map[string]bool{
		"Chrisjen Avasarala": true,
		"WaterFixer":         true,
		"N Nagata":           true,
		"GunnyDraper":        true,
	}

	for _, e := range collapsed {
		repeat := ""
		if e.count > 1 {
			repeat = fmt.Sprintf(" (x%d)", e.count)
		}
		// Tag verified empire-official messages so impersonation is obvious.
		official := ""
		if e.empireofficial {
			official = " [OFFICIAL]"
		}
		fmt.Fprintf(&b, "  [%s] %s%s: %s%s\n", e.timestamp, e.sender, official, e.content, repeat)
	}

	// Dump full JSON for debug senders — show ALL messages (including skipped)
	// to understand what fields are available for filtering.
	for _, msg := range resp.Messages {
		if debugSenders[msg.Sender] {
			raw, _ := json.MarshalIndent(msg, "    ", "  ")
			fmt.Fprintf(&b, "\n  DEBUG [%s]:\n    %s\n", msg.Sender, string(raw))
			break // Just show one example per run
		}
	}

	fmt.Fprintf(&b, "\n  %d shown (%d after dedup)\n", len(filtered), len(collapsed))
	return b.String()
}

func formatMarket(raw []byte) string {
	type MarketItem struct {
		ItemID     string        `json:"item_id"`
		ItemName   string        `json:"item_name"`
		Category   string        `json:"category,omitempty"`
		BuyOrders  []marketOrder `json:"buy_orders"`
		SellOrders []marketOrder `json:"sell_orders"`
	}

	var resp struct {
		Items []MarketItem `json:"items"`
	}

	if err := json.Unmarshal(raw, &resp); err != nil {
		return fmt.Sprintf("Error parsing market data: %v", err)
	}

	if len(resp.Items) == 0 {
		return "No market data available"
	}

	var buf bytes.Buffer

	// Group items by category
	categories := make(map[string][]MarketItem)
	categoryOrder := make([]string, 0, len(resp.Items))

	for _, item := range resp.Items {
		cat := item.Category
		if cat == "" {
			cat = "Uncategorized"
		}
		if _, exists := categories[cat]; !exists {
			categoryOrder = append(categoryOrder, cat)
		}
		categories[cat] = append(categories[cat], item)
	}

	// Sort categories alphabetically, but ensure "Uncategorized" comes first
	slices.SortFunc(categoryOrder, func(a, b string) int {
		if a == "Uncategorized" && b != "Uncategorized" {
			return -1
		}
		if a != "Uncategorized" && b == "Uncategorized" {
			return 1
		}
		return cmp.Compare(a, b)
	})

	// Sort items within each category by ItemID
	for cat := range categories {
		slices.SortFunc(categories[cat], func(a, b MarketItem) int {
			return cmp.Compare(a.ItemID, b.ItemID)
		})
	}

	// Calculate max width for combined "Name (id)" column across all items
	maxNameWidth := len("Name (id)") // Header is minimum
	for _, item := range resp.Items {
		w := len(item.ItemName) + len(item.ItemID) + 3 // " (" + id + ")"
		if w > maxNameWidth {
			maxNameWidth = w
		}
	}

	// Use a single tabwriter for all sections to ensure consistent column widths
	w := tabwriter.NewWriter(&buf, 0, 0, 1, ' ', 0)

	// Print each category section
	for idx, cat := range categoryOrder {
		items := categories[cat]

		// Add blank line before category (except first)
		if idx > 0 {
			_, _ = fmt.Fprintln(w)
		}

		// Category heading - write directly with padding to match max widths
		// We need to pad the category name to align with the table structure
		fmt.Fprintf(&buf, "%s\n", cat)
		fmt.Fprintf(&buf, "%s\n", strings.Repeat("-", len(cat)))

		// Pad Name header to max width
		nameHeader := "Name (id)"
		for len(nameHeader) < maxNameWidth {
			nameHeader += " "
		}

		// Header row (numeric columns right-aligned with leading tabs)
		_, _ = fmt.Fprintf(w, "%s\t|\tBuy\t|\tQty\t|\tSell\t|\tQty\t|\n",
			nameHeader)

		// Separator row
		nameSep := strings.Repeat("-", maxNameWidth)
		_, _ = fmt.Fprintf(w, "%s\t|\t-----\t|\t---\t|\t-----\t|\t---\t|\n",
			nameSep)

		// When the response holds only a single item — the case where the
		// caller passed a specific item_id to view_market — show up to 25
		// rows of the order book instead of just the top 2.
		maxRowsPerItem := 2
		if len(resp.Items) == 1 {
			maxRowsPerItem = 25
		}

		nameBlank := strings.Repeat(" ", maxNameWidth)

		for _, item := range items {
			buys := formatBuyOrders(item.BuyOrders)
			sells := formatSellOrders(item.SellOrders)

			// Emit at least 1 row even when both books are empty so the item
			// still appears in the table with placeholder dashes.
			rows := max(len(buys), len(sells), 1)
			truncated := rows > maxRowsPerItem
			if truncated {
				rows = maxRowsPerItem
			}

			for r := 0; r < rows; r++ {
				var name string
				if r == 0 {
					name = fmt.Sprintf("%s (%s)", item.ItemName, item.ItemID)
					for len(name) < maxNameWidth {
						name += " "
					}
				} else {
					name = nameBlank
				}

				buyPrice, buyQty := "-", "-"
				if r < len(buys) {
					buyPrice = buys[r].price
					buyQty = buys[r].qty
				}
				sellPrice, sellQty := "-", "-"
				if r < len(sells) {
					sellPrice = sells[r].price
					sellQty = sells[r].qty
				}

				_, _ = fmt.Fprintf(w, "%s\t|\t%s\t|\t%s\t|\t%s\t|\t%s\t|\n",
					name, buyPrice, buyQty, sellPrice, sellQty)
			}

			if truncated {
				extraBuys := max(len(buys)-maxRowsPerItem, 0)
				extraSells := max(len(sells)-maxRowsPerItem, 0)
				buyMore := ""
				if extraBuys > 0 {
					buyMore = fmt.Sprintf("... %d more", extraBuys)
				}
				sellMore := ""
				if extraSells > 0 {
					sellMore = fmt.Sprintf("... %d more", extraSells)
				}
				// Single trailer row: leave the side with no extras blank
				// rather than printing "0 more".
				_, _ = fmt.Fprintf(w, "%s\t|\t%s\t|\t\t|\t%s\t|\t\t|\n",
					nameBlank, buyMore, sellMore)
			}
		}
	}

	_ = w.Flush()
	return buf.String()
}

// formatCargo formats a get_cargo response as a sorted table.
func formatCargo(raw []byte) string {
	var resp struct {
		Cargo     []storageItem `json:"cargo"`
		Used      int           `json:"used"`
		Capacity  int           `json:"capacity"`
		Available int           `json:"available"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Cargo (%d/%d used, %d available)\n", resp.Used, resp.Capacity, resp.Available)

	if len(resp.Cargo) == 0 {
		b.WriteString("  (empty)\n")
		return b.String()
	}

	slices.SortFunc(resp.Cargo, func(a, b storageItem) int {
		return strings.Compare(a.ItemID, b.ItemID)
	})

	// Per-row total space used = quantity × unit size. Helps the operator
	// see at a glance which items dominate the hold (e.g. one ore stack
	// can outweigh dozens of small consumables).
	rowSpace := func(item storageItem) int {
		return int(item.Quantity) * item.Size
	}

	idW, nameW, qtyW, sizeW, totW := len("ID"), len("Name"), len("Qty"), len("Unit Size"), len("Used")
	for _, item := range resp.Cargo {
		idW = max(idW, len(item.ItemID))
		nameW = max(nameW, len(item.Name))
		qtyW = max(qtyW, len(formatFloat(item.Quantity)))
		sizeW = max(sizeW, len(strconv.Itoa(item.Size)))
		totW = max(totW, len(strconv.Itoa(rowSpace(item))))
	}

	fmt.Fprintf(&b, "  %-*s | %-*s | %*s | %*s | %*s\n",
		idW, "ID", nameW, "Name", qtyW, "Qty", sizeW, "Unit Size", totW, "Used")
	fmt.Fprintf(&b, "  %s-+-%s-+-%s-+-%s-+-%s\n",
		strings.Repeat("-", idW), strings.Repeat("-", nameW),
		strings.Repeat("-", qtyW), strings.Repeat("-", sizeW),
		strings.Repeat("-", totW))

	for _, item := range resp.Cargo {
		fmt.Fprintf(&b, "  %-*s | %-*s | %*s | %*d | %*d\n",
			idW, item.ItemID, nameW, item.Name,
			qtyW, formatFloat(item.Quantity), sizeW, item.Size,
			totW, rowSpace(item))
	}
	fmt.Fprintf(&b, "  (%d items)\n", len(resp.Cargo))
	return b.String()
}

// shipListing is a parsed listing from a browse_ships response.
type shipListing struct {
	ShipName  string  `json:"ship_name"`
	ClassID   string  `json:"class_id"`
	Category  string  `json:"category"`
	Tier      int     `json:"tier"`
	Price     float64 `json:"price"`
	Seller    string  `json:"seller"`
	ListingID string  `json:"listing_id"`
}

// Survey scanner cache - only check modules when ship might change.
var (
	surveyScannerCached bool
	hasSurveyScanner    bool
)

// formatBrowseShips formats a browse_ships response as a table.
func formatBrowseShips(raw []byte) string {
	var resp struct {
		BaseName string        `json:"base_name"`
		Count    int           `json:"count"`
		Listings []shipListing `json:"listings"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}

	var b strings.Builder
	if len(resp.Listings) == 0 {
		fmt.Fprintf(&b, "Station:  %q\n\nListings:\n", resp.BaseName)
		b.WriteString("  (no ships for sale)\n")
		return b.String()
	}
	fmt.Fprintf(&b, "Station:  %q\n\nListings (%d):\n", resp.BaseName, len(resp.Listings))

	slices.SortFunc(resp.Listings, func(a, c shipListing) int {
		if a.Price != c.Price {
			if a.Price < c.Price {
				return -1
			}
			return 1
		}
		return strings.Compare(strings.ToLower(a.ShipName), strings.ToLower(c.ShipName))
	})

	// Every column comes straight from the browse_ships listing — no
	// catalog round-trip needed. Tier is in the listing payload; the
	// class_id is the human-readable hull identifier.
	type row struct {
		shipListing
		tierStr string
	}
	rows := make([]row, len(resp.Listings))
	for i, l := range resp.Listings {
		r := row{shipListing: l}
		if l.Tier > 0 {
			r.tierStr = fmt.Sprintf("T%d", l.Tier)
		}
		rows[i] = r
	}

	shipW, catW, classW, tierW := len("Ship"), len("Category"), len("Class"), len("Tier")
	priceW, sellerW, idW := len("Price"), len("Seller"), len("Listing ID")
	for _, r := range rows {
		shipW = max(shipW, len(r.ShipName))
		catW = max(catW, len(r.Category))
		classW = max(classW, len(r.ClassID))
		tierW = max(tierW, len(r.tierStr))
		priceW = max(priceW, len(formatCredits(r.Price)))
		sellerW = max(sellerW, len(r.Seller))
		idW = max(idW, len(r.ListingID))
	}

	fmt.Fprintf(&b, "%-*s | %-*s | %-*s | %-*s | %*s | %-*s | %-*s\n",
		shipW, "Ship", catW, "Category", classW, "Class", tierW, "Tier",
		priceW, "Price", sellerW, "Seller", idW, "Listing ID")
	b.WriteString(strings.Repeat("-", shipW+catW+classW+tierW+priceW+sellerW+idW+18) + "\n")

	for _, r := range rows {
		fmt.Fprintf(&b, "%-*s | %-*s | %-*s | %-*s | %*s | %-*s | %-*s\n",
			shipW, r.ShipName, catW, r.Category, classW, r.ClassID, tierW, r.tierStr,
			priceW, formatCredits(r.Price), sellerW, r.Seller, idW, r.ListingID)
	}

	return b.String()
}

// nearbyPlayer is a parsed player from a get_nearby response.
type nearbyPlayer struct {
	Username       string `json:"username"`
	FactionTag     string `json:"faction_tag,omitempty"`
	ShipClass      string `json:"ship_class"`
	InCombat       bool   `json:"in_combat"`
	PrimaryColor   string `json:"primary_color,omitempty"`
	SecondaryColor string `json:"secondary_color,omitempty"`
}

// nearbyPirate is a parsed pirate from a get_nearby response.
type nearbyPirate struct {
	Name string `json:"name"`
	Tier string `json:"tier,omitempty"`
}

// formatNearby formats a get_nearby response as a table.
func formatNearby(raw []byte) string {
	var resp struct {
		POIID       string         `json:"poi_id"`
		Count       int            `json:"count"`
		PirateCount int            `json:"pirate_count"`
		Nearby      []nearbyPlayer `json:"nearby"`
		Pirates     []nearbyPirate `json:"pirates"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "POI ID:  %s\n", resp.POIID)
	fmt.Fprintf(&b, "Count: %d\n\n", resp.Count)

	writePlayerTable(&b, resp.Nearby)

	fmt.Fprintf(&b, "\nPirates:  %d\n", resp.PirateCount)
	for _, p := range resp.Pirates {
		tier := p.Tier
		if tier == "" {
			tier = "normal"
		}
		fmt.Fprintf(&b, "  %s (%s)\n", p.Name, tier)
	}

	return b.String()
}

// formatTravel formats a travel response with online players at the destination.
func formatTravel(raw []byte) string {
	raw = unwrapActionResult(raw)
	var resp struct {
		POI           string         `json:"poi"`
		POIID         string         `json:"poi_id"`
		ArrivalTick   int64          `json:"arrival_tick"`
		OnlinePlayers []nearbyPlayer `json:"online_players"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}

	var b strings.Builder
	poi := resp.POI
	if poi == "" {
		poi = resp.POIID
	}
	fmt.Fprintf(&b, "Arrived at %q\n\n", poi)

	writePlayerTable(&b, resp.OnlinePlayers)

	return b.String()
}

// formatError returns a friendly error message in styled mode, or the raw error otherwise.
func formatError(err error, command string, format outputFormat) string {
	// If the transport is currently disconnected, the underlying error is
	// almost always "send failed" / "not connected" / request-timeout noise
	// that tells the user nothing useful. Replace it with a hint that the
	// reconnect loop is already running.
	if globalClient != nil && !globalClient.IsConnected() {
		return "⟳ reconnecting, retry in a moment"
	}
	if format == formatStyled {
		return respfmt.Error(err, command)
	}
	return "Error: " + err.Error()
}

// formatMine formats a mining_yield (or legacy mine action_result) payload
// as a one-line summary plus per-skill XP. Skips rendering when only the
// pending-ack shape is present so callers don't see "Mined 0 ( remaining )"
// before the yield event lands.
func formatMine(raw []byte) string {
	raw = unwrapActionResult(raw)
	var resp struct {
		Quantity         float64        `json:"quantity"`
		ResourceID       string         `json:"resource_id"`
		ResourceName     string         `json:"resource_name"`
		RemainingDisplay string         `json:"remaining_display"`
		XPGained         map[string]int `json:"xp_gained,omitempty"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}
	if resp.Quantity == 0 && resp.ResourceID == "" && resp.ResourceName == "" {
		return ""
	}
	name := resp.ResourceName
	if name == "" {
		name = resp.ResourceID
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Mined %s %s ( %s remaining )", formatFloat(resp.Quantity), name, resp.RemainingDisplay)
	if len(resp.XPGained) > 0 {
		skills := make([]string, 0, len(resp.XPGained))
		for s := range resp.XPGained {
			skills = append(skills, s)
		}
		slices.Sort(skills)
		b.WriteString("\n")
		for _, s := range skills {
			fmt.Fprintf(&b, " +%d xp %s\n", resp.XPGained[s], s)
		}
	}
	return b.String()
}

// formatCraft renders the v0.389 craft/recycle job responses. The server returns
// one of four shapes (single queued job, queue listing, bulk results, dry-run
// quote); we probe distinguishing keys and render the matching one.
func formatCraft(raw []byte) string {
	raw = unwrapActionResult(raw)
	var probe struct {
		Action  string          `json:"action"`
		DryRun  bool            `json:"dry_run"`
		Jobs    json.RawMessage `json:"jobs"`
		Results json.RawMessage `json:"results"`
		JobID   string          `json:"job_id"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return ""
	}
	switch {
	case probe.DryRun:
		return formatCraftDryRun(raw)
	case len(probe.Results) > 0:
		return formatCraftBulk(raw)
	case len(probe.Jobs) > 0:
		return formatCraftQueue(raw)
	case probe.JobID != "":
		return formatCraftJobQueued(raw)
	}
	return ""
}

func formatCraftJobQueued(raw []byte) string {
	var r struct {
		JobID               string  `json:"job_id"`
		Recipe              string  `json:"recipe"`
		Mode                string  `json:"mode"`
		Venue               string  `json:"venue"`
		Runs                int     `json:"runs"`
		EffectiveTimePerRun float64 `json:"effective_time_per_run"`
		EstCompletionTick   int     `json:"est_completion_tick"`
		Message             string  `json:"message"`
		Escrowed            struct {
			Fee    int `json:"fee"`
			Labor  int `json:"labor"`
			Inputs []struct {
				Name     string `json:"name"`
				ItemID   string `json:"item_id"`
				Quantity int    `json:"quantity"`
			} `json:"inputs"`
		} `json:"escrowed"`
		Produces []struct {
			Name     string `json:"name"`
			Quantity int    `json:"quantity"`
		} `json:"produces"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "🛠  Queued %s — job %s @ %s (%d runs, ~%.0fs/run, ETA tick %d)\n",
		r.Recipe, r.JobID, r.Venue, r.Runs, r.EffectiveTimePerRun, r.EstCompletionTick)
	for _, p := range r.Produces {
		fmt.Fprintf(&b, "  → produces %s x %d\n", p.Name, p.Quantity)
	}
	if len(r.Escrowed.Inputs) > 0 {
		b.WriteString("  Escrowed inputs:\n")
		for _, in := range r.Escrowed.Inputs {
			fmt.Fprintf(&b, "    %d x %s\n", in.Quantity, in.Name)
		}
	}
	if r.Escrowed.Fee > 0 || r.Escrowed.Labor > 0 {
		fmt.Fprintf(&b, "  Escrowed credits: labor %d + fee %d\n", r.Escrowed.Labor, r.Escrowed.Fee)
	}
	if r.Message != "" {
		fmt.Fprintf(&b, "  %s\n", r.Message)
	}
	return b.String()
}

// craftStorageLabel renders a crafting_update job's storage destination in
// human terms. The server reports "station" for the station's own storage and
// "faction" for faction storage; anything else is shown verbatim.
func craftStorageLabel(s string) string {
	switch s {
	case "station", "":
		return "storage"
	case "faction":
		return "faction storage"
	default:
		return s
	}
}

// craftingUpdateLines renders a crafting_update push event into one progress
// line per job, e.g. "Crafted 4 copper_piping to storage. 22 runs remaining".
// Returns nil when there is nothing to report.
func craftingUpdateLines(ev serverapi.CraftingUpdateEvent) []string {
	var lines []string
	for _, j := range ev.Jobs {
		var b strings.Builder
		if len(j.Deposited) > 0 {
			deps := make([]string, 0, len(j.Deposited))
			for _, d := range j.Deposited {
				deps = append(deps, fmt.Sprintf("%d %s", d.Quantity, d.ItemID))
			}
			fmt.Fprintf(&b, "Crafted %s to %s.", strings.Join(deps, ", "), craftStorageLabel(j.Storage))
		} else if j.Recipe != "" {
			fmt.Fprintf(&b, "%s:", j.Recipe)
		}
		if b.Len() > 0 {
			b.WriteString(" ")
		}
		if j.Completed {
			b.WriteString("Complete")
		} else {
			fmt.Fprintf(&b, "%d runs remaining", j.RunsRemaining)
		}
		if s := b.String(); s != "" {
			lines = append(lines, s)
		}
	}
	return lines
}

// craftFacilityHeader renders a friendly label for a craft-queue facility.
// Station workshops carry a structured "<type>:<owner>:<station>" id whose
// middle segment is the owner's player id and trailing segment is the station,
// so we surface the station (which the generic "Station Workshop" venue does
// not tell you) and drop the noisy id. Faction facilities carry an opaque hash,
// which we truncate so it still disambiguates without dominating the line.
func craftFacilityHeader(facilityID, venue string) string {
	if venue == "" {
		venue = "Facility"
	}
	if parts := strings.Split(facilityID, ":"); len(parts) >= 3 {
		return fmt.Sprintf("%s @ %s", venue, parts[len(parts)-1])
	}
	if facilityID == "" {
		return venue
	}
	short := facilityID
	if len(short) > 8 {
		short = short[:8] + "…"
	}
	return fmt.Sprintf("%s [%s]", venue, short)
}

func formatCraftQueue(raw []byte) string {
	type craftJob struct {
		JobID         string  `json:"job_id"`
		FacilityID    string  `json:"facility_id"`
		Venue         string  `json:"venue"`
		Recipe        string  `json:"recipe"`
		RunsDone      int     `json:"runs_done"`
		RunsRemaining int     `json:"runs_remaining"`
		RunsTotal     int     `json:"runs_total"`
		Progress      float64 `json:"progress"`
		ETATicks      int     `json:"eta_ticks"`
		Position      int     `json:"position"`
		Status        string  `json:"status"`
		Produces      []struct {
			Name     string `json:"name"`
			Quantity int    `json:"quantity"`
		} `json:"produces"`
	}
	var r struct {
		Jobs []craftJob `json:"jobs"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return ""
	}
	var b strings.Builder
	if len(r.Jobs) == 0 {
		return "🛠  Crafting queue: (empty)\n"
	}

	// Jobs run in independent per-facility queues, so each facility has its own
	// position counter (you see #0 more than once across the flat list). Group
	// by facility_id, preserving first-seen order, so the queues read clearly.
	order := make([]string, 0)
	groups := make(map[string][]craftJob)
	for _, j := range r.Jobs {
		if _, seen := groups[j.FacilityID]; !seen {
			order = append(order, j.FacilityID)
		}
		groups[j.FacilityID] = append(groups[j.FacilityID], j)
	}

	jobWord := "jobs"
	if len(r.Jobs) == 1 {
		jobWord = "job"
	}
	if len(order) > 1 {
		fmt.Fprintf(&b, "🛠  Crafting queue (%d %s across %d facilities):\n", len(r.Jobs), jobWord, len(order))
	} else {
		fmt.Fprintf(&b, "🛠  Crafting queue (%d %s):\n", len(r.Jobs), jobWord)
	}

	for _, fid := range order {
		jobs := groups[fid]
		// A facility header is only meaningful when the server tells us which
		// facility a queue belongs to; older/sparser payloads omit it.
		if fid != "" || jobs[0].Venue != "" {
			facWord := "jobs"
			if len(jobs) == 1 {
				facWord = "job"
			}
			fmt.Fprintf(&b, "  %s — %d %s\n", craftFacilityHeader(fid, jobs[0].Venue), len(jobs), facWord)
		}
		for _, j := range jobs {
			produces := ""
			if len(j.Produces) > 0 {
				parts := make([]string, 0, len(j.Produces))
				for _, p := range j.Produces {
					parts = append(parts, fmt.Sprintf("%dx %s", p.Quantity, p.Name))
				}
				produces = " → " + strings.Join(parts, ", ")
			}
			// progress is the fraction of the CURRENT run, not the whole job, so
			// label it distinctly from the runs-done/total count to avoid reading
			// "0/5 runs (76%)" as 76% of the job.
			fmt.Fprintf(&b, "    #%d %s%s [%s] %d/%d runs done, current run %.0f%% · ETA %d ticks · %s\n",
				j.Position, j.Recipe, produces, j.JobID, j.RunsDone, j.RunsTotal, j.Progress*100, j.ETATicks, j.Status)
		}
	}
	return b.String()
}

func formatCraftBulk(raw []byte) string {
	var r struct {
		Results []struct {
			Index     int    `json:"index"`
			Success   bool   `json:"success"`
			JobID     string `json:"job_id"`
			Recipe    string `json:"recipe"`
			Runs      int    `json:"runs"`
			Error     string `json:"error"`
			ErrorCode string `json:"error_code"`
		} `json:"results"`
		Summary struct {
			Total     int `json:"total"`
			Succeeded int `json:"succeeded"`
			Failed    int `json:"failed"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "🛠  Bulk craft: %d total, %d ok, %d failed\n",
		r.Summary.Total, r.Summary.Succeeded, r.Summary.Failed)
	for _, res := range r.Results {
		if res.Success {
			fmt.Fprintf(&b, "  ✅ [%d] %s job %s (%d runs)\n", res.Index, res.Recipe, res.JobID, res.Runs)
		} else {
			fmt.Fprintf(&b, "  ❌ [%d] %s — %s (%s)\n", res.Index, res.Recipe, res.Error, res.ErrorCode)
		}
	}
	return b.String()
}

func formatCraftDryRun(raw []byte) string {
	var r struct {
		Recipe              string  `json:"recipe"`
		Quantity            int     `json:"quantity"`
		Runs                int     `json:"runs"`
		Venue               string  `json:"venue"`
		CreditsTotal        int     `json:"credits_total"`
		HaveInputs          bool    `json:"have_inputs"`
		HaveCredits         bool    `json:"have_credits"`
		EffectiveTimePerRun float64 `json:"effective_time_per_run"`
		EstCompletionTick   int     `json:"est_completion_tick"`
		Message             string  `json:"message"`
		Cost                struct {
			Fee    int `json:"fee"`
			Labor  int `json:"labor"`
			Inputs []struct {
				Name     string `json:"name"`
				Quantity int    `json:"quantity"`
			} `json:"inputs"`
		} `json:"cost"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "📋 Dry run: %s x%d → %d runs @ %s (~%.0fs/run, ETA tick %d)\n",
		r.Recipe, r.Quantity, r.Runs, r.Venue, r.EffectiveTimePerRun, r.EstCompletionTick)
	if len(r.Cost.Inputs) > 0 {
		b.WriteString("  Inputs needed:\n")
		for _, in := range r.Cost.Inputs {
			fmt.Fprintf(&b, "    %d x %s\n", in.Quantity, in.Name)
		}
	}
	fmt.Fprintf(&b, "  Credits: %d (labor %d + fee %d)\n", r.CreditsTotal, r.Cost.Labor, r.Cost.Fee)
	okMark := func(ok bool) string {
		if ok {
			return "✅"
		}
		return "❌"
	}
	fmt.Fprintf(&b, "  Have inputs: %s   Have credits: %s\n", okMark(r.HaveInputs), okMark(r.HaveCredits))
	if r.Message != "" {
		fmt.Fprintf(&b, "  %s\n", r.Message)
	}
	return b.String()
}

// missionsShowFull, when true, suppresses the 200-char description
// truncation in formatMissions. Set by the dispatcher when the user
// passes --full to the missions/get_missions command, cleared on return.
var missionsShowFull bool

// formatMissions formats a get_missions response grouped by type.
// formatViewOrders renders a view_orders response as a table of market orders,
// preceded by a header summarizing the base/scope/paging and followed by the
// server's hint. Order rows are shown in server order (sorted by sort_by).
func formatViewOrders(raw []byte) string {
	type order struct {
		ItemName       string  `json:"item_name"`
		Side           string  `json:"side"`
		OrderType      string  `json:"order_type"`
		PriceEach      float64 `json:"price_each"`
		Quantity       int     `json:"quantity"`
		FilledQuantity int     `json:"filled_quantity"`
		Remaining      int     `json:"remaining"`
		CreatedAt      string  `json:"created_at"`
		OrderID        string  `json:"order_id"`
	}
	var resp struct {
		Base       string  `json:"base"`
		Scope      string  `json:"scope"`
		SortBy     string  `json:"sort_by"`
		Page       int     `json:"page"`
		TotalPages int     `json:"total_pages"`
		Total      int     `json:"total"`
		Hint       string  `json:"hint"`
		Orders     []order `json:"orders"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}

	var b strings.Builder

	header := "Orders"
	if resp.Base != "" {
		header += " @ " + resp.Base
	}
	if resp.Scope != "" {
		header += fmt.Sprintf(" (%s)", resp.Scope)
	}
	var meta []string
	if resp.Total > 0 {
		meta = append(meta, fmt.Sprintf("%d total", resp.Total))
	}
	if resp.TotalPages > 1 {
		meta = append(meta, fmt.Sprintf("page %d/%d", resp.Page, resp.TotalPages))
	}
	if resp.SortBy != "" {
		meta = append(meta, "sorted by "+resp.SortBy)
	}
	if len(meta) > 0 {
		header += " — " + strings.Join(meta, ", ")
	}
	fmt.Fprintf(&b, "%s\n\n", header)

	if len(resp.Orders) == 0 {
		b.WriteString("  (no orders)\n")
		return b.String()
	}

	type cells struct {
		side, item, price, qty, filled, remaining, created, id string
	}
	rows := make([]cells, 0, len(resp.Orders))
	for _, o := range resp.Orders {
		side := o.Side
		if side == "" {
			side = o.OrderType
		}
		created := o.CreatedAt
		if t, err := time.Parse(time.RFC3339, o.CreatedAt); err == nil {
			created = t.Format("2006-01-02 15:04")
		}
		rows = append(rows, cells{
			side:      strings.ToUpper(side),
			item:      o.ItemName,
			price:     formatCredits(o.PriceEach),
			qty:       strconv.Itoa(o.Quantity),
			filled:    strconv.Itoa(o.FilledQuantity),
			remaining: strconv.Itoa(o.Remaining),
			created:   created,
			id:        o.OrderID,
		})
	}

	sideW, itemW, priceW := len("Side"), len("Item"), len("Price")
	qtyW, filledW, remW := len("Qty"), len("Filled"), len("Remaining")
	createdW, idW := len("Created"), len("Order ID")
	for _, r := range rows {
		sideW = max(sideW, len(r.side))
		itemW = max(itemW, len(r.item))
		priceW = max(priceW, len(r.price))
		qtyW = max(qtyW, len(r.qty))
		filledW = max(filledW, len(r.filled))
		remW = max(remW, len(r.remaining))
		createdW = max(createdW, len(r.created))
		idW = max(idW, len(r.id))
	}

	fmt.Fprintf(&b, "  %-*s | %-*s | %*s | %*s | %*s | %*s | %-*s | %-*s\n",
		sideW, "Side", itemW, "Item", priceW, "Price", qtyW, "Qty",
		filledW, "Filled", remW, "Remaining", createdW, "Created", idW, "Order ID")
	fmt.Fprintf(&b, "  %s-+-%s-+-%s-+-%s-+-%s-+-%s-+-%s-+-%s\n",
		strings.Repeat("-", sideW), strings.Repeat("-", itemW), strings.Repeat("-", priceW),
		strings.Repeat("-", qtyW), strings.Repeat("-", filledW), strings.Repeat("-", remW),
		strings.Repeat("-", createdW), strings.Repeat("-", idW))
	for _, r := range rows {
		fmt.Fprintf(&b, "  %-*s | %-*s | %*s | %*s | %*s | %*s | %-*s | %-*s\n",
			sideW, r.side, itemW, r.item, priceW, r.price, qtyW, r.qty,
			filledW, r.filled, remW, r.remaining, createdW, r.created, idW, r.id)
	}

	if resp.Hint != "" {
		fmt.Fprintf(&b, "\nℹ %s\n", resp.Hint)
	}
	return b.String()
}

// formatFactionInvites renders a faction_get_invites response as a table of
// pending invitations. The faction_id column is shown because it identifies
// the invite to accept/decline.
func formatFactionInvites(raw []byte) string {
	var resp struct {
		Invites []struct {
			FactionID   string `json:"faction_id"`
			FactionName string `json:"faction_name"`
			FactionTag  string `json:"faction_tag"`
			InvitedAt   string `json:"invited_at"`
			InvitedBy   string `json:"invited_by"`
		} `json:"invites"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}
	if len(resp.Invites) == 0 {
		return "  (no pending invitations)\n"
	}

	type cells struct{ faction, tag, by, at, id string }
	rows := make([]cells, 0, len(resp.Invites))
	for _, inv := range resp.Invites {
		at := inv.InvitedAt
		if t, err := time.Parse(time.RFC3339, inv.InvitedAt); err == nil {
			at = t.Format("2006-01-02 15:04")
		}
		rows = append(rows, cells{
			faction: inv.FactionName,
			tag:     inv.FactionTag,
			by:      inv.InvitedBy,
			at:      at,
			id:      inv.FactionID,
		})
	}

	factionW, tagW, byW := len("Faction"), len("Tag"), len("Invited By")
	atW, idW := len("Invited At"), len("Faction ID")
	for _, r := range rows {
		factionW = max(factionW, len(r.faction))
		tagW = max(tagW, len(r.tag))
		byW = max(byW, len(r.by))
		atW = max(atW, len(r.at))
		idW = max(idW, len(r.id))
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Pending Faction Invitations (%d):\n\n", len(resp.Invites))
	fmt.Fprintf(&b, "  %-*s | %-*s | %-*s | %-*s | %-*s\n",
		factionW, "Faction", tagW, "Tag", byW, "Invited By", atW, "Invited At", idW, "Faction ID")
	fmt.Fprintf(&b, "  %s-+-%s-+-%s-+-%s-+-%s\n",
		strings.Repeat("-", factionW), strings.Repeat("-", tagW), strings.Repeat("-", byW),
		strings.Repeat("-", atW), strings.Repeat("-", idW))
	for _, r := range rows {
		fmt.Fprintf(&b, "  %-*s | %-*s | %-*s | %-*s | %-*s\n",
			factionW, r.faction, tagW, r.tag, byW, r.by, atW, r.at, idW, r.id)
	}
	return b.String()
}

func formatMissions(raw []byte) string {
	var resp struct {
		Missions []struct {
			MissionID   string `json:"mission_id"`
			TemplateID  string `json:"template_id,omitempty"`
			Type        string `json:"type"`
			Title       string `json:"title"`
			Description string `json:"description,omitempty"`
			Difficulty  int    `json:"difficulty,omitempty"`
			Giver       struct {
				Name  string `json:"name,omitempty"`
				Title string `json:"title,omitempty"`
			} `json:"giver,omitempty"`
			ChainNext      string `json:"chain_next,omitempty"`
			ExpiresInTicks int    `json:"expires_in_ticks,omitempty"`
			Rewards        *struct {
				Credits    int            `json:"credits"`
				Reputation int            `json:"reputation,omitempty"`
				PirateRep  int            `json:"pirate_rep,omitempty"`
				Items      map[string]int `json:"items,omitempty"`
				SkillXP    map[string]int `json:"skill_xp,omitempty"`
			} `json:"rewards,omitempty"`
			Objectives []struct {
				Type        string `json:"type"`
				Description string `json:"description"`
				ItemID      string `json:"item_id,omitempty"`
				Quantity    int    `json:"quantity,omitempty"`
			} `json:"objectives,omitempty"`
		} `json:"missions"`
		BaseName string `json:"base_name,omitempty"`
		BaseID   string `json:"base_id,omitempty"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}

	if len(resp.Missions) == 0 {
		if resp.BaseName != "" {
			return fmt.Sprintf("No missions available at %s", resp.BaseName)
		}
		return "No missions available"
	}

	type missionObjective struct {
		Type        string
		Description string
		ItemID      string
		Quantity    int
	}
	type missionRow struct {
		MissionID      string
		TemplateID     string
		Type           string
		Title          string
		Description    string
		Difficulty     int
		GiverName      string
		GiverTitle     string
		ChainNext      string
		ExpiresInTicks int
		Credits        int
		Reputation     int
		PirateRep      int
		Items          map[string]int
		SkillXP        map[string]int
		Objectives     []missionObjective
	}

	missionsByType := make(map[string][]missionRow)
	for _, m := range resp.Missions {
		credits := 0
		reputation := 0
		pirateRep := 0
		items := make(map[string]int)
		skillXP := make(map[string]int)
		if m.Rewards != nil {
			credits = m.Rewards.Credits
			reputation = m.Rewards.Reputation
			pirateRep = m.Rewards.PirateRep
			items = m.Rewards.Items
			skillXP = m.Rewards.SkillXP
		}
		objectives := make([]missionObjective, 0, len(m.Objectives))
		for _, o := range m.Objectives {
			objectives = append(objectives, missionObjective{
				Type:        o.Type,
				Description: o.Description,
				ItemID:      o.ItemID,
				Quantity:    o.Quantity,
			})
		}
		missionsByType[m.Type] = append(missionsByType[m.Type], missionRow{
			MissionID:      m.MissionID,
			TemplateID:     m.TemplateID,
			Type:           m.Type,
			Title:          m.Title,
			Description:    m.Description,
			Difficulty:     m.Difficulty,
			GiverName:      m.Giver.Name,
			GiverTitle:     m.Giver.Title,
			ChainNext:      m.ChainNext,
			ExpiresInTicks: m.ExpiresInTicks,
			Credits:        credits,
			Reputation:     reputation,
			PirateRep:      pirateRep,
			Items:          items,
			SkillXP:        skillXP,
			Objectives:     objectives,
		})
	}

	// Sort mission types alphabetically
	types := make([]string, 0, len(missionsByType))
	for t := range missionsByType {
		types = append(types, t)
	}
	slices.Sort(types)

	displayID := func(m missionRow) string {
		if m.TemplateID != "" {
			return m.TemplateID
		}
		return m.MissionID
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Missions (%d)\n\n", len(resp.Missions))

	for _, missionType := range types {
		missions := missionsByType[missionType]

		slices.SortFunc(missions, func(a, b missionRow) int {
			return strings.Compare(displayID(a), displayID(b))
		})

		// Type header
		typeUpper := strings.ToUpper(missionType)
		fmt.Fprintf(&b, "%s\n%s\n\n", typeUpper, strings.Repeat("-", len(missionType)))

		for _, m := range missions {
			id := displayID(m)

			// Title with display ID
			if m.ChainNext != "" {
				fmt.Fprintf(&b, "%s - (%s) - Chain Mission\n", m.Title, id)
			} else {
				fmt.Fprintf(&b, "%s - (%s)\n", m.Title, id)
			}
			separator := strings.Repeat("-", len(m.Title)+len(id)+20)
			if m.ChainNext != "" {
				separator = strings.Repeat("-", len(m.Title)+len(id)+35)
			}
			fmt.Fprintf(&b, "%s\n", separator)

			// Description (truncated unless --full was requested).
			desc := m.Description
			if !missionsShowFull && len(desc) > 200 {
				desc = desc[:197] + "..."
			}
			if desc != "" {
				fmt.Fprintf(&b, "%s\n\n", desc)
			}

			// Difficulty as stars
			stars := strings.Repeat("★", m.Difficulty)
			emptyStars := strings.Repeat("☆", 10-m.Difficulty)
			fmt.Fprintf(&b, "Difficulty:  %s%s%s %d/10\n", stars, emptyStars, "", m.Difficulty)

			// Objectives — rendered as an unchecked checklist since these are
			// templates, not yet accepted. formatActiveMissions adds ✓/progress
			// once a mission has been accepted and tracked. Suffix the line
			// with "(<qty> x <item_id>)" when the objective carries an item
			// reference so the player sees the exact item_id needed without
			// pattern-matching the human description.
			if len(m.Objectives) > 0 {
				b.WriteString("Objectives:\n")
				for _, o := range m.Objectives {
					line := o.Description
					if o.ItemID != "" && o.Quantity > 0 {
						line = fmt.Sprintf("%s (%d x %s)", line, o.Quantity, o.ItemID)
					}
					fmt.Fprintf(&b, "  ☐ %s\n", line)
				}
			}

			// Rewards section
			fmt.Fprintf(&b, "Rewards:\n")

			// Credits
			if m.Credits > 0 {
				fmt.Fprintf(&b, "  credits:  %20s +%d cr\n", "", m.Credits)
			} else {
				fmt.Fprintf(&b, "  credits:  %20s 0 cr\n", "")
			}

			// Reputation
			if m.Reputation != 0 {
				fmt.Fprintf(&b, "  rep:      %20s %+d\n", "", m.Reputation)
			}
			if m.PirateRep != 0 {
				fmt.Fprintf(&b, "  pirate:   %20s %+d rep\n", "", m.PirateRep)
			}

			// Items
			if len(m.Items) > 0 {
				// Sort items by name
				var itemNames []string
				for name := range m.Items {
					itemNames = append(itemNames, name)
				}
				slices.Sort(itemNames)
				for _, name := range itemNames {
					qty := m.Items[name]
					fmt.Fprintf(&b, "  items:    %20s %s %5d units\n", "", name, qty)
				}
			}

			// Skill XP
			if len(m.SkillXP) > 0 {
				// Sort skills by name
				var skillNames []string
				for skill := range m.SkillXP {
					skillNames = append(skillNames, skill)
				}
				slices.Sort(skillNames)
				for _, skill := range skillNames {
					xp := m.SkillXP[skill]
					fmt.Fprintf(&b, "  skills:   %20s %s +%4d xp\n", "", skill, xp)
				}
			}

			b.WriteString("\n")
		}
	}

	return b.String()
}

// formatActiveMissions renders get_active_missions, similar to formatMissions
// but with per-mission progress (percent_complete) and an objectives checklist
// where each objective is prefixed with ✓ (completed) or ☐ (open) and shows
// current/required progress when present.
func formatActiveMissions(raw []byte) string {
	type activeObjective struct {
		Description string `json:"description"`
		Completed   bool   `json:"completed"`
		Current     int    `json:"current"`
		Required    int    `json:"required"`
		// SystemName is the destination system for travel/dock/deliver
		// objectives; appended to the line so the operator knows where to go.
		SystemName string `json:"system_name"`
	}
	type activeMission struct {
		MissionID   string `json:"mission_id"`
		TemplateID  string `json:"template_id"`
		Type        string `json:"type"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Difficulty  int    `json:"difficulty"`
		// PercentComplete is fractional on the wire (e.g. 33.333…); keep it a
		// float so the decode doesn't fail, and round when rendering.
		PercentComplete float64           `json:"percent_complete"`
		ExpiresInTicks  int               `json:"expires_in_ticks"`
		AcceptedAt      string            `json:"accepted_at"`
		Objectives      []activeObjective `json:"objectives"`
		Rewards         *struct {
			Credits    int            `json:"credits"`
			Reputation int            `json:"reputation,omitempty"`
			PirateRep  int            `json:"pirate_rep,omitempty"`
			Items      map[string]int `json:"items,omitempty"`
			SkillXP    map[string]int `json:"skill_xp,omitempty"`
		} `json:"rewards,omitempty"`
	}
	var resp struct {
		Missions    []activeMission `json:"missions"`
		MaxMissions int             `json:"max_missions,omitempty"`
		TotalCount  int             `json:"total_count,omitempty"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}

	if len(resp.Missions) == 0 {
		return "No active missions"
	}

	// Group by type, sort missions within each group.
	byType := make(map[string][]activeMission)
	for _, m := range resp.Missions {
		byType[m.Type] = append(byType[m.Type], m)
	}
	types := make([]string, 0, len(byType))
	for t := range byType {
		types = append(types, t)
	}
	slices.Sort(types)

	var b strings.Builder
	header := fmt.Sprintf("Active Missions (%d", len(resp.Missions))
	if resp.MaxMissions > 0 {
		header += fmt.Sprintf("/%d", resp.MaxMissions)
	}
	header += ")"
	fmt.Fprintf(&b, "%s\n\n", header)

	for _, missionType := range types {
		missions := byType[missionType]
		slices.SortFunc(missions, func(a, b activeMission) int {
			idA := a.TemplateID
			if idA == "" {
				idA = a.MissionID
			}
			idB := b.TemplateID
			if idB == "" {
				idB = b.MissionID
			}
			return strings.Compare(idA, idB)
		})

		typeUpper := strings.ToUpper(missionType)
		fmt.Fprintf(&b, "%s\n%s\n\n", typeUpper, strings.Repeat("-", len(missionType)))

		for _, m := range missions {
			displayID := m.TemplateID
			if displayID == "" {
				displayID = m.MissionID
			}
			fmt.Fprintf(&b, "%s - (%s) — %.0f%% complete\n", m.Title, displayID, m.PercentComplete)
			fmt.Fprintf(&b, "%s\n", strings.Repeat("-", len(m.Title)+len(displayID)+20))
			if m.MissionID != "" && m.MissionID != displayID {
				fmt.Fprintf(&b, "Mission ID:  %s  (use with complete_mission/abandon_mission)\n", m.MissionID)
			}
			if m.AcceptedAt != "" {
				line := fmt.Sprintf("Accepted at: %s", m.AcceptedAt)
				if m.ExpiresInTicks > 0 {
					if t, err := time.Parse(time.RFC3339, m.AcceptedAt); err == nil {
						expiresAt := t.Add(time.Duration(m.ExpiresInTicks) * 10 * time.Second)
						line += fmt.Sprintf("   Expires at: %s", expiresAt.UTC().Format(time.RFC3339))
					}
				}
				fmt.Fprintf(&b, "%s\n", line)
			}

			desc := m.Description
			if !missionsShowFull && len(desc) > 200 {
				desc = desc[:197] + "..."
			}
			if desc != "" {
				fmt.Fprintf(&b, "%s\n\n", desc)
			}

			stars := strings.Repeat("★", m.Difficulty)
			emptyStars := strings.Repeat("☆", 10-m.Difficulty)
			fmt.Fprintf(&b, "Difficulty:  %s%s %d/10\n", stars, emptyStars, m.Difficulty)

			if len(m.Objectives) > 0 {
				fmt.Fprintf(&b, "Objectives:\n")
				for _, o := range m.Objectives {
					mark := "☐"
					if o.Completed {
						mark = "✓"
					}
					line := o.Description
					// Surface the destination system for travel/dock objectives
					// (e.g. "Travel to Frontier Station in Unknown Edge"), unless
					// the description already names it.
					if o.SystemName != "" && !strings.Contains(line, o.SystemName) {
						line = fmt.Sprintf("%s in %s", line, o.SystemName)
					}
					if o.Required > 0 && !o.Completed {
						line = fmt.Sprintf("%s [%d/%d]", line, o.Current, o.Required)
					}
					fmt.Fprintf(&b, "  %s %s\n", mark, line)
				}
			}

			if m.Rewards != nil {
				fmt.Fprintf(&b, "Rewards:\n")
				if m.Rewards.Credits > 0 {
					fmt.Fprintf(&b, "  credits:  %20s +%d cr\n", "", m.Rewards.Credits)
				}
				if m.Rewards.Reputation != 0 {
					fmt.Fprintf(&b, "  rep:      %20s %+d\n", "", m.Rewards.Reputation)
				}
				if m.Rewards.PirateRep != 0 {
					fmt.Fprintf(&b, "  pirate:   %20s %+d rep\n", "", m.Rewards.PirateRep)
				}
				if len(m.Rewards.Items) > 0 {
					itemNames := make([]string, 0, len(m.Rewards.Items))
					for name := range m.Rewards.Items {
						itemNames = append(itemNames, name)
					}
					slices.Sort(itemNames)
					for _, name := range itemNames {
						fmt.Fprintf(&b, "  items:    %20s %s %5d units\n", "", name, m.Rewards.Items[name])
					}
				}
				if len(m.Rewards.SkillXP) > 0 {
					skillNames := make([]string, 0, len(m.Rewards.SkillXP))
					for skill := range m.Rewards.SkillXP {
						skillNames = append(skillNames, skill)
					}
					slices.Sort(skillNames)
					for _, skill := range skillNames {
						fmt.Fprintf(&b, "  skills:   %20s %s +%4d xp\n", "", skill, m.Rewards.SkillXP[skill])
					}
				}
			}

			b.WriteString("\n")
		}
	}

	return b.String()
}

// formatCompleteMission renders a complete_mission response: title + mission_id
// banner, the giver's flavor message, the reward block (matching the layout
// used by formatActiveMissions), and a "chain continues" hint when the mission
// unlocks a follow-up.
func formatCompleteMission(raw []byte) string {
	raw = unwrapActionResult(raw)
	var resp struct {
		Title         string         `json:"title"`
		MissionID     string         `json:"mission_id"`
		Message       string         `json:"message"`
		CreditsEarned int            `json:"credits_earned"`
		ItemsReceived map[string]int `json:"items_received,omitempty"`
		SkillXPGained map[string]int `json:"skill_xp_gained,omitempty"`
		ChainNext     string         `json:"chain_next,omitempty"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}

	var b strings.Builder
	if resp.Title != "" {
		fmt.Fprintf(&b, "✓ Completed: %s", resp.Title)
		if resp.MissionID != "" {
			fmt.Fprintf(&b, " (%s)", resp.MissionID)
		}
		b.WriteString("\n\n")
	}

	if resp.Message != "" {
		fmt.Fprintf(&b, "%q\n\n", resp.Message)
	}

	hasRewards := resp.CreditsEarned > 0 || len(resp.ItemsReceived) > 0 || len(resp.SkillXPGained) > 0
	if hasRewards {
		b.WriteString("Rewards:\n")
		if resp.CreditsEarned > 0 {
			fmt.Fprintf(&b, "  credits:  %20s +%d cr\n", "", resp.CreditsEarned)
		}
		if len(resp.ItemsReceived) > 0 {
			itemNames := make([]string, 0, len(resp.ItemsReceived))
			for name := range resp.ItemsReceived {
				itemNames = append(itemNames, name)
			}
			slices.Sort(itemNames)
			for _, name := range itemNames {
				fmt.Fprintf(&b, "  items:    %20s %s %5d units\n", "", name, resp.ItemsReceived[name])
			}
		}
		if len(resp.SkillXPGained) > 0 {
			skillNames := make([]string, 0, len(resp.SkillXPGained))
			for skill := range resp.SkillXPGained {
				skillNames = append(skillNames, skill)
			}
			slices.Sort(skillNames)
			for _, skill := range skillNames {
				fmt.Fprintf(&b, "  skills:   %20s %s +%4d xp\n", "", skill, resp.SkillXPGained[skill])
			}
		}
	}

	if resp.ChainNext != "" {
		if hasRewards {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "Chain continues: %s (use accept_mission to pick it up)\n", resp.ChainNext)
	}

	return b.String()
}

// formatDock formats a dock response with station condition and truncated story.
func formatDock(raw []byte) string {
	var resp struct {
		Base             string `json:"base"`
		StationCondition struct {
			Condition         string `json:"condition"`
			ConditionText     string `json:"condition_text"`
			SatisfactionPct   int    `json:"satisfaction_pct"`
			SatisfiedCount    int    `json:"satisfied_count"`
			TotalServiceInfra int    `json:"total_service_infra"`
		} `json:"station_condition"`
		// YourFacilities + FacilityNote are the dock rent briefing
		// (gameserver v0.347.0+): per-facility status with missed-rent cycles,
		// plus an escalating note when rent is overdue.
		YourFacilities []struct {
			Name             string `json:"name"`
			Type             string `json:"type"`
			Status           string `json:"status"`
			RentPerCycle     int    `json:"rent_per_cycle"`
			MissedRentCycles int    `json:"missed_rent_cycles"`
		} `json:"your_facilities"`
		FacilityNote string `json:"facility_note"`
		// PassengerArrivals reports aboard passengers bound for this station who
		// were auto-delivered (and their fares collected) on docking.
		PassengerArrivals struct {
			Delivered []struct {
				Name       string `json:"name"`
				Class      string `json:"class"`
				Fare       int    `json:"fare"`
				SpeedBonus int    `json:"speed_bonus"`
			} `json:"delivered"`
			FareCollected     int            `json:"fare_collected"`
			ReputationChanges map[string]int `json:"reputation_changes"`
		} `json:"passenger_arrivals"`
		Story string `json:"story"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Docked at %q\n\n", resp.Base)

	sc := resp.StationCondition
	fmt.Fprintf(&b, "Station is in %q condition.  %s\n\n", sc.Condition, sc.ConditionText)
	fmt.Fprintf(&b, "Services satisfied: %d / %d (%d%%)\n", sc.SatisfiedCount, sc.TotalServiceInfra, sc.SatisfactionPct)

	// Rent briefing: surface your facilities here and flag any missed rent
	// loudly so an agent sees a repossession risk before it's too late.
	if len(resp.YourFacilities) > 0 {
		var totRent, totMissed int
		for _, f := range resp.YourFacilities {
			totRent += f.RentPerCycle
			totMissed += f.MissedRentCycles
		}
		fmt.Fprintf(&b, "\nYour facilities here: %d (rent %d/cycle)\n", len(resp.YourFacilities), totRent)
		for _, f := range resp.YourFacilities {
			line := fmt.Sprintf("  - %s (%s): %s, %d/cycle", f.Name, f.Type, f.Status, f.RentPerCycle)
			if f.MissedRentCycles > 0 {
				line += fmt.Sprintf("  ⚠ %d cycle(s) of rent missed", f.MissedRentCycles)
			}
			fmt.Fprintf(&b, "%s\n", line)
		}
		if totMissed > 0 {
			fmt.Fprintf(&b, "⚠ RENT OVERDUE — pay soon to avoid repossession.\n")
		}
	}
	if resp.FacilityNote != "" {
		fmt.Fprintf(&b, "⚠ %s\n", resp.FacilityNote)
	}

	// Passenger deliveries: any aboard passengers bound here disembark on dock
	// and pay their fares automatically.
	if pa := resp.PassengerArrivals; len(pa.Delivered) > 0 {
		fmt.Fprintf(&b, "\nDelivered %d passenger(s) — %d cr collected\n", len(pa.Delivered), pa.FareCollected)
		for _, p := range pa.Delivered {
			// fare_collected bundles the base fare and an on-time speed bonus;
			// split the bonus out so it's clear where the credits came from.
			if p.SpeedBonus > 0 {
				fmt.Fprintf(&b, "  - %s [%s], %d cr +%d speed bonus\n", p.Name, p.Class, p.Fare, p.SpeedBonus)
			} else {
				fmt.Fprintf(&b, "  - %s [%s], %d cr\n", p.Name, p.Class, p.Fare)
			}
		}
		if len(pa.ReputationChanges) > 0 {
			reps := make([]string, 0, len(pa.ReputationChanges))
			for faction, delta := range pa.ReputationChanges {
				reps = append(reps, fmt.Sprintf("%s %+d", faction, delta))
			}
			slices.Sort(reps)
			fmt.Fprintf(&b, "  Reputation: %s\n", strings.Join(reps, ", "))
		}
	}

	if resp.Story != "" {
		story := resp.Story
		if len(story) > 200 {
			story = story[:200] + "..."
		}
		// Collapse newlines for compact display.
		story = strings.ReplaceAll(story, "\n", " ")
		fmt.Fprintf(&b, "\nStation Lore: %q\n", story)
	}

	return b.String()
}

// formatJump formats a jump response as a one-line summary.
func formatJump(raw []byte) string {
	raw = unwrapActionResult(raw)
	var resp struct {
		FromSystem string `json:"from_system"`
		System     string `json:"system"`
		POI        string `json:"poi"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}
	return fmt.Sprintf("Successfully jumped from %s to %s.  Now located at %s", resp.FromSystem, resp.System, resp.POI)
}

// wreckCargo is a parsed cargo item from a wreck.
type wreckCargo struct {
	ItemID   string  `json:"item_id"`
	Quantity float64 `json:"quantity"`
}

// wreckEntry is a parsed wreck from a get_wrecks response.
type wreckEntry struct {
	ID           string       `json:"id"`
	Type         string       `json:"type"`
	VictimName   string       `json:"victim_name"`
	ShipClass    string       `json:"ship_class"`
	Cargo        []wreckCargo `json:"cargo"`
	Modules      []string     `json:"modules"`
	SalvageValue int          `json:"salvage_value"`
}

// formatWrecks formats a get_wrecks response.
func formatWrecks(raw []byte) string {
	var resp struct {
		Count  int          `json:"count"`
		Wrecks []wreckEntry `json:"wrecks"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}

	if resp.Count == 0 || len(resp.Wrecks) == 0 {
		return "No wrecks found."
	}

	// Separate by type.
	var jettisons, ships []wreckEntry
	for _, w := range resp.Wrecks {
		if w.Type == "jettison" {
			jettisons = append(jettisons, w)
		} else {
			ships = append(ships, w)
		}
	}

	var b strings.Builder

	if len(jettisons) > 0 {
		fmt.Fprintf(&b, "Jettison Cannisters: %d\n", len(jettisons))
		for _, w := range jettisons {
			fmt.Fprintf(&b, "\nCanister: %s\n", w.ID)
			fmt.Fprintf(&b, "Owner: %q\n", w.VictimName)
			b.WriteString("Contents:\n")

			// Calculate column widths for alignment.
			idW := 0
			for _, c := range w.Cargo {
				idW = max(idW, len(c.ItemID))
			}

			for _, c := range w.Cargo {
				fmt.Fprintf(&b, "  %*s | %s\n", idW, c.ItemID, formatFloat(c.Quantity))
			}

			b.WriteString("\nTo loot:\n")
			for _, c := range w.Cargo {
				fmt.Fprintf(&b, "loot_wreck %s %s %s\n", w.ID, c.ItemID, formatFloat(c.Quantity))
			}
		}
	}

	if len(ships) > 0 {
		if len(jettisons) > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "Ship Wrecks: %d\n", len(ships))
		for _, w := range ships {
			fmt.Fprintf(&b, "\nShip: %s\n", w.ID)
			fmt.Fprintf(&b, "Owner: %q\n", w.VictimName)
			fmt.Fprintf(&b, "Class: %s\n", w.ShipClass)
			fmt.Fprintf(&b, "Salvage Value: %d\n", w.SalvageValue)
			fmt.Fprintf(&b, "Modules:  %d\n", len(w.Modules))
			if len(w.Cargo) == 0 {
				b.WriteString("Cargo:   None\n")
			} else {
				b.WriteString("Cargo:\n")
				idW := 0
				for _, c := range w.Cargo {
					idW = max(idW, len(c.ItemID))
				}
				for _, c := range w.Cargo {
					fmt.Fprintf(&b, "  %*s | %s\n", idW, c.ItemID, formatFloat(c.Quantity))
				}
			}
			b.WriteString("To salvage:\n")
			fmt.Fprintf(&b, "tow_ship %s\n", w.ID)
		}
	}

	return b.String()
}

// systemPOI is a parsed POI from a get_system response.
type systemPOI struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Online   int    `json:"online,omitempty"`
	Position struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
	} `json:"position"`
	HasBase  bool   `json:"has_base,omitempty"`
	BaseID   string `json:"base_id,omitempty"`
	BaseName string `json:"base_name,omitempty"`
}

// systemConnection is a parsed connection from a get_system response.
type systemConnection struct {
	SystemID string `json:"system_id"`
	Name     string `json:"name"`
	Distance int    `json:"distance"`
}

// formatSystem formats a get_system response with system details, connections, and POIs.
func formatSystem(raw []byte) string {
	var resp struct {
		System struct {
			ID             string             `json:"id"`
			Name           string             `json:"name"`
			Description    string             `json:"description"`
			Empire         string             `json:"empire"`
			PoliceLevel    int                `json:"police_level"`
			SecurityStatus string             `json:"security_status"`
			Connections    []systemConnection `json:"connections"`
			POIs           []systemPOI        `json:"pois"`
		} `json:"system"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}

	sys := resp.System
	// Hyperspace responses come back with no "system" object (or a zeroed one)
	// because the player is mid-jump and the server has nothing to report yet.
	// The OK message ("You are in hyperspace, jumping between systems.") is
	// already logged by the response logger upstream, so emitting an empty
	// header + "(none)" sections here is just visual noise — return blank.
	if sys.ID == "" && sys.Name == "" && len(sys.Connections) == 0 && len(sys.POIs) == 0 {
		return ""
	}
	var b strings.Builder

	// Header
	empire := sys.Empire
	if empire != "" {
		empire = strings.ToUpper(empire[:1]) + empire[1:]
	}
	fmt.Fprintf(&b, "%s (%s)   | %s\n", sys.Name, sys.ID, empire)
	// Server-live system data: always reflects current observation, so no
	// "Unexplored" case applies here. KB reads use last_visited_tick instead.
	fmt.Fprintf(&b, "Security Status: %d - %s\n", sys.PoliceLevel, sys.SecurityStatus)
	if sys.Description != "" {
		fmt.Fprintf(&b, "%s\n", sys.Description)
	}

	// Connections
	b.WriteString("\nConnections:\n")
	if len(sys.Connections) == 0 {
		b.WriteString("  (none)\n")
	} else {
		nameW, idW := 0, 0
		for _, c := range sys.Connections {
			nameW = max(nameW, len(c.Name))
			idW = max(idW, len(c.SystemID))
		}
		for _, c := range sys.Connections {
			fmt.Fprintf(&b, "    %-*s | %-*s | %d LY\n", nameW, c.Name, idW, c.SystemID, c.Distance)
		}
	}

	// POIs
	b.WriteString("\nPOIs:\n")
	if len(sys.POIs) == 0 {
		b.WriteString("  (none)\n")
	} else {
		nameW, idW, typeW := len("Name"), len("ID"), len("Type")
		for _, p := range sys.POIs {
			nameW = max(nameW, len(p.Name))
			idW = max(idW, len(p.ID))
			typeW = max(typeW, len(p.Type))
		}

		fmt.Fprintf(&b, "%-*s | %-*s | %-*s | Position\n",
			nameW, "Name", idW, "ID", typeW, "Type")
		b.WriteString(strings.Repeat("-", nameW+idW+typeW+18) + "\n")

		for _, p := range sys.POIs {
			pos := fmt.Sprintf("(%.1f, %.1f)", p.Position.X, p.Position.Y)
			fmt.Fprintf(&b, "%-*s | %-*s | %-*s | %s\n",
				nameW, p.Name, idW, p.ID, typeW, p.Type, pos)
		}
	}

	return b.String()
}

// formatCreateFaction formats a create_faction response as a one-line summary.
func formatCreateFaction(raw []byte) string {
	raw = unwrapActionResult(raw)
	var resp struct {
		FactionID string `json:"faction_id"`
		Name      string `json:"name"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}
	return fmt.Sprintf("Faction created:  %q (%s)", resp.Name, resp.FactionID)
}

// formatFactionInfo formats a faction_info response with colored names.
func formatFactionInfo(raw []byte) string {
	var resp struct {
		ID             string `json:"id"`
		Name           string `json:"name"`
		Tag            string `json:"tag"`
		Description    string `json:"description"`
		Charter        string `json:"charter"`
		LeaderUsername string `json:"leader_username"`
		MemberCount    int    `json:"member_count"`
		OwnedBases     int    `json:"owned_bases"`
		Treasury       int    `json:"treasury"`
		PrimaryColor   string `json:"primary_color"`
		SecondaryColor string `json:"secondary_color"`
		AtWar          bool   `json:"at_war"`
		IsMember       bool   `json:"is_member"`
		IsAlly         bool   `json:"is_ally"`
		IsEnemy        bool   `json:"is_enemy"`
		Members        []struct {
			Username string `json:"username"`
			Role     string `json:"role"`
			IsOnline bool   `json:"is_online"`
		} `json:"members"`
		Allies []struct {
			Name string `json:"name"`
			Tag  string `json:"tag"`
		} `json:"allies"`
		Enemies []struct {
			Name string `json:"name"`
			Tag  string `json:"tag"`
		} `json:"enemies"`
		Wars []struct {
			FactionName string `json:"faction_name"`
			FactionTag  string `json:"faction_tag"`
			DeclaredBy  string `json:"declared_by"`
		} `json:"wars"`
		FuelBunkers []struct {
			BaseID       string `json:"base_id"`
			BaseName     string `json:"base_name"`
			FuelReserve  int    `json:"fuel_reserve"`
			FuelCapacity int    `json:"fuel_capacity"`
		} `json:"fuel_bunkers"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}

	var b strings.Builder
	colorName := colorizeHex(resp.Name, resp.PrimaryColor, resp.SecondaryColor)
	colorTag := colorizeHex("["+resp.Tag+"]", resp.PrimaryColor, resp.SecondaryColor)
	fmt.Fprintf(&b, "%s %s\n", colorName, colorTag)
	fmt.Fprintf(&b, "ID: %s\n", resp.ID)
	fmt.Fprintf(&b, "Leader: %s | Members: %d | Bases: %d\n", resp.LeaderUsername, resp.MemberCount, resp.OwnedBases)

	if resp.IsMember && resp.Treasury > 0 {
		fmt.Fprintf(&b, "Treasury: %d credits\n", resp.Treasury)
	}

	if resp.Description != "" {
		fmt.Fprintf(&b, "\n%s\n", resp.Description)
	}
	if resp.Charter != "" {
		fmt.Fprintf(&b, "\nCharter: %s\n", resp.Charter)
	}

	// Relationship indicator
	switch {
	case resp.IsMember:
		fmt.Fprintf(&b, "\nYou are a member of this faction.\n")
	case resp.IsAlly:
		fmt.Fprintf(&b, "\nThis faction is an ally.\n")
	case resp.IsEnemy:
		fmt.Fprintf(&b, "\nThis faction is an enemy.\n")
	}

	// Members (only shown for own faction)
	if len(resp.Members) > 0 {
		fmt.Fprintf(&b, "\nMembers:\n")
		nameW, roleW := len("Username"), len("Role")
		for _, m := range resp.Members {
			nameW = max(nameW, len(m.Username))
			roleW = max(roleW, len(m.Role))
		}
		fmt.Fprintf(&b, "  %-*s | %-*s | Status\n", nameW, "Username", roleW, "Role")
		fmt.Fprintf(&b, "  %s-+-%s-+--------\n", strings.Repeat("-", nameW), strings.Repeat("-", roleW))
		for _, m := range resp.Members {
			status := "offline"
			if m.IsOnline {
				status = "online"
			}
			name := colorizeHex(m.Username, resp.PrimaryColor, resp.SecondaryColor)
			pad := nameW - len(m.Username)
			if pad > 0 {
				name += strings.Repeat(" ", pad)
			}
			fmt.Fprintf(&b, "  %s | %-*s | %s\n", name, roleW, m.Role, status)
		}
	}

	// Allies
	if len(resp.Allies) > 0 {
		fmt.Fprintf(&b, "\nAllies:\n")
		for _, a := range resp.Allies {
			fmt.Fprintf(&b, "  %s [%s]\n", a.Name, a.Tag)
		}
	}

	// Enemies
	if len(resp.Enemies) > 0 {
		fmt.Fprintf(&b, "\nEnemies:\n")
		for _, e := range resp.Enemies {
			fmt.Fprintf(&b, "  %s [%s]\n", e.Name, e.Tag)
		}
	}

	// Wars
	if len(resp.Wars) > 0 {
		fmt.Fprintf(&b, "\nActive Wars:\n")
		for _, w := range resp.Wars {
			fmt.Fprintf(&b, "  vs %s [%s] (declared by: %s)\n", w.FactionName, w.FactionTag, w.DeclaredBy)
		}
	}

	// Fuel bunkers (galaxy-wide summary; shown to members, gameserver v0.346.0+)
	if len(resp.FuelBunkers) > 0 {
		var totReserve, totCapacity int
		nameW := len("Base")
		for _, fb := range resp.FuelBunkers {
			label := fb.BaseName
			if label == "" {
				label = fb.BaseID
			}
			nameW = max(nameW, len(label))
			totReserve += fb.FuelReserve
			totCapacity += fb.FuelCapacity
		}
		fmt.Fprintf(&b, "\nFuel Bunkers:\n")
		fmt.Fprintf(&b, "  %-*s | %15s | Fill\n", nameW, "Base", "Reserve / Cap")
		fmt.Fprintf(&b, "  %s-+-%s-+------\n", strings.Repeat("-", nameW), strings.Repeat("-", 15))
		for _, fb := range resp.FuelBunkers {
			label := fb.BaseName
			if label == "" {
				label = fb.BaseID
			}
			pct := 0.0
			if fb.FuelCapacity > 0 {
				pct = 100 * float64(fb.FuelReserve) / float64(fb.FuelCapacity)
			}
			fmt.Fprintf(&b, "  %-*s | %6d / %-6d | %3.0f%%\n", nameW, label, fb.FuelReserve, fb.FuelCapacity, pct)
		}
		totPct := 0.0
		if totCapacity > 0 {
			totPct = 100 * float64(totReserve) / float64(totCapacity)
		}
		fmt.Fprintf(&b, "  Total reserve: %d / %d (%.0f%%)\n", totReserve, totCapacity, totPct)
	}

	return b.String()
}

// formatFactionIntelStatus renders a faction_intel_status response. Fields
// follow the live server payload (which differs from the OpenAPI spec): a
// coverage summary, contributor stats, and the most-recent submission tick.
func formatFactionIntelStatus(raw []byte) string {
	var r struct {
		IntelLevel       int    `json:"intel_level"`
		Contributors     int    `json:"contributors"`
		TopContributor   string `json:"top_contributor"`
		TopContributions int    `json:"top_contributions"`
		SystemsKnown     int    `json:"systems_known"`
		TotalSystems     int    `json:"total_systems"`
		POIsKnown        int    `json:"pois_known"`
		CoveragePct      string `json:"coverage_pct"`
		MostRecentTick   int64  `json:"most_recent_tick"`
	}
	if err := json.Unmarshal(unwrapActionResult(raw), &r); err != nil {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "🛰  Faction Intel Status\n")
	fmt.Fprintf(&b, "  Intel level:     %d\n", r.IntelLevel)
	coverage := r.CoveragePct
	if coverage == "" {
		coverage = "0"
	}
	fmt.Fprintf(&b, "  Coverage:        %s%% (%d / %d systems)\n", coverage, r.SystemsKnown, r.TotalSystems)
	fmt.Fprintf(&b, "  POIs known:      %d\n", r.POIsKnown)
	fmt.Fprintf(&b, "  Contributors:    %d\n", r.Contributors)
	if r.TopContributor != "" {
		fmt.Fprintf(&b, "  Top contributor: %s (%d)\n", r.TopContributor, r.TopContributions)
	}
	if r.MostRecentTick > 0 {
		fmt.Fprintf(&b, "  Most recent:     tick %d\n", r.MostRecentTick)
	}
	return b.String()
}

// formatFactionQueryIntel renders a faction_query_intel response: one block
// per matched system with its empire/police, submitter, POIs (and resources),
// and connections. Fields follow the live server payload.
func formatFactionQueryIntel(raw []byte) string {
	var resp struct {
		Message    string `json:"message"`
		Count      int    `json:"count"`
		Total      int    `json:"total"`
		IntelLevel int    `json:"intel_level"`
		Entries    []struct {
			SystemID      string `json:"system_id"`
			Name          string `json:"name"`
			Empire        string `json:"empire"`
			PoliceLevel   int    `json:"police_level"`
			SubmitterName string `json:"submitter_name"`
			SubmittedAt   int64  `json:"submitted_at_tick"`
			Connections   []struct {
				SystemID string `json:"system_id"`
				Distance int    `json:"distance"`
			} `json:"connections"`
			POIs []struct {
				Name      string `json:"name"`
				Type      string `json:"type"`
				Class     string `json:"class"`
				Resources []struct {
					ResourceID string  `json:"resource_id"`
					Richness   float64 `json:"richness"`
					Remaining  float64 `json:"remaining"`
				} `json:"resources"`
			} `json:"pois"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(unwrapActionResult(raw), &resp); err != nil {
		return ""
	}

	var b strings.Builder
	header := resp.Message
	if header == "" {
		header = fmt.Sprintf("Found %d system(s)", resp.Count)
	}
	fmt.Fprintf(&b, "🛰  Faction Intel Query — %s\n", header)
	fmt.Fprintf(&b, "  Intel level: %d | total: %d\n", resp.IntelLevel, resp.Total)

	for _, e := range resp.Entries {
		name := e.Name
		if name == "" {
			name = e.SystemID
		}
		meta := fmt.Sprintf("police %d", e.PoliceLevel)
		if e.Empire != "" {
			meta = e.Empire + ", " + meta
		}
		fmt.Fprintf(&b, "\n  ● %s (%s) — %s\n", name, e.SystemID, meta)
		if e.SubmitterName != "" {
			fmt.Fprintf(&b, "    by %s @ tick %d\n", e.SubmitterName, e.SubmittedAt)
		}
		if len(e.POIs) > 0 {
			fmt.Fprintf(&b, "    POIs (%d):\n", len(e.POIs))
			for _, p := range e.POIs {
				typeInfo := p.Type
				if p.Class != "" {
					typeInfo = fmt.Sprintf("%s, %s", p.Type, p.Class)
				}
				fmt.Fprintf(&b, "      - %s (%s)\n", p.Name, typeInfo)
				if len(p.Resources) > 0 {
					parts := make([]string, 0, len(p.Resources))
					for _, r := range p.Resources {
						parts = append(parts, fmt.Sprintf("%s r%.0f %.0f", r.ResourceID, r.Richness, r.Remaining))
					}
					fmt.Fprintf(&b, "          %s\n", strings.Join(parts, ", "))
				}
			}
		}
		if len(e.Connections) > 0 {
			parts := make([]string, 0, len(e.Connections))
			for _, c := range e.Connections {
				parts = append(parts, fmt.Sprintf("%s (%d)", c.SystemID, c.Distance))
			}
			fmt.Fprintf(&b, "    Connections: %s\n", strings.Join(parts, ", "))
		}
	}
	return b.String()
}

// formatDeposit formats a deposit_items response as a one-line summary.
func formatDeposit(raw []byte) string {
	return formatItemTransfer(raw, "cargo", "storage")
}

// formatItemTransfer formats a deposit_items / withdraw_items response. Both
// commands transfer items between cargo, station storage, and faction storage,
// so the message reflects the actual source/destination and resulting totals the
// server reports rather than assuming a fixed direction. The current server shape
// carries source/destination plus dest_total (total at the destination) and
// source_remaining (remaining at the source); older responses used
// direction-specific fields (storage_total/cargo_remaining for deposit,
// cargo_total/storage_remaining for withdraw), which are honored as fallbacks.
// defaultSrc/defaultDst label the transfer when the server omits source/destination.
func formatItemTransfer(raw []byte, defaultSrc, defaultDst string) string {
	raw = unwrapActionResult(raw)
	var resp struct {
		ItemID           string `json:"item_id"`
		Quantity         int    `json:"quantity"`
		Source           string `json:"source"`
		Destination      string `json:"destination"`
		DestTotal        *int   `json:"dest_total"`
		SourceRemaining  *int   `json:"source_remaining"`
		StorageTotal     *int   `json:"storage_total"`
		CargoTotal       *int   `json:"cargo_total"`
		CargoRemaining   *int   `json:"cargo_remaining"`
		StorageRemaining *int   `json:"storage_remaining"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}
	src := resp.Source
	if src == "" {
		src = defaultSrc
	}
	dst := resp.Destination
	if dst == "" {
		dst = defaultDst
	}
	destTotal := firstNonNilInt(resp.DestTotal, resp.StorageTotal, resp.CargoTotal)
	srcRemaining := firstNonNilInt(resp.SourceRemaining, resp.CargoRemaining, resp.StorageRemaining)

	msg := fmt.Sprintf("Transferred %d %s from %s to %s.", resp.Quantity, resp.ItemID, src, dst)
	if destTotal != nil {
		msg += fmt.Sprintf(" %d now in %s.", *destTotal, dst)
	}
	if srcRemaining != nil {
		msg += fmt.Sprintf(" %d left in %s.", *srcRemaining, src)
	}
	return msg
}

// firstNonNilInt returns the first non-nil pointer from ps, or nil if all are nil.
func firstNonNilInt(ps ...*int) *int {
	for _, p := range ps {
		if p != nil {
			return p
		}
	}
	return nil
}

// formatSkills formats a get_skills response as a table.
func formatSkills(raw []byte) string {
	var resp struct {
		Skills map[string]struct {
			Name      string `json:"name"`
			Category  string `json:"category"`
			Level     int    `json:"level"`
			MaxLevel  int    `json:"max_level"`
			XP        int    `json:"xp"`
			NextLvlXP int    `json:"next_level_xp"`
		} `json:"skills"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}
	if len(resp.Skills) == 0 {
		return "No skills"
	}

	type skillRow struct {
		Name     string
		Category string
		Level    int
		XP       int
		NextXP   int
		Pct      int
	}

	rows := make([]skillRow, 0, len(resp.Skills))
	for _, s := range resp.Skills {
		pct := 0
		if s.NextLvlXP > 0 {
			pct = s.XP * 100 / s.NextLvlXP
		}
		rows = append(rows, skillRow{
			Name:     s.Name,
			Category: s.Category,
			Level:    s.Level,
			XP:       s.XP,
			NextXP:   s.NextLvlXP,
			Pct:      pct,
		})
	}
	slices.SortFunc(rows, func(a, b skillRow) int {
		if c := strings.Compare(a.Category, b.Category); c != 0 {
			return c
		}
		return strings.Compare(a.Name, b.Name)
	})

	// Sub-column widths within "Progress to Next": align XP, NextXP, and pct
	// independently so the "/" and "(NN%)" tokens line up across rows.
	nameW, catW, lvlW := len("Skill"), len("Category"), len("Level")
	xpW, nextW, pctW := 0, 0, 0
	for _, r := range rows {
		nameW = max(nameW, len(r.Name))
		catW = max(catW, len(r.Category))
		lvlW = max(lvlW, len(strconv.Itoa(r.Level)))
		xpW = max(xpW, len(strconv.Itoa(r.XP)))
		nextW = max(nextW, len(strconv.Itoa(r.NextXP)))
		pctW = max(pctW, len(strconv.Itoa(r.Pct)))
	}
	// Progress format: "<xp> / <next> (<pct>%)" — total width of the cell.
	progW := xpW + 3 + nextW + 2 + pctW + 3 // " / " + " (" + "%)"
	if progW < len("Progress to Next") {
		progW = len("Progress to Next")
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Skills (%d)\n", len(rows))
	fmt.Fprintf(&b, "  %-*s | %-*s | %*s | %*s\n", nameW, "Skill", catW, "Category", lvlW, "Level", progW, "Progress to Next")
	fmt.Fprintf(&b, "  %s-+-%s-+-%s-+-%s\n",
		strings.Repeat("-", nameW), strings.Repeat("-", catW),
		strings.Repeat("-", lvlW), strings.Repeat("-", progW))

	for _, r := range rows {
		prog := fmt.Sprintf("%*d / %*d (%*d%%)", xpW, r.XP, nextW, r.NextXP, pctW, r.Pct)
		fmt.Fprintf(&b, "  %-*s | %-*s | %*d | %*s\n",
			nameW, r.Name, catW, r.Category, lvlW, r.Level, progW, prog)
	}
	return b.String()
}

// formatWithdraw formats a withdraw_items response as a one-line summary.
func formatWithdraw(raw []byte) string {
	return formatItemTransfer(raw, "storage", "cargo")
}

// formatRefuel formats a refuel response as a one-line summary.
func formatRefuel(raw []byte) string {
	raw = unwrapActionResult(raw)
	var resp struct {
		Source    string `json:"source"`
		Fuel      int    `json:"fuel"`
		FuelNow   int    `json:"fuel_now"`
		FuelMax   int    `json:"fuel_max"`
		Cost      int    `json:"cost"`
		CellsUsed int    `json:"cells_used"`
		ItemName  string `json:"item_name"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}
	source := resp.Source
	if source == "" {
		source = "(unknown)"
	}
	if resp.Fuel == 0 && resp.FuelNow > 0 && resp.FuelMax > 0 {
		return fmt.Sprintf("Refueled at %s.  Tank: %d/%d (cost %d credits).", source, resp.FuelNow, resp.FuelMax, resp.Cost)
	}
	if resp.CellsUsed > 0 && resp.ItemName != "" {
		return fmt.Sprintf("Refueled at %s.  %d units from %d × %s.", source, resp.Fuel, resp.CellsUsed, resp.ItemName)
	}
	return fmt.Sprintf("Refueled at %s.  %d units for %d credits.", source, resp.Fuel, resp.Cost)
}

// formatJettison formats a jettison response as a one-line summary.
func formatJettison(raw []byte) string {
	raw = unwrapActionResult(raw)
	var resp struct {
		ContainerID string  `json:"container_id"`
		ItemID      string  `json:"item_id"`
		Quantity    float64 `json:"quantity"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}
	return fmt.Sprintf("Jettisoned %s %s into cannister %q.", formatFloat(resp.Quantity), resp.ItemID, resp.ContainerID)
}

// formatLootWreck formats a loot_wreck response as a one-line summary.
func formatLootWreck(raw []byte) string {
	raw = unwrapActionResult(raw)
	var resp struct {
		ItemID     string  `json:"item_id"`
		Quantity   float64 `json:"quantity"`
		WreckEmpty bool    `json:"wreck_empty"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}
	msg := fmt.Sprintf("Looted %s %s from cannister.", formatFloat(resp.Quantity), resp.ItemID)
	if !resp.WreckEmpty {
		msg += " There are still more items in the cannister."
	}
	return msg
}

// writePlayerTable writes a sorted player table to b.
func writePlayerTable(b *strings.Builder, players []nearbyPlayer) {
	if len(players) == 0 {
		b.WriteString("  (no players nearby)\n")
		return
	}

	slices.SortFunc(players, func(a, c nearbyPlayer) int {
		return strings.Compare(strings.ToLower(a.Username), strings.ToLower(c.Username))
	})

	nameW, tagW, shipW, combatW := runewidth.StringWidth("Username"), runewidth.StringWidth("Faction"), runewidth.StringWidth("Ship"), runewidth.StringWidth("Combat")
	for _, p := range players {
		nameW = max(nameW, runewidth.StringWidth(p.Username))
		tagW = max(tagW, runewidth.StringWidth(p.FactionTag))
		shipW = max(shipW, runewidth.StringWidth(p.ShipClass))
	}

	fmt.Fprintf(b, "%s | %s | %s | %s\n",
		padRight("Username", nameW), padRight("Faction", tagW),
		padRight("Ship", shipW), padRight("Combat", combatW))
	b.WriteString(strings.Repeat("-", nameW+tagW+shipW+combatW+9) + "\n")

	for _, p := range players {
		combat := "no combat"
		if p.InCombat {
			combat = "COMBAT"
		}
		// Colorize name at natural length, then pad with spaces for alignment.
		name := colorizeHex(p.Username, p.PrimaryColor, p.SecondaryColor)
		pad := nameW - runewidth.StringWidth(p.Username)
		if pad > 0 {
			name += strings.Repeat(" ", pad)
		}
		fmt.Fprintf(b, "%s | %s | %s | %s |\n",
			name, padRight(p.FactionTag, tagW),
			padRight(p.ShipClass, shipW), combat)
	}
}

// padRight pads s on the right with spaces so its display width equals w.
// Uses rune-width so multibyte/double-width characters align correctly.
func padRight(s string, w int) string {
	pad := w - runewidth.StringWidth(s)
	if pad <= 0 {
		return s
	}
	return s + strings.Repeat(" ", pad)
}

// formatFloat formats a float64 nicely — as integer if whole, otherwise with decimals.
func formatFloat(f float64) string {
	if f == float64(int64(f)) {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'f', 2, 64)
}

func executeCommand(client game.GameClient, ctx context.Context, parts []string, format outputFormat) error {
	// Resolve $TOKEN$ variables against live state before dispatch. This is the
	// single chokepoint for all command paths (bare commands, single-form loops
	// via runLoopSingle, and block loops via the runStatement closure), so token
	// substitution works uniformly everywhere. An unresolved token returns a
	// *tokenError, which loops treat as a fatal abort.
	resolved, rerr := worker.ResolveTokens(parts, client.GetState())
	if rerr != nil {
		return rerr
	}
	parts = resolved
	cmd := strings.ToLower(parts[0])

	fmt.Printf("▶ Executing: %s %s\n", cmd, strings.Join(parts[1:], " "))

	switch cmd {
	case "sleep", "wait":
		if len(parts) < 2 {
			return fmt.Errorf("usage: %s <seconds | duration like 30s, 1m, 500ms>", cmd)
		}
		arg := parts[1]
		var d time.Duration
		if n, err := strconv.ParseFloat(arg, 64); err == nil {
			if n < 0 {
				return fmt.Errorf("%s: duration must be non-negative", cmd)
			}
			d = time.Duration(n * float64(time.Second))
		} else {
			parsed, perr := time.ParseDuration(arg)
			if perr != nil {
				return fmt.Errorf("%s: cannot parse %q as duration: %w", cmd, arg, perr)
			}
			if parsed < 0 {
				return fmt.Errorf("%s: duration must be non-negative", cmd)
			}
			d = parsed
		}
		fmt.Printf("⏸  Sleeping %s...\n", d)
		select {
		case <-time.After(d):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}

	// === NAVIGATION ===
	case "undock":
		return simpleCommand(client, func(ctx context.Context) error {
			err := client.Undock(ctx)
			return reconcileDockState(ctx, client, "undock", err)
		}, ctx, 12*time.Second, cmd, format)

	case "dock":
		err := simpleCommand(client, func(ctx context.Context) error {
			err := client.Dock(ctx)
			return reconcileDockState(ctx, client, "dock", err)
		}, ctx, 3*time.Second, cmd, format)
		if err == nil {
			// Best-effort: pull the local market so the demand ledger fills as
			// you travel. Stations without a market simply error and are ignored.
			if mErr := client.GetListings(ctx); mErr == nil {
				captureMarket(client, ctx)
			}
		}
		return err

	case "travel":
		if len(parts) < 2 {
			return fmt.Errorf("usage: travel <poi-id>")
		}
		target := strings.Join(parts[1:], " ")

		// Estimate travel time before sending the command.
		est := estimateTravel(client, target)
		if est.valid {
			fmt.Printf("⏱ Distance: %.1f AU | Speed: %.1f | Est. %d tick(s) (~%ds) | Est. fuel: %d\n",
				est.distance, est.speed, est.ticks, est.ticks*10, est.fuel)
		}

		// Server blocks until travel completes.
		result, err := client.Travel(ctx, target)
		if err != nil {
			return err
		}
		// Compare server-reported arrival vs. our pre-travel estimate so
		// we can spot drift in the estimator (distance/speed formula).
		if est.valid && result != nil && result.ArrivalTick > 0 && result.StartTick > 0 {
			actualTicks := int(result.ArrivalTick - result.StartTick)
			delta := actualTicks - est.ticks
			sign := "+"
			if delta < 0 {
				sign = "-"
				delta = -delta
			}
			if actualTicks == est.ticks {
				fmt.Printf("⏱ Actual: %d tick(s) — matches estimate\n", actualTicks)
			} else {
				fmt.Printf("⏱ Actual: %d tick(s) (~%ds) | estimate off by %s%d tick(s)\n",
					actualTicks, actualTicks*10, sign, delta)
			}
		}
		showLastResponse(client, format, cmd)
		return nil

	case "jump":
		if len(parts) < 2 {
			return fmt.Errorf("usage: jump <system-id>")
		}
		// Show jump distance, time, and fuel estimate from connection and ship data.
		if state := client.GetState(); state != nil {
			for _, conn := range state.System.Connections {
				if strings.EqualFold(conn.SystemID, parts[1]) || strings.EqualFold(conn.Name, parts[1]) {
					jumpTicks := max(1, 7-int(state.Ship.Speed))
					// Fuel: ceil(scale^1.5 × speed × 10.0 × 0.10)
					jumpFuel := 1
					if raw := client.GetRawJSON("ship"); len(raw) > 0 {
						var shipResp struct {
							Class *struct {
								Scale     int `json:"scale"`
								BaseSpeed int `json:"base_speed"`
							} `json:"class"`
						}
						if err := json.Unmarshal(raw, &shipResp); err == nil && shipResp.Class != nil {
							scale := float64(shipResp.Class.Scale)
							spd := float64(shipResp.Class.BaseSpeed)
							if scale > 0 && spd > 0 {
								jumpFuel = max(1, int(math.Ceil(math.Pow(scale, 1.5)*spd*10.0*0.10)))
							}
						}
					}
					fmt.Printf("⏱ Jump distance: %d ly | Est. %d tick(s) (~%ds) | Est. fuel: %d\n",
						conn.Distance, jumpTicks, jumpTicks*10, jumpFuel)
					break
				}
			}
		}
		// Server blocks until jump completes.
		_, err := client.Jump(ctx, parts[1])
		if err != nil {
			return err
		}
		showLastResponse(client, format, cmd)
		_ = client.GetSystem(ctx) // Refresh system data (POIs, connections) for the new system.
		return nil

	// === MINING & SCANNING ===
	case "mine":
		return simpleCommand(client, client.Mine, ctx, 12*time.Second, cmd, format)

	case "scan":
		// Support both: scan <target> and scan --target <target>
		var target string
		if len(parts) >= 2 && !strings.HasPrefix(parts[1], "--") {
			// Positional form: scan ThomasEdison
			target = parts[1]
		} else {
			// Flag form: scan --target ThomasEdison or scan --target=ThomasEdison
			flags, err := parseFlagArgs(parts[1:], "target")
			if err != nil {
				return err
			}
			if t, ok := flags["target"]; ok {
				target = t.(string)
			}
		}
		if target == "" {
			// Default to area scan if no target specified
			return simpleCommand(client, client.Scan, ctx, 3*time.Second, cmd, format)
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.ScanTarget(ctx, target)
		}, ctx, 3*time.Second, cmd, format)

	case "survey":
		return simpleCommand(client, client.SurveySystem, ctx, 15*time.Second, cmd, format)

	case "survey_system":
		// Rich survey: loop until no more hidden POIs are revealed, store
		// newly revealed POIs (with resource data) to the KB, and report
		// aggregate XP gained.
		surveySystem(client, ctx, format)
		return nil

	// === COMBAT ===
	case "attack":
		if len(parts) < 2 {
			return fmt.Errorf("usage: attack <target-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.Attack(ctx, parts[1])
		}, ctx, 3*time.Second, cmd, format)

	case "cloak":
		enable := len(parts) >= 2 && (parts[1] == "on" || parts[1] == "true" || parts[1] == "1")
		return simpleCommand(client, func(ctx context.Context) error {
			return client.Cloak(ctx, enable)
		}, ctx, 2*time.Second, cmd, format)

	case "battle":
		if len(parts) < 2 {
			return fmt.Errorf("usage: battle <action> [--stance <stance>] [--target_id <id>] [--side_id <id>]")
		}
		action := parts[1]
		payload := map[string]any{"action": action}
		flags, err := parseFlagArgs(parts[2:], "stance", "target_id", "side_id")
		if err != nil {
			return err
		}
		for k, v := range flags {
			if k == "side_id" {
				if n, ok := flagInt(v); ok {
					payload[k] = n
				}
			} else {
				payload[k] = v
			}
		}
		// advance/retreat/engage are battle moves that resolve over a full
		// game tick — gate the next command on a full tick so a loop issues
		// at most one per tick rather than spamming several within one tick.
		// stance/target are instantaneous configuration; keep them snappy.
		battleWait := 3 * time.Second
		switch action {
		case "advance", "retreat", "engage":
			battleWait = game.SleepTick
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.Battle(ctx, action, payload)
		}, ctx, battleWait, cmd, format)

	case "reload":
		if len(parts) < 3 {
			return fmt.Errorf("usage: reload <weapon-instance-id> <ammo-item-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.Reload(ctx, parts[1], strings.ToLower(parts[2]))
		}, ctx, 3*time.Second, cmd, format)

	case "distress_signal":
		var distressType string
		if len(parts) >= 2 {
			distressType = parts[1]
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.DistressSignal(ctx, distressType)
		}, ctx, 2*time.Second, cmd, format)

	// === COMMERCE ===
	case "sell":
		if len(parts) < 3 {
			return fmt.Errorf("usage: sell <item-id> <quantity>")
		}
		qty, err := parseQuantity(parts[2])
		if err != nil {
			return fmt.Errorf("invalid quantity: %w", err)
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.Sell(ctx, strings.ToLower(parts[1]), qty)
		}, ctx, 3*time.Second, cmd, format)

	case "sell_all_bulk":
		return simpleCommand(client, func(ctx context.Context) error {
			return client.SellAllBulk(ctx, nil)
		}, ctx, 5*time.Second, cmd, format)

	case "buy":
		if len(parts) < 3 {
			return fmt.Errorf("usage: buy <item-id> <quantity>")
		}
		qty, err := parseQuantity(parts[2])
		if err != nil {
			return fmt.Errorf("invalid quantity: %w", err)
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.Buy(ctx, strings.ToLower(parts[1]), qty)
		}, ctx, 3*time.Second, cmd, format)

	case "listings", "get_listings":
		return simpleCommand(client, client.GetListings, ctx, 2*time.Second, cmd, format)

	case "trades", "get_trades":
		return simpleCommand(client, client.GetTrades, ctx, 2*time.Second, cmd, format)

	case "view_market":
		if len(parts) < 2 {
			err := simpleCommand(client, func(ctx context.Context) error {
				return client.ViewMarket(ctx, nil)
			}, ctx, 2*time.Second, cmd, format)
			captureMarket(client, ctx)
			return err
		}
		// First non-flag arg is item_id; also accept --item_id and --category flags
		payload := make(map[string]any)
		for i := 1; i < len(parts); i++ {
			arg := parts[i]
			if key, ok := strings.CutPrefix(arg, "--"); ok {
				if k, v, ok2 := strings.Cut(key, "="); ok2 {
					payload[k] = v
				} else if i+1 < len(parts) {
					i++
					payload[key] = parts[i]
				}
			} else if payload["item_id"] == nil {
				payload["item_id"] = arg
			}
		}
		if v, ok := payload["item_id"].(string); ok {
			payload["item_id"] = strings.ToLower(v)
		}
		// since is a schema integer; coerce so it isn't sent as a JSON string.
		if v, ok := payload["since"].(string); ok {
			if n, err := strconv.Atoi(v); err == nil {
				payload["since"] = n
			}
		}
		err := simpleCommand(client, func(ctx context.Context) error {
			return client.ViewMarket(ctx, payload)
		}, ctx, 2*time.Second, cmd, format)
		// Only the full compact summary (no item_id, no category) feeds the
		// demand ledger; per-item or category-scoped calls are not captured.
		if payload["item_id"] == nil && payload["category"] == nil {
			captureMarket(client, ctx)
		}
		return err

	case "update_market":
		if globalMarketCollector == nil {
			return fmt.Errorf("update_market: market db not configured (set --market-db-path)")
		}
		if err := simpleCommand(client, func(ctx context.Context) error {
			return client.ViewMarket(ctx, nil)
		}, ctx, 2*time.Second, cmd, format); err != nil {
			return err
		}
		if err := market.CaptureFromClient(ctx, client, globalMarketCollector); err != nil {
			return fmt.Errorf("update_market: capture: %w", err)
		}
		station := ""
		if state := client.GetState(); state != nil {
			station = state.CurrentPOI
		}
		fmt.Printf("✓ Captured market data for %s\n", station)
		return nil

	case "view_orders":
		if len(parts) > 1 {
			// parseFlagArgs already converts numeric values to int, so page /
			// page_size land here as int and the old strconv.Atoi(v.(string))
			// re-conversion panicked on its very first numeric flag. Drop it.
			payload, err := parseFlagArgs(parts[1:], "item_id", "order_type", "page", "page_size", "scope", "search", "sort_by", "station_id")
			if err != nil {
				return err
			}
			if v, ok := flagString(payload["item_id"]); ok {
				payload["item_id"] = strings.ToLower(v)
			}
			return simpleCommand(client, func(ctx context.Context) error {
				return client.RawCommand(ctx, "view_orders", payload)
			}, ctx, 2*time.Second, cmd, format)
		}
		return simpleCommand(client, client.ViewOrders, ctx, 2*time.Second, cmd, format)

	case "create_sell_order":
		if len(parts) < 4 {
			return fmt.Errorf("usage: create_sell_order <item-id> <quantity> <price-each>")
		}
		qty, err := strconv.Atoi(parts[2])
		if err != nil {
			return fmt.Errorf("invalid quantity: %w", err)
		}
		price, err := strconv.Atoi(parts[3])
		if err != nil {
			return fmt.Errorf("invalid price: %w", err)
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.CreateSellOrder(ctx, map[string]any{
				"item_id":    strings.ToLower(parts[1]),
				"quantity":   qty,
				"price_each": price,
			})
		}, ctx, 3*time.Second, cmd, format)

	case "create_buy_order":
		if len(parts) < 4 {
			return fmt.Errorf("usage: create_buy_order <item-id> <quantity> <price-each>")
		}
		qty, err := strconv.Atoi(parts[2])
		if err != nil {
			return fmt.Errorf("invalid quantity: %w", err)
		}
		price, err := strconv.Atoi(parts[3])
		if err != nil {
			return fmt.Errorf("invalid price: %w", err)
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.CreateBuyOrder(ctx, map[string]any{
				"item_id":    strings.ToLower(parts[1]),
				"quantity":   qty,
				"price_each": price,
			})
		}, ctx, 3*time.Second, cmd, format)

	case "cancel_order":
		if len(parts) < 2 {
			return fmt.Errorf("usage: cancel_order <order-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.CancelOrder(ctx, map[string]any{"order_id": parts[1]})
		}, ctx, 3*time.Second, cmd, format)

	case "modify_order":
		if len(parts) < 2 {
			return fmt.Errorf("usage: modify_order <order-id> --new_price <price>")
		}
		payload := map[string]any{"order_id": parts[1]}
		flags, err := parseFlagArgs(parts[2:], "new_price")
		if err != nil {
			return err
		}
		if v, ok := flags["new_price"]; ok {
			if n, ok := flagInt(v); ok {
				payload["new_price"] = n
			}
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.ModifyOrder(ctx, payload)
		}, ctx, 3*time.Second, cmd, format)

	case "estimate_purchase":
		if len(parts) < 3 {
			return fmt.Errorf("usage: estimate_purchase <item-id> <quantity>")
		}
		qty, err := strconv.Atoi(parts[2])
		if err != nil {
			return fmt.Errorf("invalid quantity: %w", err)
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.EstimatePurchase(ctx, strings.ToLower(parts[1]), qty)
		}, ctx, 2*time.Second, cmd, format)

	case "list_ship_for_sale":
		if len(parts) < 3 {
			return fmt.Errorf("usage: list_ship_for_sale <ship-id> <price>")
		}
		price, err := strconv.Atoi(parts[2])
		if err != nil {
			return fmt.Errorf("invalid price: %w", err)
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.ListShipForSale(ctx, parts[1], float64(price))
		}, ctx, 3*time.Second, cmd, format)

	case "name_ship":
		if len(parts) < 2 {
			return fmt.Errorf("usage: name_ship <name>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "name_ship", map[string]any{
				"name": strings.Join(parts[1:], " "),
			})
		}, ctx, 2*time.Second, cmd, format)

	case "commission_quote":
		if len(parts) < 2 {
			return fmt.Errorf("usage: commission_quote <ship-class>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.CommissionQuote(ctx, parts[1])
		}, ctx, 2*time.Second, cmd, format)

	case "commission_status":
		var baseID string
		if len(parts) >= 2 {
			baseID = parts[1]
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.CommissionStatus(ctx, baseID)
		}, ctx, 2*time.Second, cmd, format)

	case "cancel_commission":
		if len(parts) < 2 {
			return fmt.Errorf("usage: cancel_commission <commission-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.CancelCommission(ctx, parts[1])
		}, ctx, 3*time.Second, cmd, format)

	case "commission_ship":
		if len(parts) < 2 {
			return fmt.Errorf("usage: commission_ship <ship-class> [--provide_materials true|false]")
		}
		payload := map[string]any{}
		// Parse positional and flags
		for i := 1; i < len(parts); i++ {
			arg := parts[i]
			if key, ok := strings.CutPrefix(arg, "--"); ok {
				if k, v, ok2 := strings.Cut(key, "="); ok2 {
					payload[k] = v
				} else if i+1 < len(parts) {
					i++
					payload[key] = parts[i]
				}
			} else if payload["ship_class"] == nil {
				payload["ship_class"] = arg
			}
		}
		// Convert provide_materials to bool
		if v, ok := payload["provide_materials"]; ok {
			payload["provide_materials"] = flagBool(v)
		}
		shipClass, _ := payload["ship_class"].(string)
		provideMaterials, _ := payload["provide_materials"].(bool)
		return simpleCommand(client, func(ctx context.Context) error {
			return client.CommissionShip(ctx, shipClass, provideMaterials)
		}, ctx, 5*time.Second, cmd, format)

	case "supply_commission":
		if len(parts) < 4 {
			return fmt.Errorf("usage: supply_commission <commission-id> <item-id> <quantity>")
		}
		qty, err := strconv.Atoi(parts[3])
		if err != nil {
			return fmt.Errorf("invalid quantity: %w", err)
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "supply_commission", map[string]any{
				"commission_id": parts[1], "item_id": strings.ToLower(parts[2]), "quantity": qty,
			})
		}, ctx, 3*time.Second, cmd, format)

	case "trade_offer":
		if len(parts) < 2 {
			return fmt.Errorf("usage: trade_offer <target-id> [--offer_credits <n>] [--request_credits <n>]")
		}
		tradePayload := map[string]any{}
		flags, err := parseFlagArgs(parts[2:], "offer_credits", "request_credits")
		if err != nil {
			return err
		}
		for _, k := range []string{"offer_credits", "request_credits"} {
			if v, ok := flags[k]; ok {
				if n, ok := flagInt(v); ok {
					tradePayload[k] = n
				}
			}
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.TradeOffer(ctx, parts[1], tradePayload)
		}, ctx, 3*time.Second, cmd, format)

	case "trade_accept":
		if len(parts) < 2 {
			return fmt.Errorf("usage: trade_accept <trade-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.TradeAccept(ctx, parts[1])
		}, ctx, 3*time.Second, cmd, format)

	case "trade_cancel":
		if len(parts) < 2 {
			return fmt.Errorf("usage: trade_cancel <trade-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.TradeCancel(ctx, parts[1])
		}, ctx, 3*time.Second, cmd, format)

	case "trade_decline":
		if len(parts) < 2 {
			return fmt.Errorf("usage: trade_decline <trade-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.TradeDecline(ctx, parts[1])
		}, ctx, 3*time.Second, cmd, format)

	// === CRAFTING ===
	case "craft":
		craftArgs, flags := partitionFlagsKV(parts[1:])
		// `craft queue` (or action=queue / --action=queue) lists current jobs
		// instead of queuing.
		if (len(craftArgs) >= 1 && craftArgs[0] == "queue") || flags["action"] == "queue" {
			return simpleCommand(client, func(ctx context.Context) error {
				return client.RawCommand(ctx, "craft", map[string]any{"action": "queue"})
			}, ctx, 0, cmd, format)
		}
		// `craft --file <path>` reads a JSON array of job objects and submits
		// them as a single bulk request (each job queued independently).
		if path := flags["file"]; path != "" {
			jobs, err := loadCraftJobs(path)
			if err != nil {
				return err
			}
			return simpleCommand(client, func(ctx context.Context) error {
				return client.CraftBulk(ctx, jobs)
			}, ctx, 5*time.Second, cmd, format)
		}
		if len(craftArgs) < 1 {
			return fmt.Errorf("usage: craft <recipe-id> [quantity] [--deliver_to=storage|faction] [--facility_id=ID] [--preset=fast|cheap|workshop] [--dry_run] | craft --file <path.json> | craft queue")
		}
		recipeID := craftArgs[0]
		qty := 1
		if len(craftArgs) >= 2 {
			n, err := strconv.Atoi(craftArgs[1])
			if err != nil {
				return fmt.Errorf("invalid quantity: %w", err)
			}
			qty = n
		}
		deliverTo := flags["deliver_to"]
		switch deliverTo {
		case "", "storage", "faction":
		default:
			return fmt.Errorf("invalid deliver_to %q (must be storage or faction)", deliverTo)
		}
		preset := flags["preset"]
		switch preset {
		case "", "fast", "cheap", "workshop":
		default:
			return fmt.Errorf("invalid preset %q (must be fast, cheap, or workshop)", preset)
		}
		_, dryRun := flags["dry_run"]
		facilityID := flags["facility_id"]
		// Fast path: plain craft with no advanced flags uses the typed client
		// method (correct async terminator, validated quantity).
		if !dryRun && preset == "" && facilityID == "" {
			return simpleCommand(client, func(ctx context.Context) error {
				return client.CraftWithOptions(ctx, recipeID, qty, deliverTo)
			}, ctx, 5*time.Second, cmd, format)
		}
		// Advanced path: build the full payload and submit generically.
		payload := map[string]any{"recipe_id": recipeID, "quantity": qty}
		if deliverTo != "" {
			payload["deliver_to"] = deliverTo
		}
		if facilityID != "" {
			payload["facility_id"] = facilityID
		}
		if preset != "" {
			payload["preset"] = preset
		}
		if dryRun {
			payload["dry_run"] = true
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "craft", payload)
		}, ctx, 0, cmd, format)

	case "recycle":
		recArgs, flags := partitionFlagsKV(parts[1:])
		if (len(recArgs) >= 1 && recArgs[0] == "queue") || flags["action"] == "queue" {
			// recycle jobs appear in the shared craft queue.
			return simpleCommand(client, func(ctx context.Context) error {
				return client.RawCommand(ctx, "craft", map[string]any{"action": "queue"})
			}, ctx, 0, cmd, format)
		}
		if len(recArgs) < 1 {
			return fmt.Errorf("usage: recycle <recipe-id> [quantity] [--deliver_to=storage|faction] [--facility_id=ID] [--dry_run]")
		}
		recipeID := recArgs[0]
		qty := 1
		if len(recArgs) >= 2 {
			n, err := strconv.Atoi(recArgs[1])
			if err != nil {
				return fmt.Errorf("invalid quantity: %w", err)
			}
			qty = n
		}
		deliverTo := flags["deliver_to"]
		switch deliverTo {
		case "", "storage", "faction":
		default:
			return fmt.Errorf("invalid deliver_to %q (must be storage or faction)", deliverTo)
		}
		_, dryRun := flags["dry_run"]
		facilityID := flags["facility_id"]
		if !dryRun && facilityID == "" {
			return simpleCommand(client, func(ctx context.Context) error {
				return client.RecycleWithOptions(ctx, recipeID, qty, deliverTo)
			}, ctx, 5*time.Second, cmd, format)
		}
		payload := map[string]any{"recipe_id": recipeID, "quantity": qty}
		if deliverTo != "" {
			payload["deliver_to"] = deliverTo
		}
		if facilityID != "" {
			payload["facility_id"] = facilityID
		}
		if dryRun {
			payload["dry_run"] = true
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "recycle", payload)
		}, ctx, 0, cmd, format)

	case "recipes", "get_recipes":
		return simpleCommand(client, client.GetRecipes, ctx, 2*time.Second, cmd, format)

	case "craftable":
		return handleCraftable(client, ctx, parts, ensureCraftingDB(), format)

	case "plan":
		return handlePlan(client, ctx, parts, ensureCraftingDB(), format)

	// === SHIP MAINTENANCE ===
	case "refuel":
		if len(parts) > 1 {
			payload, err := parseFlagArgs(parts[1:], "item_id", "quantity", "target")
			if err != nil {
				return err
			}
			if v, ok := payload["quantity"]; ok {
				if n, ok := flagInt(v); ok {
					payload["quantity"] = n
				}
			}
			if v, ok := flagString(payload["item_id"]); ok {
				payload["item_id"] = strings.ToLower(v)
			}
			return simpleCommand(client, func(ctx context.Context) error {
				return client.RawCommand(ctx, "refuel", payload)
			}, ctx, 3*time.Second, cmd, format)
		}
		return simpleCommand(client, client.Refuel, ctx, 3*time.Second, cmd, format)

	case "repair":
		if len(parts) > 1 {
			payload, err := parseFlagArgs(parts[1:], "item_id", "quantity", "target")
			if err != nil {
				return err
			}
			if v, ok := payload["quantity"]; ok {
				if n, ok := flagInt(v); ok {
					payload["quantity"] = n
				}
			}
			if v, ok := flagString(payload["item_id"]); ok {
				payload["item_id"] = strings.ToLower(v)
			}
			return simpleCommand(client, func(ctx context.Context) error {
				return client.RepairWith(ctx, payload)
			}, ctx, 3*time.Second, cmd, format)
		}
		return simpleCommand(client, client.Repair, ctx, 3*time.Second, cmd, format)

	case "install", "install_mod":
		if len(parts) < 2 {
			return fmt.Errorf("usage: install <item-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.InstallMod(ctx, strings.ToLower(parts[1]))
		}, ctx, 3*time.Second, cmd, format)

	case "uninstall", "uninstall_mod":
		if len(parts) < 2 {
			return fmt.Errorf("usage: uninstall <module-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.UninstallMod(ctx, parts[1])
		}, ctx, 3*time.Second, cmd, format)

	case "buy_ship":
		if len(parts) < 2 {
			return fmt.Errorf("usage: buy_ship <ship-class>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.BuyShip(ctx, parts[1])
		}, ctx, 5*time.Second, cmd, format)

	case "buy_listed_ship":
		if len(parts) < 2 {
			return fmt.Errorf("usage: buy_listed_ship <listing-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.BuyListedShip(ctx, parts[1])
		}, ctx, 5*time.Second, cmd, format)

	case "browse_ships":
		var payload map[string]any
		if len(parts) > 1 {
			payload = make(map[string]any)
			for i := 1; i < len(parts); i++ {
				arg := parts[i]
				if key, ok := strings.CutPrefix(arg, "--"); ok {
					if k, v, ok2 := strings.Cut(key, "="); ok2 {
						payload[k] = v
					} else if i+1 < len(parts) {
						i++
						payload[key] = parts[i]
					}
				}
			}
		}
		// max_price is a schema integer; coerce so it isn't sent as a string.
		// (Safe when payload is nil: the assertion on a nil map is a no-op.)
		if v, ok := payload["max_price"].(string); ok {
			if n, err := strconv.Atoi(v); err == nil {
				payload["max_price"] = n
			}
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.BrowseShips(ctx, payload)
		}, ctx, 2*time.Second, cmd, format)

	case "list_ships":
		return simpleCommand(client, client.ListShips, ctx, 2*time.Second, cmd, format)

	case "switch_ship":
		if len(parts) < 2 {
			return fmt.Errorf("usage: switch_ship <ship-id>")
		}
		err := simpleCommand(client, func(ctx context.Context) error {
			return client.SwitchShip(ctx, parts[1])
		}, ctx, 5*time.Second, cmd, format)
		if err == nil {
			_ = client.GetShip(ctx) // Refresh ship data for new ship.
			invalidateSurveyScannerCache()
		}
		return err

	case "sell_ship":
		if len(parts) < 2 {
			return fmt.Errorf("usage: sell_ship <ship-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.SellShip(ctx, parts[1])
		}, ctx, 3*time.Second, cmd, format)

	// === DRONES ===
	// These dispatch to the real client methods (which Submit and await the
	// terminal action_result/action_error) instead of the generic passthrough,
	// so deferred drone mutations are actually waited on rather than returning
	// on the synchronous "pending" ack.
	case "get_drones":
		return simpleCommand(client, client.GetDrones, ctx, 2*time.Second, cmd, format)

	case "get_drone":
		droneID := resolveArg(parts[1:], "drone_id")
		if droneID == "" {
			return fmt.Errorf("usage: get_drone <drone-id>  (or --drone_id <id>)")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.GetDrone(ctx, droneID)
		}, ctx, 2*time.Second, cmd, format)

	case "load_drone":
		itemID := resolveArg(parts[1:], "item_id")
		if itemID == "" {
			return fmt.Errorf("usage: load_drone <item-id>  (or --item_id <id>; e.g. mining_drone, combat_drone)")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.LoadDrone(ctx, strings.ToLower(itemID))
		}, ctx, 2*time.Second, cmd, format)

	case "unload_drone":
		droneID := resolveArg(parts[1:], "drone_id")
		if droneID == "" {
			return fmt.Errorf("usage: unload_drone <drone-id>  (or --drone_id <id>)")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.UnloadDrone(ctx, droneID)
		}, ctx, 2*time.Second, cmd, format)

	case "deploy_drone":
		// --all (bool flag) or positional/no-arg "all" deploys every in-bay
		// drone at the current location in one tick; --drone_id <id> or a
		// bare ID deploys a single drone. The server's per-drone bandwidth
		// check still applies, so over-bandwidth drones are silently skipped.
		args := parts[1:]
		all := false
		for _, a := range args {
			if a == "--all" {
				all = true
			}
		}
		droneID := ""
		if !all {
			droneID = resolveArg(args, "drone_id")
			if strings.EqualFold(droneID, "all") {
				all = true
				droneID = ""
			}
		}
		if !all && droneID == "" {
			return fmt.Errorf("usage: deploy_drone <drone-id>|--all  (or --drone_id <id>; see get_drones)")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.DeployDrone(ctx, droneID, all)
		}, ctx, 2*time.Second, cmd, format)

	case "bulk_upload_drone_script":
		return handleBulkUploadDroneScript(client, ctx, parts)

	case "set_drone_name", "name_drone":
		// usage: set_drone_name <drone-id> <name>...   (or pass --drone_id, --name)
		// Pass an empty name to clear. Names show in get_drones and pair with
		// drone_id on mining_yield events.
		positional, flags := partitionFlags(parts[1:])
		droneID := flags["drone_id"]
		name := flags["name"]
		if droneID == "" && len(positional) >= 1 {
			droneID = positional[0]
		}
		if name == "" && len(positional) >= 2 {
			// Rejoin the remaining positionals so multi-word names work
			// without quoting (e.g. `set_drone_name abc Lucky Miner`).
			name = strings.Join(positional[1:], " ")
		}
		if droneID == "" {
			return fmt.Errorf("usage: set_drone_name <drone-id> <name>...  (or --drone_id <id> --name <n>; empty name clears)")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.SetDroneName(ctx, droneID, name)
		}, ctx, 2*time.Second, cmd, format)

	case "recall_drone":
		// --all (bool flag) or positional/no-arg "all" recalls every drone at
		// the current location; --drone_id <id> or a bare ID recalls one.
		args := parts[1:]
		all := false
		for _, a := range args {
			if a == "--all" {
				all = true
			}
		}
		droneID := ""
		if !all {
			droneID = resolveArg(args, "drone_id")
			if strings.EqualFold(droneID, "all") {
				all = true
				droneID = ""
			}
		}
		if !all && droneID == "" {
			all = true
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RecallDrone(ctx, droneID, all)
		}, ctx, 2*time.Second, cmd, format)

	case "upload_drone_script":
		// Two flags consumed here: --drone_id <id> / --drone_id=<id> identifies
		// the target drone (or first positional token); --file <path> /
		// --file=<path> loads the DroneLang source from a file, avoiding
		// shell-quoting headaches with multi-line scripts. Anything else is the
		// inline script body; --file takes precedence over inline tokens.
		args := parts[1:]
		droneID, filePath := "", ""
		var inline []string
		for i := 0; i < len(args); i++ {
			a := args[i]
			switch {
			case a == "--drone_id":
				if i+1 >= len(args) {
					return fmt.Errorf("upload_drone_script: --drone_id requires a value")
				}
				i++
				droneID = args[i]
			case strings.HasPrefix(a, "--drone_id="):
				droneID = strings.TrimPrefix(a, "--drone_id=")
			case a == "--file":
				if i+1 >= len(args) {
					return fmt.Errorf("upload_drone_script: --file requires a path")
				}
				i++
				filePath = args[i]
			case strings.HasPrefix(a, "--file="):
				filePath = strings.TrimPrefix(a, "--file=")
			default:
				inline = append(inline, a)
			}
		}
		if droneID == "" {
			if len(inline) == 0 {
				return fmt.Errorf("usage: upload_drone_script <drone-id> [<script...> | --file <path>]  (omit script to clear)")
			}
			droneID = inline[0]
			inline = inline[1:]
		}
		var script string
		if filePath != "" {
			data, err := os.ReadFile(filePath)
			if err != nil {
				return fmt.Errorf("upload_drone_script: read %s: %w", filePath, err)
			}
			script = string(data)
		} else {
			script = strings.Join(inline, " ")
		}
		if len(script) > 2000 {
			return fmt.Errorf("upload_drone_script: script is %d chars (max 2000)", len(script))
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.UploadDroneScript(ctx, droneID, script)
		}, ctx, 2*time.Second, cmd, format)

	// === INSURANCE ===
	case "buy_insurance":
		ticks := 100
		if len(parts) >= 2 {
			var err error
			ticks, err = strconv.Atoi(parts[1])
			if err != nil {
				return fmt.Errorf("invalid ticks: %w", err)
			}
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.BuyInsurance(ctx, ticks)
		}, ctx, 2*time.Second, cmd, format)

	case "claim_insurance":
		return simpleCommand(client, client.ClaimInsurance, ctx, 3*time.Second, cmd, format)

	case "get_insurance_quote", "insurance_quote":
		return simpleCommand(client, client.GetInsuranceQuote, ctx, 2*time.Second, "get_insurance_quote", format)

	// === CARGO & STORAGE ===
	case "cargo", "get_cargo":
		return simpleCommand(client, client.GetCargo, ctx, 2*time.Second, cmd, format)

	case "deposit", "deposit_items":
		if len(parts) < 3 {
			return fmt.Errorf("usage: deposit <item-id> <quantity> [--source=<scope>] [--target=<scope>]")
		}
		qty, err := parseQuantity(parts[2])
		if err != nil {
			return fmt.Errorf("invalid quantity: %w", err)
		}
		flags, err := parseFlagArgs(parts[3:], "source", "target")
		if err != nil {
			return err
		}
		if len(flags) > 0 {
			payload := map[string]any{
				"item_id":  strings.ToLower(parts[1]),
				"quantity": int(qty),
			}
			maps.Copy(payload, flags)
			return simpleCommand(client, func(ctx context.Context) error {
				return client.DepositItemsPayload(ctx, payload)
			}, ctx, 3*time.Second, cmd, format)
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.DepositItems(ctx, strings.ToLower(parts[1]), qty)
		}, ctx, 3*time.Second, cmd, format)

	case "deposit_all":
		return simpleCommand(client, client.DepositAllItems, ctx, 5*time.Second, cmd, format)

	case "withdraw", "withdraw_items":
		if len(parts) < 3 {
			return fmt.Errorf("usage: withdraw <item-id> <quantity> [--source=<scope>] [--target=<scope>]")
		}
		qty, err := parseQuantity(parts[2])
		if err != nil {
			return fmt.Errorf("invalid quantity: %w", err)
		}
		flags, err := parseFlagArgs(parts[3:], "source", "target")
		if err != nil {
			return err
		}
		if len(flags) > 0 {
			payload := map[string]any{
				"item_id":  strings.ToLower(parts[1]),
				"quantity": int(qty),
			}
			maps.Copy(payload, flags)
			return simpleCommand(client, func(ctx context.Context) error {
				return client.WithdrawItemsPayload(ctx, payload)
			}, ctx, 3*time.Second, cmd, format)
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.WithdrawItems(ctx, strings.ToLower(parts[1]), qty)
		}, ctx, 3*time.Second, cmd, format)

	case "storage", "view_storage":
		// Optional --station_id <id> (or station_id=<id>) routes to ViewStorageAt
		// so callers can inspect remote storage without docking. With no flag,
		// uses the current docked station (must have a storage service).
		//
		// Display-only flags: --group (alias --by-category) groups items into
		// catalog-derived category sections; --filter <substr> (alias --match,
		// or a bare positional arg) keeps only items whose item_id or name
		// contains the substring, case-insensitive.
		var stationID string
		var opts storageFmtOptions
		for i := 1; i < len(parts); i++ {
			arg := parts[i]
			switch {
			case strings.HasPrefix(arg, "--station_id="):
				stationID = strings.TrimPrefix(arg, "--station_id=")
			case arg == "--station_id" && i+1 < len(parts):
				i++
				stationID = parts[i]
			case strings.HasPrefix(arg, "station_id="):
				stationID = strings.TrimPrefix(arg, "station_id=")
			case arg == "--group" || arg == "--by-category" || arg == "-g":
				opts.group = true
			case strings.HasPrefix(arg, "--filter="):
				opts.filter = strings.TrimPrefix(arg, "--filter=")
			case strings.HasPrefix(arg, "--match="):
				opts.filter = strings.TrimPrefix(arg, "--match=")
			case (arg == "--filter" || arg == "--match") && i+1 < len(parts):
				i++
				opts.filter = parts[i]
			case !strings.HasPrefix(arg, "-"):
				// Bare positional argument is treated as the filter substring.
				opts.filter = arg
			}
		}
		storageFmtOpts = opts
		defer func() { storageFmtOpts = storageFmtOptions{} }()
		if stationID != "" {
			return simpleCommand(client, func(ctx context.Context) error {
				return client.ViewStorageAt(ctx, stationID)
			}, ctx, 2*time.Second, cmd, format)
		}
		return simpleCommand(client, client.ViewStorage, ctx, 2*time.Second, cmd, format)

	case "storage_at":
		if len(parts) < 2 {
			return fmt.Errorf("usage: storage_at <station-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.ViewStorageAt(ctx, parts[1])
		}, ctx, 2*time.Second, cmd, format)

	case "jettison":
		if len(parts) < 3 {
			return fmt.Errorf("usage: jettison <item-id> <quantity>")
		}
		qty, err := parseQuantity(parts[2])
		if err != nil {
			return fmt.Errorf("invalid quantity: %w", err)
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.Jettison(ctx, strings.ToLower(parts[1]), qty)
		}, ctx, 2*time.Second, cmd, format)

	// === WRECKS ===
	case "wrecks", "get_wrecks":
		return simpleCommand(client, client.GetWrecks, ctx, 2*time.Second, cmd, format)

	case "loot", "loot_wreck":
		if len(parts) < 4 {
			return fmt.Errorf("usage: loot <wreck-id> <item-id> <quantity>")
		}
		qty, err := parseQuantity(parts[3])
		if err != nil {
			return fmt.Errorf("invalid quantity: %w", err)
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.LootWreck(ctx, parts[1], strings.ToLower(parts[2]), qty)
		}, ctx, 3*time.Second, cmd, format)

	case "salvage", "salvage_wreck":
		if len(parts) < 2 {
			return fmt.Errorf("usage: salvage <wreck-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.SalvageWreck(ctx, parts[1])
		}, ctx, 5*time.Second, cmd, format)

	case "tow", "tow_wreck":
		if len(parts) < 2 {
			return fmt.Errorf("usage: tow <wreck-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.TowWreck(ctx, parts[1])
		}, ctx, 3*time.Second, cmd, format)

	case "use_item":
		if len(parts) < 2 {
			return fmt.Errorf("usage: use_item <item-id> [quantity]")
		}
		useQty := 0
		if len(parts) >= 3 {
			if n, err := strconv.Atoi(parts[2]); err == nil {
				useQty = n
			}
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.UseItem(ctx, strings.ToLower(parts[1]), useQty)
		}, ctx, 3*time.Second, cmd, format)

	case "repair_module":
		if len(parts) < 2 {
			return fmt.Errorf("usage: repair_module <module-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "repair_module", map[string]any{"module_id": parts[1]})
		}, ctx, 3*time.Second, cmd, format)

	// === QUERIES ===
	case "status", "get_status":
		return simpleCommand(client, client.GetStatus, ctx, 2*time.Second, cmd, format)

	case "system", "get_system":
		return simpleCommand(client, client.GetSystem, ctx, 2*time.Second, cmd, format)

	case "ship", "get_ship":
		return simpleCommand(client, client.GetShip, ctx, 2*time.Second, cmd, format)

	case "skills", "get_skills":
		return simpleCommand(client, client.GetSkills, ctx, 2*time.Second, cmd, format)

	case "poi", "get_poi":
		return simpleCommand(client, client.GetPOI, ctx, 2*time.Second, cmd, format)

	case "base", "get_base":
		return simpleCommand(client, client.GetBase, ctx, 2*time.Second, cmd, format)

	case "map", "get_map":
		// Check for --system_id flag or bare force arg
		mapFlags, err := parseFlagArgs(parts[1:], "system_id")
		if err != nil {
			return err
		}
		if sysID, ok := mapFlags["system_id"]; ok {
			return simpleCommand(client, func(ctx context.Context) error {
				return client.RawCommand(ctx, "get_map", map[string]any{"system_id": sysID})
			}, ctx, 5*time.Second, cmd, format)
		}
		force := len(parts) >= 2 && (parts[1] == "force" || parts[1] == "1")
		return simpleCommand(client, func(ctx context.Context) error {
			return client.GetMap(ctx, force)
		}, ctx, 5*time.Second, cmd, format)

	case "nearby", "get_nearby":
		return simpleCommand(client, client.GetNearby, ctx, 2*time.Second, cmd, format)

	case "get_system_agents", "system_agents":
		return simpleCommand(client, client.GetSystemAgents, ctx, 2*time.Second, cmd, format)

	case "version", "get_version":
		return simpleCommand(client, client.GetVersion, ctx, 2*time.Second, cmd, format)

	case "find_route":
		if len(parts) < 2 {
			return fmt.Errorf("usage: find_route <system-id>")
		}
		route, err := client.FindRoute(ctx, parts[1])
		if err != nil {
			return err
		}
		fmt.Println("\n📍 Route:")
		for i, step := range route {
			fmt.Printf("  %d. %s (%d jumps)\n", i+1, step.Name, step.Jumps)
		}
		return nil

	case "nearest":
		if len(parts) < 2 {
			return fmt.Errorf("usage: nearest <poi_type>\nExample: nearest station")
		}
		return handleNearestCommand(ctx, client, parts[1:], format)

	case "catalog":
		if len(parts) < 2 {
			return fmt.Errorf("usage: catalog <type> [--page N] [--page_size N] [--search text] [--category cat] [--class cls] [--empire emp] [--id id] [--tier N]")
		}
		payload := map[string]any{"type": parts[1]}
		flags, err := parseFlagArgs(parts[2:], "page", "page_size", "search", "category", "class", "empire", "id", "tier", "commissionable")
		if err != nil {
			return err
		}
		for k, v := range flags {
			switch k {
			case "page", "page_size", "tier":
				if n, ok := flagInt(v); ok {
					payload[k] = n
				}
			case "commissionable":
				payload[k] = flagBool(v)
			default:
				payload[k] = v
			}
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "catalog", payload)
		}, ctx, 2*time.Second, cmd, format)

	case "search_systems":
		if len(parts) < 2 {
			return fmt.Errorf("usage: search_systems <query>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.SearchSystems(ctx, strings.Join(parts[1:], " "))
		}, ctx, 2*time.Second, cmd, format)

	case "get_guide":
		var guide string
		if len(parts) >= 2 {
			guide = parts[1]
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.GetGuide(ctx, guide)
		}, ctx, 2*time.Second, cmd, format)

	case "server_help":
		payload, err := parseFlagArgs(parts[1:], "command", "category")
		if err != nil {
			return err
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.Help(ctx, payload)
		}, ctx, 2*time.Second, "help", format)

	case "get_tax_estimate", "tax_estimate", "taxes":
		return simpleCommand(client, client.GetTaxEstimate, ctx, 2*time.Second, "get_tax_estimate", format)

	case "get_notifications":
		payload, err := parseFlagArgs(parts[1:], "clear", "limit")
		if err != nil {
			return err
		}
		if v, ok := payload["clear"]; ok {
			payload["clear"] = flagBool(v)
		}
		if v, ok := payload["limit"]; ok {
			if n, ok := flagInt(v); ok {
				payload["limit"] = n
			}
		}
		if len(payload) == 0 {
			return simpleCommand(client, client.GetNotifications, ctx, 2*time.Second, cmd, format)
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "get_notifications", payload)
		}, ctx, 2*time.Second, cmd, format)

	case "fleet":
		if len(parts) < 2 {
			return fmt.Errorf("usage: fleet <action> [--player_id <id>]")
		}
		playerID := ""
		fleetFlags, err := parseFlagArgs(parts[2:], "player_id")
		if err != nil {
			return err
		}
		if v, ok := fleetFlags["player_id"]; ok {
			playerID, _ = flagString(v)
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.Fleet(ctx, parts[1], playerID)
		}, ctx, 2*time.Second, cmd, format)

	case "set_home_base":
		if len(parts) < 2 {
			return fmt.Errorf("usage: set_home_base <base-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.SetHomeBase(ctx, parts[1])
		}, ctx, 2*time.Second, cmd, format)

	// === FACTIONS ===
	case "create_faction":
		if len(parts) < 3 {
			return fmt.Errorf("usage: create_faction <name> <tag>  (tag must be 4 chars, quote the name if it has spaces)")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.CreateFaction(ctx, map[string]any{"name": parts[1], "tag": parts[2]})
		}, ctx, 3*time.Second, cmd, format)

	case "join_faction":
		if len(parts) < 2 {
			return fmt.Errorf("usage: join_faction <faction-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.JoinFaction(ctx, parts[1])
		}, ctx, 2*time.Second, cmd, format)

	case "leave_faction":
		return simpleCommand(client, client.LeaveFaction, ctx, 2*time.Second, cmd, format)

	case "faction_info":
		// No-arg form looks up the player's own faction; route through the
		// migrated client.FactionInfo so execQuery blocks for the reply.
		// With a faction-id/tag arg the REPL still uses RawCommand because
		// FactionInfo doesn't support a payload — that path remains racy
		// until faction_info(faction_id) is migrated.
		if len(parts) < 2 {
			return simpleCommand(client, client.FactionInfo, ctx, 2*time.Second, cmd, format)
		}
		factionRef := parts[1]
		if len(factionRef) <= 6 {
			// Looks like a tag — normalize to uppercase and resolve to ID via faction_list.
			factionRef = strings.ToUpper(factionRef)
			if id := resolveFactionTag(client, ctx, factionRef); id != "" {
				factionRef = id
			}
		}
		payload := map[string]any{"faction_id": factionRef}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "faction_info", payload)
		}, ctx, 2*time.Second, cmd, format)

	case "faction_list":
		// --seed pages through every faction and seeds the KB factions table
		// with the lightweight header fields faction_list carries (--limit /
		// --offset are ignored in this mode).
		for _, a := range parts[1:] {
			if a == "--seed" || a == "-s" {
				return seedFactionsFromList(ctx, client)
			}
		}
		flFlags, err := parseFlagArgs(parts[1:], "limit", "offset")
		if err != nil {
			return err
		}
		flistLimit, flistOffset := 0, 0
		if v, ok := flFlags["limit"]; ok {
			if n, ok := flagInt(v); ok {
				flistLimit = n
			}
		}
		if v, ok := flFlags["offset"]; ok {
			if n, ok := flagInt(v); ok {
				flistOffset = n
			}
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.FactionList(ctx, flistLimit, flistOffset)
		}, ctx, 2*time.Second, cmd, format)

	case "faction_edit":
		// Usage: faction_edit [--description "text"] [--charter "text"]
		//   [--primary_color "#hex"] [--secondary_color "#hex"]
		//   [--ally_intel_opt_out true|false] [--ally_fuel_access true|false]
		payload, err := parseFlagArgs(parts[1:], "charter", "description", "primary_color", "secondary_color",
			"ally_intel_opt_out", "ally_fuel_access")
		if err != nil {
			return err
		}
		// The two ally toggles are booleans server-side; parseFlagArgs yields a
		// string/int, so coerce them to real JSON bools.
		if err := coerceBoolFlags(payload, "ally_intel_opt_out", "ally_fuel_access"); err != nil {
			return fmt.Errorf("faction_edit: %w", err)
		}
		if len(payload) == 0 {
			return fmt.Errorf("usage: faction_edit [--description \"text\"] [--charter \"text\"] [--primary_color \"#hex\"] [--secondary_color \"#hex\"] [--ally_intel_opt_out true|false] [--ally_fuel_access true|false]")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.FactionEdit(ctx, payload)
		}, ctx, 2*time.Second, cmd, format)

	case "faction_invite":
		if len(parts) < 2 {
			return fmt.Errorf("usage: faction_invite <player-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.FactionInvite(ctx, parts[1])
		}, ctx, 2*time.Second, cmd, format)

	case "faction_kick":
		if len(parts) < 2 {
			return fmt.Errorf("usage: faction_kick <player-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.FactionKick(ctx, parts[1])
		}, ctx, 2*time.Second, cmd, format)

	case "faction_promote":
		if len(parts) < 3 {
			return fmt.Errorf("usage: faction_promote <player-id> <role-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.FactionPromote(ctx, parts[1], parts[2])
		}, ctx, 2*time.Second, cmd, format)

	case "faction_get_invites":
		return simpleCommand(client, client.FactionGetInvites, ctx, 2*time.Second, cmd, format)

	case "faction_decline_invite":
		if len(parts) < 2 {
			return fmt.Errorf("usage: faction_decline_invite <faction-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.FactionDeclineInvite(ctx, parts[1])
		}, ctx, 2*time.Second, cmd, format)

	case "faction_declare_war":
		if len(parts) < 2 {
			return fmt.Errorf("usage: faction_declare_war <target-faction-id> [reason]")
		}
		warReason := ""
		if len(parts) >= 3 {
			warReason = strings.Join(parts[2:], " ")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.FactionDeclareWar(ctx, parts[1], warReason)
		}, ctx, 3*time.Second, cmd, format)

	case "faction_propose_peace":
		if len(parts) < 2 {
			return fmt.Errorf("usage: faction_propose_peace <target-faction-id> [terms]")
		}
		peaceTerms := ""
		if len(parts) >= 3 {
			peaceTerms = strings.Join(parts[2:], " ")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.FactionProposePeace(ctx, parts[1], peaceTerms)
		}, ctx, 3*time.Second, cmd, format)

	case "faction_accept_peace":
		if len(parts) < 2 {
			return fmt.Errorf("usage: faction_accept_peace <target-faction-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.FactionAcceptPeace(ctx, parts[1])
		}, ctx, 3*time.Second, cmd, format)

	case "faction_propose_ally":
		if len(parts) < 2 {
			return fmt.Errorf("usage: faction_propose_ally <target-faction-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.FactionProposeAlly(ctx, parts[1])
		}, ctx, 2*time.Second, cmd, format)

	case "faction_accept_ally":
		if len(parts) < 2 {
			return fmt.Errorf("usage: faction_accept_ally <target-faction-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.FactionAcceptAlly(ctx, parts[1])
		}, ctx, 2*time.Second, cmd, format)

	case "faction_remove_ally":
		if len(parts) < 2 {
			return fmt.Errorf("usage: faction_remove_ally <target-faction-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.FactionRemoveAlly(ctx, parts[1])
		}, ctx, 2*time.Second, cmd, format)

	case "faction_set_enemy":
		if len(parts) < 2 {
			return fmt.Errorf("usage: faction_set_enemy <target-faction-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.FactionSetEnemy(ctx, parts[1])
		}, ctx, 2*time.Second, cmd, format)

	case "faction_deposit_credits":
		if len(parts) < 2 {
			return fmt.Errorf("usage: faction_deposit_credits <amount>")
		}
		amount, err := strconv.Atoi(parts[1])
		if err != nil {
			return fmt.Errorf("invalid amount: %w", err)
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.FactionDepositCredits(ctx, float64(amount))
		}, ctx, 3*time.Second, cmd, format)

	case "faction_withdraw_credits":
		if len(parts) < 2 {
			return fmt.Errorf("usage: faction_withdraw_credits <amount>")
		}
		amount, err := strconv.Atoi(parts[1])
		if err != nil {
			return fmt.Errorf("invalid amount: %w", err)
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.FactionWithdrawCredits(ctx, float64(amount))
		}, ctx, 3*time.Second, cmd, format)

	case "faction_deposit_items":
		if len(parts) < 3 {
			return fmt.Errorf("usage: faction_deposit_items <item-id> <quantity> [--source=<scope>] [--target=<scope>]")
		}
		qty, err := parseQuantity(parts[2])
		if err != nil {
			return fmt.Errorf("invalid quantity: %w", err)
		}
		flags, err := parseFlagArgs(parts[3:], "source", "target")
		if err != nil {
			return err
		}
		if len(flags) > 0 {
			payload := map[string]any{
				"item_id":  strings.ToLower(parts[1]),
				"quantity": int(qty),
			}
			maps.Copy(payload, flags)
			return simpleCommand(client, func(ctx context.Context) error {
				return client.FactionDepositItemsPayload(ctx, payload)
			}, ctx, 3*time.Second, cmd, format)
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.FactionDepositItems(ctx, strings.ToLower(parts[1]), int(qty))
		}, ctx, 3*time.Second, cmd, format)

	case "faction_withdraw_items":
		if len(parts) < 3 {
			return fmt.Errorf("usage: faction_withdraw_items <item-id> <quantity> [--source=<scope>] [--target=<scope>]")
		}
		qty, err := parseQuantity(parts[2])
		if err != nil {
			return fmt.Errorf("invalid quantity: %w", err)
		}
		flags, err := parseFlagArgs(parts[3:], "source", "target")
		if err != nil {
			return err
		}
		if len(flags) > 0 {
			payload := map[string]any{
				"item_id":  strings.ToLower(parts[1]),
				"quantity": int(qty),
			}
			maps.Copy(payload, flags)
			return simpleCommand(client, func(ctx context.Context) error {
				return client.FactionWithdrawItemsPayload(ctx, payload)
			}, ctx, 3*time.Second, cmd, format)
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.FactionWithdrawItems(ctx, strings.ToLower(parts[1]), int(qty))
		}, ctx, 3*time.Second, cmd, format)

	case "view_faction_storage":
		// Display-only flags mirror `storage`: --group (alias --by-category)
		// groups items into catalog-derived category sections; --filter <substr>
		// (alias --match, or a bare positional) keeps only items whose item_id or
		// name contains the substring, case-insensitive.
		var opts storageFmtOptions
		for i := 1; i < len(parts); i++ {
			arg := parts[i]
			switch {
			case arg == "--group" || arg == "--by-category" || arg == "-g":
				opts.group = true
			case strings.HasPrefix(arg, "--filter="):
				opts.filter = strings.TrimPrefix(arg, "--filter=")
			case strings.HasPrefix(arg, "--match="):
				opts.filter = strings.TrimPrefix(arg, "--match=")
			case (arg == "--filter" || arg == "--match") && i+1 < len(parts):
				i++
				opts.filter = parts[i]
			case !strings.HasPrefix(arg, "-"):
				opts.filter = arg
			}
		}
		storageFmtOpts = opts
		defer func() { storageFmtOpts = storageFmtOptions{} }()
		return simpleCommand(client, client.ViewFactionStorage, ctx, 2*time.Second, cmd, format)

	case "faction_create_buy_order":
		if len(parts) < 4 {
			return fmt.Errorf("usage: faction_create_buy_order <item-id> <quantity> <price-each>")
		}
		qty, err := strconv.Atoi(parts[2])
		if err != nil {
			return fmt.Errorf("invalid quantity: %w", err)
		}
		price, err := strconv.Atoi(parts[3])
		if err != nil {
			return fmt.Errorf("invalid price: %w", err)
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.FactionCreateBuyOrder(ctx, strings.ToLower(parts[1]), float64(price), qty)
		}, ctx, 3*time.Second, cmd, format)

	case "faction_create_sell_order":
		if len(parts) < 4 {
			return fmt.Errorf("usage: faction_create_sell_order <item-id> <quantity> <price-each>")
		}
		qty, err := strconv.Atoi(parts[2])
		if err != nil {
			return fmt.Errorf("invalid quantity: %w", err)
		}
		price, err := strconv.Atoi(parts[3])
		if err != nil {
			return fmt.Errorf("invalid price: %w", err)
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.FactionCreateSellOrder(ctx, strings.ToLower(parts[1]), float64(price), qty)
		}, ctx, 3*time.Second, cmd, format)

	case "faction_create_role":
		if len(parts) < 3 {
			return fmt.Errorf("usage: faction_create_role <name> <priority>")
		}
		priority, err := strconv.Atoi(parts[2])
		if err != nil {
			return fmt.Errorf("invalid priority: %w", err)
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.FactionCreateRole(ctx, parts[1], priority, nil)
		}, ctx, 2*time.Second, cmd, format)

	case "faction_edit_role":
		if len(parts) < 2 {
			return fmt.Errorf("usage: faction_edit_role <role-id> [--name \"name\"]")
		}
		editRolePayload, err := parseFlagArgs(parts[2:], "name")
		if err != nil {
			return err
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.FactionEditRole(ctx, parts[1], editRolePayload)
		}, ctx, 2*time.Second, cmd, format)

	case "faction_delete_role":
		if len(parts) < 2 {
			return fmt.Errorf("usage: faction_delete_role <role-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.FactionDeleteRole(ctx, parts[1])
		}, ctx, 2*time.Second, cmd, format)

	case "faction_submit_intel":
		// Reads saved get_poi intel file(s) and submits them. --file may point
		// to a single get_poi JSON file or a directory of them (grouped by
		// system into one submission).
		_, flags := partitionFlags(parts[1:])
		path := flags["file"]
		if path == "" {
			return fmt.Errorf("usage: faction_submit_intel --file <path>  (a get_poi JSON file or a system dir, e.g. under %s)", globalIntelDir)
		}
		return submitFactionIntel(client, ctx, path, format)

	case "faction_submit_trade_intel":
		return fmt.Errorf("faction_submit_trade_intel requires complex payload; use the generic passthrough or MCP directly")

	case "faction_query_intel":
		payload, err := parseFlagArgs(parts[1:], "poi_type", "resource_type", "system_id", "system_name")
		if err != nil {
			return err
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.FactionQueryIntel(ctx, payload)
		}, ctx, 2*time.Second, cmd, format)

	case "faction_query_trade_intel":
		payload, err := parseFlagArgs(parts[1:], "base_id", "item_id", "station_name")
		if err != nil {
			return err
		}
		if v, ok := payload["item_id"].(string); ok {
			payload["item_id"] = strings.ToLower(v)
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.FactionQueryTradeIntel(ctx, payload)
		}, ctx, 2*time.Second, cmd, format)

	case "faction_intel_status":
		return simpleCommand(client, client.FactionIntelStatus, ctx, 2*time.Second, cmd, format)

	case "faction_trade_intel_status":
		return simpleCommand(client, client.FactionTradeIntelStatus, ctx, 2*time.Second, cmd, format)

	case "faction_rooms":
		return simpleCommand(client, client.FactionRooms, ctx, 2*time.Second, cmd, format)

	case "faction_visit_room":
		if len(parts) < 2 {
			return fmt.Errorf("usage: faction_visit_room <room-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.FactionVisitRoom(ctx, parts[1])
		}, ctx, 2*time.Second, cmd, format)

	case "faction_write_room":
		payload, err := parseFlagArgs(parts[1:], "room_id", "name", "description", "access")
		if err != nil {
			return err
		}
		if len(payload) == 0 {
			return fmt.Errorf("usage: faction_write_room [--room_id id] --name \"name\" --description \"text\" [--access public|faction]")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.FactionWriteRoom(ctx, payload)
		}, ctx, 2*time.Second, cmd, format)

	case "faction_delete_room":
		if len(parts) < 2 {
			return fmt.Errorf("usage: faction_delete_room <room-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.FactionDeleteRoom(ctx, parts[1])
		}, ctx, 2*time.Second, cmd, format)

	case "faction_post_mission":
		return fmt.Errorf("faction_post_mission requires complex payload; use the generic passthrough or MCP directly")

	case "faction_list_missions":
		return simpleCommand(client, client.FactionListMissions, ctx, 2*time.Second, cmd, format)

	case "faction_cancel_mission":
		if len(parts) < 2 {
			return fmt.Errorf("usage: faction_cancel_mission <template-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.FactionCancelMission(ctx, parts[1])
		}, ctx, 3*time.Second, cmd, format)

	// === COMMUNICATION ===
	case "chat":
		if len(parts) < 3 {
			return fmt.Errorf("usage: chat <channel> <message>")
		}
		channel := parts[1]
		var msg string
		var target string

		// Private messages require a target username: chat private <target> <message>
		if strings.EqualFold(channel, "private") {
			if len(parts) < 4 {
				return fmt.Errorf("usage: chat private <target> <message>")
			}
			target = parts[2]
			msg = strings.Join(parts[3:], " ")
		} else {
			// Public channels: chat local|system|faction <message>
			msg = strings.Join(parts[2:], " ")
		}

		return simpleCommand(client, func(ctx context.Context) error {
			return client.Chat(ctx, channel, msg, target)
		}, ctx, 2*time.Second, cmd, format)

	case "chat_history", "get_chat_history":
		if len(parts) < 2 {
			return fmt.Errorf("usage: chat_history <channel> [--target_id <username>] [--before <ts>] [--after <ts>] [--limit <n>]")
		}
		channel := parts[1]
		// Parse optional --target_id flag. For the private channel, omitting it
		// returns the whole DM inbox (v0.397.0+); passing it reads a single
		// conversation.
		flagArgs, err := parseFlagArgs(parts[2:], "target_id", "before", "after", "limit")
		if err != nil {
			return err
		}
		payload := make(map[string]any)
		if targetID, ok := flagArgs["target_id"]; ok {
			payload["target_id"] = targetID
		}
		if before, ok := flagArgs["before"]; ok {
			payload["before"] = before
		}
		if after, ok := flagArgs["after"]; ok {
			payload["after"] = after
		}
		// parseFlagArgs auto-converts a numeric --limit to int, so the old
		// limitStr.(string) assertion panicked; use flagInt to handle both.
		if limitVal, ok := flagArgs["limit"]; ok {
			if n, ok := flagInt(limitVal); ok {
				payload["limit"] = n
			}
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.GetChatHistory(ctx, channel, payload)
		}, ctx, 2*time.Second, cmd, format)

	case "send_gift":
		// Usage: send_gift <recipient> <item_id> <quantity> [--message "text"]
		//        send_gift <recipient> credits <amount> [--message "text"]
		//        send_gift <recipient> ship <ship_id> [--message "text"]
		if len(parts) < 4 {
			return fmt.Errorf("usage: send_gift <recipient> <item_id> <quantity> [--message \"text\"]\n" +
				"       send_gift <recipient> credits <amount>\n" +
				"       send_gift <recipient> ship <ship_id>")
		}
		payload := map[string]any{"recipient": parts[1]}
		switch parts[2] {
		case "credits":
			amount, err := parseQuantity(parts[3])
			if err != nil {
				return fmt.Errorf("invalid credits amount: %w", err)
			}
			payload["credits"] = amount
		case "ship":
			payload["ship_id"] = parts[3]
		default:
			qty, err := parseQuantity(parts[3])
			if err != nil {
				return fmt.Errorf("invalid quantity: %w", err)
			}
			payload["item_id"] = strings.ToLower(parts[2])
			payload["quantity"] = qty
		}
		// Parse optional --message flag
		msgArgs, err := parseFlagArgs(parts[4:], "message")
		if err != nil {
			return err
		}
		if msg, ok := msgArgs["message"]; ok {
			payload["message"] = msg
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.SendGift(ctx, payload)
		}, ctx, 3*time.Second, cmd, format)

	// === FORUM ===
	case "forum_list":
		page := 1
		if len(parts) >= 2 {
			var err error
			page, err = strconv.Atoi(parts[1])
			if err != nil {
				return fmt.Errorf("invalid page number: %w", err)
			}
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.ForumList(ctx, page)
		}, ctx, 2*time.Second, cmd, format)

	case "forum_thread":
		if len(parts) < 2 {
			return fmt.Errorf("usage: forum_thread <thread-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.ForumGetThread(ctx, parts[1])
		}, ctx, 2*time.Second, cmd, format)

	case "forum_create_thread":
		if len(parts) < 3 {
			return fmt.Errorf("usage: forum_create_thread <title> <content> [--category <cat>]")
		}
		// Find --category flag; everything else after title is content
		title := parts[1]
		category := ""
		var contentParts []string
		for i := 2; i < len(parts); i++ {
			if parts[i] == "--category" && i+1 < len(parts) {
				i++
				category = parts[i]
			} else {
				contentParts = append(contentParts, parts[i])
			}
		}
		content := strings.Join(contentParts, " ")
		return simpleCommand(client, func(ctx context.Context) error {
			return client.ForumCreateThread(ctx, title, content, category)
		}, ctx, 3*time.Second, cmd, format)

	case "forum_reply":
		if len(parts) < 3 {
			return fmt.Errorf("usage: forum_reply <thread-id> <content>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.ForumReply(ctx, parts[1], strings.Join(parts[2:], " "))
		}, ctx, 3*time.Second, cmd, format)

	case "forum_upvote":
		if len(parts) < 2 {
			return fmt.Errorf("usage: forum_upvote <thread-id> [--reply_id <id>]")
		}
		replyID := ""
		upvoteFlags, err := parseFlagArgs(parts[2:], "reply_id")
		if err != nil {
			return err
		}
		if v, ok := upvoteFlags["reply_id"]; ok {
			replyID, _ = flagString(v)
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.ForumUpvote(ctx, parts[1], replyID)
		}, ctx, 2*time.Second, cmd, format)

	case "forum_delete_thread":
		if len(parts) < 2 {
			return fmt.Errorf("usage: forum_delete_thread <thread-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.ForumDeleteThread(ctx, parts[1])
		}, ctx, 3*time.Second, cmd, format)

	case "forum_delete_reply":
		if len(parts) < 2 {
			return fmt.Errorf("usage: forum_delete_reply <reply-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.ForumDeleteReply(ctx, parts[1])
		}, ctx, 3*time.Second, cmd, format)

	// === NOTES ===
	case "notes", "get_notes":
		return simpleCommand(client, client.GetNotes, ctx, 2*time.Second, cmd, format)

	case "create_note":
		if len(parts) < 3 {
			return fmt.Errorf("usage: create_note <title> <content>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.CreateNote(ctx, parts[1], strings.Join(parts[2:], " "))
		}, ctx, 2*time.Second, cmd, format)

	case "read_note":
		if len(parts) < 2 {
			return fmt.Errorf("usage: read_note <note-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.ReadNote(ctx, parts[1])
		}, ctx, 2*time.Second, cmd, format)

	case "write_note":
		if len(parts) < 3 {
			return fmt.Errorf("usage: write_note <note-id> <content>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.WriteNote(ctx, parts[1], strings.Join(parts[2:], " "))
		}, ctx, 2*time.Second, cmd, format)

	// === MISSIONS ===
	case "missions", "get_missions":
		if slices.Contains(parts[1:], "--full") {
			missionsShowFull = true
			defer func() { missionsShowFull = false }()
		}
		return simpleCommand(client, client.GetMissions, ctx, 2*time.Second, cmd, format)

	case "active_missions", "get_active_missions":
		if slices.Contains(parts[1:], "--full") {
			missionsShowFull = true
			defer func() { missionsShowFull = false }()
		}
		return simpleCommand(client, client.GetActiveMissions, ctx, 2*time.Second, cmd, format)

	case "accept_mission":
		if len(parts) < 2 {
			return fmt.Errorf("usage: accept_mission <mission-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.AcceptMission(ctx, parts[1])
		}, ctx, 2*time.Second, cmd, format)

	case "complete_mission":
		if len(parts) < 2 {
			return fmt.Errorf("usage: complete_mission <mission-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.CompleteMission(ctx, parts[1])
		}, ctx, 3*time.Second, cmd, format)

	case "abandon_mission":
		if len(parts) < 2 {
			return fmt.Errorf("usage: abandon_mission <mission-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.AbandonMission(ctx, parts[1])
		}, ctx, 3*time.Second, cmd, format)

	case "decline_mission":
		if len(parts) < 2 {
			return fmt.Errorf("usage: decline_mission <template-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.DeclineMission(ctx, parts[1])
		}, ctx, 3*time.Second, cmd, format)

	case "view_completed_mission":
		if len(parts) < 2 {
			return fmt.Errorf("usage: view_completed_mission <template-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "view_completed_mission", map[string]any{"template_id": parts[1]})
		}, ctx, 2*time.Second, cmd, format)

	// === ACTION LOG ===
	case "get_action_log", "action_log":
		payload, err := parseFlagArgs(parts[1:], "category", "faction_id", "page", "page_size")
		if err != nil {
			return err
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.GetActionLog(ctx, payload)
		}, ctx, 2*time.Second, cmd, format)

	// === CAPTAIN'S LOG ===
	case "log":
		if len(parts) < 2 {
			return fmt.Errorf("usage: log <entry>")
		}
		entry := strings.Join(parts[1:], " ")
		return simpleCommand(client, func(ctx context.Context) error {
			return client.CaptainsLogAdd(ctx, entry)
		}, ctx, 2*time.Second, cmd, format)

	case "captains_log_get":
		if len(parts) < 2 {
			return fmt.Errorf("usage: captains_log_get <index>")
		}
		idx, err := strconv.Atoi(parts[1])
		if err != nil {
			return fmt.Errorf("invalid index: %w", err)
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.CaptainsLogGet(ctx, idx)
		}, ctx, 2*time.Second, cmd, format)

	case "captains_log_list":
		var payload map[string]any
		if len(parts) >= 2 {
			if idx, err := strconv.Atoi(parts[1]); err == nil {
				payload = map[string]any{"index": idx}
			}
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "captains_log_list", payload)
		}, ctx, 2*time.Second, cmd, format)

	// === STATE ===
	case "state":
		printState(client)
		return nil

	case "raw":
		if len(parts) < 2 {
			return fmt.Errorf("usage: raw <key>")
		}
		data := client.GetRawJSON(parts[1])
		if len(data) == 0 {
			return fmt.Errorf("no data found for key: %s", parts[1])
		}
		fmt.Printf("\n📄 Raw JSON [%s]:\n", parts[1])
		var prettyJSON map[string]any
		if err := json.Unmarshal(data, &prettyJSON); err == nil {
			pretty, _ := json.MarshalIndent(prettyJSON, "", "  ")
			fmt.Println(string(pretty))
		} else {
			fmt.Println(string(data))
		}
		return nil

	// === STATION FACILITIES ===
	case "facility":
		if len(parts) < 2 {
			return fmt.Errorf("usage: facility <action> [arg...] [--flag value...]\n" +
				"  actions: types, build, list, owned, toggle, upgrades, upgrade,\n" +
				"           job_add, job_list, job_cancel, job_reorder, set_output_price, set_access,\n" +
				"           list_for_sale, browse_for_sale, buy_listing, cancel_listing,\n" +
				"           faction_build, faction_upgrade, faction_list, faction_owned, faction_toggle,\n" +
				"           transfer, personal_build, personal_decorate, personal_visit, help\n" +
				"  positional args by action:\n" +
				"           build/types/personal_build/faction_build <facility_type>\n" +
				"           set_access <public|private>     set_output_price <item_id> <price>\n" +
				"           buy_listing/cancel_listing <listing_id>\n" +
				"  job flags: --facility_id ID --recipe_id ID --quantity N --job_id ID --position N\n" +
				"  business flags: --item_id ID --price N --access private|public\n" +
				"  flags:   --show_station_facilities  (list: also show the station's own facilities)")
		}
		// Parse all args uniformly via buildFacilityPayload: --flag value pairs,
		// --flag=value, and bare key=value tokens go straight into the payload
		// (including action=...); bare positionals map to action-specific keys
		// (see facilityPositionalKeys) — e.g. `set_access public` -> access=public.
		payload, showStation, err := buildFacilityPayload(parts)
		if err != nil {
			return err
		}
		// Toggle station-facility rendering for this invocation only. execMu
		// serializes foreground/background commands, so this package-level flag
		// is read safely during the synchronous response formatting below.
		showStationFacilities = showStation
		defer func() { showStationFacilities = false }()
		return simpleCommand(client, func(ctx context.Context) error {
			return client.Facility(ctx, payload)
		}, ctx, 5*time.Second, cmd, format)

	case "sellable":
		opts := sellableOptions{}
		for i := 1; i < len(parts); i++ {
			arg := parts[i]
			switch {
			case arg == "--detail" || arg == "-d":
				opts.detail = true
			case strings.HasPrefix(arg, "--min-proceeds="):
				v := strings.TrimPrefix(arg, "--min-proceeds=")
				n, err := strconv.ParseInt(v, 10, 64)
				if err != nil {
					return fmt.Errorf("sellable: --min-proceeds: %w", err)
				}
				opts.minProceeds = n
			case arg == "--min-proceeds":
				if i+1 >= len(parts) {
					return fmt.Errorf("sellable: --min-proceeds requires a value")
				}
				i++
				n, err := strconv.ParseInt(parts[i], 10, 64)
				if err != nil {
					return fmt.Errorf("sellable: --min-proceeds: %w", err)
				}
				opts.minProceeds = n
			default:
				return fmt.Errorf("sellable: unknown flag %q", arg)
			}
		}
		return runSellable(client, ctx, opts, format)

	case "demand":
		if len(parts) >= 2 && parts[1] == "history" {
			return runDemandHistory(ctx, parts[2:], format)
		}
		opts, err := parseDemandOptions(parts[1:])
		if err != nil {
			return err
		}
		return runDemand(client, ctx, opts, format)

	// === APPEARANCE ===
	case "set_colors":
		if len(parts) < 3 {
			return fmt.Errorf("usage: set_colors <primary-hex> <secondary-hex>  (e.g. set_colors #FF0000 #00FFFF)")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.SetColors(ctx, parts[1], parts[2])
		}, ctx, 2*time.Second, cmd, format)

	case "set_anonymous":
		if len(parts) < 2 {
			return fmt.Errorf("usage: set_anonymous <true|false>")
		}
		anon := strings.EqualFold(parts[1], "true") || parts[1] == "1"
		return simpleCommand(client, func(ctx context.Context) error {
			return client.SetAnonymous(ctx, anon)
		}, ctx, 2*time.Second, cmd, format)

	case "set_status":
		// Usage: set_status --status_message "text" [--clan_tag "TAG"]
		payload, err := parseFlagArgs(parts[1:], "status_message", "clan_tag")
		if err != nil {
			return err
		}
		if len(payload) == 0 {
			return fmt.Errorf("usage: set_status --status_message \"text\" [--clan_tag \"TAG\"]")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.SetPlayerStatus(ctx, payload)
		}, ctx, 2*time.Second, cmd, format)

	// === AUTOPILOT & EXPLORE ===
	case "autopilot", "ap":
		return autopilot(client, ctx, parts, format)
	case "plan_route", "plan-route":
		return planRoute(client, ctx, parts, format)
	case "explore":
		return explore(client, ctx, format)
	case "auto_explore", "auto-explore":
		return autoExplore(client, ctx, parts, format)

	// === KNOWLEDGE BASE UPDATE COMMANDS ===
	case "update_system":
		return kbUpdateSystem(client, ctx)
	case "update_poi":
		return kbUpdatePOI(client, ctx)
	case "update_station", "update_base":
		return kbUpdateStation(client, ctx)
	case "update_facilities":
		return kbUpdateFacilities(client, ctx)
	case "update_missions":
		return kbUpdateMissions(client, ctx)
	case "update_all":
		return kbUpdateAll(client, ctx)
	case "update_faction_data", "update_faction":
		return kbUpdateFaction(client, ctx)
	case "seen_factions":
		return cmdSeenFactions(ctx, parts[1:])

	case "passenger", "passenger_catalog":
		return cmdPassengerCatalog(ctx, parts[1:])

	// === PASSENGERS ===
	// The generic passthrough maps positional args to arg1/arg2, but these
	// commands take named parameters, so they need explicit handlers. RawCommand
	// blocks on the terminal response (load/unload are deferred mutations whose
	// real result arrives on the next tick), so the styled formatters render the
	// actual outcome rather than the bare "pending" ack.
	case "list_passengers", "passengers":
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "list_passengers", nil)
		}, ctx, 0, "list_passengers", format)

	case "list_station_passengers", "station_passengers":
		payload := map[string]any{}
		if len(parts) > 1 {
			payload["station"] = strings.Join(parts[1:], " ")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "list_station_passengers", payload)
		}, ctx, 0, "list_station_passengers", format)

	case "load_passenger":
		if len(parts) < 2 {
			return fmt.Errorf("usage: load_passenger <destination-station> (loads all waiting passengers bound for that station)")
		}
		dest := strings.Join(parts[1:], " ")
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "load_passenger", map[string]any{"destination": dest})
		}, ctx, 0, "load_passenger", format)

	case "unload_passenger":
		if len(parts) < 2 {
			return fmt.Errorf("usage: unload_passenger <passenger-name-or-citizen-id>")
		}
		name := strings.Join(parts[1:], " ")
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "unload_passenger", map[string]any{"name": name})
		}, ctx, 0, "unload_passenger", format)

	default:
		// Generic passthrough: send any unrecognized command directly to the server.
		// Parse --key value, --key=value flags, and bare positional args.
		args := make(map[string]any)
		positional := 0
		for i := 1; i < len(parts); i++ {
			arg := parts[i]
			if key, ok := strings.CutPrefix(arg, "--"); ok {
				if k, v, ok2 := strings.Cut(key, "="); ok2 {
					args[k] = v
				} else if i+1 < len(parts) {
					i++
					args[key] = parts[i]
				}
			} else {
				positional++
				args[fmt.Sprintf("arg%d", positional)] = arg
			}
		}
		if len(args) == 0 {
			args = nil
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, cmd, args)
		}, ctx, 2*time.Second, cmd, format)
	}
}

// rawJSONKeyForCommand maps a REPL command name to the storage key the game
// client uses for its response payload (see pkg/game/client.go storeRawJSON).
// simpleCommand prefers this key over "_last" because "_last" is a single
// shared slot that gets clobbered by any concurrent command response — in
// particular the silent background chat poller on WS, whose get_chat_history
// reply can overwrite "_last" between a foreground command finishing and the
// REPL reading the result.
// rawJSONKeyForCommand maps REPL command names to non-matching storage keys.
// Entries are required only when the storeRawJSON content-shape key differs
// from the command name (e.g. browse_ships → "listings", view_storage →
// "storage", list_ships → "ships"). Commands whose action_result is keyed
// by their own name fall through to lookupRawJSON's `client.GetRawJSON(cmd)`
// default — no entry needed.
var rawJSONKeyForCommand = map[string]string{
	"get_ship":   "ship",
	"get_cargo":  "cargo",
	"get_status": "status",
	// get_state is undocumented but the server answers it with a full state
	// dump. Its frame carries no "action" field, so storeRawJSON files it by
	// content shape ("player" present) under "status" — the same slot as
	// get_status. Map it here so the generic passthrough can find and print
	// the payload (raw mode dumps it; styled mode falls back to pretty JSON
	// since there is no get_state formatter).
	"get_state":            "status",
	"get_system":           "system",
	"get_poi":              "poi",
	"storage_at":           "storage",
	"view_storage":         "storage",
	"view_faction_storage": "faction_storage",
	"browse_ships":         "listings",
	"view_market":          "market",
	"view_orders":          "orders",
	"get_missions":         "missions",
	"get_active_missions":  "active_missions",
	"get_wrecks":           "wrecks",
	"get_drones":           "drones",
	"get_location":         "location",
	"get_recipes":          "recipes",
	"get_base":             "base",
	"get_chat_history":     "chat_history",
	"survey_system":        "survey",
	"get_skills":           "skills",
	"get_nearby":           "nearby",
	"map":                  "systems",
	"get_map":              "systems",
	"list_ships":           "ships",
	"get_notes":            "notes",
	"read_note":            "note",
	"get_action_log":       "action_log",
	"action_log":           "action_log",
	"get_tax_estimate":     "tax_estimate",
	"tax_estimate":         "tax_estimate",
	"get_achievements":     "achievements",
	"achievements":         "achievements",
	// get_insurance_quote arrives as a TypeOK frame that storeRawJSON keys by
	// content shape under "insurance_quote", not the command name.
	"get_insurance_quote": "insurance_quote",
	// MCP caches FactionQueryIntel under "faction_intel"; the WS store keys
	// it there too (see storeRawJSON). Map the command so lookup finds it.
	"faction_query_intel": "faction_intel",
}

// lookupRawJSON returns the raw JSON payload for command. It first checks
// rawJSONKeyForCommand (for TypeOK responses that storeRawJSON keys by
// content shape — e.g. "ships", "storage", "market") and falls back to
// the command name itself, since action_result frames are stored under
// their `command` field. Critically, this does NOT fall back to "_last":
// that shared slot is racy (the background chat poller, for example, can
// clobber it between the foreground command finishing and the REPL
// reading the result), so commands without a per-command storage key
// just print nothing rather than stale data.
func lookupRawJSON(client game.GameClient, command string) []byte {
	cmd := strings.ToLower(command)
	if key := rawJSONKeyForCommand[cmd]; key != "" {
		return client.GetRawJSON(key)
	}
	return client.GetRawJSON(cmd)
}

// showLastResponse prints the most recent server response for command.
func showLastResponse(client game.GameClient, format outputFormat, command string) {
	if raw := lookupRawJSON(client, command); len(raw) > 0 {
		printResponse(raw, format, command)
	}
}

// chooseResponseJSON returns the JSON payload bytes to display for a command.
// When sink was populated by the request_id-correlated await path (Type set),
// it marshals the sink's payload — the exact frame for THIS command, immune to
// the command-keyed-slot clobber race. Otherwise (MCP transport, or commands
// not yet on Submit) it falls back to the legacy command-keyed lookup.
func chooseResponseJSON(sink protocol.Response, client game.GameClient, command string) []byte {
	if sink.Type != "" && len(sink.Payload) > 0 {
		if raw, err := json.Marshal(sink.Payload); err == nil {
			return raw
		}
	}
	return lookupRawJSON(client, command)
}

// chooseErrorJSON mirrors chooseResponseJSON for the error path: prefer the
// correlated sink (await populates resp even on a terminal *ServerError),
// otherwise the dedicated _last_error slot.
func chooseErrorJSON(sink protocol.Response, client game.GameClient) []byte {
	if sink.Type != "" && len(sink.Payload) > 0 {
		if raw, err := json.Marshal(sink.Payload); err == nil {
			return raw
		}
	}
	return client.GetRawJSON("_last_error")
}

// simpleCommand executes a command, prints the server response, then waits.
//
// On *game.GoalReachedError simpleCommand PROPAGATES the sentinel back to
// the caller. Two consumers need to see it:
//   - executeLoop (in loop context) recognizes the sentinel and exits the
//     innermost loop cleanly with a 🎯 line.
//   - The REPL dispatcher (standalone) recognizes the sentinel and prints
//     a ✓ line instead of ❌.
//
// Previously simpleCommand printed ✓ itself and returned nil, which hid
// the signal from executeLoop — the loop saw "iteration succeeded" and
// ran the next one, and so on until the count was exhausted.
//
// The result sink (game.WithResultSink) captures the exact request_id-correlated
// server frame for THIS command, eliminating the racy command-keyed-slot clobber
// that a concurrent background command's TypeOK response could trigger.
func simpleCommand(client game.GameClient, fn func(context.Context) error, ctx context.Context, wait time.Duration, command string, format outputFormat) error {
	var sink protocol.Response
	cctx := game.WithResultSink(ctx, &sink)
	if err := fn(cctx); err != nil {
		// Propagate the goal-reached sentinel unchanged for the loop
		// executor / REPL dispatcher to display.
		var goal *game.GoalReachedError
		if errors.As(err, &goal) {
			return err
		}
		// In raw/JSON modes, surface the server's actual error frame. Prefer the
		// correlated sink (await captures error frames too); fall back to the
		// dedicated _last_error slot when the sink is empty (e.g. send-failure
		// before any frame, or MCP transport).
		if format != formatStyled {
			if raw := chooseErrorJSON(sink, client); len(raw) > 0 {
				printResponse(raw, format, command)
			}
		}
		return err
	}
	if raw := chooseResponseJSON(sink, client, command); len(raw) > 0 {
		printResponse(raw, format, command)
	}
	if wait > 0 {
		time.Sleep(wait)
	}
	return nil
}

// reconcileDockState swallows a dock/undock timeout when the underlying
// transition actually happened — typically because the WS dropped between
// the action committing server-side and the terminal frame reaching us, and
// the client's auto-reconnect hydrated state from the post-action welcome.
//
// On non-timeout errors, returns err unchanged. On success, refreshes status
// so the printed response reflects the post-transition state.
func reconcileDockState(ctx context.Context, client game.GameClient, action string, err error) error {
	if err == nil {
		return nil
	}
	if !strings.Contains(err.Error(), "timeout waiting for "+action) {
		return err
	}
	if !client.IsConnected() {
		return err
	}
	// Refresh state so we read the post-action snapshot, not whatever was
	// cached before the disconnect.
	statusCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if statusErr := client.GetStatus(statusCtx); statusErr != nil {
		return err
	}
	state := client.GetState()
	if state == nil {
		return err
	}
	docked := state.IsDocked()
	wantDocked := action == "dock"
	if docked == wantDocked {
		fmt.Printf("⚠ %s timed out (likely WS drop) but state confirms transition; treating as success\n", action)
		return nil
	}
	return err
}

// travelEstimate holds pre-travel distance, tick, and fuel estimates.
type travelEstimate struct {
	valid    bool
	distance float64
	speed    float64
	ticks    int
	fuel     int
}

// estimateTravel estimates distance, ticks, and fuel cost for traveling to a target POI.
func estimateTravel(client game.GameClient, targetPOI string) travelEstimate {
	state := client.GetState()
	if state == nil {
		return travelEstimate{}
	}

	speed := state.Ship.Speed
	if speed <= 0 {
		return travelEstimate{}
	}

	// Find current and target POI positions.
	var curPos, targetPos *game.Position
	for i := range state.System.POIs {
		poi := &state.System.POIs[i]
		if poi.ID == state.CurrentPOI {
			curPos = &poi.Position
		}
		if poi.ID == targetPOI {
			targetPos = &poi.Position
		}
	}
	if curPos == nil || targetPos == nil {
		return travelEstimate{}
	}

	dx := targetPos.X - curPos.X
	dy := targetPos.Y - curPos.Y
	distance := math.Sqrt(dx*dx + dy*dy)
	if distance <= 0 {
		return travelEstimate{}
	}

	ticks := max(int(math.Ceil(distance/speed)), 1)

	// Estimate fuel cost from ship class data: ceil(scale^1.5 × speed × distance × 0.07)
	fuel := 0
	if raw := client.GetRawJSON("ship"); len(raw) > 0 {
		var shipResp struct {
			Class *struct {
				Scale     int `json:"scale"`
				BaseSpeed int `json:"base_speed"`
			} `json:"class"`
		}
		if err := json.Unmarshal(raw, &shipResp); err == nil && shipResp.Class != nil {
			scale := float64(shipResp.Class.Scale)
			spd := float64(shipResp.Class.BaseSpeed)
			if scale > 0 && spd > 0 {
				fuel = int(math.Ceil(math.Pow(scale, 1.5) * spd * distance * 0.07))
			}
		}
	}

	return travelEstimate{
		valid:    true,
		speed:    speed,
		distance: distance,
		ticks:    ticks,
		fuel:     fuel,
	}
}

// colorizeHex wraps text with ANSI 24-bit color escape sequences.
// primary sets the foreground, secondary sets the background.
// Either can be "" to skip. Colors are "#RRGGBB" hex strings.
func colorizeHex(text, primary, secondary string) string {
	if primary == "" && secondary == "" {
		return text
	}
	var prefix string
	if r, g, b, ok := parseHexColor(primary); ok {
		prefix += fmt.Sprintf("\033[38;2;%d;%d;%dm", r, g, b)
	}
	if r, g, b, ok := parseHexColor(secondary); ok {
		prefix += fmt.Sprintf("\033[48;2;%d;%d;%dm", r, g, b)
	}
	if prefix == "" {
		return text
	}
	return prefix + text + "\033[0m"
}

// parseHexColor parses a "#RRGGBB" string into RGB components.
func parseHexColor(s string) (r, g, b uint8, ok bool) {
	s = strings.TrimPrefix(s, "#")
	if len(s) != 6 {
		return 0, 0, 0, false
	}
	val, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return 0, 0, 0, false
	}
	return uint8(val >> 16), uint8(val >> 8), uint8(val), true
}

// resolveFactionTag looks up a faction tag via faction_list and returns the faction ID, or "" if not found.
func resolveFactionTag(client game.GameClient, ctx context.Context, tag string) string {
	tag = strings.ToUpper(tag)
	if err := client.FactionList(ctx, 0, 0); err != nil {
		return ""
	}
	raw := client.GetRawJSON("_last")
	if len(raw) == 0 {
		return ""
	}
	var resp struct {
		Factions []struct {
			ID  string `json:"id"`
			Tag string `json:"tag"`
		} `json:"factions"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}
	for _, f := range resp.Factions {
		if strings.EqualFold(f.Tag, tag) {
			return f.ID
		}
	}
	return ""
}

// resolveArg returns the value for `key` from args, accepting either:
//  1. flag form: --key=value or --key value, or
//  2. the first positional token (one not starting with "--").
//
// When walking positional tokens it skips any "--other-flag" plus its value so
// it doesn't mistake an unrelated flag's value for a positional. Returns "" if
// neither form yields a value. Used by drone REPL cases that want users to be
// able to write `load_drone mining_drone` or `load_drone --item_id mining_drone`
// interchangeably.
func resolveArg(args []string, key string) string {
	flag := "--" + key
	prefix := flag + "="
	for i := 0; i < len(args); i++ {
		a := args[i]
		if v, ok := strings.CutPrefix(a, prefix); ok {
			return v
		}
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "--") {
			if !strings.Contains(a, "=") && i+1 < len(args) {
				i++
			}
			continue
		}
		return a
	}
	return ""
}

// flagInt extracts an int from a parseFlagArgs value, which may be int
// (auto-converted from a numeric token) or string (everything else). Returns
// ok=false for unparseable strings or unrelated types. Use at call sites
// that need int payload fields (page, page_size, quantity, side_id, etc.) —
// the older v.(string) + strconv.Atoi pattern panics when parseFlagArgs hits
// a numeric token first.
func flagInt(v any) (int, bool) {
	switch tv := v.(type) {
	case int:
		return tv, true
	case string:
		n, err := strconv.Atoi(tv)
		return n, err == nil
	default:
		return 0, false
	}
}

// flagString extracts a string from a parseFlagArgs value. ints are
// stringified — parseFlagArgs auto-converts numeric flags, so an all-digits
// id like a player_id "12345" lands as int 12345 even though semantically
// it's text.
func flagString(v any) (string, bool) {
	switch tv := v.(type) {
	case string:
		return tv, true
	case int:
		return strconv.Itoa(tv), true
	default:
		return "", false
	}
}

// flagBool interprets a parseFlagArgs value as a boolean. Accepts the string
// forms "true"/"1" (case-insensitive on the word) and the int forms 0/non-0
// — parseFlagArgs hands us int 1 when the user wrote `--clear 1`.
func flagBool(v any) bool {
	switch tv := v.(type) {
	case bool:
		return tv
	case int:
		return tv != 0
	case string:
		return strings.EqualFold(tv, "true") || tv == "1"
	default:
		return false
	}
}

// partitionFlagBool reads a boolean flag from a partitionFlags result map.
// partitionFlags records bare flags (`--dry_run`) as `""`, which is also the
// zero value for an absent key — so the presence check matters. A bare flag
// is true; explicit `--dry_run=false`/`=0` is false; anything else falls
// through flagBool's string rules.
func partitionFlagBool(flags map[string]string, key string) bool {
	v, ok := flags[key]
	if !ok {
		return false
	}
	if v == "" {
		return true
	}
	return strings.EqualFold(v, "true") || v == "1"
}

// coerceBoolFlags rewrites the named payload entries from their parsed string/int
// form (as produced by parseFlagArgs) into real JSON booleans, so commands whose
// server fields are typed boolean send `true`/`false` rather than "true"/"false".
// Missing keys are left untouched; a present value that is not a recognizable
// boolean (per strconv.ParseBool: 1/t/T/TRUE/true, 0/f/F/FALSE/false, …) is an
// error. The bare-flag form (`--ally_fuel_access`) arrives as "true" and is
// accepted as true.
func coerceBoolFlags(payload map[string]any, keys ...string) error {
	for _, k := range keys {
		v, ok := payload[k]
		if !ok {
			continue
		}
		b, err := strconv.ParseBool(fmt.Sprint(v))
		if err != nil {
			return fmt.Errorf("--%s must be true or false, got %q", k, fmt.Sprint(v))
		}
		payload[k] = b
	}
	return nil
}

// parseFlagArgs parses `--key value`, `--key=value`, and bare `--flag` (→ "true")
// tokens for the named keys into a payload map, converting integer-looking
// values to ints. Flags must use two dashes. A single-dash long flag such as
// `-category` is almost always a typo for `--category` and would otherwise be
// silently dropped, so it returns an error pointing the operator at the two-dash
// form. Negative-number values (e.g. `-5`) pass through untouched.
func parseFlagArgs(args []string, keys ...string) (map[string]any, error) {
	allowed := make(map[string]bool, len(keys))
	for _, k := range keys {
		allowed[k] = true
	}
	result := make(map[string]any)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "--") {
			// Reject a single-dash long flag (-name, where name starts with a
			// letter) instead of dropping it silently. Values like -5 are fine.
			if len(arg) > 1 && arg[0] == '-' {
				if c := arg[1]; (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
					return nil, fmt.Errorf("flag %q must use two dashes: --%s", arg, strings.TrimPrefix(arg, "-"))
				}
			}
			continue
		}
		trimmed := strings.TrimPrefix(arg, "--")

		var key, value string
		if k, v, ok := strings.Cut(trimmed, "="); ok {
			// --key=value form: value is in the same token.
			key, value = k, v
		} else {
			// --key value form. If there's no next token, or the next token
			// is itself a flag, treat this as a bare boolean flag ("true").
			// This makes `... --detail` and `--detail --max 10` both work as
			// the operator expects.
			key = trimmed
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
				value = "true"
			} else {
				i++
				value = args[i]
			}
		}
		if !allowed[key] {
			continue
		}

		// Try to parse as integer first
		if intVal, err := strconv.Atoi(value); err == nil {
			result[key] = intVal
		} else {
			// Keep as string if not a number
			result[key] = value
		}
	}
	return result, nil
}

// partitionFlags separates positional arguments from "--flag" / "--flag=value"
// / "--flag value" flags so flags may appear in any position. For the
// space-separated form, the following token is consumed as the value unless it
// is itself a flag.
func partitionFlags(args []string) (positional []string, flags map[string]string) {
	flags = make(map[string]string)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "--") {
			positional = append(positional, arg)
			continue
		}
		trimmed := strings.TrimPrefix(arg, "--")
		if k, v, ok := strings.Cut(trimmed, "="); ok {
			flags[k] = v
			continue
		}
		if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
			flags[trimmed] = args[i+1]
			i++
			continue
		}
		flags[trimmed] = ""
	}
	return positional, flags
}

// partitionFlagsKV is partitionFlags plus folding of bare `key=value` tokens
// (e.g. `action=queue`) into flags. Use it for commands whose positional
// arguments are identifiers that never contain '=' (recipe ids), so the
// unprefixed key=value form works alongside --flags. Not used for commands
// with free-text positionals (e.g. drone names) where '=' may be literal.
func partitionFlagsKV(args []string) (positional []string, flags map[string]string) {
	raw, flags := partitionFlags(args)
	for _, a := range raw {
		if k, v, ok := strings.Cut(a, "="); ok && k != "" {
			flags[k] = v
			continue
		}
		positional = append(positional, a)
	}
	return positional, flags
}

func parseQuantity(s string) (float64, error) {
	// Try parsing as float first
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		// Try parsing as int
		i, err2 := strconv.Atoi(s)
		if err2 != nil {
			return 0, fmt.Errorf("cannot parse %q as number", s)
		}
		return float64(i), nil
	}
	return f, nil
}

func printState(client game.GameClient) {
	state := client.GetState()

	// Print summary
	fmt.Printf("\n📊 State Summary:\n")
	fmt.Printf("  Player: %s\n", state.Player.Username)
	fmt.Printf("  Credits: %.0f\n", state.Credits)
	fmt.Printf("  Location: %s / %s\n", state.System.Name, state.CurrentPOI)
	if state.Doc {
		fmt.Printf("  Status: Docked\n")
	} else {
		fmt.Printf("  Status: In space\n")
	}
	fmt.Printf("  Hull: %.0f/%.0f\n", state.Ship.Hull, state.Ship.MaxHull)
	fmt.Printf("  Fuel: %.0f/%.0f\n", state.Ship.Fuel, state.Ship.MaxFuel)
	fmt.Printf("  Cargo: %.0f/%.0f\n", state.Ship.CargoUsed, state.Ship.CargoCapacity)

	// For full JSON, user can use 'raw' command with specific keys
	fmt.Println("\n💡 Tip: Use 'raw <key>' to see full JSON for specific data")
	fmt.Println("   Available keys: player, ship, system, poi, etc.")
}

// executeLogicalCommand dispatches one logical command string — a bare command
// or a loop (single or block form) — and renders the statusline afterward. It
// returns a non-nil error only for conditions that should stop a running
// script: a non-force loop failure, a fatal *tokenError, or a bare-command
// error. Ordinary per-command errors are printed here; the REPL ignores the
// return value, while `run` uses it to stop a script.
func executeLogicalCommand(client game.GameClient, ctx context.Context, cmd string, format outputFormat, cfg PlayAsConfig, agentID string) error {
	firstLine := cmd
	if nl := strings.IndexByte(firstLine, '\n'); nl >= 0 {
		firstLine = firstLine[:nl]
	}
	parts := worker.SplitArgs(firstLine)
	if len(parts) == 0 {
		return nil
	}
	command := strings.ToLower(parts[0])

	// Wake on input: if the transport dropped and the background reconnector has
	// gone dormant after exhausting a burst, a user command re-arms it and waits
	// briefly so a now-restored connection lets this command run instead of
	// failing. WaitForReady returns immediately once connected.
	if wsClient, ok := client.(*game.Client); ok && !wsClient.IsConnected() {
		if wsClient.RequestReconnect() {
			fmt.Println("⟳ disconnected — reconnecting…")
		}
		// Brief wait so a now-restored connection lets this command through;
		// returns immediately on success, and we fall through to run (and fail
		// gracefully) if the outage persists rather than blocking the REPL.
		_ = wsClient.WaitForReady(ctx, game.SleepMedium)
	}

	var resultErr error
	if command == "loop" {
		if !worker.HasTopLevelOpenBrace(cmd) {
			resultErr = runLoopSingle(client, ctx, parts, format)
		} else {
			stmt := worker.Statement{Raw: cmd, Tokens: worker.SplitArgs(firstLine)}
			count, force, body, isBlock, perr := worker.ParseLoopHeader(stmt)
			switch {
			case perr != nil:
				fmt.Printf("❌ %v\n", perr)
				resultErr = perr
			case !isBlock:
				resultErr = fmt.Errorf("loop: expected block body")
				fmt.Printf("❌ %v\n", resultErr)
			default:
				stmts, serr := worker.ParseStatements(body)
				switch {
				case serr != nil:
					fmt.Printf("❌ %v\n", serr)
					resultErr = serr
				case len(stmts) == 0:
					resultErr = fmt.Errorf("loop: empty block")
					fmt.Printf("❌ %v\n", resultErr)
				default:
					preview := worker.BlockPreview(stmts)
					if force {
						fmt.Printf("🔁 Repeating { %s } %d time(s) (force mode)...\n", preview, count)
					} else {
						fmt.Printf("🔁 Repeating { %s } %d time(s)...\n", preview, count)
					}
					runStatement := func(tokens []string) error {
						return executeCommand(client, ctx, tokens, format)
					}
					resultErr = worker.ExecuteLoop(ctx, os.Stdout, count, force, stmts, 0, runStatement)
				}
			}
		}
	} else {
		startTime := time.Now()
		if err := executeCommand(client, ctx, parts, format); err != nil {
			var goal *game.GoalReachedError
			if errors.As(err, &goal) {
				fmt.Printf("✓ goal reached: %s\n", goal.Message)
			} else {
				fmt.Printf("❌ %s\n", formatError(err, command, format))
				resultErr = err
			}
		} else {
			fmt.Printf("✓ Completed in %v\n", time.Since(startTime))
		}
	}

	if sl := renderStatusline(client, cfg, agentID); sl != "" {
		fmt.Println(sl)
	}
	fmt.Println()
	return resultErr
}

// runLoopSingle handles the legacy "loop [-f] <count> <command...>" form
// with a single command. It returns a non-nil stopping error when the loop
// should abort the enclosing script: a fatal *tokenError or a non-force
// command failure. Goal-reached exits and validation/usage errors return nil
// (the script should continue or the bad input has already been reported).
func runLoopSingle(client game.GameClient, ctx context.Context, parts []string, format outputFormat) error {
	if len(parts) < 3 {
		fmt.Println("Usage: loop [-f] <count> <command...>")
		fmt.Println("       loop [-f] <count> { stmt; stmt; ... }")
		fmt.Println("  -f  Force: continue on errors instead of stopping")
		fmt.Println("Examples: loop 5 mine")
		fmt.Println("          loop -f 20 mine")
		fmt.Println("          loop 10 sell iron_ore 5")
		fmt.Println("          loop 3 { travel sol_belt; mine; mine; dock }")
		fmt.Println()
		return nil
	}
	forceLoop := false
	argIdx := 1
	if parts[argIdx] == "-f" {
		forceLoop = true
		argIdx++
		if argIdx >= len(parts)-1 {
			fmt.Println("Usage: loop [-f] <count> <command...>")
			fmt.Println()
			return nil
		}
	}
	count, countErr := strconv.Atoi(parts[argIdx])
	if countErr != nil || count < 1 {
		fmt.Printf("❌ Invalid count: %s (must be a positive integer)\n\n", parts[argIdx])
		return nil //nolint:nilerr // validation failure: bad user input is not a script-stopping condition
	}
	loopParts := parts[argIdx+1:]
	loopCmd := strings.Join(loopParts, " ")
	if forceLoop {
		fmt.Printf("🔁 Repeating %q %d time(s) (force mode)...\n", loopCmd, count)
	} else {
		fmt.Printf("🔁 Repeating %q %d time(s)...\n", loopCmd, count)
	}
	errs := 0
	var stopErr error
	for i := range count {
		fmt.Printf("── [%d/%d] %s\n", i+1, count, loopCmd)
		startTime := time.Now()
		if cerr := executeCommand(client, ctx, loopParts, format); cerr != nil {
			// Goal-reached is a positive exit (innermost only). -f does not
			// override — re-running a satisfied command is pointless.
			var goal *game.GoalReachedError
			if errors.As(cerr, &goal) {
				fmt.Printf("🎯 goal reached: %s → exiting loop\n", goal.Message)
				break
			}
			var tokErr *worker.TokenError
			if errors.As(cerr, &tokErr) {
				fmt.Printf("❌ %s → aborting loop\n", tokErr)
				stopErr = cerr
				break
			}
			errs++
			fmt.Printf("❌ %s\n", formatError(cerr, loopParts[0], format))
			if !forceLoop {
				fmt.Printf("Stopping loop after %d/%d iterations\n", i+1, count)
				stopErr = cerr
				break
			}
			fmt.Printf("⚠️  Error %d (continuing due to -f)...\n", errs)
			continue
		}
		duration := time.Since(startTime)
		fmt.Printf("✓ [%d/%d] Completed in %v\n", i+1, count, duration)
	}
	if forceLoop && errs > 0 {
		fmt.Printf("🔁 Loop finished with %d error(s) out of %d iterations\n", errs, count)
	}
	return stopErr
}

func printHelp() {
	fmt.Println("\n📖 Available Commands:")
	fmt.Println("\n=== NAVIGATION ===")
	fmt.Println("  dock, undock              - Dock/undock from current POI")
	fmt.Println("  travel <poi>              - Travel to a POI")
	fmt.Println("  jump <system>             - Jump to another system")
	fmt.Println("  find_route <system>       - Find route to system")
	fmt.Println("  nearest <poi_type>        - Find nearest POIs by type (e.g., 'nearest station')")
	fmt.Println("  autopilot <system> [poi]  - Auto-navigate to system (and optional POI)")
	fmt.Println("  plan_route [--return] <systems...>  - Optimal jump order to visit systems; prints autopilot cmds")
	fmt.Println("  explore                   - Visit all POIs in current system (nearest-first)")
	fmt.Println("  auto_explore [--max-hops N]")
	fmt.Println("                            - Tour multiple systems: explore + jump outward, refuel at stations")

	fmt.Println("\n=== MINING & COMBAT ===")
	fmt.Println("  mine, scan, survey        - Mining and scanning operations")
	fmt.Println("  attack <target-id>        - Attack a target")
	fmt.Println("  cloak [on|off]           - Toggle cloaking device")

	fmt.Println("\n=== COMMERCE ===")
	fmt.Println("  sell <item> <qty>         - Sell items")
	fmt.Println("  buy <item> <qty>          - Buy items")
	fmt.Println("  listings, trades          - View market listings/trades")
	fmt.Println("  view_market <item>        - View market for item")
	fmt.Println("  update_market             - Capture current station's market into market DB")
	fmt.Println("  view_orders               - View your orders")
	fmt.Println("  create_sell_order <item> <qty> <price>  - Create sell order")
	fmt.Println("  create_buy_order <item> <qty> <price>   - Create buy order")
	fmt.Println("  demand [--station-only] [--hide-player-only] [--include-mine] [--show-none-onhand] [--only fulfillable|craftable|all] [--sort price|proceeds|age] [--item id] [--station id] [--min-price N] [--max-age D] [--limit N] - where can I sell? (captured buy-order demand)")
	fmt.Println("       (defaults: skip rows that are entirely my own orders, and rows with 0 on-hand & 0 craftable)")
	fmt.Println("  demand history <item> [--station id] [--limit N]   - demand price/qty trend per station")

	fmt.Println("\n=== CRAFTING ===")
	fmt.Println("  craft <recipe> [qty] [--deliver_to=storage|faction] [--facility_id=ID] [--preset=fast|cheap|workshop] [--dry_run] - Queue a crafting job")
	fmt.Println("  craft queue - List your current crafting jobs")
	fmt.Println("  recycle <recipe> [qty] [--deliver_to=storage|faction] [--facility_id=ID] [--dry_run] - Queue a recycling job (lossy)")
	fmt.Println("  recipes                   - Get available recipes")
	fmt.Println("  craftable [--reachable] [--category C] [--search S] [--detail] [--include-facility-only] [--include-ship-passive] [--sort=name|category|can_make_asc|id] - what you can build now")
	fmt.Println("  plan <recipe-or-item-id> [qty] [--reachable]   - gap analysis; prints craft cmd when ready")

	fmt.Println("\n=== SHIP ===")
	fmt.Println("  refuel, repair            - Refuel and repair ship")
	fmt.Println("  install, install_mod <item>  - Install equipment")
	fmt.Println("  uninstall, uninstall_mod <module> - Uninstall module")
	fmt.Println("  buy_ship <class>          - Buy a new ship")
	fmt.Println("  browse_ships [--base_id X] - Browse ships for sale (at current or specified base)")
	fmt.Println("  list_ships                - List your ships")
	fmt.Println("  switch_ship <ship-id>     - Switch to another ship")
	fmt.Println("  sell_ship <ship-id>       - Sell a ship")

	fmt.Println("\n=== DRONES ===")
	fmt.Println("  get_drones, drones                  - List your drones")
	fmt.Println("  deploy_drone [<id>|--all]           - Launch one drone (or --all in-bay drones in one tick)")
	fmt.Println("  recall_drone [<id>|--all]           - Recall one drone (or --all at current location)")
	fmt.Println("  set_drone_name <drone-id> <name>... - Rename a drone (≤32 chars; empty clears)")
	fmt.Println("  upload_drone_script <drone-id> <script>  - Upload DroneLang script to a deployed drone")
	fmt.Println("  bulk_upload_drone_script --file P [--type T] [--status S] [--name N] [--dry_run]")
	fmt.Println("                                      - Push one script to every matching drone (1 tick each)")

	fmt.Println("\n=== CARGO & STORAGE ===")
	fmt.Println("  cargo                     - View ship cargo")
	fmt.Println("  deposit <item> <qty> [--source=<s>] [--target=<s>]   - Deposit items (source/target: cargo|storage|faction)")
	fmt.Println("  deposit_all               - Deposit all items")
	fmt.Println("  withdraw <item> <qty> [--source=<s>] [--target=<s>]  - Withdraw items (source/target: cargo|storage|faction)")
	fmt.Println("  storage [filter] [--group] [--station_id <id>]  - View storage (--group: by category; filter: id/name substring)")
	fmt.Println("  storage_at <id>           - View storage at a remote station")
	fmt.Println("  jettison <item> <qty>     - Jettison cargo")

	fmt.Println("\n=== WRECKS ===")
	fmt.Println("  wrecks                    - List nearby wrecks")
	fmt.Println("  loot <wreck> <item> <qty> - Loot from wreck")
	fmt.Println("  salvage <wreck>           - Salvage entire wreck")

	fmt.Println("\n=== QUERIES ===")
	fmt.Println("  status, system, ship      - Get current status/system/ship")
	fmt.Println("  skills, poi, base         - Get skills/POI/base info")
	fmt.Println("  map, nearby, version      - Get map/nearby/version")
	fmt.Println("  state                     - Show state summary")
	fmt.Println("  get_tax_estimate, taxes   - Show property/income tax assessment + sales rates")
	fmt.Println("  raw <key>                 - Show raw JSON for key")

	fmt.Println("\n=== FACTIONS ===")
	fmt.Println("  create_faction <name> <tag>  - Create a faction (tag = 4 chars)")
	fmt.Println("  join_faction <id>            - Join a faction")
	fmt.Println("  leave_faction                - Leave current faction")
	fmt.Println("  faction_info [faction-id]     - View faction details")
	fmt.Println("  faction_list [--seed]         - List all factions (--seed pages all and seeds the KB)")
	fmt.Println("  faction_edit --description \"text\" --charter \"text\" [--primary_color \"#hex\"] [--secondary_color \"#hex\"] [--ally_intel_opt_out true|false] [--ally_fuel_access true|false]")
	fmt.Println("  faction_invite <player-id>    - Invite a player")
	fmt.Println("  faction_kick <player-id>      - Kick a member")
	fmt.Println("  faction_promote <player> <role> - Promote/demote member")
	fmt.Println("  faction_get_invites           - View pending invitations")
	fmt.Println("  faction_decline_invite <id>   - Decline invitation")
	fmt.Println("  faction_declare_war <id> [reason]  - Declare war")
	fmt.Println("  faction_propose_peace <id> [terms] - Propose peace")
	fmt.Println("  faction_accept_peace <id>     - Accept peace proposal")
	fmt.Println("  faction_propose_ally <id>     - Propose a mutual alliance")
	fmt.Println("  faction_accept_ally <id>      - Accept an alliance proposal")
	fmt.Println("  faction_remove_ally <id>      - Dissolve an alliance")
	fmt.Println("  faction_set_enemy <id>        - Mark faction as enemy")
	fmt.Println("  faction_deposit_credits <amt> - Deposit credits to treasury")
	fmt.Println("  faction_withdraw_credits <amt> - Withdraw from treasury")
	fmt.Println("  faction_deposit_items <item> <qty>  - Deposit to faction storage")
	fmt.Println("  faction_withdraw_items <item> <qty> - Withdraw from faction storage")
	fmt.Println("  view_faction_storage [filter] [--group]  - View faction storage (--group: by category; filter: id/name substring)")
	fmt.Println("  faction_create_buy_order <item> <qty> <price>  - Faction buy order")
	fmt.Println("  faction_create_sell_order <item> <qty> <price> - Faction sell order")
	fmt.Println("  faction_rooms                 - List faction rooms")
	fmt.Println("  faction_visit_room <id>       - Visit a room")
	fmt.Println("  faction_write_room --name \"n\" --description \"d\" [--room_id id]")
	fmt.Println("  faction_delete_room <id>      - Delete a room")
	fmt.Println("  faction_query_intel --system_name \"name\"  - Query intel DB")
	fmt.Println("  faction_query_trade_intel --item_id \"id\"  - Query trade intel")
	fmt.Println("  faction_intel_status           - Intel coverage stats")
	fmt.Println("  faction_trade_intel_status      - Trade intel coverage")
	fmt.Println("  faction_create_role <name> <priority> - Create role")
	fmt.Println("  faction_edit_role <id> [--name \"n\"]    - Edit role")
	fmt.Println("  faction_delete_role <id>       - Delete role")
	fmt.Println("  faction_list_missions          - List faction missions")
	fmt.Println("  faction_cancel_mission <id>    - Cancel a faction mission")

	fmt.Println("\n=== COMMUNICATION ===")
	fmt.Println("  chat <channel> <msg>                    - Send chat message")
	fmt.Println("  chat private <target> <msg>            - Send private message")
	fmt.Println("  chat_history <channel>                  - Get chat history")
	fmt.Println("  chat_history private                    - Get whole DM inbox (newest-first)")
	fmt.Println("  chat_history private --target_id <name> - Get one DM conversation")
	fmt.Println("  send_gift <recipient> <item_id> <qty>  - Send items")
	fmt.Println("  send_gift <recipient> credits <amount> - Send credits")
	fmt.Println("  send_gift <recipient> ship <ship_id>   - Send ship")

	fmt.Println("\n=== FORUM ===")
	fmt.Println("  forum_list [page]         - List forum threads")
	fmt.Println("  forum_thread <id>         - Get forum thread")

	fmt.Println("\n=== KNOWLEDGE BASE ===")
	fmt.Println("  update_system             - Save current system data to KB")
	fmt.Println("  update_poi                - Save current POI data to KB")
	fmt.Println("  update_station            - Save base, market, ships to KB (must be docked)")
	fmt.Println("  update_facilities         - Save facility details to KB (must be docked)")
	fmt.Println("  update_missions           - Save mission board templates to KB")
	fmt.Println("  update_all                - Run all update commands for current location")
	fmt.Println("  update_faction_data       - Save faction data to KB (must be in a faction)")
	fmt.Println("  seen_factions [--seed]    - List factions seen on other agents; --seed backfills them into the factions table")
	fmt.Println("  passenger [<id>] [--empire X] - Browse the stored passenger catalog (Name (id), Empire, Bio); <id> shows full detail")

	fmt.Println("\n=== OTHER ===")
	fmt.Println("  log <entry>               - Add captain's log entry")
	fmt.Println("  notes                     - Get your notes")
	fmt.Println("  missions [--full], accept_mission - Mission commands (--full skips description truncation)")
	fmt.Println("  action_log [--category X] [--page N] - Action history")
	fmt.Println("  loop [-f] <count> <command>        - Repeat a command N times (-f continues on errors)")
	fmt.Println("  loop [-f] <count> { stmt; stmt }   - Repeat a block; stmts may nest and use newlines or ';'")
	fmt.Println("  sellable [-d] [--min-proceeds N]   - What can I sell here? (cargo+storage @ this station's market)")
	fmt.Println("  sleep <secs> | wait <duration>     - Pause N seconds (or 30s, 1m, 500ms); Ctrl-C interrupts")
	fmt.Println("  history                   - Show last 25 commands (persisted across sessions)")
	fmt.Println("  mbox                      - Show unread message counts")
	fmt.Println("  mbox list [ch] [--unread] - List messages (newest first)")
	fmt.Println("  mbox search <query>       - Full-text search messages")
	fmt.Println("  mbox show <id>            - Show message detail")
	fmt.Println("  mbox mark-read <id>|--all - Mark messages as read")
	fmt.Println("  mbox backfill [--channel] [-f] - Deep crawl message history (-f resets cursor)")
	fmt.Println("  mbox sources              - Push/backfill/reconcile counts")
	fmt.Println("  schedule_add <hourly|daily|weekly> <command...> - Run a command on a recurring schedule (runs once now)")
	fmt.Println("  schedule_remove <id>      - Remove a scheduled command")
	fmt.Println("  view_scheduled            - List scheduled commands")
	fmt.Println("  set_format <mode>         - Set output: raw, json, or styled")
	fmt.Println("  set_debug <true|false>    - Toggle game client debug logging at runtime")
	fmt.Println("  help                      - Show this help")
	fmt.Println("  exit, quit                - Exit terminal")
	fmt.Println()
	fmt.Println("📝 All commands are case-insensitive")
	fmt.Println()
}

// --- Mbox command handlers ---

func handleMboxCommand(store *mbox.Store, ing *mbox.Ingester, bl *mbox.Blocklist, client game.GameClient, ctx context.Context, args []string) {
	if len(args) == 0 {
		counts, err := store.UnreadCounts()
		if err != nil {
			fmt.Printf("error: %v\n", err)
			return
		}
		for _, ch := range knownChatChannels {
			n := counts[ch]
			color := channelColors[ch]
			reset := "\033[0m"
			if n > 0 {
				fmt.Printf("  %s%-9s%s %d unread\n", color, ch, reset, n)
			} else {
				fmt.Printf("  %-9s 0 unread\n", ch)
			}
		}
		return
	}

	sub := strings.ToLower(args[0])
	switch sub {
	case "list":
		mboxList(store, args[1:])
	case "show":
		if len(args) < 2 {
			fmt.Println("usage: mbox show <id>")
			return
		}
		mboxShow(store, args[1])
	case "search":
		if len(args) < 2 {
			fmt.Println("usage: mbox search <query> [--channel <ch>]")
			return
		}
		mboxSearch(store, args[1:])
	case "mark-read", "read":
		// `read` kept as a deprecated alias for muscle memory. `show`
		// already displays message content so `read` as a name is
		// ambiguous (open vs. mark-read). Prefer `mark-read`.
		mboxRead(store, args[1:])
	case "delete":
		if len(args) < 2 {
			fmt.Println("usage: mbox delete <id>")
			return
		}
		mboxDelete(store, args[1])
	case "restore":
		if len(args) < 2 {
			fmt.Println("usage: mbox restore <id>")
			return
		}
		mboxRestore(store, args[1])
	case "backfill":
		mboxBackfill(ing, client, ctx, args[1:])
	case "sources":
		mboxSources(store)
	case "mark-spam":
		mboxMarkSpam(store, bl, args[1:])
	case "unmark-spam":
		mboxUnmarkSpam(store, bl, args[1:])
	case "spam-list":
		mboxSpamList(bl)
	default:
		fmt.Println("mbox commands: list, show, search, mark-read, delete, restore, backfill, sources, mark-spam, unmark-spam, spam-list")
		fmt.Println("  mbox                                      show unread counts")
		fmt.Println("  mbox list [channel|spam] [--unread] [-n N] list messages (ID prefix shown)")
		fmt.Println("  mbox show <id>                            show message detail (accepts ID prefix)")
		fmt.Println("  mbox search <query> [--channel <ch>]      full-text search")
		fmt.Println("  mbox mark-read <id>|--all|--channel <ch>  mark as read")
		fmt.Println("  mbox delete <id>                          soft-delete a message (reversible)")
		fmt.Println("  mbox restore <id>                         undo a soft-delete")
		fmt.Println("  mbox backfill [--channel <ch>] [--limit N] [-f|--reset]")
		fmt.Println("  mbox sources                              source breakdown (push/poll/sent/...)")
		fmt.Println("  mbox mark-spam <user>                     block a sender (alias: mark_spam)")
		fmt.Println("  mbox unmark-spam <user>                   unblock a sender (alias: unmark_spam)")
		fmt.Println("  mbox spam-list                            list blocked senders (alias: spam_list)")
	}
}

// mboxMarkSpam blocks a sender (by sender_id or display name) and moves any of
// their already-stored messages into the spam folder.
func mboxMarkSpam(store *mbox.Store, bl *mbox.Blocklist, args []string) {
	if len(args) == 0 {
		fmt.Println("usage: mark_spam <user>  (sender id or display name)")
		return
	}
	user := strings.Join(args, " ")
	added, err := bl.Add(user)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	n, err := store.MarkSpamBySender(user)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	if added {
		fmt.Printf("blocked %q; moved %d message(s) to spam\n", user, n)
	} else {
		fmt.Printf("%q already blocked; moved %d message(s) to spam\n", user, n)
	}
}

// mboxUnmarkSpam unblocks a sender and restores their spam-flagged messages.
func mboxUnmarkSpam(store *mbox.Store, bl *mbox.Blocklist, args []string) {
	if len(args) == 0 {
		fmt.Println("usage: unmark_spam <user>  (sender id or display name)")
		return
	}
	user := strings.Join(args, " ")
	removed, err := bl.Remove(user)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	n, err := store.UnmarkSpamBySender(user)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	if removed {
		fmt.Printf("unblocked %q; restored %d message(s) from spam\n", user, n)
	} else {
		fmt.Printf("%q was not blocked; restored %d message(s) from spam\n", user, n)
	}
}

// mboxSpamList prints the blocked senders.
func mboxSpamList(bl *mbox.Blocklist) {
	entries := bl.List()
	if len(entries) == 0 {
		fmt.Println("  (no blocked senders)")
		return
	}
	fmt.Printf("  %d blocked sender(s):\n", len(entries))
	for _, e := range entries {
		fmt.Printf("    %s\n", e)
	}
}

func mboxList(store *mbox.Store, args []string) {
	q := mbox.Query{Limit: 20}
	for i := 0; i < len(args); i++ {
		switch strings.ToLower(args[i]) {
		case "--unread":
			q.UnreadOnly = true
		case "-n":
			if i+1 < len(args) {
				i++
				if n, err := strconv.Atoi(args[i]); err == nil {
					q.Limit = n
				}
			}
		case "spam":
			// `mbox list spam` shows the spam folder rather than a channel.
			q.SpamOnly = true
		default:
			if q.Channel == "" {
				q.Channel = strings.ToLower(args[i])
			}
		}
	}

	msgs, err := store.List(q)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	if len(msgs) == 0 {
		fmt.Println("  (no messages)")
		return
	}
	for _, m := range msgs {
		printMboxMessage(m)
	}
}

// resolveMboxID accepts a full ID or an unambiguous prefix and returns
// the matching full ID. Returns "" and prints an error message if no
// match or the prefix is ambiguous.
func resolveMboxID(store *mbox.Store, id string) string {
	if m, err := store.Get(id); err == nil && m != nil {
		return m.ID
	}
	m, err := store.GetByPrefix(id)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return ""
	}
	if m == nil {
		fmt.Printf("message %q not found\n", id)
		return ""
	}
	return m.ID
}

func mboxDelete(store *mbox.Store, id string) {
	full := resolveMboxID(store, id)
	if full == "" {
		return
	}
	if err := store.SoftDelete(full); err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	fmt.Printf("deleted %s\n", full)
}

func mboxRestore(store *mbox.Store, id string) {
	full := resolveMboxID(store, id)
	if full == "" {
		return
	}
	if err := store.Restore(full); err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	fmt.Printf("restored %s\n", full)
}

func mboxShow(store *mbox.Store, id string) {
	msg, err := store.Get(id)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	if msg == nil {
		// Try prefix lookup so `mbox show <short-id>` works with the
		// 8-char prefix displayed by `mbox list`.
		msg, err = store.GetByPrefix(id)
		if err != nil {
			fmt.Printf("error: %v\n", err)
			return
		}
	}
	if msg == nil {
		fmt.Printf("message %q not found\n", id)
		return
	}
	color := channelColors[msg.Channel]
	reset := "\033[0m"
	fmt.Printf("  ID:        %s\n", msg.ID)
	fmt.Printf("  Channel:   %s%s%s\n", color, msg.Channel, reset)
	fmt.Printf("  Sender:    %s (%s)\n", msg.Sender, msg.SenderID)
	fmt.Printf("  Time:      %s (%s)\n", msg.TimestampUTC.Format(time.RFC3339), mboxRelativeTime(msg.TimestampUTC))
	if msg.TargetID != "" {
		fmt.Printf("  Target:    %s", msg.TargetID)
		if msg.TargetName != "" {
			fmt.Printf(" (%s)", msg.TargetName)
		}
		fmt.Println()
	}
	fmt.Printf("  Source:    %s\n", msg.Source)
	if msg.DeletedAt != nil {
		fmt.Printf("  Deleted:   %s (soft-deleted; use `mbox restore %s` to undo)\n",
			msg.DeletedAt.Format(time.RFC3339), msg.ID[:8])
	}
	read := "unread"
	if msg.ReadAt != nil {
		read = msg.ReadAt.Format(time.RFC3339)
	}
	fmt.Printf("  Read:      %s\n", read)
	fmt.Printf("  Content:\n    %s\n", msg.Content)
}

func mboxSearch(store *mbox.Store, args []string) {
	if len(args) == 0 {
		return
	}
	text := args[0]
	q := mbox.Query{Limit: 20}
	for i := 1; i < len(args); i++ {
		if strings.ToLower(args[i]) == "--channel" && i+1 < len(args) {
			i++
			q.Channel = strings.ToLower(args[i])
		}
	}

	msgs, err := store.Search(text, q)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	total, err := store.SearchCount(text, q)
	if err != nil {
		// Non-fatal — fall back to displayed count.
		total = len(msgs)
	}
	if total == 0 {
		fmt.Println("  (no results)")
		return
	}
	for _, m := range msgs {
		printMboxMessage(m)
	}
	if total > len(msgs) {
		fmt.Printf("  (%d results, showing first %d)\n", total, len(msgs))
	} else {
		fmt.Printf("  (%d results)\n", total)
	}
}

func mboxRead(store *mbox.Store, args []string) {
	if len(args) == 0 {
		fmt.Println("usage: mbox read <id> | --all | --channel <ch>")
		return
	}
	switch strings.ToLower(args[0]) {
	case "--all":
		for _, ch := range knownChatChannels {
			_ = store.MarkChannelRead(ch)
		}
		fmt.Println("  marked all messages read")
	case "--channel":
		if len(args) < 2 {
			fmt.Println("usage: mbox read --channel <ch>")
			return
		}
		ch := strings.ToLower(args[1])
		if err := store.MarkChannelRead(ch); err != nil {
			fmt.Printf("error: %v\n", err)
			return
		}
		fmt.Printf("  marked %s messages read\n", ch)
	default:
		full := resolveMboxID(store, args[0])
		if full == "" {
			return
		}
		if err := store.MarkRead(full); err != nil {
			fmt.Printf("error: %v\n", err)
			return
		}
		fmt.Printf("  marked %s read\n", full)
	}
}

func mboxBackfill(ing *mbox.Ingester, client game.GameClient, ctx context.Context, args []string) {
	opts := mbox.BackfillOptions{
		Channels:      []string{"system", "local", "faction"},
		MaxPerChannel: 500,
	}
	for i := 0; i < len(args); i++ {
		switch strings.ToLower(args[i]) {
		case "--channel":
			if i+1 < len(args) {
				i++
				opts.Channels = []string{strings.ToLower(args[i])}
			}
		case "--limit":
			if i+1 < len(args) {
				i++
				if n, err := strconv.Atoi(args[i]); err == nil {
					opts.MaxPerChannel = n
				}
			}
		case "-f", "--reset":
			opts.ResetCursor = true
		}
	}

	resetNote := ""
	if opts.ResetCursor {
		resetNote = " (cursor reset)"
	}
	fmt.Printf("  backfilling %v (max %d per channel)%s...\n", opts.Channels, opts.MaxPerChannel, resetNote)
	report, err := ing.Backfill(ctx, client, opts)
	if err != nil {
		fmt.Printf("  error: %v\n", err)
		return
	}
	for ch, cr := range report.Channels {
		suffix := ""
		if cr.Capped {
			suffix = " (more available)"
		}
		fmt.Printf("  %s: %d messages%s\n", ch, cr.Fetched, suffix)
	}
}

func mboxSources(store *mbox.Store) {
	counts, err := store.SourceCounts()
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	if len(counts) == 0 {
		fmt.Println("  (no messages)")
		return
	}
	for _, src := range []string{"push", "backfill", "reconcile"} {
		if n, ok := counts[src]; ok {
			fmt.Printf("  %-12s %d\n", src, n)
		}
	}
}

func printMboxMessage(m mbox.Message) {
	color := channelColors[m.Channel]
	reset := "\033[0m"
	bold := "\033[1m"
	dim := "\033[2m"

	unreadMarker := "  "
	senderFmt := m.Sender
	if m.ReadAt == nil {
		unreadMarker = "* "
		senderFmt = bold + m.Sender + reset
	}

	// Verified empire-official messages get a small inline glyph next
	// to the sender so player impersonation of officials is obvious at
	// a glance (server v0.294.0+).
	if m.EmpireOfficial {
		cyan := "\033[36m"
		senderFmt = cyan + "✴" + reset + " " + senderFmt
	}

	// Direction indicator: → for messages we sent, ← for received.
	// Prefer the "sent" Source tag (stamped by the Ingester when
	// SetSelfID is wired); fall back to comparing sender IDs at
	// display time for messages ingested before the tag existed.
	arrow := "\u2190" // ← received
	if m.Source == "sent" || (globalClient != nil && m.SenderID != "" && m.SenderID == mboxSelfID()) {
		arrow = "\u2192" // → sent
	}

	// Short ID prefix for use with `mbox show <id>`. 8 hex chars is
	// enough disambiguation for any realistic mbox size; pad with
	// spaces when a message has no ID so columns line up.
	shortID := m.ID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	if shortID == "" {
		shortID = "        "
	}

	// Channel label is padded to the width of the longest known channel
	// name ("emergency" = 9) so the column lines up regardless of channel.
	fmt.Printf("%s%s[%-9s]%s %s%s%s %s %6s  %s  %s\n",
		unreadMarker, color, m.Channel, reset,
		dim, shortID, reset, arrow,
		mboxRelativeTime(m.TimestampUTC), senderFmt, mboxTruncate(m.Content, 60))
}

// mboxSelfID returns the logged-in player's internal ID, or "" if
// unavailable. Used by display code so direction can be inferred even
// for records that predate the Ingester SelfID tagging.
func mboxSelfID() string {
	if globalClient == nil {
		return ""
	}
	state := globalClient.GetState()
	if state == nil {
		return ""
	}
	return state.Player.ID
}

func mboxRelativeTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func mboxTruncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

// chatPoller periodically polls chat channels and prints new messages.
//
// Over MCP the poller is the primary source of chat: interval defaults to
// SleepChatPoll, silent is false, and each new message is printed + ingested.
//
// Over WebSocket, chat is delivered as push events; the poller runs in
// "reconciler" mode (longer interval, silent=true) to backfill the mbox with
// anything missed during a ~15s reconnect window. Printing to the terminal is
// handled from the SetOnChatMessage push callback instead — see wiring in
// runREPL.
type chatPoller struct {
	client     game.GameClient
	ctx        context.Context
	cancel     context.CancelFunc
	seen       map[string]bool   // Message IDs already displayed.
	lastSeenTS map[string]string // Per-channel newest timestamp_utc, used as `after` cursor.
	mu         sync.Mutex
	username   string          // Our own username, to skip own messages.
	ingester   *mbox.Ingester  // Optional: ingest polled messages into mbox.
	blocklist  *mbox.Blocklist // Optional: blocked senders are not printed.
	interval   time.Duration   // Poll interval (defaults to SleepChatPoll).
	silent     bool            // When true, ingest only; don't print to terminal.
}

// activeChatChannels returns the channels that should be polled based on player state.
// Faction channel is only included if the player is in a faction.
//
// "private" is polled unconditionally: as of server v0.397.0, get_chat_history
// on the private channel with no target_id returns the whole DM inbox (every
// private message across all conversations, newest-first), so a single poll
// surfaces new direct messages from anyone without knowing the sender ahead of
// time.
func activeChatChannels(client game.GameClient) []string {
	channels := []string{"system", "local", "private"}

	// Check if player is in a faction
	state := client.GetState()
	if state != nil && state.Player.FactionID != "" {
		channels = append(channels, "faction")
	}

	return channels
}

// channelColors maps channel names to ANSI color codes for display.
var channelColors = map[string]string{
	"system":    "\033[36m", // cyan
	"local":     "\033[33m", // yellow
	"faction":   "\033[35m", // magenta
	"private":   "\033[32m", // green
	"emergency": "\033[31m", // red
}

// knownChatChannels is the full set of channels the mbox tracks, including
// push-only ones like "emergency" that the server broadcasts but doesn't
// expose via get_chat_history. Used for unread-count display and bulk
// mark-read operations.
var knownChatChannels = []string{"system", "local", "faction", "private", "emergency"}

func newChatPoller(client game.GameClient, ctx context.Context, username string) *chatPoller {
	pollCtx, cancel := context.WithCancel(ctx)
	return &chatPoller{
		client:     client,
		ctx:        pollCtx,
		cancel:     cancel,
		seen:       make(map[string]bool),
		lastSeenTS: make(map[string]string),
		username:   username,
		interval:   game.SleepChatPoll,
	}
}

func (cp *chatPoller) start() {
	// Seed seen messages so we don't replay history on startup.
	cp.seedSeen()

	interval := cp.interval
	if interval <= 0 {
		interval = game.SleepChatPoll
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-cp.ctx.Done():
				return
			case <-ticker.C:
				cp.poll()
			}
		}
	}()
}

func (cp *chatPoller) stop() {
	cp.cancel()
}

// seedSeen fetches current history for each channel and marks all messages as seen.
func (cp *chatPoller) seedSeen() {
	for _, ch := range activeChatChannels(cp.client) {
		msgs, _ := cp.fetchMessages(ch)
		cp.mu.Lock()
		for _, m := range msgs {
			cp.seen[m.ID] = true
		}
		cp.mu.Unlock()
	}
}

// poll fetches new messages from all channels and (unless silent) prints them.
// When the server reports has_more for a channel (more messages exist beyond
// the first page), delegates to Ingester.Backfill so the mbox catches up via
// before-cursor pagination. Dedup is handled by the Store (Ingester.Backfill
// stops when it hits a message ID that's already persisted).
func (cp *chatPoller) poll() {
	for _, ch := range activeChatChannels(cp.client) {
		msgs, hasMore := cp.fetchMessages(ch)
		if len(msgs) == 0 {
			continue
		}

		// Ingest all fetched messages into mbox (deduped by Store).
		if cp.ingester != nil {
			for _, m := range msgs {
				apiMsg := m
				if apiMsg.Channel == "" {
					apiMsg.Channel = ch
				}
				cp.ingester.HandlePolled(apiMsg)
			}

			// Server signalled older messages we didn't fetch. Walk back
			// via the Ingester's paginated backfill, which stops the first
			// time it hits an ID we've already stored.
			if hasMore {
				_, err := cp.ingester.Backfill(cp.ctx, cp.client, mbox.BackfillOptions{
					Channels:      []string{ch},
					MaxPerChannel: 500,
				})
				if err != nil {
					log.Printf("[mbox] poll backfill %s: %v", ch, err)
				}
			}
		}

		if cp.silent {
			continue
		}

		// Messages come newest-first; reverse to print chronologically.
		slices.Reverse(msgs)
		for _, m := range msgs {
			cp.displayMessage(ch, m)
		}
	}
}

// displayMessage renders one chat message to the terminal, applying dedup,
// self-skip, and system/local target-system filtering. Shared between the
// polling path (MCP primary) and the WS push callback.
func (cp *chatPoller) displayMessage(channel string, m serverapi.ChatMessage) {
	if channel == "" {
		channel = m.Channel
	}

	cp.mu.Lock()
	if cp.seen[m.ID] {
		cp.mu.Unlock()
		return
	}
	cp.seen[m.ID] = true
	cp.mu.Unlock()

	// Skip our own messages.
	if strings.EqualFold(m.Sender, cp.username) {
		return
	}

	// Skip blocked senders — they're captured silently in the spam folder.
	if cp.blocklist.IsBlocked(m.SenderID, m.Sender) {
		return
	}

	// Filter system/local messages by target system.
	if channel == "system" || channel == "local" {
		currentSystemID := ""
		if globalClient != nil {
			if state := globalClient.GetState(); state != nil {
				currentSystemID = state.System.ID
			}
		}
		if m.TargetID != "" && currentSystemID != "" && !strings.EqualFold(m.TargetID, currentSystemID) {
			return
		}
	}

	// Debug: dump full JSON for specific senders to investigate filtering.
	if m.Sender == "N Nagata" || m.Sender == "GunnyDraper" || m.Sender == "Chrisjen Avasarala" {
		raw, _ := json.MarshalIndent(m, "  ", "  ")
		fmt.Printf("\r  DEBUG POLLER [%s]:\n  %s\n", m.Sender, string(raw))
	}

	color := channelColors[channel]
	reset := "\033[0m"
	fmt.Printf("\r%s[%s]%s %s: %s\n", color, channel, reset, m.Sender, m.Content)
}

// fetchMessages pulls the latest page of messages for channel. The returned
// hasMore flag mirrors the server's `has_more` field — when true there are
// older messages beyond this page that the caller should backfill (see
// poll() which delegates to Ingester.Backfill in that case).
func (cp *chatPoller) fetchMessages(channel string) (msgs []serverapi.ChatMessage, hasMore bool) {
	payload := map[string]any{"limit": 100}
	cp.mu.Lock()
	if ts := cp.lastSeenTS[channel]; ts != "" {
		payload["after"] = ts
	}
	cp.mu.Unlock()

	if err := cp.client.GetChatHistory(cp.ctx, channel, payload); err != nil {
		return nil, false
	}
	raw := cp.client.GetRawJSON("_last")
	if len(raw) == 0 {
		return nil, false
	}
	var resp struct {
		Messages []serverapi.ChatMessage `json:"messages"`
		HasMore  bool                    `json:"has_more"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, false
	}

	// Server returns messages newest-first; advance the cursor to the newest
	// timestamp so the next poll only sees strictly newer messages.
	if len(resp.Messages) > 0 {
		newest := resp.Messages[0].TimestampUTC
		for _, m := range resp.Messages[1:] {
			if m.TimestampUTC > newest {
				newest = m.TimestampUTC
			}
		}
		if newest != "" {
			cp.mu.Lock()
			if newest > cp.lastSeenTS[channel] {
				cp.lastSeenTS[channel] = newest
			}
			cp.mu.Unlock()
		}
	}

	return resp.Messages, resp.HasMore
}

// isTankFullError reports whether err is the server's "fuel tank already
// full" condition. The shape has varied across refactors — legacy callers
// saw a plain error whose text contained "tank_full"; after the goal-reached
// work, Refuel may return a structured *game.ServerError{Code:"tank_full"}
// or (once Task 4 lands) a *game.GoalReachedError{Code:"tank_full"}. This
// helper accepts all three so explore/auto-explore don't flap between
// migrations.
func isTankFullError(err error) bool {
	if err == nil {
		return false
	}
	var se *game.ServerError
	if errors.As(err, &se) && se.Code == "tank_full" {
		return true
	}
	var goal *game.GoalReachedError
	if errors.As(err, &goal) && goal.Code == "tank_full" {
		return true
	}
	return strings.Contains(err.Error(), "tank_full")
}

// runScript loads a script (by name or explicit path) and executes its logical
// commands in order. Execution stops at the first command that returns a
// stopping error (non-force loop failure or fatal *tokenError).
func runScript(client game.GameClient, ctx context.Context, arg string, format outputFormat, cfg PlayAsConfig, agentID string) {
	path, ok := worker.ResolveScriptArg(arg, agentID)
	if !ok {
		fmt.Printf("❌ script %q not found (searched %s)\n",
			arg, strings.Join(worker.ScriptSearchPaths(agentID), ", "))
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Printf("❌ run: %v\n", err)
		return
	}
	cmds, err := worker.SplitScriptCommands(string(data))
	if err != nil {
		fmt.Printf("❌ run %s: %v\n", path, err)
		return
	}
	fmt.Printf("▶ Running script %s (%d command(s))\n", path, len(cmds))
	for _, c := range cmds {
		if ctx.Err() != nil {
			return
		}
		if err := executeLogicalCommand(client, ctx, c, format, cfg, agentID); err != nil {
			fmt.Printf("⏹ script stopped: %v\n", err)
			return
		}
	}
	fmt.Printf("✓ script %s complete\n", path)
}
