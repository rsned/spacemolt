package linedash

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// Alert is a human-readable problem raised on a node and rolled up to the line.
type Alert string

// The alerts the evaluator can raise.
const (
	AlertInputsStarved   Alert = "output stalled waiting on inputs"
	AlertMarketSaturated Alert = "market saturated"
	// AlertFacilityBusy is reserved: facility busy has no capture signal;
	// reserved, never raised in v1.
	AlertFacilityBusy   Alert = "facility busy"
	AlertHaulerStarved  Alert = "hauler starved"
	AlertSourceDepleted Alert = "source depleted"
	AlertAgentBroke     Alert = "agent broke/stranded"
	AlertNoBotsLive     Alert = "no bots on node"  // all attached miners away/removed
	AlertOutputNotDrawn Alert = "output not drawn" // produce output tank growing, no faction_pull/withdraw
	AlertDataStale      Alert = "data stale"
)

// AgentReport is one attached agent's condition as shown on a node.
type AgentReport struct {
	AgentID, Fleet, Role, Where         string // Where = "<system>/<poi>" or "docked <poi>"
	Live, Healthy, Quarantined, Removed bool
	AwayFleet, Phase                    string // secondment
	Credits, FuelPct                    float64
	Broke, Stranded                     bool
	LastSeen                            time.Time
}

// NodeReport is the evaluated state of one node.
type NodeReport struct {
	ID                         string
	Type                       NodeType
	Name                       string
	State                      State
	Alerts                     []Alert
	Tanks                      []TankLevel
	Units                      map[string]float64 // item -> summed units
	Agents                     []AgentReport
	LiveAgents, AssignedAgents int     // slider figures
	Demand                     *Demand // sell nodes
	Ask                        *OurAsk // sell nodes
	LastEventAt                time.Time
	Stale                      bool     // freshest relevant source older than StaleData
	Detail                     []string // human lines shown under the node
	X, Y                       int      // filled by layout
}

// EdgeReport is the evaluated state of one edge.
type EdgeReport struct {
	From, To         string
	Kind             EdgeKind
	Item, Label      string
	State            State
	Agents           []string
	LastMovedAt      time.Time
	MovedUnitsWindow int // RateWindow sum of gift_sent units by edge agents for Item
}

// LineReport is the evaluated state of a whole line.
type LineReport struct {
	ID, Name                                   string
	State                                      State
	Alerts                                     []Alert
	ProducedPerDay, SoldPerDay, CapacityPerDay int
	EffPct                                     int
	Nodes                                      []NodeReport
	Edges                                      []EdgeReport
	StaleSources                               []string // ids of nodes whose data is stale, in node order
	GeneratedAt                                time.Time
}

// Inputs is everything the evaluator reads; it does no I/O itself.
type Inputs struct {
	Now     time.Time
	Cfg     Thresholds
	Roster  map[string]RosterEntry
	Live    map[string]Live
	Tanks   map[string][]TankLevel // node id -> levels
	Events  []Event                // window = RateWindow, all line agents
	Cursors map[string]time.Time
	Demand  map[string]Demand  // node id -> demand (sell nodes)
	Asks    map[string]*OurAsk // node id -> ask (sell nodes)
}

// AgentsOf returns every agent on any node, edge or holder of the line,
// deduped and sorted: the query set for events and live data.
func AgentsOf(line Line) []string {
	var out []string
	for _, n := range line.Nodes {
		out = append(out, n.Agents...)
		for _, h := range n.Holders {
			if h.Agent != "" {
				out = append(out, h.Agent)
			}
		}
	}
	for _, e := range line.Edges {
		out = append(out, e.Agents...)
	}
	slices.Sort(out)

	return slices.Compact(out)
}

// evaluator carries one Evaluate call's inputs and the line graph.
type evaluator struct {
	line  Line
	in    Inputs
	nodes map[string]Node
}

