package worker

import (
	"context"

	"github.com/rsned/spacemolt/pkg/huntdecide"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/navigation"
	"github.com/rsned/spacemolt/pkg/reload"
)

// huntJobKind names the job a decision is made under: "home" on the home
// grounds, "need" with the need's mission id, otherwise "mission" with the
// active instance id (or the board id when there is none).
func huntJobKind(job huntJob) (kind, id string) {
	switch {
	case job.home:
		return "home", "home grounds"
	case job.need != nil:
		return "need", job.need.MissionID
	}
	if job.activeID != "" {
		return "mission", job.activeID
	}
	return "mission", job.boardID
}

// huntDecisionState builds the compact state a decider sees, field by field.
// It never copies game.State wholesale: that struct carries the account
// password. lo supplies the weapons and the ammo aboard, so no second get_ship
// is made; the knowledge base supplies weapon reach, cooldown and damage type
// and the jump distances (-1 when unknown or when deps.KB is nil).
func huntDecisionState(ctx context.Context, deps HuntDeps, job huntJob, s *huntdecide.Sortie, lo reload.Loadout) huntdecide.State {
	st := deps.Client.GetState()
	out := huntdecide.State{
		AgentID:        deps.AgentID,
		PoliceLevel:    -1,
		JumpsToStation: -1,
		JumpsToHome:    -1,
		Weapons:        []huntdecide.Weapon{},
		Ammo:           []huntdecide.Ammo{},
		// GameVersion is the server's version only (set below; empty while
		// unknown). The worker's own build is WorkerCommit.
		WorkerCommit: deps.Commit,
	}
	if st != nil {
		out.ShipClass = st.Ship.ClassID
		out.Hull, out.MaxHull = st.Ship.Hull, st.Ship.MaxHull
		out.Shield, out.MaxShield = st.Ship.Shield, st.Ship.MaxShield
		out.Fuel, out.MaxFuel = st.Ship.Fuel, st.Ship.MaxFuel
		out.FreeHold = int(st.Ship.CargoCapacity - st.Ship.CargoUsed)
		out.Credits = st.Credits
		out.System = st.System.ID
		if out.System == "" {
			out.System = st.CurrentSystem
		}
		out.POI = st.CurrentPOI
		out.Docked = st.Doc
		if st.System.ID != "" {
			out.PoliceLevel = st.System.PoliceLevel
		}
		out.Tick = st.CurrentTick
		out.GameVersion = st.ServerVersion
	}

	out.Weapons, out.Ammo = huntDecisionWeapons(ctx, deps.KB, lo)
	out.Job = huntDecisionJob(job)
	if s != nil {
		out.Sortie = huntdecide.SortieSoFar{
			TicksOut:     out.Tick - s.StartTick,
			Grounds:      append([]huntdecide.GroundVisit{}, s.Grounds...),
			Fights:       s.Fights,
			Kills:        s.Kills,
			ReloadsSpent: s.ReloadsSpent,
			LastEngaged:  s.LastEngaged,
		}
	}
	if out.Sortie.Grounds == nil {
		out.Sortie.Grounds = []huntdecide.GroundVisit{}
	}
	out.JumpsToStation, out.JumpsToHome = huntDecisionJumps(ctx, deps, out.System)
	return out
}

// huntDecisionJob projects the job.
func huntDecisionJob(job huntJob) huntdecide.Job {
	kind, id := huntJobKind(job)
	j := huntdecide.Job{Kind: kind, ID: id, Species: job.required, Target: job.target, Done: job.baseline}
	if job.need != nil {
		if len(job.need.Species) > 0 {
			j.Species = job.need.Species[0]
		}
		j.Target = job.need.Quantity
	}
	return j
}

