package worker

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/huntdecide"
	"github.com/rsned/spacemolt/pkg/reload"
)

// A sortie is one job, dock to dock, across as many grounds as it takes
// (spec: "Sortie shape"). Its state lives in <agent dir>/hunt-sortie.json so
// it survives across Hunt passes and restarts; its start and end are written
// to the decision log. With no AgentID every function here is a no-op on disk
// and the in-memory *Sortie still works.

// huntAgentDir is <AgentsDir or DefaultAgentsDir>/<AgentID>; "" when AgentID
// is empty.
func huntAgentDir(deps HuntDeps) string {
	if deps.AgentID == "" {
		return ""
	}
	return filepath.Join(cmp.Or(deps.AgentsDir, DefaultAgentsDir), deps.AgentID)
}

// huntWho is the name a hunt log line carries.
func huntWho(deps HuntDeps) string {
	return cmp.Or(deps.AgentID, "hunt")
}

// huntLoadSortie returns the open sortie, or nil. A corrupt file (including
// one that decodes with no id) logs once, is removed, and counts as none.
func huntLoadSortie(deps HuntDeps, out io.Writer) *huntdecide.Sortie {
	dir := huntAgentDir(deps)
	if dir == "" {
		return nil
	}
	s, err := huntdecide.LoadSortie(dir)
	if err == nil && s != nil && s.ID == "" {
		err = errors.New("no sortie id")
	}
	if err == nil {
		return s
	}
	fmt.Fprintf(out, "hunt[%s]: sortie file unreadable (%v); treating it as no open sortie\n", huntWho(deps), err) //nolint:errcheck
	if cerr := huntdecide.ClearSortie(dir); cerr != nil {
		fmt.Fprintf(out, "hunt[%s]: %v\n", huntWho(deps), cerr) //nolint:errcheck
	}
	return nil
}

// huntSaveSortie writes the sortie file; a failure is logged and the pass goes on.
func huntSaveSortie(deps HuntDeps, out io.Writer, s *huntdecide.Sortie) {
	dir := huntAgentDir(deps)
	if dir == "" || s == nil {
		return
	}
	if err := s.Save(dir); err != nil {
		fmt.Fprintf(out, "hunt[%s]: sortie file not saved: %v\n", huntWho(deps), err) //nolint:errcheck
	}
}

// huntSortieRecord appends a sortie_start or sortie_end record; a failure is
// logged and the pass goes on.
func huntSortieRecord(ctx context.Context, deps HuntDeps, out io.Writer, typ string, s *huntdecide.Sortie, job huntJob, lo reload.Loadout, result string) {
	if huntAgentDir(deps) == "" {
		return
	}
	now := huntNow(deps)
	rec := huntdecide.SortieRecord{
		Type: typ, At: now, SortieID: s.ID, State: huntDecisionState(ctx, deps, job, s, lo), Result: result,
	}
	if err := deps.Decisions.Append(now, rec); err != nil {
		fmt.Fprintf(out, "hunt[%s]: %s record not written: %v\n", huntWho(deps), typ, err) //nolint:errcheck
	}
}

// huntReadLoadout is the loadout for a record; empty when the ship cannot be read.
func huntReadLoadout(ctx context.Context, deps HuntDeps) reload.Loadout {
	lo, err := reload.FetchLoadout(ctx, deps.Client, deps.KB)
	if err != nil {
		return reload.Loadout{}
	}
	return lo
}

// huntNewSortie starts an in-memory sortie for job: nothing is written until
// huntOpenSortie.
func huntNewSortie(deps HuntDeps, job huntJob) *huntdecide.Sortie {
	s := huntdecide.NewSortie(deps.AgentID, huntNow(deps))
	s.JobKind, s.JobID = huntJobKind(job)
	if st := deps.Client.GetState(); st != nil {
		s.StartTick = st.CurrentTick
	}
	return s
}

// huntOpenSortie starts a sortie for job (s, or a new one when s is nil),
// writes the sortie_start record and saves the file.
func huntOpenSortie(ctx context.Context, deps HuntDeps, out io.Writer, job huntJob, lo reload.Loadout, s *huntdecide.Sortie) *huntdecide.Sortie {
	if s == nil {
		s = huntNewSortie(deps, job)
	}
	if huntAgentDir(deps) == "" {
		return s
	}
	huntSortieRecord(ctx, deps, out, "sortie_start", s, job, lo, "")
	huntSaveSortie(deps, out, s)
	fmt.Fprintf(out, "hunt[%s]: sortie %s out for %s %s\n", huntWho(deps), s.ID, s.JobKind, s.JobID) //nolint:errcheck
	return s
}