// Evaluate classifies every node and edge of line and computes its rates.
func Evaluate(line Line, in Inputs) LineReport {
	ev := &evaluator{line: line, in: in, nodes: make(map[string]Node, len(line.Nodes))}
	for _, n := range line.Nodes {
		ev.nodes[n.ID] = n
	}
	r := LineReport{ID: line.ID, Name: line.Name, CapacityPerDay: line.CapacityPerDay, GeneratedAt: in.Now}
	index := make(map[string]int, len(line.Nodes))
	for i, n := range line.Nodes {
		r.Nodes = append(r.Nodes, ev.evalNode(n))
		index[n.ID] = i
	}
	for _, e := range line.Edges {
		er, haulerStarved := ev.evalEdge(e)
		if haulerStarved {
			addAlert(&r.Nodes[index[e.From]], AlertHaulerStarved)
		}
		r.Edges = append(r.Edges, er)
	}
	for i := range r.Nodes {
		if ev.applyStale(&r.Nodes[i]) {
			r.StaleSources = append(r.StaleSources, r.Nodes[i].ID)
		}
	}
	// Craft edges take the produce node's final (post-stale) state.
	for i, e := range line.Edges {
		if e.Kind == EdgeCraft {
			r.Edges[i].State = craftEdgeState(e, r.Nodes, index, ev.nodes)
		}
	}
	r.State, r.Alerts = rollUp(r.Nodes, r.Edges)
	r.ProducedPerDay = ev.producedPerDay()
	r.SoldPerDay = ev.soldPerDay()
	if line.CapacityPerDay > 0 {
		r.EffPct = r.ProducedPerDay * 100 / line.CapacityPerDay
	}

	return r
}

// evalNode builds the common parts of a node report and applies its type rule.
func (ev *evaluator) evalNode(n Node) NodeReport {
	nr := NodeReport{
		ID: n.ID, Type: n.Type, Name: n.Name,
		Tanks:          ev.in.Tanks[n.ID],
		Units:          map[string]float64{},
		AssignedAgents: len(n.Agents),
	}
	for _, it := range n.Items {
		nr.Units[it] = SumUnits(nr.Tanks, it)
	}
	for _, a := range n.Agents {
		ar := ev.agentReport(a)
		nr.Agents = append(nr.Agents, ar)
		if ar.Live && !ar.Removed && ar.AwayFleet == "" {
			nr.LiveAgents++
		}
		if ar.Broke || ar.Stranded {
			addAlert(&nr, AlertAgentBroke)
		}
	}
	switch n.Type {
	case NodeMine:
		ev.evalMine(n, &nr)
	case NodePurchase:
		ev.evalPurchase(n, &nr)
	case NodeStore:
		ev.evalStore(n, &nr)
	case NodeProduce:
		ev.evalProduce(n, &nr)
	case NodeSell:
		ev.evalSell(n, &nr)
	default:
		// Convert nodes have no v1 rule.
		nr.State = StateUnknown
		nr.Detail = append(nr.Detail, fmt.Sprintf("no rule for %s nodes", n.Type))
	}

	return nr
}

// agentReport joins roster and live data. An agent absent from Live has zero
// figures and is never flagged broke or stranded (no data). Stranded also
// needs a known MaxFuel.
func (ev *evaluator) agentReport(id string) AgentReport {
	re := ev.in.Roster[id]
	ar := AgentReport{
		AgentID: id, Fleet: re.Fleet, Role: re.Role, Removed: re.Removed,
		AwayFleet: re.AwayFleet, Phase: re.Phase,
	}
	l, ok := ev.in.Live[id]
	if !ok {
		return ar
	}
	if ar.Fleet == "" {
		ar.Fleet = l.Fleet
	}
	ar.Where = l.System + "/" + l.POI
	if l.Docked {
		ar.Where = "docked " + l.POI
	}
	ar.Live = ev.within(l.LastSeen, ev.in.Cfg.StaleData)
	ar.Healthy, ar.Quarantined = l.Healthy, l.Quarantined
	ar.Credits, ar.LastSeen = l.Credits, l.LastSeen
	if l.MaxFuel > 0 {
		ar.FuelPct = l.Fuel / l.MaxFuel * 100
	}
	ar.Broke = l.Credits < float64(ev.in.Cfg.BrokeCredits)
	ar.Stranded = !l.Docked && l.MaxFuel > 0 && ar.FuelPct < float64(ev.in.Cfg.FuelFloorPct)

	return ar
}

