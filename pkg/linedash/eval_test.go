package linedash

import (
	"slices"
	"testing"
	"time"
)

var evalNow = time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)

// at returns evalNow minus h hours.
func at(h float64) time.Time {
	return evalNow.Add(-time.Duration(h * float64(time.Hour)))
}

func fixtureLine(t *testing.T) Line {
	t.Helper()
	c, err := LoadLines("testdata/lines_moonshine.yaml")
	if err != nil {
		t.Fatalf("LoadLines: %v", err)
	}

	return c.Lines[0]
}

// baseInputs returns inputs where every line agent is live, fresh, docked,
// funded and fuelled, with no tanks and no events.
func baseInputs(line Line) Inputs {
	in := Inputs{
		Now:     evalNow,
		Cfg:     DefaultThresholds(),
		Roster:  map[string]RosterEntry{},
		Live:    map[string]Live{},
		Tanks:   map[string][]TankLevel{},
		Cursors: map[string]time.Time{},
		Demand:  map[string]Demand{},
		Asks:    map[string]*OurAsk{},
	}
	for _, a := range AgentsOf(line) {
		in.Roster[a] = RosterEntry{AgentID: a, Fleet: "fleet", Role: "role"}
		in.Live[a] = Live{AgentID: a, Fleet: "fleet", System: "sys", POI: "poi", Docked: true, Healthy: true,
			Credits: 10000, Fuel: 100, MaxFuel: 100, LastSeen: evalNow, CapturedAt: evalNow}
		in.Cursors[a] = evalNow
	}

	return in
}

func tank(item string, units float64) TankLevel {
	return TankLevel{Holder: Holder{Faction: "CRFT", Station: "voss_redoubt_station"}, Item: item, Units: units, CapturedAt: evalNow}
}

func nodeOf(t *testing.T, r LineReport, id string) NodeReport {
	t.Helper()
	for _, n := range r.Nodes {
		if n.ID == id {
			return n
		}
	}
	t.Fatalf("node %q not in report", id)

	return NodeReport{}
}

func edgeOf(t *testing.T, r LineReport, from, to string) EdgeReport {
	t.Helper()
	for _, e := range r.Edges {
		if e.From == from && e.To == to {
			return e
		}
	}
	t.Fatalf("edge %s->%s not in report", from, to)

	return EdgeReport{}
}

func agentOf(t *testing.T, n NodeReport, id string) AgentReport {
	t.Helper()
	for _, a := range n.Agents {
		if a.AgentID == id {
			return a
		}
	}
	t.Fatalf("agent %q not on node %q", id, n.ID)

	return AgentReport{}
}

func wantState(t *testing.T, what string, got, want State) {
	t.Helper()
	if got != want {
		t.Errorf("%s state = %q, want %q", what, got, want)
	}
}

func hasAlert(n NodeReport, a Alert) bool { return slices.Contains(n.Alerts, a) }

func drop(sender string, h float64) Event {
	return Event{AgentID: "trader-9", Type: EventGiftReceived, At: at(h), Item: "water_ice", Units: 100, Sender: sender}
}

func gift(agent, item string, units int, h float64) Event {
	return Event{AgentID: agent, Type: EventGiftSent, At: at(h), Item: item, Units: units}
}

func craft(agent string, runs int, h float64) Event {
	return Event{AgentID: agent, Type: EventCrafted, At: at(h), Item: "pirate_moonshine", Runs: runs, Recipe: "distill_moonshine"}
}

func fill(agent, role, item, station string, units int, h float64) Event {
	return Event{AgentID: agent, Type: EventExchangeFill, At: at(h), Item: item, Units: units, Role: role, Station: station}
}

// stillInputsFull stocks voss_inputs with enough for one batch.
func stillInputsFull(in *Inputs) {
	in.Tanks["voss_inputs"] = []TankLevel{tank("water_ice", 600), tank("pharmaceutical_culture", 120)}
}