// huntCloseSortie writes sortie_end, calls deps.ReportSortie(result), and
// clears the file. Nil sortie: no-op.
func huntCloseSortie(ctx context.Context, deps HuntDeps, out io.Writer, s *huntdecide.Sortie, job huntJob, result string) {
	if s == nil {
		return
	}
	if dir := huntAgentDir(deps); dir != "" {
		huntSortieRecord(ctx, deps, out, "sortie_end", s, job, huntReadLoadout(ctx, deps), result)
		if err := huntdecide.ClearSortie(dir); err != nil {
			fmt.Fprintf(out, "hunt[%s]: %v\n", huntWho(deps), err) //nolint:errcheck
		}
		fmt.Fprintf(out, "hunt[%s]: sortie %s closed: %s (%d ground(s), %d fight(s), %d kill(s))\n", //nolint:errcheck
			huntWho(deps), s.ID, result, len(s.Grounds), s.Fights, s.Kills)
	}
	if deps.ReportSortie != nil {
		deps.ReportSortie(result)
	}
}

// huntSortiePending records the result a sortie closes with once docked.
func huntSortiePending(deps HuntDeps, out io.Writer, s *huntdecide.Sortie, result string) {
	s.PendingResult = result
	huntSaveSortie(deps, out, s)
}

// huntSortieJobStub is the job a sortie names, for the record that closes it
// when the job itself is no longer in hand.
func huntSortieJobStub(s *huntdecide.Sortie) huntJob {
	switch s.JobKind {
	case "home":
		return huntJob{boardID: "home grounds", title: "home grounds", home: true}
	case "need":
		return huntJob{boardID: s.JobID, need: &huntNeed{MissionID: s.JobID}}
	}
	return huntJob{boardID: s.JobID, activeID: s.JobID}
}

// huntResumeSortieJob rebuilds the job of a sortie found open out at a
// ground: home from the file, a mission or need from the held missions
// (huntResumeJob, so every resume gate applies again). ok=false when the job
// is no longer held.
func huntResumeSortieJob(ctx context.Context, deps HuntDeps, out io.Writer, who string, s *huntdecide.Sortie, wildlifeOnly bool) (huntJob, bool) {
	if s.JobKind == "home" {
		return huntSortieJobStub(s), true
	}
	job, ok := huntResumeJob(ctx, deps, out, who, wildlifeOnly)
	if !ok {
		fmt.Fprintf(out, "hunt[%s]: sortie %s: %s %s is no longer held\n", who, s.ID, s.JobKind, s.JobID) //nolint:errcheck
		return huntJob{}, false
	}
	kind, id := huntJobKind(job)
	if kind != s.JobKind || (s.JobID != id && s.JobID != job.activeID && s.JobID != job.boardID) {
		fmt.Fprintf(out, "hunt[%s]: sortie %s was for %s %s, but the held job is %s %s\n", who, s.ID, s.JobKind, s.JobID, kind, id) //nolint:errcheck
		return huntJob{}, false
	}
	return job, true
}

// huntSortieGround takes the sortie to its next ground with quarry, within
// huntMaxSortieGrounds across the sortie. Each ground reached is added to
// s.Grounds and the file saved. ok=false sets PendingResult no_quarry: the cap
// is reached or no ground with quarry was found.
func huntSortieGround(ctx context.Context, deps HuntDeps, out io.Writer, who string, job huntJob, s *huntdecide.Sortie, wildlifeOnly bool) ([]serverapi.NearbyCreature, bool) {
	remaining := huntMaxSortieGrounds - len(s.Grounds)
	if remaining <= 0 {
		fmt.Fprintf(out, "hunt[%s]: sortie %s has visited %d ground(s), the most one sortie may; %s held for next pass\n", //nolint:errcheck
			who, s.ID, len(s.Grounds), job.boardID)
		huntSortiePending(deps, out, s, huntdecide.ResultNoQuarry)
		return nil, false
	}
	quarry, reached, ok := huntFindQuarry(ctx, deps, out, who, job, wildlifeOnly, s.Visited(), remaining)
	if len(reached) > 0 {
		s.Grounds = append(s.Grounds, reached...)
		huntSaveSortie(deps, out, s)
	}
	if !ok {
		huntSortiePending(deps, out, s, huntdecide.ResultNoQuarry)
		return nil, false
	}
	return quarry, true
}

// huntReturnOptions are the options of a return decision: continue only when
// the ship can fire, rearm only with an ammo-fed weapon fitted (not explored
// before the first fight), repair always (never explored).
func huntReturnOptions(st huntdecide.State, s *huntdecide.Sortie) []huntdecide.Option {
	hullFrac := 0.0
	if st.MaxHull > 0 {
		hullFrac = st.Hull / st.MaxHull
	}
	reserve := huntHasReserve(st)
	var opts []huntdecide.Option
	if huntCanFire(st) {
		opts = append(opts, huntdecide.Option{
			ID: huntOptContinue, Features: map[string]any{"hull_frac": hullFrac, "has_reserve": reserve},
		})
	}
	if slices.ContainsFunc(st.Weapons, func(w huntdecide.Weapon) bool { return w.AmmoType != "" }) {
		opts = append(opts, huntdecide.Option{
			ID: huntOptRearm, Features: map[string]any{"has_reserve": reserve, "jumps_to_home": st.JumpsToHome},
			NoExplore: s.Fights < 1,
		})
	}
	opts = append(opts, huntdecide.Option{
		ID: huntOptRepair, Features: map[string]any{"hull_frac": hullFrac, "jumps_to_station": st.JumpsToStation},
		NoExplore: true,
	})
	return opts
}

