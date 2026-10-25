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
	// AlertFacilityBusy is reserved: facility busy has no capture signal, so
	// it is never raised in v1.
	AlertFacilityBusy   Alert = "facility busy"
	AlertHaulerStarved  Alert = "hauler starved"
	AlertSourceDepleted Alert = "source depleted"
	AlertAgentBroke     Alert = "agent broke/stranded"
	AlertNoBotsLive     Alert = "no bots on node"  // all attached miners away/removed
	AlertOutputNotDrawn Alert = "output not drawn" // produce output tank growing, no faction_pull/withdraw
	AlertDataStale      Alert = "data stale"
)

// lastKnown prefixes the rule verdict of a stale node or edge in Detail.
const lastKnown = "last known: "

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
	Stale                      bool     // a source the node rule read is zero or older than StaleData
	Detail                     []string // human lines shown under the node
	X, Y                       int      // filled by layout

	srcs []source
}

// EdgeReport is the evaluated state of one edge.
type EdgeReport struct {
	From, To         string
	Kind             EdgeKind
	Item, Label      string
	State            State
	Agents           []string
	LastMovedAt      time.Time
	MovedUnitsWindow int      // RateWindow sum of gift_sent units by edge agents for Item
	Stale            bool     // a source the edge rule read is zero or older than StaleData
	Detail           []string // stale sources and the last-known verdict of a stale edge

	srcs []source
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
	StaleSources                               []string // stale node ids in node order, then stale edges as "<from>-><to>"
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

// source is one input a verdict read and when it was captured. A zero time
// means never captured.
type source struct {
	name string
	at   time.Time
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

// SellItem is the item a sell node is judged on: the line's output item when
// the node carries it (or carries nothing), else the node's first item.
func SellItem(n Node, outputItem string) string {
	if slices.Contains(n.Items, outputItem) || len(n.Items) == 0 {
		return outputItem
	}

	return n.Items[0]
}

// evaluator carries one Evaluate call's inputs and the line graph.
type evaluator struct {
	line  Line
	in    Inputs
	nodes map[string]Node
}

// Evaluate classifies every node and edge of line and computes its rates.
//
// Every rule records the sources it read: the action-log cursors of the
// agents whose logs carry its events, the capture time of every tank level,
// and a sell node's exchange-row time. The oldest source decides: a node or
// edge with any source zero or older than StaleData is unknown, carries only
// AlertDataStale, and keeps its rule verdict as "last known" detail.
// Heartbeats (Live.LastSeen) are never a node source.
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
	var staleEdges []string
	for _, e := range line.Edges {
		er, haulerStarved := ev.evalEdge(e)
		if ev.applyStaleEdge(&er) {
			staleEdges = append(staleEdges, e.From+"->"+e.To)
		} else if haulerStarved {
			addAlert(&r.Nodes[index[e.From]], AlertHaulerStarved)
		}
		r.Edges = append(r.Edges, er)
	}
	for i := range r.Nodes {
		if ev.applyStale(&r.Nodes[i]) {
			r.StaleSources = append(r.StaleSources, r.Nodes[i].ID)
		}
	}
	r.StaleSources = append(r.StaleSources, staleEdges...)
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
		Units:          map[string]float64{},
		AssignedAgents: len(n.Agents),
	}
	nr.Tanks = ev.useTanks(&nr.srcs, n.ID)
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
// figures and is never flagged broke or stranded (no data). Broke and
// stranded are raised only for a live (fresh last_seen) agent that is neither
// removed nor seconded away; stranded also needs a known MaxFuel.
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
	if ar.Live && !ar.Removed && ar.AwayFleet == "" {
		ar.Broke = l.Credits < float64(ev.in.Cfg.BrokeCredits)
		ar.Stranded = !l.Docked && l.MaxFuel > 0 && ar.FuelPct < float64(ev.in.Cfg.FuelFloorPct)
	}

	return ar
}