func TestEvalAgentBrokeAndStranded(t *testing.T) {
	line := fixtureLine(t)
	in := baseInputs(line)
	l := in.Live["craftsman-8"]
	l.Credits = 100
	in.Live["craftsman-8"] = l
	l = in.Live["craftsman-10"]
	l.Docked, l.Fuel, l.MaxFuel, l.System, l.POI = false, 5, 100, "voss", "belt"
	in.Live["craftsman-10"] = l

	r := Evaluate(line, in)
	still := nodeOf(t, r, "still")
	c8, c10 := agentOf(t, still, "craftsman-8"), agentOf(t, still, "craftsman-10")
	if !c8.Broke || c8.Stranded {
		t.Errorf("craftsman-8 broke=%v stranded=%v, want true/false", c8.Broke, c8.Stranded)
	}
	if c10.Broke || !c10.Stranded || c10.FuelPct != 5 {
		t.Errorf("craftsman-10 broke=%v stranded=%v fuel=%v, want false/true/5", c10.Broke, c10.Stranded, c10.FuelPct)
	}
	if c10.Where != "voss/belt" || c8.Where != "docked poi" {
		t.Errorf("where = %q / %q, want voss/belt / docked poi", c10.Where, c8.Where)
	}
	if !hasAlert(still, AlertAgentBroke) {
		t.Errorf("still alerts %v lack %q", still.Alerts, AlertAgentBroke)
	}
	if hasAlert(nodeOf(t, r, "culture_buy"), AlertAgentBroke) {
		t.Error("culture_buy flagged broke with a funded agent")
	}
}

func TestEvalAgentAbsentFromLiveNotFlagged(t *testing.T) {
	line := fixtureLine(t)
	in := baseInputs(line)
	delete(in.Live, "trader-10")

	r := Evaluate(line, in)
	n := nodeOf(t, r, "culture_buy")
	a := agentOf(t, n, "trader-10")
	if a.Live || a.Broke || a.Stranded || a.Credits != 0 || a.FuelPct != 0 {
		t.Errorf("absent agent = %+v, want not live, not flagged, zero figures", a)
	}
	if hasAlert(n, AlertAgentBroke) {
		t.Error("absent agent raised broke alert")
	}
}

func TestEvalAgentLiveNeedsFreshLastSeen(t *testing.T) {
	line := fixtureLine(t)
	in := baseInputs(line)
	l := in.Live["trader-10"]
	l.LastSeen = at(3)
	in.Live["trader-10"] = l

	n := nodeOf(t, Evaluate(line, in), "culture_buy")
	if agentOf(t, n, "trader-10").Live || n.LiveAgents != 0 || n.AssignedAgents != 1 {
		t.Errorf("live=%d assigned=%d, want 0/1 for a stale last_seen", n.LiveAgents, n.AssignedAgents)
	}
}

func TestEvalMineNodeAllBotsAway(t *testing.T) {
	line := fixtureLine(t)
	in := baseInputs(line)
	for i, a := range []string{"miningbot_0000", "miningbot_0001", "miningbot_0002"} {
		e := in.Roster[a]
		e.AwayFleet, e.Phase = "salvage", []string{"outbound", "away", "returning"}[i]
		in.Roster[a] = e
	}
	e := in.Roster["miningbot_0003"]
	e.Removed = true
	in.Roster["miningbot_0003"] = e
	in.Events = []Event{drop("miningbot_0000", 1)}

	n := nodeOf(t, Evaluate(line, in), "ice_mine")
	wantState(t, "ice_mine", n.State, StateStarved)
	if !hasAlert(n, AlertNoBotsLive) {
		t.Errorf("alerts %v lack %q", n.Alerts, AlertNoBotsLive)
	}
	if n.LiveAgents != 0 || n.AssignedAgents != 4 {
		t.Errorf("live/assigned = %d/%d, want 0/4", n.LiveAgents, n.AssignedAgents)
	}
	if !slices.Contains(n.Detail, "miningbot_0001: away in salvage") {
		t.Errorf("detail %v lacks away line", n.Detail)
	}
}

func TestEvalMineOptimalOnRecentDrop(t *testing.T) {
	line := fixtureLine(t)
	in := baseInputs(line)
	in.Events = []Event{drop("miningbot_0001", 1)}

	n := nodeOf(t, Evaluate(line, in), "ice_mine")
	wantState(t, "ice_mine", n.State, StateOptimal)
	if !n.LastEventAt.Equal(at(1)) {
		t.Errorf("LastEventAt = %v, want %v", n.LastEventAt, at(1))
	}
}