// huntReturnDecision asks the seam after a fight. Returns huntOptContinue,
// huntOptRearm or huntOptRepair. When nothing can fire no decision is made and
// the return is forced: huntOptRearm. An unreadable ship continues: the hard
// gates between engagements still apply.
func huntReturnDecision(ctx context.Context, deps HuntDeps, out io.Writer, job huntJob, s *huntdecide.Sortie) string {
	who := huntWho(deps)
	lo, err := reload.FetchLoadout(ctx, deps.Client, deps.KB)
	if err != nil {
		fmt.Fprintf(out, "hunt[%s]: return check skipped: %v\n", who, err) //nolint:errcheck
		return huntOptContinue
	}
	st := huntDecisionState(ctx, deps, job, s, lo)
	if !huntCanFire(st) {
		fmt.Fprintf(out, "hunt[%s]: no weapon can fire after the fight; returning to rearm\n", who) //nolint:errcheck
		return huntOptRearm
	}
	opts := huntReturnOptions(st, s)
	choice := huntDecider(deps).Decide(ctx, huntdecide.Decision{
		Kind: huntdecide.KindReturn, SortieID: s.ID, Seq: s.NextSeq(), State: st, Options: opts,
	})
	huntSaveSortie(deps, out, s)
	id := choice.OptionID
	if !slices.ContainsFunc(opts, func(o huntdecide.Option) bool { return o.ID == id }) {
		id = huntOptContinue
	}
	if id != huntOptContinue {
		fmt.Fprintf(out, "hunt[%s]: return decision: %s (%s)\n", who, id, choice.Reason) //nolint:errcheck
	}
	return id
}

// huntHeadHome sets s.PendingResult, saves, and flies to the rearm or repair
// station and docks. The next pass, docked, closes the sortie. A failed leg
// is logged (and a battle escaped); the next pass recovers to a station.
func huntHeadHome(ctx context.Context, deps HuntDeps, out io.Writer, who string, s *huntdecide.Sortie, choice string) {
	result := huntdecide.ResultReturnedDry
	if choice == huntOptRepair {
		result = huntdecide.ResultReturnedDamaged
	}
	huntSortiePending(deps, out, s, result)

	var err error
	if choice == huntOptRepair {
		err = huntGoRepair(ctx, deps, out, who)
	} else {
		err = huntGoRearm(ctx, deps, out, who, huntReadLoadout(ctx, deps))
	}
	if err != nil {
		fmt.Fprintf(out, "hunt[%s]: heading home (%s): %v; the next pass recovers to a station\n", who, result, err) //nolint:errcheck
		huntEscapeBattle(ctx, deps, out, who, "heading home", err)
	}
}

// huntGoRepair flies to the nearest repair station and docks.
func huntGoRepair(ctx context.Context, deps HuntDeps, out io.Writer, who string) error {
	st := deps.Client.GetState()
	if st == nil || deps.KB == nil {
		return errors.New("no position or knowledge base to find a repair station")
	}
	from := cmp.Or(st.System.ID, st.CurrentSystem)
	sys, poi := huntNearestRepairStation(ctx, deps.KB, from, "")
	if poi == "" {
		return fmt.Errorf("no repair station known within %d jump(s) of %s", huntRepairSearchJumps, from)
	}
	fmt.Fprintf(out, "hunt[%s]: hull %.0f/%.0f; going to repair at %s/%s\n", who, st.Ship.Hull, st.Ship.MaxHull, sys, poi) //nolint:errcheck
	publishActivity(deps.SetActivity, "Hunt · going to repair")
	if err := huntFlyTo(ctx, deps, out, false, sys, poi); err != nil {
		return fmt.Errorf("repair leg to %s: %w", poi, err)
	}
	return nil
}

// huntLoadoutAmmo is the total of fitting ammo items aboard. A stack that
// feeds several weapons counts once.
func huntLoadoutAmmo(lo reload.Loadout) int {
	seen := map[string]bool{}
	n := 0
	for _, e := range lo.Plan {
		for _, c := range e.Candidates {
			if !seen[c.ItemID] {
				seen[c.ItemID] = true
				n += int(c.Quantity)
			}
		}
	}
	return n
}

// huntReloadCounted is huntReload that also reports how many ammo items the
// reload consumed: the drop in fitting ammo aboard across it.
func huntReloadCounted(ctx context.Context, deps HuntDeps, out io.Writer, who string) (armed bool, spent int) {
	lo, err := reload.FetchLoadout(ctx, deps.Client, deps.KB)
	if err != nil {
		fmt.Fprintf(out, "hunt[%s]: reload check skipped: %v\n", who, err) //nolint:errcheck
		return true, 0
	}
	armed = huntReloadPlan(ctx, deps, out, who, lo.Plan)
	if len(lo.Plan) == 0 {
		return armed, 0
	}
	after, err := reload.FetchLoadout(ctx, deps.Client, deps.KB)
	if err != nil {
		return armed, 0
	}
	return armed, max(0, huntLoadoutAmmo(lo)-huntLoadoutAmmo(after))
}