// huntDecisionWeapons projects the loadout into weapons and the ammo stacks
// that feed them. Reach, cooldown and damage type come from the catalog's
// module weapon block (knowledge.ItemWeapon); missing data stays zero.
func huntDecisionWeapons(ctx context.Context, kb knowledge.Base, lo reload.Loadout) ([]huntdecide.Weapon, []huntdecide.Ammo) {
	weapons := []huntdecide.Weapon{}
	ammo := []huntdecide.Ammo{}
	seen := map[string]bool{}

	fill := func(w *huntdecide.Weapon) {
		if kb == nil || w.TypeID == "" {
			return
		}
		item, err := kb.GetItem(ctx, w.TypeID)
		if err != nil || item == nil || item.Module == nil || item.Module.Weapon == nil {
			return
		}
		iw := item.Module.Weapon
		w.DamageType = iw.DamageType
		w.Cooldown = iw.Cooldown
		if iw.Reach != nil {
			w.Reach = *iw.Reach
		}
	}

	for _, e := range lo.Plan {
		w := huntdecide.Weapon{
			ModuleID: e.WeaponID, TypeID: e.WeaponTypeID, AmmoType: e.AmmoType,
			MagazineSize: e.MagazineSize, Loaded: e.CurrentAmmo, LoadedAmmoID: e.LoadedAmmoID,
		}
		fill(&w)
		weapons = append(weapons, w)
		for _, c := range e.Candidates {
			key := e.AmmoType + "\x00" + c.ItemID
			if seen[key] {
				continue
			}
			seen[key] = true
			ammo = append(ammo, huntdecide.Ammo{AmmoType: e.AmmoType, ItemID: c.ItemID, Aboard: int(c.Quantity)})
		}
	}
	for _, f := range lo.Free {
		w := huntdecide.Weapon{ModuleID: f.ModuleID, TypeID: f.TypeID}
		fill(&w)
		weapons = append(weapons, w)
	}
	return weapons, ammo
}

// huntDecisionJumps returns the jumps from system `from` to the nearest
// repair station and to the home station, over the policed graph; -1 when
// unknown or when there is no knowledge base.
func huntDecisionJumps(ctx context.Context, deps HuntDeps, from string) (toStation, toHome int) {
	if deps.KB == nil || from == "" {
		return -1, -1
	}
	systems, err := deps.KB.GetSystems(ctx)
	if err != nil {
		return -1, -1
	}
	conns, err := deps.KB.GetConnections(ctx)
	if err != nil {
		return -1, -1
	}
	homeSys := huntHomeStationSystem(ctx, deps.KB, deps.HomeStation, systems)
	graph := huntPolicedGraph(conns, systems, from, homeSys == huntHomeSystem, huntPoliceFloor(deps))

	var targets []string
	stationSys, _ := huntNearestRepairStation(ctx, deps.KB, from, "", huntPoliceFloor(deps))
	if stationSys != "" {
		targets = append(targets, stationSys)
	}
	if homeSys != "" {
		targets = append(targets, homeSys)
	}
	if len(targets) == 0 {
		return -1, -1
	}
	dist := navigation.BFSJumps(graph, from, targets)
	jumps := func(sys string) int {
		if sys == "" {
			return -1
		}
		if d, ok := dist[sys]; ok && d < navigation.RouteInf {
			return d
		}
		return -1
	}
	return jumps(stationSys), jumps(homeSys)
}

// huntStationPOI maps a station id that may be a base id to its POI id; an id
// the KB has no base for is returned as is (it may already be a POI id).
func huntStationPOI(ctx context.Context, kb knowledge.Base, station string) string {
	if base, err := kb.GetBase(ctx, station); err == nil && base != nil && base.POIID != "" {
		return base.POIID
	}
	return station
}

// huntHomeStationSystem resolves the home station (a base id or a POI id) to
// the system that holds its POI; empty when it is unset or unknown.
func huntHomeStationSystem(ctx context.Context, kb knowledge.Base, station string, systems []knowledge.System) string {
	if station == "" {
		return ""
	}
	poiID := huntStationPOI(ctx, kb, station)
	for _, sys := range systems {
		pois, err := kb.GetPOIs(ctx, sys.ID)
		if err != nil {
			continue
		}
		for _, p := range pois {
			if p.ID == poiID {
				return sys.ID
			}
		}
	}
	return ""
}