func TestEvalMineSourceDepleted(t *testing.T) {
	cases := map[string][]Event{
		"no drops":            nil,
		"drop too old":        {drop("miningbot_0001", 5)},
		"sender not attached": {drop("trader-10", 1)},
		"wrong item":          {{AgentID: "trader-9", Type: EventGiftReceived, At: at(1), Item: "iron_ore", Units: 9, Sender: "miningbot_0001"}},
		"not a holder":        {{AgentID: "trader-10", Type: EventGiftReceived, At: at(1), Item: "water_ice", Units: 9, Sender: "miningbot_0001"}},
		"future drop":         {drop("miningbot_0001", -1)},
	}
	for name, evs := range cases {
		t.Run(name, func(t *testing.T) {
			line := fixtureLine(t)
			in := baseInputs(line)
			in.Events = evs
			n := nodeOf(t, Evaluate(line, in), "ice_mine")
			wantState(t, "ice_mine", n.State, StateStarved)
			if !hasAlert(n, AlertSourceDepleted) {
				t.Errorf("alerts %v lack %q", n.Alerts, AlertSourceDepleted)
			}
		})
	}
}

func TestEvalMineChokedWhenDropTankFullAndHaulIdle(t *testing.T) {
	line := fixtureLine(t)
	in := baseInputs(line)
	in.Tanks["ice_mine"] = []TankLevel{tank("water_ice", 700), tank("water_ice", 500)}
	in.Events = []Event{drop("miningbot_0001", 1), gift("trader-9", "water_ice", 50, 5)}

	n := nodeOf(t, Evaluate(line, in), "ice_mine")
	wantState(t, "ice_mine", n.State, StateChoked)
	if n.Units["water_ice"] != 1200 {
		t.Errorf("Units = %v, want 1200", n.Units)
	}

	in.Events = append(in.Events, gift("trader-9", "water_ice", 50, 1))
	wantState(t, "ice_mine after haul", nodeOf(t, Evaluate(line, in), "ice_mine").State, StateOptimal)
}

func TestEvalPurchase(t *testing.T) {
	line := fixtureLine(t)

	in := baseInputs(line)
	in.Tanks["culture_buy"] = []TankLevel{tank("pharmaceutical_culture", 1000)}
	wantState(t, "full tank", nodeOf(t, Evaluate(line, in), "culture_buy").State, StateOptimal)

	in = baseInputs(line)
	in.Events = []Event{fill("trader-10", "buyer", "pharmaceutical_culture", "sirius_observatory_station", 20, 2)}
	n := nodeOf(t, Evaluate(line, in), "culture_buy")
	wantState(t, "recent buy", n.State, StateOptimal)
	if !n.LastEventAt.Equal(at(2)) {
		t.Errorf("LastEventAt = %v, want %v", n.LastEventAt, at(2))
	}

	in = baseInputs(line)
	in.Events = []Event{fill("trader-10", "seller", "pharmaceutical_culture", "sirius_observatory_station", 20, 2)}
	n = nodeOf(t, Evaluate(line, in), "culture_buy")
	wantState(t, "no buy", n.State, StateStarved)
	if !slices.Contains(n.Detail, "no purchase in 24h") {
		t.Errorf("detail %v lacks no-purchase line", n.Detail)
	}
}

func TestEvalStoreChokedNoOutflow(t *testing.T) {
	line := fixtureLine(t)

	// Store feeding sell edges: outflow is the outgoing edges' gift_sent.
	in := baseInputs(line)
	in.Tanks["voss_output"] = []TankLevel{tank("pirate_moonshine", 1500)}
	wantState(t, "voss_output idle", nodeOf(t, Evaluate(line, in), "voss_output").State, StateChoked)
	in.Events = []Event{gift("craftsman-8", "pirate_moonshine", 50, 1)}
	wantState(t, "voss_output consigning", nodeOf(t, Evaluate(line, in), "voss_output").State, StateOptimal)

	// Store feeding a produce node: outflow is that node's completed crafts.
	in = baseInputs(line)
	in.Tanks["voss_inputs"] = []TankLevel{tank("water_ice", 1500)}
	wantState(t, "voss_inputs idle", nodeOf(t, Evaluate(line, in), "voss_inputs").State, StateChoked)
	in.Events = []Event{gift("craftsman-8", "pirate_moonshine", 50, 1)}
	wantState(t, "voss_inputs gift is not outflow", nodeOf(t, Evaluate(line, in), "voss_inputs").State, StateChoked)
	in.Events = []Event{craft("craftsman-10", 60, 1)}
	wantState(t, "voss_inputs crafting", nodeOf(t, Evaluate(line, in), "voss_inputs").State, StateOptimal)
}