// evalMine: drops are gift_received rows logged by the node's holder agents
// whose Sender is an attached agent, for a node item.
func (ev *evaluator) evalMine(n Node, nr *NodeReport) {
	holders := holderAgents(n)
	lastDrop := ev.newest(func(e Event) bool {
		return e.Type == EventGiftReceived && slices.Contains(holders, e.AgentID) &&
			slices.Contains(n.Agents, e.Sender) && slices.Contains(n.Items, e.Item)
	})
	nr.LastEventAt = lastDrop
	switch {
	case nr.LiveAgents == 0:
		setState(nr, StateStarved, AlertNoBotsLive)
		for _, a := range nr.Agents {
			if a.AwayFleet != "" {
				nr.Detail = append(nr.Detail, fmt.Sprintf("%s: %s in %s", a.AgentID, a.Phase, a.AwayFleet))
			}
		}
	case ev.within(lastDrop, ev.in.Cfg.MineWindow):
		nr.State = StateOptimal
	default:
		setState(nr, StateStarved, AlertSourceDepleted)
	}
	if ev.anyChoked(nr) && !ev.haulMoved(ev.outgoing(n.ID, EdgeHaul)) {
		nr.State = StateChoked
	}
}

// evalPurchase: a hauler lot in the tank, or a buy within RateWindow, is optimal.
func (ev *evaluator) evalPurchase(n Node, nr *NodeReport) {
	lastBuy := ev.newest(func(e Event) bool {
		return e.Type == EventExchangeFill && e.Role == "buyer" &&
			slices.Contains(n.Items, e.Item) && slices.Contains(n.Agents, e.AgentID)
	})
	nr.LastEventAt = lastBuy
	if ev.anyChoked(nr) || ev.within(lastBuy, ev.in.Cfg.RateWindow) {
		nr.State = StateOptimal

		return
	}
	nr.State = StateStarved
	nr.Detail = append(nr.Detail, "no purchase in "+fmtDur(ev.in.Cfg.RateWindow))
}

// evalStore: empty is starved; a full item with no outflow in HaulWindow is choked.
func (ev *evaluator) evalStore(n Node, nr *NodeReport) {
	outflow := ev.newest(ev.storeOutflow(n.ID))
	nr.LastEventAt = latest(outflow, ev.newest(ev.storeInflow(n.ID)))
	empty := true
	for _, u := range nr.Units {
		if u != 0 {
			empty = false
		}
	}
	switch {
	case empty:
		nr.State = StateStarved
	case ev.anyChoked(nr) && !ev.within(outflow, ev.in.Cfg.HaulWindow):
		nr.State = StateChoked
	default:
		nr.State = StateOptimal
	}
}

// evalProduce: inputs below one batch starve the still; inputs present with no
// craft in CraftWindow is choked; an undrawn full output tank is choked.
func (ev *evaluator) evalProduce(n Node, nr *NodeReport) {
	lastCraft := ev.newest(craftedBy(ev.nodes[n.ID]))
	nr.LastEventAt = lastCraft
	upstream := ev.tanksOf(ev.from(n.ID))
	starved := false
	for _, item := range sortedKeys(n.InputsPerRun) {
		need := n.InputsPerRun[item] * n.BatchRuns
		if have := SumUnits(upstream, item); have < float64(need) {
			starved = true
			nr.Detail = append(nr.Detail, fmt.Sprintf("%s %.0f/%d for one batch", item, have, need))
		}
	}
	switch {
	case starved:
		setState(nr, StateStarved, AlertInputsStarved)
	case ev.within(lastCraft, ev.in.Cfg.CraftWindow):
		nr.State = StateOptimal
	default:
		nr.State = StateChoked
		nr.Detail = append(nr.Detail, "inputs present, no craft in "+fmtDur(ev.in.Cfg.CraftWindow))
	}
	out := ev.line.OutputItem
	lastDraw := ev.newest(func(e Event) bool {
		return e.Type == EventWithdraw && e.Item == out && slices.Contains(n.Agents, e.AgentID)
	})
	if SumUnits(ev.tanksOf(ev.to(n.ID)), out) >= float64(ev.in.Cfg.ChokeUnits) &&
		!ev.within(lastDraw, ev.in.Cfg.HaulWindow) {
		setState(nr, StateChoked, AlertOutputNotDrawn)
	}
}

