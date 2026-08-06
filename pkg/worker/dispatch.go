package worker

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rsned/spacemolt/pkg/assets"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
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
	Client game.GameClient
	KB     knowledge.Base
	Market *market.Collector
	// Assets is the agent asset + capability ledger (data/assets.db). Nil
	// disables capture entirely: asset capture is strictly less important than
	// any paying activity and must never be a new way for a pass to fail.
	Assets  *assets.Store
	Out     io.Writer
	AgentID string      // claim owner for opportunity-claiming roles (e.g. hauler)
	Station string      // home base id: pins the assist and missionrunner roles
	Rescue  RescueQueue // shared stranded-worker rescue queue, used by the assist role
	// AgentsDir is where agent credential files live (<AgentsDir>/<agent-id>/
	// credentials.json), used by Deliver to resolve a gift recipient's agent
	// id to its in-game username. Empty -> DefaultAgentsDir ("data/agents").
	AgentsDir string
	// EnableFreight opts the missions role into the /shipping carrier path
	// (freight evaluated co-equally with the mission board). Default false =
	// the freight code is fully inert.
	EnableFreight bool
	// FreightMaxPackages caps concurrent freight contracts; see
	// MissionDeps.FreightMaxPackages. 0/1 = single-contract v1 behavior.
	FreightMaxPackages int
	// FreightBootstrap enables the probationary loss-leader floor relaxation for
	// the missions role. Defaults on when EnableFreight is set; false is the
	// kill switch. See MissionDeps.FreightBootstrap.
	FreightBootstrap bool
	// MissionCategories is the mission-board category allowlist for the
	// missions role (nil/empty -> delivery only; the learning pool adds
	// "exploration").
	MissionCategories []string

	// treasury rate-limits faction-treasury rescue withdrawals across idle passes.
	// Held here (not per Run call) so the cooldown survives between command passes.
	treasury *treasuryRescue
	// shuttle carries cross-pass shuttle memory (dry-pass streak + reposition
	// cursor) so the idle→idle→reposition cadence survives between passes.
	shuttle *shuttleState
	// mission carries cross-pass mission-runner memory (dry-pass streak +
	// reposition cursor), the shuttleState pattern.
	mission *missionRunState
	// freightPersistOnce guards the one-time load+wire of freight-held persistence.
	freightPersistOnce sync.Once

	// craftPollSleep is the sleep-with-cancellation used between craft_node's
	// completion-poll retries (waitForCraftJob). Defaults to a real ctx-aware
	// time.Sleep (craftPollSleepFunc); tests override it with a zero-delay
	// stand-in so polling loops don't add real wall-clock time to the suite.
	craftPollSleep func(ctx context.Context, d time.Duration) error
	// minePollSleep is the sleep-with-cancellation used between MineQty's
	// Mine() passes (game.SleepTick cadence). Same default/override pattern
	// as craftPollSleep.
	minePollSleep func(ctx context.Context, d time.Duration) error

	// handoffPersist is the CAS-transition primitive HandoffPass/moveHandoffStock
	// use for every write to the handoff queue (progress persist, done, failed).
	// Defaults to a direct pass-through to Queue.Transition; tests override it
	// to inject a persist error (as opposed to a plain CAS-mismatch, which is
	// already reachable by racing a second *handoff.Queue handle against the
	// same file) at an exact call, mirroring the craftPollSleep override pattern.
	handoffPersist handoffTransition

	// ensureHomeNav navigates the ship to (system, poi) for the ensure_home
	// command. Defaults to a real Autopilot round-trip; tests override it to
	// avoid driving the routing loop against a stub client.
	ensureHomeNav func(ctx context.Context, system, poi string) error

	// activitySink, when wired via SetActivitySink, receives a short
	// human-readable description of the role's current unit of work (e.g.
	// "Mission Steel Plate Order"). The worker's heartbeat goroutine reads the
	// same atomic, so roles publish through setActivity rather than touching it
	// directly. nil (tests, non-heartbeat callers) makes setActivity a no-op.
	activitySink *atomic.Pointer[string]
}

// SetActivitySink wires the shared atomic the worker heartbeat reads so role
// commands can publish a "current activity" string for the fleet status page.
// The caller (cmd/worker) owns the pointer; the dispatch only writes through it.
func (d *WorkerDispatch) SetActivitySink(p *atomic.Pointer[string]) { d.activitySink = p }

// setActivity publishes s as the worker's current activity (cleared with ""),
// no-op when no sink is wired.
func (d *WorkerDispatch) setActivity(s string) {
	if d.activitySink != nil {
		d.activitySink.Store(&s)
	}
}

// publishActivity is the nil-safe way a role reports its current activity: the
// SetActivity hook is nil in tests and any non-dispatch caller.
func publishActivity(set func(string), s string) {
	if set != nil {
		set(s)
	}
}