func TestEvalStoreEmptyStarved(t *testing.T) {
	line := fixtureLine(t)
	in := baseInputs(line)
	in.Tanks["voss_output"] = []TankLevel{tank("pirate_moonshine", 0)}
	wantState(t, "voss_output", nodeOf(t, Evaluate(line, in), "voss_output").State, StateStarved)

	in.Tanks["voss_output"] = []TankLevel{tank("pirate_moonshine", 30)}
	wantState(t, "voss_output stocked", nodeOf(t, Evaluate(line, in), "voss_output").State, StateOptimal)
}

func TestEvalProduceStarvedBelowOneBatch(t *testing.T) {
	line := fixtureLine(t)
	in := baseInputs(line)
	in.Tanks["voss_inputs"] = []TankLevel{tank("water_ice", 300), tank("pharmaceutical_culture", 500)}
	in.Events = []Event{craft("craftsman-8", 60, 0.5)}

	n := nodeOf(t, Evaluate(line, in), "still")
	wantState(t, "still", n.State, StateStarved)
	if !hasAlert(n, AlertInputsStarved) {
		t.Errorf("alerts %v lack %q", n.Alerts, AlertInputsStarved)
	}
	if !slices.Equal(n.Detail, []string{"water_ice 300/600 for one batch"}) {
		t.Errorf("detail = %v", n.Detail)
	}
}

func TestEvalProduceOptimal(t *testing.T) {
	line := fixtureLine(t)
	in := baseInputs(line)
	stillInputsFull(&in)
	in.Events = []Event{craft("craftsman-8", 60, 0.5)}

	n := nodeOf(t, Evaluate(line, in), "still")
	wantState(t, "still", n.State, StateOptimal)
	if !n.LastEventAt.Equal(at(0.5)) {
		t.Errorf("LastEventAt = %v, want %v", n.LastEventAt, at(0.5))
	}

	// Inputs present but the still has not run: choked.
	in.Events = []Event{craft("craftsman-8", 60, 2), {AgentID: "craftsman-8", Type: EventCrafted, At: at(0.5), Runs: 1, Recipe: "other"}}
	n = nodeOf(t, Evaluate(line, in), "still")
	wantState(t, "still idle", n.State, StateChoked)
	if !slices.Contains(n.Detail, "inputs present, no craft in 1h") {
		t.Errorf("detail %v lacks idle line", n.Detail)
	}
}

func TestEvalProduceChokedOutputNotDrawn(t *testing.T) {
	line := fixtureLine(t)
	in := baseInputs(line)
	stillInputsFull(&in)
	in.Tanks["voss_output"] = []TankLevel{tank("pirate_moonshine", 1500)}
	in.Events = []Event{craft("craftsman-8", 60, 0.5)}

	n := nodeOf(t, Evaluate(line, in), "still")
	wantState(t, "still", n.State, StateChoked)
	if !hasAlert(n, AlertOutputNotDrawn) {
		t.Errorf("alerts %v lack %q", n.Alerts, AlertOutputNotDrawn)
	}

	in.Events = append(in.Events, Event{AgentID: "craftsman-10", Type: EventWithdraw, At: at(1), Item: "pirate_moonshine", Units: 200})
	n = nodeOf(t, Evaluate(line, in), "still")
	wantState(t, "still drawn", n.State, StateOptimal)
}