// evalSell: see the brief's ordered rules. The floor comes from the first
// attached agent with a shopkeep rule for the item; with no rule the floor is
// 0, so an ask is only "at the floor" when its price is <= 0 (in practice
// never). An ask with an unknown PostedAt is never stale.
func (ev *evaluator) evalSell(n Node, nr *NodeReport) {
	item := ev.sellItem(n)
	if d, ok := ev.in.Demand[n.ID]; ok {
		nr.Demand = &d
	}
	ask := ev.in.Asks[n.ID]
	nr.Ask = ask
	lastFill := ev.newest(func(e Event) bool {
		return e.Type == EventExchangeFill && e.Role == "seller" && e.Station == n.Station &&
			slices.Contains(n.Items, e.Item) && slices.Contains(n.Agents, e.AgentID)
	})
	nr.LastEventAt = lastFill
	stale := ask != nil && !ask.PostedAt.IsZero() && ev.in.Now.Sub(ask.PostedAt) > ev.in.Cfg.SellStale
	switch {
	case ask == nil && nr.Units[item] < float64(ev.in.Cfg.SellLot):
		nr.State = StateStarved
		nr.Detail = append(nr.Detail, fmt.Sprintf("stock %.0f below one lot", nr.Units[item]))
	case stale && ask.Price <= float64(ev.shopFloor(n, item)):
		setState(nr, StateChoked, AlertMarketSaturated)
	case ev.within(lastFill, ev.in.Cfg.SellWindow):
		nr.State = StateOptimal
	case ask != nil && !stale:
		nr.State = StateOptimal
	default:
		nr.State = StateStarved
	}
}

// evalEdge classifies a haul/consign edge. haulerStarved reports that goods
// wait on the from node, which then carries AlertHaulerStarved. Craft edges get
// their state later from the produce node.
func (ev *evaluator) evalEdge(e Edge) (EdgeReport, bool) {
	er := EdgeReport{From: e.From, To: e.To, Kind: e.Kind, Item: e.Item, Label: e.Label, Agents: e.Agents}
	if e.Kind == EdgeCraft {
		return er, false
	}
	moved := movedBy(e)
	er.LastMovedAt = ev.newest(moved)
	for _, x := range ev.in.Events {
		if moved(x) && ev.within(x.At, ev.in.Cfg.RateWindow) {
			er.MovedUnitsWindow += x.Units
		}
	}
	switch {
	case ev.within(er.LastMovedAt, ev.in.Cfg.HaulWindow):
		er.State = StateOptimal
	case SumUnits(ev.tanksOf(e.From), e.Item) >= float64(ev.in.Cfg.SellLot):
		er.State = StateChoked

		return er, true
	default:
		er.State = StateStarved
	}

	return er, false
}

