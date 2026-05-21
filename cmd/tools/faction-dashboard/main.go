// Command faction-dashboard collects comprehensive faction data from member
// agents into the shared knowledge base and renders static HTML dashboards.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/faction"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

func main() {
	kbPath := flag.String("kb", "data/spacemolt-knowledge.db", "Shared knowledge base SQLite path")
	outputDir := flag.String("output", "data/reports/factions", "Output directory for HTML")
	agentsFlag := flag.String("agents", "", "Comma-separated agent filter (default: all in data/agents/)")
	delay := flag.Int("delay", 3, "Seconds between agent connections")
	debug := flag.Bool("debug", false, "Game client debug logging")
	renderOnly := flag.Bool("render-only", false, "Skip collection; render from existing KB data")
	flag.Parse()

	logger := log.New(os.Stdout, "[faction-dashboard] ", log.LstdFlags)

	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: *kbPath, WAL: true})
	if err != nil {
		logger.Fatalf("open KB: %v", err)
	}
	defer func() { _ = kb.Close() }()

	if !*renderOnly {
		agents, err := resolveAgents(*agentsFlag)
		if err != nil {
			logger.Fatalf("resolve agents: %v", err)
		}
		collectAll(kb, agents, *delay, *debug, logger)
	}

	if err := renderAll(kb, *outputDir, logger); err != nil {
		logger.Fatalf("render: %v", err)
	}
}

// resolveAgents returns agent IDs to process (those with credentials.json),
// sorted numerically by agent number so the true founder (lowest number) is
// processed first per faction.
func resolveAgents(filter string) ([]string, error) {
	if filter != "" {
		agents := strings.Split(filter, ",")
		slices.SortFunc(agents, func(a, b string) int {
			n := agentNumber(a) - agentNumber(b)
			if n != 0 {
				return n
			}
			return strings.Compare(a, b)
		})
		return agents, nil
	}
	entries, err := os.ReadDir("data/agents")
	if err != nil {
		return nil, err
	}
	var agents []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join("data", "agents", e.Name(), "credentials.json")); err == nil {
			agents = append(agents, e.Name())
		}
	}
	slices.SortFunc(agents, func(a, b string) int {
		n := agentNumber(a) - agentNumber(b)
		if n != 0 {
			return n
		}
		return strings.Compare(a, b)
	})
	return agents, nil
}

// agentNumber extracts the numeric suffix from an agent ID (founder = lowest).
func agentNumber(agentID string) int {
	parts := strings.Split(agentID, "-")
	if len(parts) > 1 {
		if n, err := strconv.Atoi(parts[len(parts)-1]); err == nil {
			return n
		}
	}
	return 1 << 30
}

// collectAll connects each agent and runs the Collector. The first agent seen
// per faction (lowest number, processed in sorted order) collects faction-wide
// data; all agents collect station-scoped data.
func collectAll(kb *knowledge.SQLiteKB, agents []string, delaySec int, debug bool, logger *log.Logger) {
	collector := faction.NewCollector(kb, logger)
	factionWideDone := map[string]bool{}

	for i, agentID := range agents {
		if i > 0 {
			time.Sleep(time.Duration(delaySec) * time.Second)
		}
		logger.Printf("[%d/%d] %s", i+1, len(agents), agentID)
		func() {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			client, _, err := game.InitializeAgent(agentID, logger, ctx, debug)
			if err != nil {
				logger.Printf("  connect failed: %v", err)
				return
			}
			defer func() { _ = client.Close() }()

			if err := client.GetStatus(ctx); err != nil {
				logger.Printf("  get_status failed: %v", err)
			} else {
				time.Sleep(game.SleepQuick)
			}

			factionID := client.GetState().Player.FactionID
			if factionID == "" {
				logger.Printf("  not in a faction; skipping")
				return
			}
			includeWide := !factionWideDone[factionID]
			if err := collector.Collect(ctx, client, includeWide); err != nil {
				logger.Printf("  collect failed: %v", err)
				return
			}
			if includeWide {
				factionWideDone[factionID] = true
			}
		}()
	}
}

// renderAll writes one HTML page per faction plus an index.
func renderAll(kb *knowledge.SQLiteKB, outputDir string, logger *log.Logger) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}
	ctx := context.Background()
	ids, err := kb.ListFactionIDs(ctx)
	if err != nil {
		return err
	}
	var cards []indexCard
	for _, id := range ids {
		view, err := kb.LoadFactionView(ctx, id)
		if err != nil {
			logger.Printf("load %s: %v", id, err)
			continue
		}
		if view == nil {
			continue
		}
		html, err := renderFactionHTML(view)
		if err != nil {
			logger.Printf("render %s: %v", id, err)
			continue
		}
		path := filepath.Join(outputDir, "faction-"+view.Faction.Tag+".html")
		if err := os.WriteFile(path, []byte(html), 0o644); err != nil {
			return err
		}
		logger.Printf("wrote %s", path)
		cards = append(cards, indexCard{
			Tag:        view.Faction.Tag,
			Name:       view.Faction.Name,
			Treasury:   view.Faction.Treasury,
			Members:    view.Faction.MemberCount,
			CapturedAt: view.Faction.CapturedAt,
		})
	}
	indexHTML, err := renderIndexHTML(cards)
	if err != nil {
		return err
	}
	indexPath := filepath.Join(outputDir, "index.html")
	if err := os.WriteFile(indexPath, []byte(indexHTML), 0o644); err != nil {
		return err
	}
	logger.Printf("wrote %s (%d factions)", indexPath, len(cards))
	return nil
}