func TestEvalSellSaturatedAtFloor(t *testing.T) {
	line := fixtureLine(t)
	in := baseInputs(line)
	e := in.Roster["marketbot_alhena"]
	e.Shopkeep = []string{"iron_ore:10", "pirate_moonshine:900:700"}
	in.Roster["marketbot_alhena"] = e
	in.Asks["alhena_sell"] = &OurAsk{Station: "voss_redoubt_station", Price: 700, Units: 50, PostedAt: at(7)}
	in.Demand["alhena_sell"] = Demand{Station: "voss_redoubt_station", BidUnits: 10, BestBid: 300}

	n := nodeOf(t, Evaluate(line, in), "alhena_sell")
	wantState(t, "alhena_sell", n.State, StateChoked)
	if !hasAlert(n, AlertMarketSaturated) {
		t.Errorf("alerts %v lack %q", n.Alerts, AlertMarketSaturated)
	}
	if n.Ask == nil || n.Ask.Price != 700 || n.Demand == nil || n.Demand.BestBid != 300 {
		t.Errorf("ask/demand = %+v / %+v", n.Ask, n.Demand)
	}

	// Above the floor and stale, no fill: starved, not saturated.
	in.Asks["alhena_sell"] = &OurAsk{Price: 800, PostedAt: at(7)}
	wantState(t, "above floor", nodeOf(t, Evaluate(line, in), "alhena_sell").State, StateStarved)

	// At the floor but fresh: optimal.
	in.Asks["alhena_sell"] = &OurAsk{Price: 700, PostedAt: at(1)}
	wantState(t, "fresh ask", nodeOf(t, Evaluate(line, in), "alhena_sell").State, StateOptimal)
}

func TestEvalSellNoShopRuleNeverAtFloor(t *testing.T) {
	line := fixtureLine(t)
	in := baseInputs(line)
	in.Asks["alhena_sell"] = &OurAsk{Price: 1, PostedAt: at(7)}

	n := nodeOf(t, Evaluate(line, in), "alhena_sell")
	if hasAlert(n, AlertMarketSaturated) {
		t.Error("saturated raised with no shopkeep rule")
	}
}

func TestEvalSellStarvedNoStockNoAsk(t *testing.T) {
	line := fixtureLine(t)
	in := baseInputs(line)
	in.Tanks["alhena_sell"] = []TankLevel{tank("pirate_moonshine", 10)}

	n := nodeOf(t, Evaluate(line, in), "alhena_sell")
	wantState(t, "alhena_sell", n.State, StateStarved)
	if !slices.Contains(n.Detail, "stock 10 below one lot") {
		t.Errorf("detail %v lacks stock line", n.Detail)
	}
}

func TestEvalSellOptimalOnFill(t *testing.T) {
	line := fixtureLine(t)
	in := baseInputs(line)
	in.Tanks["alhena_sell"] = []TankLevel{tank("pirate_moonshine", 100)}
	in.Events = []Event{fill("marketbot_alhena", "seller", "pirate_moonshine", "voss_redoubt_station", 5, 2)}

	n := nodeOf(t, Evaluate(line, in), "alhena_sell")
	wantState(t, "alhena_sell", n.State, StateOptimal)
	if !n.LastEventAt.Equal(at(2)) {
		t.Errorf("LastEventAt = %v, want %v", n.LastEventAt, at(2))
	}

	in.Events = []Event{fill("marketbot_alhena", "seller", "pirate_moonshine", "treasure_cache_trading_post", 5, 2)}
	wantState(t, "fill at another station", nodeOf(t, Evaluate(line, in), "alhena_sell").State, StateStarved)
}

func TestEvalEdgeHaulerStarved(t *testing.T) {
	line := fixtureLine(t)
	in := baseInputs(line)
	in.Tanks["ice_mine"] = []TankLevel{tank("water_ice", 200)}
	in.Events = []Event{drop("miningbot_0000", 1)}

	r := Evaluate(line, in)
	e := edgeOf(t, r, "ice_mine", "voss_inputs")
	wantState(t, "ice haul", e.State, StateChoked)
	if !hasAlert(nodeOf(t, r, "ice_mine"), AlertHaulerStarved) {
		t.Errorf("ice_mine alerts %v lack %q", nodeOf(t, r, "ice_mine").Alerts, AlertHaulerStarved)
	}
	if !slices.Equal(e.Agents, []string{"trader-9"}) || e.Item != "water_ice" || e.Kind != EdgeHaul {
		t.Errorf("edge = %+v", e)
	}

	in.Events = append(in.Events, gift("trader-9", "water_ice", 40, 1), gift("trader-9", "water_ice", 25, 10),
		gift("trader-9", "pirate_moonshine", 99, 0.5))
	r = Evaluate(line, in)
	e = edgeOf(t, r, "ice_mine", "voss_inputs")
	wantState(t, "ice haul moving", e.State, StateOptimal)
	if !e.LastMovedAt.Equal(at(1)) || e.MovedUnitsWindow != 65 {
		t.Errorf("LastMovedAt=%v moved=%d, want %v / 65", e.LastMovedAt, e.MovedUnitsWindow, at(1))
	}
	if hasAlert(nodeOf(t, r, "ice_mine"), AlertHaulerStarved) {
		t.Error("hauler starved raised while hauling")
	}
}

