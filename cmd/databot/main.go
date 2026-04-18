// Command databot runs the dataservice query responder as a specific agent.
//
// Usage:
//
//	databot --agent-id databot --db-path data/spacemolt-knowledge.db
//
// The agent logs in, stays docked, polls private chat for queries, and
// responds using the dataservice handler registry. Multiple databot
// instances may run concurrently under different agent IDs for load
// scaling; each uses its own mbox but shares the knowledge-base file.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/rsned/spacemolt/pkg/dataservice"
	"github.com/rsned/spacemolt/pkg/dataservice/handlers"
	"github.com/rsned/spacemolt/pkg/galaxy"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/mbox"
)

func main() {
	agentID := flag.String("agent-id", "databot", "Agent identity to run as (must have credentials in data/agents/<id>/)")
	dbPath := flag.String("db-path", "data/spacemolt-knowledge.db", "Path to shared SQLite knowledge base")
	mboxPath := flag.String("mbox-path", "", "Path to agent mbox SQLite DB (default: data/agents/<agent-id>/mbox.db)")
	pollInterval := flag.Duration("poll-interval", 5*time.Second, "Chat-history poll interval")
	replyPace := flag.Duration("reply-pace", game.SleepTick, "Minimum interval between outgoing chat replies")
	debug := flag.Bool("debug", false, "Enable debug logging")
	flag.Parse()

	if *mboxPath == "" {
		*mboxPath = filepath.Join("data", "agents", *agentID, "mbox.db")
	}

	logger := log.New(os.Stdout, fmt.Sprintf("[DATABOT-%s] ", *agentID), log.LstdFlags)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// 1. Connect + log in via WSS. MCP does not surface inbound private DMs
	//    to third-party senders (the private channel history query requires
	//    target_id, and get_notifications chat events are not plumbed into
	//    state yet). WSS pushes chat via SetOnChatMessage, which is how this
	//    binary actually receives work. Revisit when MCP inbox support lands.
	logger.Printf("Initializing agent %s...", *agentID)
	client, creds, err := game.InitializeAgent(*agentID, logger, ctx, *debug)
	if err != nil {
		logger.Fatalf("InitializeAgent: %v", err)
	}
	defer func() { _ = client.Close() }()
	logger.Printf("Connected as %s (empire %s)", creds.Username, creds.Empire)

	// Server addresses private-channel messages with target_id as the
	// internal player ID hash, not the --agent-id flag value. Capture it
	// now so the dispatch filter matches incoming DMs correctly.
	playerID := client.GetState().Player.ID
	if playerID == "" {
		logger.Fatalf("player ID missing from state after login")
	}
	logger.Printf("Player ID: %s", playerID)

	// 2. Open shared knowledge base.
	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: *dbPath, WAL: true})
	if err != nil {
		logger.Fatalf("open KB %s: %v", *dbPath, err)
	}
	defer func() { _ = kb.Close() }()

	// 3. Build galaxy graph once at startup.
	graph := &galaxy.GalaxyGraph{}
	if err := graph.BuildFromDB(ctx, kb); err != nil {
		logger.Fatalf("BuildFromDB: %v", err)
	}
	stats := graph.Stats()
	logger.Printf("Galaxy graph built: %d systems, %d edges in %v", stats.NodeCount, stats.EdgeCount, stats.BuildTime)

	// 4. Game clock (optional but enriches replies with freshness).
	clock, err := game.NewGameClock(ctx, client, logger)
	if err != nil {
		logger.Printf("game clock unavailable: %v (continuing without tick info)", err)
	}
	tickFn := func() int64 {
		if clock == nil {
			return 0
		}
		return clock.Tick()
	}

	// 5. Open mbox and wire WSS chat pushes directly into it. This is the
	//    primary ingest path on WSS; the Fetch loop is kept as a no-op for
	//    interface compatibility and as a future home for transport
	//    mechanisms that require explicit polling.
	store, err := mbox.Open(*mboxPath)
	if err != nil {
		logger.Fatalf("open mbox %s: %v", *mboxPath, err)
	}
	defer func() { _ = store.Close() }()

	ingester := mbox.NewIngester(store)
	ingester.SetSelfID(playerID)
	client.SetOnChatMessage(ingester.HandlePush)

	// 6. Build registry + handlers.
	deps := dataservice.Deps{KB: kb, Graph: graph, Tick: tickFn}
	registry := dataservice.NewRegistry(deps)
	registry.Register(&handlers.Nearest{})

	// 7. Wire HistoryFetcher + Replier over the game client.
	fetcher := newPushOnlyFetcher()
	replier := newClientReplier(client)

	// 8. Run.
	svc, err := dataservice.NewService(dataservice.Config{
		AgentID:      playerID,
		Registry:     registry,
		Mbox:         store,
		Fetcher:      fetcher,
		Replier:      replier,
		Logger:       logger,
		PollInterval: *pollInterval,
		ReplyPace:    *replyPace,
	})
	if err != nil {
		logger.Fatalf("NewService: %v", err)
	}

	logger.Printf("dataservice running; agent=%s player_id=%s mbox=%s poll=%s", *agentID, playerID, *mboxPath, *pollInterval)
	if err := svc.Run(ctx); err != nil {
		logger.Fatalf("service run: %v", err)
	}
	logger.Printf("shutdown complete")
}

// pushOnlyFetcher is a no-op HistoryFetcher used when the game client
// pushes chat messages directly into mbox via SetOnChatMessage. The
// Service's ingest loop is kept running so future transports that need
// explicit polling can substitute a real fetcher without changing the
// Service contract.
type pushOnlyFetcher struct{}

func newPushOnlyFetcher() *pushOnlyFetcher { return &pushOnlyFetcher{} }

// Fetch returns no messages. Inbound chat arrives via server push.
func (pushOnlyFetcher) Fetch(_ context.Context, _ int) ([]serverapi.ChatMessage, error) {
	return nil, nil
}

// clientReplier implements dataservice.Replier over *game.Client.
type clientReplier struct{ client *game.Client }

func newClientReplier(c *game.Client) *clientReplier { return &clientReplier{client: c} }

func (r *clientReplier) Reply(ctx context.Context, targetID, content string) error {
	return r.client.Chat(ctx, "private", content, targetID)
}