// applyStale greys a node whose freshest source (tank capture, attached
// agent's cursor or last_seen) is older than StaleData. A node with no
// sources at all is not stale. Rule-derived alerts are kept.
func (ev *evaluator) applyStale(nr *NodeReport) bool {
	n := ev.nodes[nr.ID]
	if len(nr.Tanks) == 0 && len(n.Agents) == 0 {
		return false
	}
	freshest := Freshest(nr.Tanks)
	for _, a := range n.Agents {
		freshest = latest(freshest, ev.in.Cursors[a])
		if l, ok := ev.in.Live[a]; ok {
			freshest = latest(freshest, l.LastSeen)
		}
	}
	if ev.within(freshest, ev.in.Cfg.StaleData) {
		return false
	}
	nr.Stale = true
	setState(nr, StateUnknown, AlertDataStale)

	return true
}

func (ev *evaluator) producedPerDay() int {
	total := 0
	for _, n := range ev.line.Nodes {
		if n.Type != NodeProduce {
			continue
		}
		crafted := craftedBy(n)
		for _, e := range ev.in.Events {
			if crafted(e) && ev.within(e.At, ev.in.Cfg.RateWindow) {
				total += e.Runs * n.UnitsPerRun
			}
		}
	}

	return total
}

// soldPerDay counts only the line's output item, at each sell node's station,
// by that node's agents, so a shared shopkeeper's other lines are excluded.
func (ev *evaluator) soldPerDay() int {
	total := 0
	for _, n := range ev.line.Nodes {
		if n.Type != NodeSell {
			continue
		}
		for _, e := range ev.in.Events {
			if e.Type == EventExchangeFill && e.Role == "seller" && e.Item == ev.line.OutputItem &&
				e.Station == n.Station && slices.Contains(n.Agents, e.AgentID) && ev.within(e.At, ev.in.Cfg.RateWindow) {
				total += e.Units
			}
		}
	}

	return total
}

// within reports t is set, not after Now, and no older than w.
func (ev *evaluator) within(t time.Time, w time.Duration) bool {
	return !t.IsZero() && !t.After(ev.in.Now) && ev.in.Now.Sub(t) <= w
}

// newest returns the latest At among matching events not after Now.
func (ev *evaluator) newest(match func(Event) bool) time.Time {
	var t time.Time
	for _, e := range ev.in.Events {
		if match(e) && !e.At.After(ev.in.Now) && e.At.After(t) {
			t = e.At
		}
	}

	return t
}

// anyChoked reports any node item at or above ChokeUnits.
func (ev *evaluator) anyChoked(nr *NodeReport) bool {
	for _, u := range nr.Units {
		if u >= float64(ev.in.Cfg.ChokeUnits) {
			return true
		}
	}

	return false
}

func (ev *evaluator) tanksOf(nodeID string) []TankLevel { return ev.in.Tanks[nodeID] }

// outgoing returns the node's outgoing edges of kind k.
func (ev *evaluator) outgoing(nodeID string, k EdgeKind) []Edge {
	var out []Edge
	for _, e := range ev.line.Edges {
		if e.From == nodeID && e.Kind == k {
			out = append(out, e)
		}
	}

	return out
}

// from returns the upstream store of a produce node: its incoming craft edge's from.
func (ev *evaluator) from(nodeID string) string {
	for _, e := range ev.line.Edges {
		if e.To == nodeID && e.Kind == EdgeCraft {
			return e.From
		}
	}

	return ""
}

// to returns the downstream store of a produce node: its outgoing craft edge's to.
func (ev *evaluator) to(nodeID string) string {
	if out := ev.outgoing(nodeID, EdgeCraft); len(out) > 0 {
		return out[0].To
	}

	return ""
}

// haulMoved reports any of the edges moved within HaulWindow.
func (ev *evaluator) haulMoved(edges []Edge) bool {
	for _, e := range edges {
		if ev.within(ev.newest(movedBy(e)), ev.in.Cfg.HaulWindow) {
			return true
		}
	}

	return false
}

// storeOutflow: completed crafts of the produce node a store feeds, else
// gift_sent on the store's outgoing haul/consign edges.
func (ev *evaluator) storeOutflow(nodeID string) func(Event) bool {
	if craft := ev.outgoing(nodeID, EdgeCraft); len(craft) > 0 {
		return craftedBy(ev.nodes[craft[0].To])
	}
	edges := append(ev.outgoing(nodeID, EdgeHaul), ev.outgoing(nodeID, EdgeConsign)...)

	return anyMoved(edges)
}