// evalMine: drops are gift_received rows logged by the node's holder agents
// whose Sender is an attached agent, for a node item. The holders' cursors are
// the event sources; the miners' logs are never read.
func (ev *evaluator) evalMine(n Node, nr *NodeReport) {
	holders := holderAgents(n)
	ref := ev.useCursors(&nr.srcs, holders)
	lastDrop := ev.newest(func(e Event) bool {
		return e.Type == EventGiftReceived && slices.Contains(holders, e.AgentID) &&
			slices.Contains(n.Agents, e.Sender) && slices.Contains(n.Items, e.Item)
	})
	nr.LastEventAt = lastDrop
	switch {
	case nr.LiveAgents == 0:
		if !ev.anyLiveRecord(n.Agents) {
			// No status data for any miner: unknown, not "no bots".
			nr.srcs = append(nr.srcs, source{name: "status files (attached agents)"})
		}
		setState(nr, StateStarved, AlertNoBotsLive)
		for _, a := range nr.Agents {
			if a.AwayFleet != "" {
				nr.Detail = append(nr.Detail, fmt.Sprintf("%s: %s in %s", a.AgentID, a.Phase, a.AwayFleet))
			}
		}
	case ev.seen(lastDrop, ref, ev.in.Cfg.MineWindow):
		nr.State = StateOptimal
	default:
		setState(nr, StateStarved, AlertSourceDepleted)
	}
	if ev.anyChoked(nr) && !ev.haulMoved(&nr.srcs, ev.outgoing(n.ID, EdgeHaul)) {
		nr.State = StateChoked
	}
}

// evalPurchase: a hauler lot in the tank, or a buy within RateWindow, is optimal.
func (ev *evaluator) evalPurchase(n Node, nr *NodeReport) {
	ref := ev.useCursors(&nr.srcs, n.Agents)
	lastBuy := ev.newest(func(e Event) bool {
		return e.Type == EventExchangeFill && e.Role == "buyer" &&
			slices.Contains(n.Items, e.Item) && slices.Contains(n.Agents, e.AgentID)
	})
	nr.LastEventAt = lastBuy
	if ev.anyChoked(nr) || ev.seen(lastBuy, ref, ev.in.Cfg.RateWindow) {
		nr.State = StateOptimal

		return
	}
	nr.State = StateStarved
	nr.Detail = append(nr.Detail, "no purchase in "+fmtDur(ev.in.Cfg.RateWindow))
}

// evalStore: empty is starved; a full item with no outflow in HaulWindow is
// choked. The outflow agents' cursors are the event sources.
func (ev *evaluator) evalStore(n Node, nr *NodeReport) {
	ref := ev.useCursors(&nr.srcs, ev.storeOutflowAgents(n.ID))
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
	case ev.anyChoked(nr) && !ev.seen(outflow, ref, ev.in.Cfg.HaulWindow):
		nr.State = StateChoked
	default:
		nr.State = StateOptimal
	}
}

// evalProduce: inputs below one batch starve the still; inputs present with no
// craft in CraftWindow is choked; an undrawn full output tank is choked. The
// upstream and downstream stores' tanks are sources too.
func (ev *evaluator) evalProduce(n Node, nr *NodeReport) {
	ref := ev.useCursors(&nr.srcs, n.Agents)
	lastCraft := ev.newest(craftedBy(ev.nodes[n.ID]))
	nr.LastEventAt = lastCraft
	upstream := ev.useTanks(&nr.srcs, ev.from(n.ID))
	downstream := ev.useTanks(&nr.srcs, ev.to(n.ID))
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
	case ev.seen(lastCraft, ref, ev.in.Cfg.CraftWindow):
		nr.State = StateOptimal
	default:
		nr.State = StateChoked
		nr.Detail = append(nr.Detail, "inputs present, no craft in "+fmtDur(ev.in.Cfg.CraftWindow))
	}
	out := ev.line.OutputItem
	lastDraw := ev.newest(func(e Event) bool {
		return e.Type == EventWithdraw && e.Item == out && slices.Contains(n.Agents, e.AgentID)
	})
	if SumUnits(downstream, out) >= float64(ev.in.Cfg.ChokeUnits) &&
		!ev.seen(lastDraw, ref, ev.in.Cfg.HaulWindow) {
		setState(nr, StateChoked, AlertOutputNotDrawn)
	}
}