// ensureFreightPersistence loads any persisted held-freight set into mission state
// and wires the persist callback so later mutations write through to disk. Inert
// unless this is a freight worker with an AgentID (no file churn for the general
// pool). Idempotent: the load+wire happens at most once per dispatch.
func (d *WorkerDispatch) ensureFreightPersistence() {
	if !d.EnableFreight || d.AgentID == "" || d.mission == nil {
		return
	}
	d.freightPersistOnce.Do(func() {
		path := freightHeldPath(d.agentsDir(), d.AgentID)
		if held, err := loadHeldFreight(path); err != nil {
			fmt.Fprintf(d.Out, "freight: load held set %s: %v; starting empty\n", path, err) //nolint:errcheck
		} else {
			for _, c := range held {
				d.mission.addHeldFreight(c)
			}
		}
		d.mission.persistHeld = func(cs []*serverapi.ShipmentContract) {
			if err := saveHeldFreight(path, cs); err != nil {
				fmt.Fprintf(d.Out, "freight: persist held set: %v\n", err) //nolint:errcheck
			}
		}
	})
}

// NewWorkerDispatch builds a dispatch over the given client, KB, and optional
// market collector. out receives human-readable progress lines (worker stdout /
// logs).
func NewWorkerDispatch(client game.GameClient, kb knowledge.Base, mc *market.Collector, out io.Writer) *WorkerDispatch {
	if out == nil {
		out = io.Discard
	}
	d := &WorkerDispatch{
		Client: client, KB: kb, Market: mc, Out: out,
		treasury:       &treasuryRescue{},
		shuttle:        &shuttleState{},
		mission:        &missionRunState{},
		craftPollSleep: craftPollSleepFunc,
		minePollSleep:  craftPollSleepFunc,
		handoffPersist: defaultHandoffPersist,
	}
	d.ensureHomeNav = func(ctx context.Context, system, poi string) error {
		return Autopilot(ctx, AutopilotDeps{Client: d.Client, Out: d.Out}, system, poi)
	}
	return d
}

// supported is the curated command set. Keep in sync with data/scripts and
// data/overmind/roles.yaml; roles_test.go enforces that every command named
// there is present here.
var supported = map[string]bool{
	"undock": true, "dock": true, "travel": true, "jump": true, "autopilot": true, "ensure_home": true,
	"explore": true, "scan": true, "haul": true, "shuttle": true, "assist": true, "missions": true,
	"mine": true, "mine_qty": true, "deliver": true, "buy_directed": true, "craft_node": true,
	"refuel": true, "repair": true, "deposit_all": true, "sell_all": true,
	"view_market": true, "facilities": true, "kb_update": true,
	"update_market": true, "capture_fuel": true, "capture_profile": true,
	"capture_storage": true, "capture_faction": true,
	"get_status": true, "get_system": true, "get_cargo": true,
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
	case "ensure_home":
		return d.ensureHome(ctx)
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
			Treasury:    d.treasury,
			FuelPrices:  d.Market,
			SetActivity: d.setActivity,
			RecaptureBuyMarket: func(ctx context.Context) error {
				if err := d.Client.ViewMarket(ctx, map[string]any{}); err != nil {
					return err
				}
				return market.CaptureFromClient(ctx, d.Client, d.Market)
			},
		})
	case "missions":
		if d.Market == nil {
			fmt.Fprintln(d.Out, "missions: market collector not configured (use --market-db-path)") //nolint:errcheck
			return nil
		}
		d.ensureFreightPersistence()
		return Missions(ctx, MissionDeps{
			Client: d.Client, KB: d.KB, Market: d.Market, Out: d.Out, AgentID: d.AgentID,
			Treasury: d.treasury, FuelPrices: d.Market, State: d.mission,
			HomeStation:        d.Station,
			Categories:         d.MissionCategories,
			EnableFreight:      d.EnableFreight,
			FreightMaxPackages: d.FreightMaxPackages,
			FreightBootstrap:   d.FreightBootstrap,
			SetActivity:        d.setActivity,
			// Exploration dock legs double as market-coverage sweeps: full
			// view_market capture into market.db (haul's recapture pattern)
			// plus the knowledge-side demand ledger.
			capture: func(ctx context.Context) error {
				if err := d.Client.ViewMarket(ctx, map[string]any{}); err != nil {
					return err
				}
				CaptureMarket(ctx, d.Client, d.KB)
				return market.CaptureFromClient(ctx, d.Client, d.Market)
			},
		})
	case "shuttle":
		return Shuttle(ctx, ShuttleDeps{
			Client: d.Client, KB: d.KB, Out: d.Out, AgentID: d.AgentID, Treasury: d.treasury, State: d.shuttle,
			SetActivity: d.setActivity,
		})
	case "assist":
		if d.Rescue == nil {
			return fmt.Errorf("assist: no rescue queue configured (--rescue-queue)")
		}
		return Assist(ctx, AssistDeps{
			Client: d.Client, KB: d.KB, Queue: d.Rescue, Out: d.Out,
			AgentID: d.AgentID, HomeStation: d.Station,
			SetActivity: d.setActivity,
		})
	case "deliver":
		if len(args) < 5 {
			return fmt.Errorf("deliver: want ITEM QTY FROM TO RECIPIENT, got %v", args)
		}
		qty, err := strconv.Atoi(args[1])
		if err != nil || qty < 1 {
			return fmt.Errorf("deliver: bad qty %q", args[1])
		}
		return d.Deliver(ctx, args[0], qty, args[2], args[3], args[4])
	case "buy_directed":
		if len(args) < 5 {
			return fmt.Errorf("buy_directed: want ITEM QTY STATION MAX_UNIT_PRICE RECIPIENT, got %v", args)
		}
		qty, err := strconv.Atoi(args[1])
		if err != nil || qty < 1 {
			return fmt.Errorf("buy_directed: bad qty %q", args[1])
		}
		maxUnit, err := strconv.ParseFloat(args[3], 64)
		if err != nil {
			return fmt.Errorf("buy_directed: bad max unit price %q", args[3])
		}
		return d.BuyDirected(ctx, args[0], qty, args[2], maxUnit, args[4])
	case "mine_qty":
		if len(args) < 4 {
			return fmt.Errorf("mine_qty: want ITEM QTY TO RECIPIENT, got %v", args)
		}
		qty, err := strconv.Atoi(args[1])
		if err != nil || qty < 1 {
			return fmt.Errorf("mine_qty: bad qty %q", args[1])
		}
		return d.MineQty(ctx, args[0], qty, args[2], args[3])
	case "craft_node":
		if len(args) < 4 {
			return fmt.Errorf("craft_node: want RECIPE NUM_OUTPUTS STATION FACILITY [EST_FEE], got %v", args)
		}
		numOutputs, err := strconv.Atoi(args[1])
		if err != nil || numOutputs < 1 {
			return fmt.Errorf("craft_node: bad num_outputs %q", args[1])
		}
		var estFee float64
		if len(args) >= 5 && args[4] != "" {
			estFee, err = strconv.ParseFloat(args[4], 64)
			if err != nil {
				return fmt.Errorf("craft_node: bad est_fee %q", args[4])
			}
		}
		return d.CraftOutputs(ctx, args[0], numOutputs, args[2], args[3], estFee)
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
	case "capture_fuel":
		if d.Market == nil {
			return fmt.Errorf("capture_fuel: market collector not configured (use --market-db-path)")
		}
		if err := d.Client.GetBase(ctx); err != nil {
			return err
		}
		return market.CaptureFuelFromClient(ctx, d.Client, d.Market, d.AgentID)
	case "capture_profile":
		// Nil store = capture disabled; not an error. See the Assets field.
		return assets.CaptureProfile(ctx, d.Client, d.Assets, d.AgentID, time.Now())
	case "capture_storage":
		return assets.CaptureStorage(ctx, d.Client, d.Assets, d.AgentID, time.Now())
	case "capture_faction":
		return assets.CaptureFaction(ctx, d.Client, d.Assets, d.AgentID, time.Now())
	default:
		return fmt.Errorf("worker dispatch: unsupported command %q", cmd)
	}
}

