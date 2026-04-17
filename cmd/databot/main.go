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
	debug := flag.Bool("debug", false, "Enable WS debug logging")
	flag.Parse()

	if *mboxPath == "" {
		*mboxPath = filepath.Join("data", "agents", *agentID, "mbox.db")
	}

	logger := log.New(os.Stdout, fmt.Sprintf("[DATABOT-%s] ", *agentID), log.LstdFlags)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// 1. Connect + log in.
	logger.Printf("Initializing agent %s...", *agentID)
	client, creds, err := game.InitializeAgent(*agentID, logger, ctx, *debug)
	if err != nil {
		logger.Fatalf("InitializeAgent: %v", err)
	}
	defer func() { _ = client.Close() }()
	logger.Printf("Connected as %s (empire %s)", creds.Username, creds.Empire)

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

	// 5. Open mbox.
	store, err := mbox.Open(*mboxPath)
	if err != nil {
		logger.Fatalf("open mbox %s: %v", *mboxPath, err)
	}
	defer func() { _ = store.Close() }()

	// 6. Build registry + handlers.
	deps := dataservice.Deps{KB: kb, Graph: graph, Tick: tickFn}
	registry := dataservice.NewRegistry(deps)
	registry.Register(&handlers.Nearest{})

	// 7. Wire HistoryFetcher + Replier over the game client.
	fetcher := newClientFetcher(client)
	replier := newClientReplier(client)

	// 8. Run.
	svc, err := dataservice.NewService(dataservice.Config{
		AgentID:      *agentID,
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

	logger.Printf("dataservice running; agent=%s mbox=%s poll=%s", *agentID, *mboxPath, *pollInterval)
	if err := svc.Run(ctx); err != nil {
		logger.Fatalf("service run: %v", err)
	}
	logger.Printf("shutdown complete")
}

// clientFetcher implements dataservice.HistoryFetcher over pkg/game.Client.
type clientFetcher struct{ client *game.Client }

func newClientFetcher(c *game.Client) *clientFetcher { return &clientFetcher{client: c} }

// Fetch issues a get_chat_history call for the private channel and returns
// the parsed messages stored in State.LastChatHistory.
func (f *clientFetcher) Fetch(ctx context.Context, limit int) ([]serverapi.ChatMessage, error) {
	if err := f.client.GetChatHistory(ctx, "private", map[string]any{"limit": limit}); err != nil {
		return nil, err
	}
	state := f.client.GetState()
	if state == nil {
		return nil, nil
	}
	out := make([]serverapi.ChatMessage, 0, len(state.LastChatHistory))
	for _, m := range state.LastChatHistory {
		out = append(out, serverapi.ChatMessage{
			ID:           m.ID,
			Channel:      m.Channel,
			SenderID:     m.SenderID,
			Sender:       m.Sender,
			Content:      m.Content,
			TargetID:     m.TargetID,
			TimestampUTC: m.Timestamp,
		})
	}
	return out, nil
}

// clientReplier implements dataservice.Replier over pkg/game.Client.
type clientReplier struct{ client *game.Client }

func newClientReplier(c *game.Client) *clientReplier { return &clientReplier{client: c} }

func (r *clientReplier) Reply(ctx context.Context, targetID, content string) error {
	return r.client.Chat(ctx, "private", content, targetID)
}