// evalSell: see the brief's ordered rules. The floor comes from the first
// attached agent with a shopkeep rule for the item; with no rule the floor is
// 0, so an ask is only "at the floor" when its price is <= 0 (in practice
// never). An ask with an unknown PostedAt is never stale. An ask seen on the
// exchange is a source at its SeenAt; an ask known only from shopkeep.json
// (zero SeenAt) is the worker's own current state and adds no source.
func (ev *evaluator) evalSell(n Node, nr *NodeReport) {
	ref := ev.useCursors(&nr.srcs, n.Agents)
	item := SellItem(n, ev.line.OutputItem)
	if d, ok := ev.in.Demand[n.ID]; ok {
		nr.Demand = &d
	}
	ask := ev.in.Asks[n.ID]
	nr.Ask = ask
	if ask != nil && !ask.SeenAt.IsZero() {
		nr.srcs = append(nr.srcs, source{name: "our ask on the exchange", at: ask.SeenAt})
	}
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
	case ev.seen(lastFill, ref, ev.in.Cfg.SellWindow):
		nr.State = StateOptimal
	case ask != nil && !stale:
		nr.State = StateOptimal
	default:
		nr.State = StateStarved
	}
}

// evalEdge classifies a haul/consign edge from its agents' gift_sent rows and
// the from node's tanks. haulerStarved reports that goods wait on the from
// node, which then carries AlertHaulerStarved unless the edge is stale. Craft
// edges get their state later from the produce node.
func (ev *evaluator) evalEdge(e Edge) (EdgeReport, bool) {
	er := EdgeReport{From: e.From, To: e.To, Kind: e.Kind, Item: e.Item, Label: e.Label, Agents: e.Agents}
	if e.Kind == EdgeCraft {
		return er, false
	}
	ref := ev.useCursors(&er.srcs, e.Agents)
	from := ev.useTanks(&er.srcs, e.From)
	moved := movedBy(e)
	er.LastMovedAt = ev.newest(moved)
	for _, x := range ev.in.Events {
		if moved(x) && ev.within(x.At, ev.in.Cfg.RateWindow) {
			er.MovedUnitsWindow += x.Units
		}
	}
	switch {
	case ev.seen(er.LastMovedAt, ref, ev.in.Cfg.HaulWindow):
		er.State = StateOptimal
	case SumUnits(from, e.Item) >= float64(ev.in.Cfg.SellLot):
		er.State = StateChoked

		return er, true
	default:
		er.State = StateStarved
	}

	return er, false
}

// applyStale greys a node when any source its rule read is zero or older than
// StaleData. The alerts become exactly [AlertDataStale]; the stale sources,
// the rule's state, alerts and detail move into Detail.
func (ev *evaluator) applyStale(nr *NodeReport) bool {
	stale := ev.staleSources(nr.srcs)
	if len(stale) == 0 {
		return false
	}
	detail := append(stale, lastKnown+string(nr.State))
	for _, a := range nr.Alerts {
		detail = append(detail, lastKnown+string(a))
	}
	for _, d := range nr.Detail {
		detail = append(detail, lastKnown+d)
	}
	nr.Stale, nr.State, nr.Alerts, nr.Detail = true, StateUnknown, []Alert{AlertDataStale}, detail

	return true
}

// applyStaleEdge greys an edge whose sources are stale, as applyStale does.
func (ev *evaluator) applyStaleEdge(er *EdgeReport) bool {
	stale := ev.staleSources(er.srcs)
	if len(stale) == 0 {
		return false
	}
	er.Stale, er.Detail = true, append(stale, lastKnown+string(er.State))
	er.State = StateUnknown

	return true
}

// staleSources returns one "stale: <source> <age>" line per distinct source
// name that is zero or older than StaleData, judged by its oldest time.
func (ev *evaluator) staleSources(srcs []source) []string {
	oldest := map[string]time.Time{}
	var names []string
	for _, s := range srcs {
		t, seen := oldest[s.name]
		if !seen {
			names = append(names, s.name)
		}
		if !seen || s.at.Before(t) {
			oldest[s.name] = s.at
		}
	}
	var out []string
	for _, name := range names {
		t := oldest[name]
		switch {
		case t.IsZero():
			out = append(out, "stale: "+name+" never captured")
		case !ev.within(t, ev.in.Cfg.StaleData):
			out = append(out, "stale: "+name+" "+fmtDur(ev.in.Now.Sub(t).Round(time.Minute))+" old")
		}
	}

	return out
}