func TestEvalEdgeStarvedNothingToMove(t *testing.T) {
	line := fixtureLine(t)
	in := baseInputs(line)
	in.Tanks["ice_mine"] = []TankLevel{tank("water_ice", 49)}

	r := Evaluate(line, in)
	wantState(t, "ice haul", edgeOf(t, r, "ice_mine", "voss_inputs").State, StateStarved)
	if hasAlert(nodeOf(t, r, "ice_mine"), AlertHaulerStarved) {
		t.Error("hauler starved raised with nothing to move")
	}
}

func TestEvalCraftEdgesTakeProduceState(t *testing.T) {
	line := fixtureLine(t)
	in := baseInputs(line)
	stillInputsFull(&in)
	in.Events = []Event{craft("craftsman-8", 60, 0.5)}

	r := Evaluate(line, in)
	wantState(t, "inputs->still", edgeOf(t, r, "voss_inputs", "still").State, StateOptimal)
	wantState(t, "still->output", edgeOf(t, r, "still", "voss_output").State, StateOptimal)

	in.Events = nil
	r = Evaluate(line, in)
	wantState(t, "inputs->still idle", edgeOf(t, r, "voss_inputs", "still").State, StateChoked)
	wantState(t, "still->output idle", edgeOf(t, r, "still", "voss_output").State, StateChoked)
}

func TestEvalStaleSourceGreysNode(t *testing.T) {
	line := fixtureLine(t)
	in := baseInputs(line)
	stillInputsFull(&in)
	in.Events = []Event{craft("craftsman-8", 60, 0.5)}
	for _, a := range []string{"craftsman-8", "craftsman-10"} {
		l := in.Live[a]
		l.LastSeen = at(3)
		in.Live[a] = l
		in.Cursors[a] = at(3)
	}
	stale := tank("pirate_moonshine", 30)
	stale.CapturedAt = at(5)
	in.Tanks["voss_output"] = []TankLevel{stale}

	r := Evaluate(line, in)
	still := nodeOf(t, r, "still")
	wantState(t, "still", still.State, StateUnknown)
	if !still.Stale || !hasAlert(still, AlertDataStale) {
		t.Errorf("still stale=%v alerts=%v", still.Stale, still.Alerts)
	}
	wantState(t, "still->output", edgeOf(t, r, "still", "voss_output").State, StateUnknown)
	out := nodeOf(t, r, "voss_output")
	wantState(t, "voss_output", out.State, StateUnknown)
	if !out.Stale {
		t.Error("voss_output with a 5h-old tank not stale")
	}
	// voss_inputs has fresh tanks; no-source nodes are never stale.
	if nodeOf(t, r, "voss_inputs").Stale {
		t.Error("voss_inputs stale with fresh tanks")
	}
	if !slices.Equal(r.StaleSources, []string{"still", "voss_output"}) {
		t.Errorf("StaleSources = %v", r.StaleSources)
	}

	// One fresh cursor keeps the node fresh.
	in.Cursors["craftsman-10"] = at(1)
	if nodeOf(t, Evaluate(line, in), "still").Stale {
		t.Error("still stale with one fresh cursor")
	}
}

func TestEvalStaleNodeWithoutSources(t *testing.T) {
	line := fixtureLine(t)
	in := baseInputs(line)
	if nodeOf(t, Evaluate(line, in), "voss_output").Stale {
		t.Error("node with no tanks and no agents marked stale")
	}
}

