package linedash

import (
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
)

// ticksPerDay is one game day (operator-confirmed, 2026-10-31).
const ticksPerDay = 8640.0

// Category, kind and purpose labels the board groups and tags by.
const (
	CategoryUpkeep = "Station upkeep"
	CategorySales  = "Market/Sales"
	categoryOther  = "Other"

	KindHand     = "hand"
	KindFacility = "facility"
	KindSales    = "sales"

	PurposeUpkeep  = "upkeep"
	PurposeForSale = "for_sale"
	PurposeFeeds   = "feeds"

	noteNoSupply = "supply not on record"
)

// Listing is one of our standing asks at a station.
type Listing struct {
	StationID, Item, Agent string
}

// DeriveInputs is everything Derive reads; it does no I/O.
type DeriveInputs struct {
	Roster      map[string]RosterEntry
	Schedules   map[string][]Task
	Ref         RefData
	Listings    []Listing
	FactionTag  string
	DeferFleets []string // fleets whose lines are deferred (the drone marketbots' ore conversion)
}

// delivery is one deliver task, optionally fed by a buy_directed of the same
// agent and item.
type delivery struct {
	agent, item, from string
	toBase            string
	keys              []string
	buy               *TaskSpec
}

// production is one (station, recipe) as the schedules describe it.
type production struct {
	st       Station
	recipe   Recipe
	out      string
	maxQty   int
	crafters []string
	keys     map[string][]string
}

type deriver struct {
	in          DeriveInputs
	prods       map[string]*production // base|recipe
	deliveries  []delivery
	deposits    map[string][]string // base|item -> agents
	depositKeys map[string][]string // agent -> keys
}

