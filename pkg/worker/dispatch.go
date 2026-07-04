package worker

import (
	"context"
	"fmt"
	"io"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/market"
)

// WorkerDispatch is the lean, headless command dispatch used by cmd/worker. It
// covers the curated worker-script vocabulary only; each command maps directly
// to a game.GameClient method, plus shared KB-capture for tracking commands.
// Unknown commands return an error (never silently ignored). KB may be nil, in
// which case tracking commands degrade to a no-op capture (movement/mining still
// work).
type WorkerDispatch struct {
	Client  game.GameClient
	KB      knowledge.Base
	Market  *market.Collector
	Out     io.Writer
	AgentID string      // claim owner for opportunity-claiming roles (e.g. hauler)
	Station string      // home station POI id, used by the assist role
	Rescue  RescueQueue // shared stranded-worker rescue queue, used by the assist role

	// treasury rate-limits faction-treasury rescue withdrawals across idle passes.
	// Held here (not per Run call) so the cooldown survives between command passes.
	treasury *treasuryRescue
	// shuttle carries cross-pass shuttle memory (dry-pass streak + reposition
	// cursor) so the idle→idle→reposition cadence survives between passes.
	shuttle *shuttleState
}

// NewWorkerDispatch builds a dispatch over the given client, KB, and optional
// market collector. out receives human-readable progress lines (worker stdout /
// logs).
func NewWorkerDispatch(client game.GameClient, kb knowledge.Base, mc *market.Collector, out io.Writer) *WorkerDispatch {
	if out == nil {
		out = io.Discard
	}
	return &WorkerDispatch{Client: client, KB: kb, Market: mc, Out: out, treasury: &treasuryRescue{}, shuttle: &shuttleState{}}
}

// supported is the curated command set. Keep in sync with data/scripts and
// data/overmind/roles.yaml; roles_test.go enforces that every command named
// there is present here.
var supported = map[string]bool{
	"undock": true, "dock": true, "travel": true, "jump": true, "autopilot": true,
	"explore": true, "scan": true, "haul": true, "shuttle": true, "assist": true,
	"mine":   true,
	"refuel": true, "repair": true, "deposit_all": true, "sell_all": true,
	"view_market": true, "facilities": true, "kb_update": true,
	"update_market": true,
	"get_status":    true, "get_system": true, "get_cargo": true,
}

// Supports reports whether cmd is in the curated worker vocabulary.
func (d *WorkerDispatch) Supports(cmd string) bool { return supported[cmd] }

// Run dispatches one tokenized command. Token resolution ($SYSTEM$, $STATION$,
// POI-type tokens) is the caller's responsibility (RunStanding resolves before
// calling) — Run treats tokens as literal.
func (d *WorkerDispatch) Run(ctx context.Context, tokens []string) error {
	if len(tokens) == 0 {
		return nil
	}
	cmd := tokens[0]
	args := tokens[1:]
	switch cmd {
	case "undock":
		return d.Client.Undock(ctx)
	case "dock":
		return d.Client.Dock(ctx)
	case "mine":
		return d.Client.Mine(ctx)
	case "refuel":
		return d.Client.Refuel(ctx)
	case "repair":
		return d.Client.Repair(ctx)
	case "deposit_all":
		return d.Client.DepositAllItems(ctx)
	case "sell_all":
		return d.Client.SellAllBulk(ctx, nil)
	case "travel":
		if len(args) < 1 {
			return fmt.Errorf("travel: missing target POI")
		}
		_, err := d.Client.Travel(ctx, args[0])
		return err
	case "jump":
		if len(args) < 1 {
			return fmt.Errorf("jump: missing target system")
		}
		_, err := d.Client.Jump(ctx, args[0])
		return err
	case "autopilot":
		if len(args) < 1 {
			return fmt.Errorf("autopilot: missing target system")
		}
		poi := ""
		if len(args) >= 2 {
			poi = args[1]
		}
		return Autopilot(ctx, AutopilotDeps{
			Client: d.Client,
			Out:    d.Out,
			OnWaypoint: func(ctx context.Context) error {
				if d.KB == nil {
					return nil
				}
				if err := KBUpdateSystem(ctx, d.Client, d.KB, ""); err != nil {
					return err
				}
				return KBUpdatePOI(ctx, d.Client, d.KB, "")
			},
		}, args[0], poi)
	case "explore":
		return Explore(ctx, ExploreDeps{Client: d.Client, KB: d.KB, Out: d.Out})
	case "haul":
		if d.Market == nil {
			fmt.Fprintln(d.Out, "haul: market collector not configured (use --market-db-path)") //nolint:errcheck
			return nil
		}
		return Haul(ctx, HaulDeps{
			Client: d.Client, KB: d.KB, Market: d.Market, Out: d.Out, AgentID: d.AgentID,
			Treasury: d.treasury,
			RecaptureBuyMarket: func(ctx context.Context) error {
				if err := d.Client.ViewMarket(ctx, map[string]any{}); err != nil {
					return err
				}
				return market.CaptureFromClient(ctx, d.Client, d.Market)
			},
		})
	case "shuttle":
		return Shuttle(ctx, ShuttleDeps{
			Client: d.Client, KB: d.KB, Out: d.Out, AgentID: d.AgentID, Treasury: d.treasury, State: d.shuttle,
		})
	case "assist":
		if d.Rescue == nil {
			return fmt.Errorf("assist: no rescue queue configured (--rescue-queue)")
		}
		return Assist(ctx, AssistDeps{
			Client: d.Client, KB: d.KB, Queue: d.Rescue, Out: d.Out,
			AgentID: d.AgentID, HomeStation: d.Station,
		})
	case "scan":
		return d.Client.Scan(ctx)
	case "get_status":
		return d.Client.GetStatus(ctx)
	case "get_system":
		return d.Client.GetSystem(ctx)
	case "get_cargo":
		return d.Client.GetCargo(ctx)
	case "view_market":
		if err := d.Client.ViewMarket(ctx, map[string]any{}); err != nil {
			return err
		}
		CaptureMarket(ctx, d.Client, d.KB)
		return nil
	case "facilities":
		if err := d.Client.Facility(ctx, map[string]any{}); err != nil {
			return err
		}
		return KBUpdateFacilities(ctx, d.Client, d.KB)
	case "kb_update":
		// detectedBy is empty here; Tasks 9/10 will wire the real agent id
		// once WorkerDispatch gains an agent-id field via standing behavior.
		return KBUpdateAll(ctx, d.Client, d.KB, d.Market, "")
	case "update_market":
		// Lightweight, market-only capture into market.db (mirrors play_as):
		// prime the raw cache with ViewMarket, then write the snapshot.
		// CaptureFromClient no-ops gracefully when not at a station.
		if d.Market == nil {
			return fmt.Errorf("update_market: market collector not configured (use --market-db-path)")
		}
		if err := d.Client.ViewMarket(ctx, map[string]any{}); err != nil {
			return err
		}
		return market.CaptureFromClient(ctx, d.Client, d.Market)
	default:
		return fmt.Errorf("worker dispatch: unsupported command %q", cmd)
	}
}