func TestEvalRates(t *testing.T) {
	line := fixtureLine(t)
	in := baseInputs(line)
	in.Events = []Event{
		craft("craftsman-8", 60, 2), craft("craftsman-10", 60, 10),
		craft("craftsman-8", 60, 30), // outside the rate window
		{AgentID: "craftsman-8", Type: EventCrafted, At: at(1), Runs: 60, Recipe: "other"},
		fill("marketbot_alhena", "seller", "pirate_moonshine", "voss_redoubt_station", 40, 3),
		fill("marketbot_treasure_cache", "seller", "pirate_moonshine", "treasure_cache_trading_post", 25, 20),
	}

	r := Evaluate(line, in)
	if r.ProducedPerDay != 720 || r.SoldPerDay != 65 || r.CapacityPerDay != 19200 || r.EffPct != 3 {
		t.Errorf("produced=%d sold=%d cap=%d eff=%d, want 720/65/19200/3",
			r.ProducedPerDay, r.SoldPerDay, r.CapacityPerDay, r.EffPct)
	}
	if r.ID != "moonshine" || r.Name != "Pirate Moonshine" || !r.GeneratedAt.Equal(evalNow) {
		t.Errorf("header = %q %q %v", r.ID, r.Name, r.GeneratedAt)
	}
}

func TestEvalRatesZeroCapacity(t *testing.T) {
	line := fixtureLine(t)
	line.CapacityPerDay = 0
	in := baseInputs(line)
	in.Events = []Event{craft("craftsman-8", 60, 2)}
	if r := Evaluate(line, in); r.EffPct != 0 || r.ProducedPerDay != 360 {
		t.Errorf("eff=%d produced=%d, want 0/360", r.EffPct, r.ProducedPerDay)
	}
}

func TestEvalSoldUnitsFilteredByLineItem(t *testing.T) {
	line := fixtureLine(t)
	in := baseInputs(line)
	in.Events = []Event{
		fill("marketbot_alhena", "seller", "pirate_moonshine", "voss_redoubt_station", 40, 3),
		fill("marketbot_alhena", "seller", "bootleg_cigars", "voss_redoubt_station", 500, 3),        // another line's item
		fill("marketbot_alhena", "buyer", "pirate_moonshine", "voss_redoubt_station", 300, 3),       // a buy
		fill("trader-9", "seller", "pirate_moonshine", "voss_redoubt_station", 300, 3),              // not a sell agent
		fill("marketbot_alhena", "seller", "pirate_moonshine", "treasure_cache_trading_post", 7, 3), // wrong node station
	}
	if r := Evaluate(line, in); r.SoldPerDay != 40 {
		t.Errorf("SoldPerDay = %d, want 40", r.SoldPerDay)
	}
}

func TestEvalLineStateWorstAndAlertsDeduped(t *testing.T) {
	line := fixtureLine(t)
	in := baseInputs(line)
	for _, a := range []string{"craftsman-8", "trader-10", "marketbot_alhena"} {
		l := in.Live[a]
		l.Credits = 1
		in.Live[a] = l
	}
	in.Tanks["voss_output"] = []TankLevel{tank("pirate_moonshine", 1500)}

	r := Evaluate(line, in)
	wantState(t, "line", r.State, StateChoked)
	n := 0
	for _, a := range r.Alerts {
		if a == AlertAgentBroke {
			n++
		}
	}
	if n != 1 {
		t.Errorf("AlertAgentBroke appears %d times in %v, want once", n, r.Alerts)
	}
	// ice_mine (first node) raises source depleted before culture_buy's broke.
	if i, j := slices.Index(r.Alerts, AlertSourceDepleted), slices.Index(r.Alerts, AlertAgentBroke); i < 0 || j < 0 || i > j {
		t.Errorf("alerts %v not in node order", r.Alerts)
	}

	// All optimal except one unknown -> unknown.
	if got := worst([]State{StateOptimal, StateUnknown, StateOptimal}); got != StateUnknown {
		t.Errorf("worst = %q, want unknown", got)
	}
	if got := worst([]State{StateUnknown, StateStarved}); got != StateStarved {
		t.Errorf("worst = %q, want starved", got)
	}
}

func TestAgentsOf(t *testing.T) {
	want := []string{
		"craftsman-10", "craftsman-8", "marketbot_alhena", "marketbot_treasure_cache",
		"miningbot_0000", "miningbot_0001", "miningbot_0002", "miningbot_0003", "trader-10", "trader-9",
	}
	if got := AgentsOf(fixtureLine(t)); !slices.Equal(got, want) {
		t.Errorf("AgentsOf = %v\nwant %v", got, want)
	}
}