// useCursors records each agent's action-log cursor as a source and returns
// the reference time of the rule's absence windows: the oldest set cursor,
// capped at Now, so a window never reaches past what the logs can know. With
// no set cursor the reference is Now (the node is stale anyway when a cursor
// is missing).
func (ev *evaluator) useCursors(srcs *[]source, agents []string) time.Time {
	ref := ev.in.Now
	for _, a := range agents {
		at := ev.in.Cursors[a]
		*srcs = append(*srcs, source{name: a + " action log", at: at})
		if !at.IsZero() && at.Before(ref) {
			ref = at
		}
	}

	return ref
}

// useTanks returns a node's tank levels and records every level's capture
// time as a source. A node with holders but no levels (assets.db unreadable)
// records one never-captured source.
func (ev *evaluator) useTanks(srcs *[]source, nodeID string) []TankLevel {
	levels := ev.in.Tanks[nodeID]
	if len(levels) == 0 && len(ev.nodes[nodeID].Holders) > 0 {
		*srcs = append(*srcs, source{name: nodeID + " storage"})
	}
	for _, l := range levels {
		*srcs = append(*srcs, source{name: holderName(l.Holder) + " storage", at: l.CapturedAt})
	}

	return levels
}

func holderName(h Holder) string {
	if h.Agent != "" {
		return h.Agent + "@" + h.Station
	}

	return h.Faction + "@" + h.Station
}

// anyLiveRecord reports any of agents has a status record at all.
func (ev *evaluator) anyLiveRecord(agents []string) bool {
	for _, a := range agents {
		if _, ok := ev.in.Live[a]; ok {
			return true
		}
	}

	return false
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

// within reports t is set, not after Now, and no older than w. Rates and
// heartbeats use it; absence verdicts use seen.
func (ev *evaluator) within(t time.Time, w time.Duration) bool {
	return !t.IsZero() && !t.After(ev.in.Now) && ev.in.Now.Sub(t) <= w
}

// seen reports an event at t falls in the absence window ending at ref (the
// oldest event-source cursor): t is set, not after Now, and not before ref-w.
func (ev *evaluator) seen(t, ref time.Time, w time.Duration) bool {
	return !t.IsZero() && !t.After(ev.in.Now) && !t.Before(ref.Add(-w))
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

// haulMoved reports any of the edges moved within HaulWindow, recording the
// edge agents' cursors as sources of the caller's verdict.
func (ev *evaluator) haulMoved(srcs *[]source, edges []Edge) bool {
	moved := false
	for _, e := range edges {
		ref := ev.useCursors(srcs, e.Agents)
		if ev.seen(ev.newest(movedBy(e)), ref, ev.in.Cfg.HaulWindow) {
			moved = true
		}
	}

	return moved
}

// storeOutflow: completed crafts of the produce node a store feeds, else
// gift_sent on the store's outgoing haul/consign edges.
func (ev *evaluator) storeOutflow(nodeID string) func(Event) bool {
	if craft := ev.outgoing(nodeID, EdgeCraft); len(craft) > 0 {
		return craftedBy(ev.nodes[craft[0].To])
	}

	return anyMoved(ev.storeOutEdges(nodeID))
}

// storeOutflowAgents are the agents whose logs carry a store's outflow: the
// fed produce node's agents, else the outgoing haul/consign edges' agents.
func (ev *evaluator) storeOutflowAgents(nodeID string) []string {
	if craft := ev.outgoing(nodeID, EdgeCraft); len(craft) > 0 {
		return ev.nodes[craft[0].To].Agents
	}
	var agents []string
	for _, e := range ev.storeOutEdges(nodeID) {
		agents = append(agents, e.Agents...)
	}

	return agents
}

func (ev *evaluator) storeOutEdges(nodeID string) []Edge {
	return append(ev.outgoing(nodeID, EdgeHaul), ev.outgoing(nodeID, EdgeConsign)...)
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