// Derive builds every line from the fleet's own configuration: craft tasks
// make production lines at the crafter's pinned station, deliver/buy tasks
// make the hauls and purchases that feed them, fleet-yaml consign/shopkeep/
// faction_pull rules and our asks make the sales side, and listings of goods
// not produced at a station make Market/Sales lines. Only pinned, live agents
// outside DeferFleets make lines; hauls from any live agent link them.
func Derive(in DeriveInputs) []Line {
	d := &deriver{in: in, prods: map[string]*production{}, deposits: map[string][]string{}, depositKeys: map[string][]string{}}
	d.scan()

	var lines []Line
	for _, key := range sortedMapKeys(d.prods) {
		lines = append(lines, d.productionLine(d.prods[key]))
	}
	lines = append(lines, d.salesLines()...)
	shareAndDepend(lines)
	sort.SliceStable(lines, func(i, j int) bool {
		a, b := lines[i], lines[j]
		if a.StationName != b.StationName {
			return a.StationName < b.StationName
		}
		if a.Category != b.Category {
			return a.Category < b.Category
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.ID < b.ID
	})

	return lines
}

// pinned resolves a live, pinned, non-deferred agent's station.
func (d *deriver) pinned(agent string) (Station, bool) {
	e, ok := d.in.Roster[agent]
	if !ok || e.Removed || e.Station == "" || slices.Contains(d.in.DeferFleets, e.Fleet) {
		return Station{}, false
	}
	return d.in.Ref.Station(e.Station)
}

func (d *deriver) live(agent string) bool {
	e, ok := d.in.Roster[agent]
	return !ok || !e.Removed
}

func (d *deriver) scan() {
	buys := map[string]TaskSpec{}
	buyKeys := map[string]string{}
	for _, agent := range sortedMapKeys(d.in.Schedules) {
		if !d.live(agent) {
			continue
		}
		st, isPinned := d.pinned(agent)
		for _, t := range d.in.Schedules[agent] {
			spec, ok := ParseTask(t.Command)
			if !ok {
				continue
			}
			key := fmt.Sprintf("task:%d", t.ID)
			switch spec.Verb {
			case "craft":
				if isPinned {
					d.addCraft(st, agent, spec, key)
				}
			case "faction_deposit":
				if isPinned && !forbidden(spec.Item) {
					k := st.BaseID + "|" + spec.Item
					d.deposits[k] = appendUnique(d.deposits[k], agent)
					d.depositKeys[agent] = appendUnique(d.depositKeys[agent], key)
				}
			case "buy_directed":
				buys[agent+"|"+spec.Item] = spec
				buyKeys[agent+"|"+spec.Item] = key
			case "deliver":
				to, ok := d.in.Ref.Station(spec.To)
				if !ok || forbidden(spec.Item) {
					continue
				}
				d.deliveries = append(d.deliveries, delivery{agent: agent, item: spec.Item, from: spec.From, toBase: to.BaseID, keys: []string{key}})
			}
		}
	}
	for i := range d.deliveries {
		dl := &d.deliveries[i]
		if b, ok := buys[dl.agent+"|"+dl.item]; ok {
			dl.buy = &b
			dl.keys = append(dl.keys, buyKeys[dl.agent+"|"+dl.item])
		}
	}
}

func (d *deriver) addCraft(st Station, agent string, spec TaskSpec, key string) {
	rc, ok := d.in.Ref.Recipes[spec.Recipe]
	if !ok || len(rc.Outputs) == 0 {
		return
	}
	for item := range rc.Inputs {
		if forbidden(item) {
			return
		}
	}
	for item := range rc.Outputs {
		if forbidden(item) {
			return
		}
	}
	k := st.BaseID + "|" + rc.ID
	p := d.prods[k]
	if p == nil {
		p = &production{st: st, recipe: rc, out: primaryOutput(rc), keys: map[string][]string{}}
		d.prods[k] = p
	}
	p.crafters = appendUnique(p.crafters, agent)
	p.maxQty = max(p.maxQty, spec.Qty)
	p.keys[agent] = appendUnique(p.keys[agent], key)
}

func (d *deriver) productionLine(p *production) Line {
	base := p.st.BaseID
	inputs := sortedMapKeys(p.recipe.Inputs)
	units := p.recipe.Outputs[p.out]
	l := Line{
		ID: base + ":" + p.recipe.ID, Name: d.itemName(p.out), OutputItem: p.out,
		StationID: base, StationName: p.st.Name, SystemName: p.st.SystemName,
		StationCondition: p.st.Condition, StationPct: p.st.SatisfactionPct,
		TaskKeys: map[string][]string{},
	}
	for a, ks := range p.keys {
		l.TaskKeys[a] = append(l.TaskKeys[a], ks...)
	}
	slices.Sort(p.crafters)
	tankHolders := func() []Holder {
		hs := []Holder{{Faction: d.in.FactionTag, Station: base}}
		for _, a := range p.crafters {
			hs = append(hs, Holder{Agent: a, Station: base})
		}
		return hs
	}
	inNode := Node{ID: "inputs", Type: NodeStore, Name: "Inputs", Items: inputs, Holders: tankHolders()}
	for _, item := range inputs {
		for _, a := range d.deposits[base+"|"+item] {
			inNode.Agents = appendUnique(inNode.Agents, a)
			l.TaskKeys[a] = appendUnique(l.TaskKeys[a], d.depositKeys[a]...)
		}
	}

	facility, isFacility := d.facilityFor(base, p.recipe.ID)
	prodName := "Station Workshop (hand craft)"
	l.Kind = KindHand
	if isFacility {
		prodName, l.Kind = facility.Type, KindFacility
	}
	prod := Node{
		ID: "produce", Type: NodeProduce, Name: prodName, Items: []string{p.out}, Agents: p.crafters,
		Station: base, Recipe: p.recipe.ID, UnitsPerRun: max(units, 1), InputsPerRun: p.recipe.Inputs,
		BatchRuns: max(1, int(math.Ceil(float64(p.maxQty)/float64(max(units, 1))))),
	}
	outNode := Node{ID: "output", Type: NodeStore, Name: "Output", Items: []string{p.out}, Holders: tankHolders()}

	var nodes []Node
	var edges []Edge
	nodes, edges = d.addSources(&l, nodes, edges, base, inputs, "inputs")
	nodes = append(nodes, inNode, prod)
	edges = append(edges, Edge{From: "inputs", To: "produce", Kind: EdgeCraft, Label: "craft"},
		Edge{From: "produce", To: "output", Kind: EdgeCraft, Label: "craft"})

	sold := false
	for _, a := range p.crafters {
		for _, rule := range d.in.Roster[a].Consign {
			parts := strings.Split(rule, ":")
			if len(parts) < 2 || parts[0] != p.out {
				continue
			}
			id := "consign:" + parts[1]
			if !slices.ContainsFunc(nodes, func(n Node) bool { return n.ID == id }) {
				nodes = append(nodes, Node{ID: id, Type: NodeStore, Name: "Consigned to " + parts[1], Items: []string{p.out},
					Holders: []Holder{{Agent: parts[1], Station: base}}})
			}
			edges = append(edges, Edge{From: "output", To: id, Kind: EdgeConsign, Item: p.out, Agents: []string{a}, Label: "consign"})
			l.TaskKeys[a] = appendUnique(l.TaskKeys[a], "behaviour:consign")
			sold = true
		}
	}
	for _, a := range sortedMapKeys(d.in.Roster) {
		st, ok := d.pinned(a)
		if !ok || st.BaseID != base {
			continue
		}
		e := d.in.Roster[a]
		if slices.Contains(e.FactionPull, p.out) {
			outNode.Agents = appendUnique(outNode.Agents, a)
			l.TaskKeys[a] = appendUnique(l.TaskKeys[a], "behaviour:faction_pull")
		}
		if slices.ContainsFunc(e.Shopkeep, func(r string) bool { return strings.HasPrefix(r, p.out+":") }) {
			nodes = appendSell(nodes, a, base, p.out, d.in.FactionTag)
			l.TaskKeys[a] = appendUnique(l.TaskKeys[a], "behaviour:shopkeep")
			sold = true
		}
	}
	for _, li := range d.in.Listings {
		if st, ok := d.in.Ref.Station(li.StationID); ok && st.BaseID == base && li.Item == p.out {
			nodes = appendSell(nodes, li.Agent, base, p.out, d.in.FactionTag)
			sold = true
		}
	}
	nodes = append(nodes, outNode)
	for _, n := range nodes {
		for _, a := range append(slices.Clone(n.Agents), holderAgents(n)...) {
			if _, ok := d.pinned(a); ok {
				l.TaskKeys[a] = appendUnique(l.TaskKeys[a], "idle:ensure_home")
			}
		}
	}
	l.Nodes, l.Edges = nodes, edges

	l.CapacityPerDay = capacityPerDay(p.recipe, units, facility, isFacility, d.in.Ref.Facilities[base])
	switch {
	case d.isUpkeep(base, p.out):
		l.Purpose, l.Category = PurposeUpkeep, CategoryUpkeep
	case sold:
		l.Purpose, l.Category = PurposeForSale, d.category(p.out)
	default:
		l.Purpose, l.Category = PurposeFeeds, d.category(p.out)
	}

	return l
}

// addSources adds a purchase or store node per delivery of one of items to
// base, each with a haul edge into target.
func (d *deriver) addSources(l *Line, nodes []Node, edges []Edge, base string, items []string, target string) ([]Node, []Edge) {
	for _, dl := range d.deliveries {
		if dl.toBase != base || !slices.Contains(items, dl.item) {
			continue
		}
		fromName, fromID := dl.from, dl.from
		if st, ok := d.in.Ref.Station(dl.from); ok {
			fromName, fromID = st.Name, st.BaseID
		}
		var n Node
		if dl.buy != nil {
			n = Node{ID: "buy:" + dl.item + ":" + dl.agent, Type: NodePurchase, Name: d.itemName(dl.item) + " purchase at " + fromName,
				Items: []string{dl.item}, Agents: []string{dl.agent}, Station: fromID, MaxPrice: dl.buy.MaxPrice,
				Holders: []Holder{{Agent: dl.agent, Station: fromID}}}
		} else {
			n = Node{ID: "src:" + dl.item + ":" + dl.agent, Type: NodeStore, Name: d.itemName(dl.item) + " from " + fromName,
				Items: []string{dl.item}, Agents: []string{dl.agent}}
		}
		if slices.ContainsFunc(nodes, func(x Node) bool { return x.ID == n.ID }) {
			continue
		}
		nodes = append(nodes, n)
		edges = append(edges, Edge{From: n.ID, To: target, Kind: EdgeHaul, Item: dl.item, Agents: []string{dl.agent}, Label: "haul"})
		l.TaskKeys[dl.agent] = appendUnique(l.TaskKeys[dl.agent], dl.keys...)
	}

	return nodes, edges
}

// salesLines makes a Market/Sales line per (station, item) we list but do not
// produce there.
func (d *deriver) salesLines() []Line {
	type sale struct {
		st     Station
		item   string
		agents []string
	}
	sales := map[string]*sale{}
	for _, li := range d.in.Listings {
		st, ok := d.in.Ref.Station(li.StationID)
		if !ok || forbidden(li.Item) {
			continue
		}
		produced := false
		for _, p := range d.prods {
			if p.st.BaseID == st.BaseID && p.out == li.Item {
				produced = true
			}
		}
		if produced {
			continue
		}
		k := st.BaseID + "|" + li.Item
		if sales[k] == nil {
			sales[k] = &sale{st: st, item: li.Item}
		}
		if li.Agent != "" {
			sales[k].agents = appendUnique(sales[k].agents, li.Agent)
		}
	}
	var lines []Line
	for _, k := range sortedMapKeys(sales) {
		s := sales[k]
		base := s.st.BaseID
		l := Line{
			ID: base + ":sell:" + s.item, Name: d.itemName(s.item), OutputItem: s.item, CapacityPerDay: 1,
			StationID: base, StationName: s.st.Name, SystemName: s.st.SystemName,
			StationCondition: s.st.Condition, StationPct: s.st.SatisfactionPct,
			Kind: KindSales, Purpose: PurposeForSale, Category: CategorySales, TaskKeys: map[string][]string{},
		}
		slices.Sort(s.agents)
		sell := Node{ID: "sell", Type: NodeSell, Name: "Sell at " + s.st.Name, Items: []string{s.item}, Agents: s.agents,
			Station: base, Holders: []Holder{{Faction: d.in.FactionTag, Station: base}}}
		var nodes []Node
		var edges []Edge
		nodes, edges = d.addSources(&l, nodes, edges, base, []string{s.item}, "sell")
		if len(edges) == 0 {
			sell.Note = noteNoSupply
		}
		l.Nodes, l.Edges = append(nodes, sell), edges
		lines = append(lines, l)
	}

	return lines
}

// shareAndDepend fills each inputs node's ItemShares/SharedBy and each
// production line's DependedOn from the other lines at its station.
func shareAndDepend(lines []Line) {
	consumers := map[string][]int{} // base|item -> indexes of production lines consuming it
	for i, l := range lines {
		if l.Kind == KindSales {
			continue
		}
		for _, n := range l.Nodes {
			if n.Type == NodeProduce {
				for item := range n.InputsPerRun {
					k := l.StationID + "|" + item
					consumers[k] = append(consumers[k], i)
				}
			}
		}
	}
	for i := range lines {
		l := &lines[i]
		for ni := range l.Nodes {
			n := &l.Nodes[ni]
			if n.ID != "inputs" {
				continue
			}
			n.ItemShares = map[string]int{}
			for _, item := range n.Items {
				c := len(consumers[l.StationID+"|"+item])
				n.ItemShares[item] = c
				n.SharedBy = max(n.SharedBy, c)
			}
		}
		if l.Kind == KindSales {
			continue
		}
		for _, j := range consumers[l.StationID+"|"+l.OutputItem] {
			if j == i {
				continue
			}
			switch lines[j].Purpose {
			case PurposeUpkeep:
				l.DependedOn.Upkeep++
			case PurposeForSale:
				l.DependedOn.Sales++
			default:
				l.DependedOn.Production++
			}
		}
	}
}

func (d *deriver) facilityFor(base, recipe string) (Facility, bool) {
	for _, f := range d.in.Ref.Facilities[base] {
		if f.RecipeID == recipe {
			return f, true
		}
	}
	return Facility{}, false
}

func (d *deriver) isUpkeep(base, item string) bool {
	for _, f := range d.in.Ref.Facilities[base] {
		if d.in.Ref.Upkeep[f.Type][item] > 0 {
			return true
		}
	}
	return false
}

func (d *deriver) category(item string) string {
	c := d.in.Ref.ItemCategory[item]
	if c == "" {
		return categoryOther
	}
	return strings.ToUpper(c[:1]) + c[1:]
}

func (d *deriver) itemName(item string) string {
	if n := d.in.Ref.ItemName[item]; n != "" {
		return n
	}
	return item
}

// capacityPerDay is the line's output at 100% utilisation. ticks/run is the
// recipe's crafting_time by hand, or crafting_time / (5 * 3^(level-1) *
// (1 + 0.15 * admin tier)) in a facility, where the admin tier is the level of
// a Faction Desk / Faction Office at the station.
func capacityPerDay(rc Recipe, units int, f Facility, isFacility bool, atBase []Facility) int {
	ticks := rc.CraftingTime
	if isFacility {
		tier := 0
		for _, x := range atBase {
			if x.Type == "Faction Desk" || x.Type == "Faction Office" {
				tier = max(tier, x.Level)
			}
		}
		ticks /= 5 * math.Pow(3, float64(max(f.Level, 1)-1)) * (1 + 0.15*float64(tier))
	}
	if ticks <= 0 {
		return 1
	}
	return max(1, int(ticksPerDay/ticks*float64(units)))
}

func primaryOutput(rc Recipe) string {
	best, bestQty := "", -1
	for _, item := range sortedMapKeys(rc.Outputs) {
		if rc.Outputs[item] > bestQty {
			best, bestQty = item, rc.Outputs[item]
		}
	}
	return best
}

// appendSell adds a sell node for agent's ask; an ask no pinned agent owns
// (a faction sell order) is the "market" node, held in faction storage.
func appendSell(nodes []Node, agent, base, item, factionTag string) []Node {
	n := Node{ID: "sell:" + agent, Type: NodeSell, Name: "Sell (" + agent + ")", Items: []string{item},
		Agents: []string{agent}, Station: base, Holders: []Holder{{Agent: agent, Station: base}, {Faction: factionTag, Station: base}}}
	if agent == "" {
		n.ID, n.Name, n.Agents, n.Holders = "sell:market", "Sell (faction order)", nil, []Holder{{Faction: factionTag, Station: base}}
	}
	if slices.ContainsFunc(nodes, func(x Node) bool { return x.ID == n.ID }) {
		return nodes
	}
	return append(nodes, n)
}

func forbidden(item string) bool { return slices.Contains(forbiddenItems, item) }

func appendUnique(s []string, vs ...string) []string {
	for _, v := range vs {
		if !slices.Contains(s, v) {
			s = append(s, v)
		}
	}
	return s
}

func sortedMapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