// ensureHome parks a resident worker docked at its configured home station
// (d.Station). It no-ops when no home is configured or the ship is already
// docked there. The home *system* is resolved live via FindRoute — the last
// hop's system, or the current system when the route is empty — mirroring the
// mobile-home resolution in assist. Best-effort: every failure logs and returns
// nil so the standing loop simply retries on the next idle pass.
func (d *WorkerDispatch) ensureHome(ctx context.Context) error {
	home := d.Station
	if home == "" {
		return nil
	}
	st := d.Client.GetState()
	if st != nil && st.CurrentPOI == home && st.Doc {
		return nil // already parked and docked at home
	}
	route, err := d.Client.FindRoute(ctx, home)
	if err != nil {
		fmt.Fprintf(d.Out, "ensure_home: find_route %s: %v\n", home, err) //nolint:errcheck
		return nil
	}
	system := ""
	if len(route) > 0 {
		system = route[len(route)-1].SystemID
	} else if st != nil {
		system = st.System.ID
	}
	if system == "" {
		fmt.Fprintf(d.Out, "ensure_home: cannot resolve home system for %s\n", home) //nolint:errcheck
		return nil
	}
	// Travel only when we are not already sitting at the home POI. Re-traveling
	// to a POI we already occupy auto-undocks us every pass (the assist thrash),
	// so the dock never sticks.
	if st == nil || st.CurrentPOI != home {
		if nerr := d.ensureHomeNav(ctx, system, home); nerr != nil {
			fmt.Fprintf(d.Out, "ensure_home: navigate to %s/%s: %v\n", system, home, nerr) //nolint:errcheck
			return nil
		}
	}
	if derr := d.Client.Dock(ctx); derr != nil && !strings.Contains(derr.Error(), "Already docked") {
		fmt.Fprintf(d.Out, "ensure_home: dock %s: %v\n", home, derr) //nolint:errcheck
	}
	return nil
}
