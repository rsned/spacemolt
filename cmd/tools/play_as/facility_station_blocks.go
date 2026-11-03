package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// stationBlocks are the station-wide blocks of a `facility list` response that
// sit beside the facility arrays: the power grid, life support, the shared
// personnel/medical/marine pools, and the station's own build queue. Each is
// omitted by the server when it does not apply.
type stationBlocks struct {
	Power        *serverapi.StationPower        `json:"power"`
	LifeSupport  *serverapi.LifeSupport         `json:"life_support"`
	ServicePools *serverapi.StationServicePools `json:"service_pools"`
	Construction *serverapi.StationConstruction `json:"construction"`
}

// renderStationBlocks appends the station-wide blocks of a `facility list`
// response to b. Shortfalls are marked ⚠ and the server's remediation advice
// follows as 💡, so what to deliver is readable without the raw JSON. A
// response carrying none of the blocks adds nothing.
func renderStationBlocks(b *strings.Builder, raw []byte) {
	var s stationBlocks
	if err := json.Unmarshal(raw, &s); err != nil {
		return
	}
	pools := servicePoolRows(s.ServicePools)
	if s.Power != nil || s.LifeSupport != nil || len(pools) > 0 {
		b.WriteString("\n  Station status:\n")
		renderPower(b, s.Power)
		renderLifeSupport(b, s.LifeSupport)
		renderServicePools(b, pools)
	}
	renderConstruction(b, s.Construction)
}

func renderPower(b *strings.Builder, p *serverapi.StationPower) {
	if p == nil {
		return
	}
	fmt.Fprintf(b, "    Power: supply %d, draw %d", p.Supply, p.CurrentDraw)
	if p.BatteryCapacity > 0 {
		fmt.Fprintf(b, ", battery %d/%d", p.BatteryStored, p.BatteryCapacity)
	}
	fmt.Fprintf(b, ", efficiency %.0f%%", p.Efficiency*100)
	if p.Efficiency < 1 {
		b.WriteString("  ⚠ throttled: production and recyclers here run slower")
	}
	b.WriteString("\n")
	if len(p.FuelInputs) > 0 {
		fmt.Fprintf(b, "      Fuel / cycle: %s\n", itemsPerCycle(p.FuelInputs))
	}
	if p.Remediation != "" {
		fmt.Fprintf(b, "      💡 %s\n", p.Remediation)
	}
}

func renderLifeSupport(b *strings.Builder, ls *serverapi.LifeSupport) {
	if ls == nil {
		return
	}
	plants := "plants"
	if ls.Plants == 1 {
		plants = "plant"
	}
	fmt.Fprintf(b, "    Life support: supply %d / demand %d, %d %s", ls.Supply, ls.Demand, ls.Plants, plants)
	if ls.Demand > ls.Supply {
		fmt.Fprintf(b, "  ⚠ short %d", ls.Demand-ls.Supply)
	}
	b.WriteString("\n")
	if len(ls.Maintenance) > 0 {
		cycle := "cycle"
		if ls.MaintenanceCycleTick > 0 {
			cycle = fmt.Sprintf("%d ticks", ls.MaintenanceCycleTick)
		}
		fmt.Fprintf(b, "      Upkeep / %s: %s\n", cycle, itemsPerCycle(ls.Maintenance))
	}
	if len(ls.Starved) > 0 {
		fmt.Fprintf(b, "      ⚠ Starved: %s\n", itemsPerCycle(ls.Starved))
	}
	if ls.Remediation != "" {
		fmt.Fprintf(b, "      💡 %s\n", ls.Remediation)
	}
}

// servicePoolRow pairs a pool with its display label. A pool the station has
// no capacity for is omitted by the server and decodes to zero capacity.
type servicePoolRow struct {
	label string
	pool  serverapi.StationServicePoolStatus
}

func servicePoolRows(sp *serverapi.StationServicePools) []servicePoolRow {
	if sp == nil {
		return nil
	}
	var rows []servicePoolRow
	for _, r := range []servicePoolRow{
		{"Personnel", sp.Personnel},
		{"Medical", sp.Medical},
		{"Marine training", sp.MarineTraining},
	} {
		if r.pool.Capacity > 0 {
			rows = append(rows, r)
		}
	}
	return rows
}

func renderServicePools(b *strings.Builder, rows []servicePoolRow) {
	if len(rows) == 0 {
		return
	}
	b.WriteString("    Service pools:\n")
	for _, r := range rows {
		p := r.pool
		fmt.Fprintf(b, "      %-15s %5d/%-5d refill %d/cycle", r.label, p.Remaining, p.Capacity, p.RefillPerCycle)
		if p.SupplyItem != "" {
			fmt.Fprintf(b, ", supply %s", p.SupplyItem)
			if p.NextCycleSupplyRequired > 0 {
				fmt.Fprintf(b, " (next %d)", p.NextCycleSupplyRequired)
			}
		}
		if p.CrewTreatmentPrice > 0 || p.MarineTreatmentPrice > 0 {
			fmt.Fprintf(b, ", treatment crew %d / marine %d", p.CrewTreatmentPrice, p.MarineTreatmentPrice)
		}
		if p.Remaining == 0 {
			b.WriteString("  ⚠ exhausted")
		}
		b.WriteString("\n")
	}
}

func renderConstruction(b *strings.Builder, c *serverapi.StationConstruction) {
	if c == nil || len(c.UnderConstruction)+len(c.Pending) == 0 {
		return
	}
	b.WriteString("\n  Station construction:\n")
	for _, e := range c.UnderConstruction {
		renderConstructionEntry(b, e)
	}
	for _, e := range c.Pending {
		renderConstructionEntry(b, e)
	}
}

func renderConstructionEntry(b *strings.Builder, e serverapi.StationConstructionEntry) {
	name := e.Name
	if name == "" {
		name = e.DefinitionID
	}
	fmt.Fprintf(b, "    %s", name)
	if e.Category != "" {
		fmt.Fprintf(b, " (%s)", e.Category)
	}
	if e.Status != "" {
		fmt.Fprintf(b, " — %s", e.Status)
	}
	if e.TicksUntilComplete > 0 {
		fmt.Fprintf(b, ", %d ticks left", e.TicksUntilComplete)
	}
	if e.BuildCost > 0 {
		fmt.Fprintf(b, ", build %s cr", formatCredits(float64(e.BuildCost)))
	}
	b.WriteString("\n")
	// Only shortfalls are listed: a fully stocked material needs no action,
	// and a long bill of satisfied lines would bury the ones that do.
	for _, m := range e.Materials {
		missing := m.QuantityMissing
		if missing == 0 && m.QuantityInStorage < m.QuantityRequired {
			missing = m.QuantityRequired - m.QuantityInStorage
		}
		if missing <= 0 {
			continue
		}
		label := m.Name
		if label == "" {
			label = m.ItemID
		}
		fmt.Fprintf(b, "      ⚠ %s: missing %d (%d/%d in storage)\n", label, missing, m.QuantityInStorage, m.QuantityRequired)
	}
}

// itemsPerCycle renders "Name qty, Name qty" for a per-cycle item list,
// falling back to the item id when the server sends no display name.
func itemsPerCycle(items []serverapi.LifeSupportMaintenance) string {
	parts := make([]string, 0, len(items))
	for _, it := range items {
		label := it.Name
		if label == "" {
			label = it.ItemID
		}
		parts = append(parts, fmt.Sprintf("%s %d", label, it.QuantityPerCycle))
	}
	return strings.Join(parts, ", ")
}