// storeInflow: gift_sent on incoming haul/consign edges, or completed crafts
// of the upstream produce node.
func (ev *evaluator) storeInflow(nodeID string) func(Event) bool {
	var edges []Edge
	var producers []Node
	for _, e := range ev.line.Edges {
		switch {
		case e.To != nodeID:
		case e.Kind == EdgeCraft:
			producers = append(producers, ev.nodes[e.From])
		default:
			edges = append(edges, e)
		}
	}
	moved := anyMoved(edges)

	return func(e Event) bool {
		if moved(e) {
			return true
		}
		for _, p := range producers {
			if craftedBy(p)(e) {
				return true
			}
		}

		return false
	}
}

// sellItem is the line's output item when the node carries it, else the
// node's first item.
func (ev *evaluator) sellItem(n Node) string {
	if slices.Contains(n.Items, ev.line.OutputItem) || len(n.Items) == 0 {
		return ev.line.OutputItem
	}

	return n.Items[0]
}

func (ev *evaluator) shopFloor(n Node, item string) int {
	for _, a := range n.Agents {
		if _, floor, ok := ShopFloor(ev.in.Roster[a].Shopkeep, item); ok {
			return floor
		}
	}

	return 0
}

// craftEdgeState is the state of the produce node at either end of a craft edge.
func craftEdgeState(e Edge, reports []NodeReport, index map[string]int, nodes map[string]Node) State {
	for _, id := range []string{e.To, e.From} {
		if nodes[id].Type == NodeProduce {
			return reports[index[id]].State
		}
	}

	return StateUnknown
}

func craftedBy(n Node) func(Event) bool {
	return func(e Event) bool {
		return e.Type == EventCrafted && e.Recipe == n.Recipe && slices.Contains(n.Agents, e.AgentID)
	}
}

func movedBy(e Edge) func(Event) bool {
	return func(x Event) bool {
		return x.Type == EventGiftSent && x.Item == e.Item && slices.Contains(e.Agents, x.AgentID)
	}
}

func anyMoved(edges []Edge) func(Event) bool {
	return func(x Event) bool {
		for _, e := range edges {
			if movedBy(e)(x) {
				return true
			}
		}

		return false
	}
}

func holderAgents(n Node) []string {
	var out []string
	for _, h := range n.Holders {
		if h.Agent != "" {
			out = append(out, h.Agent)
		}
	}

	return out
}

var stateRank = map[State]int{StateOptimal: 0, StateUnknown: 1, StateStarved: 2, StateChoked: 3}

// worst returns the most severe state: Choked > Starved > Unknown > Optimal.
func worst(states []State) State {
	w := StateOptimal
	for _, s := range states {
		if stateRank[s] > stateRank[w] {
			w = s
		}
	}

	return w
}

// rollUp computes the line state over nodes and edges and the deduped alerts
// in node order (edges carry no alerts of their own).
func rollUp(nodes []NodeReport, edges []EdgeReport) (State, []Alert) {
	var states []State
	var alerts []Alert
	for _, n := range nodes {
		states = append(states, n.State)
		for _, a := range n.Alerts {
			if !slices.Contains(alerts, a) {
				alerts = append(alerts, a)
			}
		}
	}
	for _, e := range edges {
		states = append(states, e.State)
	}

	return worst(states), alerts
}

func setState(nr *NodeReport, s State, a Alert) {
	nr.State = s
	addAlert(nr, a)
}

func addAlert(nr *NodeReport, a Alert) {
	if !slices.Contains(nr.Alerts, a) {
		nr.Alerts = append(nr.Alerts, a)
	}
}

func latest(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}

	return a
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	return keys
}

// fmtDur renders 24h0m0s as 24h and 1h30m0s as 1h30m.
func fmtDur(d time.Duration) string {
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}

	return s
}
