package sweep

import (
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
)

// The frontier run: every frontier system fully explored once (survey at full
// power, every POI visited), by several explorers sharing this ledger. Each
// ship works one lane-connected cluster at a time and claims it whole, so two
// ships never tour the same systems.

// FrontierStatus is where a system stands in the run.
type FrontierStatus string

// Frontier system statuses.
const (
	FrontierTodo       FrontierStatus = "todo"       // nobody has it
	FrontierClaimed    FrontierStatus = "claimed"    // a ship is working its cluster
	FrontierDone       FrontierStatus = "done"       // surveyed at full power, every POI visited
	FrontierIncomplete FrontierStatus = "incomplete" // visited or attempted, with Reason; not retried in this run
)

// FrontierSystem is one frontier system in the run.
type FrontierSystem struct {
	ID          string         `json:"id"`
	Name        string         `json:"name,omitempty"`
	X           float64        `json:"x"`
	Y           float64        `json:"y"`
	Wedge       string         `json:"wedge"`   // compass direction from the galaxy centre
	Cluster     string         `json:"cluster"` // id of its lane-connected group at generation
	Status      FrontierStatus `json:"status"`
	Claimant    string         `json:"claimant,omitempty"`
	SurveyPower int            `json:"survey_power,omitempty"`
	POIsSeen    int            `json:"pois_seen,omitempty"`
	POIsVisited int            `json:"pois_visited,omitempty"`
	Relics      int            `json:"relics,omitempty"`
	Skipped     []string       `json:"skipped,omitempty"` // POI ids not visited
	Reason      string         `json:"reason,omitempty"`  // why it is incomplete
	UpdatedUTC  time.Time      `json:"updated_utc"`
}

// FrontierRun is the shared ledger of one frontier run. Lanes are the jump
// lanes between two frontier systems; lanes into charted space are not kept.
type FrontierRun struct {
	GeneratedUTC time.Time                  `json:"generated_utc"`
	Systems      map[string]*FrontierSystem `json:"systems"`
	Lanes        [][2]string                `json:"lanes"`
}

// FrontierInput is a system to put in the run. A non-empty Lethal is a
// standing reason never to enter it (a Repeater Monolith, a leviathan).
type FrontierInput struct {
	ID, Name string
	X, Y     float64
	Lethal   string
}

// wedgeNames are the eight compass directions, counter-clockwise from +x.
var wedgeNames = [8]string{"E", "NE", "N", "NW", "W", "SW", "S", "SE"}

// Wedges lists the eight compass directions, counter-clockwise from east.
func Wedges() []string { return wedgeNames[:] }

// Wedge names the 45° compass wedge (x, y) lies in, seen from the galaxy
// centre: E is centred on 0°, NE on 45°, and so on counter-clockwise.
func Wedge(x, y float64) string {
	deg := math.Atan2(y, x) * 180 / math.Pi
	if deg < 0 {
		deg += 360
	}
	return wedgeNames[int((deg+22.5)/45)%8]
}

func newFrontierSystem(in FrontierInput, now time.Time) *FrontierSystem {
	s := &FrontierSystem{
		ID: in.ID, Name: in.Name, X: in.X, Y: in.Y,
		Wedge: Wedge(in.X, in.Y), Cluster: in.ID,
		Status: FrontierTodo, UpdatedUTC: now.UTC(),
	}
	if in.Lethal != "" {
		s.Status, s.Reason = FrontierIncomplete, "hostile: "+in.Lethal
	}
	return s
}

// NewFrontierRun builds a ledger of systems, grouping them into clusters by
// the lanes that join two of them. A cluster's id is its smallest system id.
func NewFrontierRun(systems []FrontierInput, lanes [][2]string, now time.Time) *FrontierRun {
	r := &FrontierRun{GeneratedUTC: now.UTC(), Systems: make(map[string]*FrontierSystem, len(systems))}
	for _, in := range systems {
		r.Systems[in.ID] = newFrontierSystem(in, now)
	}
	for _, l := range lanes {
		r.addLane(l[0], l[1])
	}
	for _, id := range slices.Sorted(maps.Keys(r.Systems)) {
		if r.Systems[id].Cluster != id {
			continue // already labelled from a smaller id
		}
		for _, member := range r.reach(id, func(*FrontierSystem) bool { return true }) {
			if r.Systems[member].Cluster == member {
				r.Systems[member].Cluster = id
			}
		}
	}
	return r
}

// addLane records a lane between two systems of the run; it reports whether
// the ledger changed.
func (r *FrontierRun) addLane(a, b string) bool {
	if a == b || r.Systems[a] == nil || r.Systems[b] == nil {
		return false
	}
	if a > b {
		a, b = b, a
	}
	lane := [2]string{a, b}
	if slices.Contains(r.Lanes, lane) {
		return false
	}
	r.Lanes = append(r.Lanes, lane)
	return true
}

// neighbours lists the systems one lane from id, sorted.
func (r *FrontierRun) neighbours(id string) []string {
	var out []string
	for _, l := range r.Lanes {
		switch id {
		case l[0]:
			out = append(out, l[1])
		case l[1]:
			out = append(out, l[0])
		}
	}
	slices.Sort(out)
	return out
}

// reach lists the systems lane-connected to from, breadth first, crossing only
// systems pass accepts. from itself is always first.
func (r *FrontierRun) reach(from string, pass func(*FrontierSystem) bool) []string {
	seen := map[string]bool{from: true}
	queue := []string{from}
	for i := 0; i < len(queue); i++ {
		for _, n := range r.neighbours(queue[i]) {
			if s := r.Systems[n]; s != nil && !seen[n] && pass(s) {
				seen[n] = true
				queue = append(queue, n)
			}
		}
	}
	return queue
}

// Active reports whether any system has been claimed or worked.
func (r *FrontierRun) Active() bool {
	for _, s := range r.Systems {
		if s.Claimant != "" {
			return true
		}
	}
	return false
}

// LoadFrontierRun reads a run ledger.
func LoadFrontierRun(path string) (*FrontierRun, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("frontier run read: %w", err)
	}
	var r FrontierRun
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("frontier run decode %s: %w", path, err)
	}
	if r.Systems == nil {
		r.Systems = map[string]*FrontierSystem{}
	}
	return &r, nil
}

// SaveFrontierRun writes a run ledger atomically (temp file + rename).
func SaveFrontierRun(path string, r *FrontierRun) error {
	data, err := json.MarshalIndent(r, "", " ")
	if err != nil {
		return fmt.Errorf("frontier run marshal: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("frontier run mkdir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("frontier run write: %w", err)
	}
	return os.Rename(tmp, path)
}

// withFrontierRun runs fn on the ledger under an exclusive flock on path.lock
// and saves it when fn reports a change: worker processes share the file.
func withFrontierRun(path string, fn func(r *FrontierRun) (bool, error)) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("frontier run mkdir: %w", err)
	}
	lock, err := os.OpenFile(path+".lock", os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return fmt.Errorf("frontier run lock: %w", err)
	}
	defer lock.Close() //nolint:errcheck
	if ferr := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); ferr != nil {
		return fmt.Errorf("frontier run flock: %w", ferr)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) //nolint:errcheck
	r, err := LoadFrontierRun(path)
	if err != nil {
		return err
	}
	changed, err := fn(r)
	if err != nil || !changed {
		return err
	}
	return SaveFrontierRun(path, r)
}

// ClaimCluster claims for agent every unfinished system lane-connected to
// systemID, and returns them breadth first from systemID. Finished systems
// are crossed but not returned; incomplete ones, and systems another agent
// has claimed, are neither crossed nor returned: when a lane turns out to
// join two ships' clusters each keeps its own. ok is false, and nothing is
// claimed, only when systemID itself is another agent's claim. A ship that
// already holds the cluster gets it back.
func ClaimCluster(path, agent, systemID string, now time.Time) (ids []string, ok bool, err error) {
	err = withFrontierRun(path, func(r *FrontierRun) (bool, error) {
		if r.Systems[systemID] == nil {
			return false, fmt.Errorf("frontier run: %s is not in the ledger", systemID)
		}
		others := func(s *FrontierSystem) bool { return s.Status == FrontierClaimed && s.Claimant != agent }
		if others(r.Systems[systemID]) {
			return false, nil
		}
		reached := r.reach(systemID, func(s *FrontierSystem) bool { return s.Status != FrontierIncomplete && !others(s) })
		for _, id := range reached {
			if s := r.Systems[id]; s.Status == FrontierTodo || s.Status == FrontierClaimed {
				ids = append(ids, id)
			}
		}
		for _, id := range ids {
			s := r.Systems[id]
			s.Status, s.Claimant, s.UpdatedUTC = FrontierClaimed, agent, now.UTC()
		}
		ok = true
		return len(ids) > 0, nil
	})
	return ids, ok && err == nil, err
}

// AddSystem puts a system the run did not know into the ledger, joined by a
// lane to laneTo ("" for none). It joins laneTo's cluster, and is claimed by
// agent when laneTo is agent's claim or there is no laneTo. A system already
// in the ledger only gains the lane.
func AddSystem(path string, in FrontierInput, laneTo, agent string, now time.Time) error {
	return withFrontierRun(path, func(r *FrontierRun) (bool, error) {
		if r.Systems[in.ID] != nil {
			return r.addLane(in.ID, laneTo), nil
		}
		s := newFrontierSystem(in, now)
		r.Systems[in.ID] = s
		via := r.Systems[laneTo]
		if via != nil {
			s.Cluster = via.Cluster
			r.addLane(in.ID, laneTo)
		}
		mine := via == nil || (via.Claimant == agent && via.Status != FrontierTodo)
		if s.Status == FrontierTodo && mine {
			s.Status, s.Claimant = FrontierClaimed, agent
		}
		return true, nil
	})
}

// FinishSystem records the outcome of agent's work on a system: s carries the
// ID, the Status (done or incomplete) and the counts. It is refused when the
// system is claimed, or was already finished, by another agent.
func FinishSystem(path, agent string, s FrontierSystem, now time.Time) error {
	if s.Status != FrontierDone && s.Status != FrontierIncomplete {
		return fmt.Errorf("frontier run: finish %s with status %q", s.ID, s.Status)
	}
	return withFrontierRun(path, func(r *FrontierRun) (bool, error) {
		cur := r.Systems[s.ID]
		if cur == nil {
			return false, fmt.Errorf("frontier run: %s is not in the ledger", s.ID)
		}
		if cur.Status != FrontierTodo && cur.Claimant != "" && cur.Claimant != agent {
			return false, fmt.Errorf("frontier run: %s is %s by %s, not %s", s.ID, cur.Status, cur.Claimant, agent)
		}
		cur.Status, cur.Claimant, cur.UpdatedUTC = s.Status, agent, now.UTC()
		cur.SurveyPower, cur.POIsSeen, cur.POIsVisited, cur.Relics = s.SurveyPower, s.POIsSeen, s.POIsVisited, s.Relics
		cur.Skipped, cur.Reason = s.Skipped, s.Reason
		return true, nil
	})
}

// ReopenUnreachable puts systems written off as unreachable (by drift or by
// lane) back to todo: the given ids, or every such system when none is given.
// It returns the ids it reopened, sorted. Other incomplete systems are left.
func ReopenUnreachable(path string, now time.Time, ids ...string) (reopened []string, err error) {
	err = withFrontierRun(path, func(r *FrontierRun) (bool, error) {
		for id, s := range r.Systems {
			if len(ids) > 0 && !slices.Contains(ids, id) {
				continue
			}
			if s.Status != FrontierIncomplete || !strings.HasPrefix(s.Reason, "unreachable") {
				continue
			}
			s.Status, s.Claimant, s.Reason, s.UpdatedUTC = FrontierTodo, "", "", now.UTC()
			reopened = append(reopened, id)
		}
		slices.Sort(reopened)
		return len(reopened) > 0, nil
	})
	return reopened, err
}

// SetPositions moves the unfinished systems named in pos to the positions
// given, and returns how many moved. Finished systems and ids the ledger does
// not know are left; a system's wedge and cluster do not change.
func SetPositions(path string, pos map[string]Point, now time.Time) (moved int, err error) {
	err = withFrontierRun(path, func(r *FrontierRun) (bool, error) {
		for id, p := range pos {
			s := r.Systems[id]
			if s == nil || s.Status == FrontierDone || (s.X == p.X && s.Y == p.Y) {
				continue
			}
			s.X, s.Y, s.UpdatedUTC = p.X, p.Y, now.UTC()
			moved++
		}
		return moved > 0, nil
	})
	return moved, err
}

// nearestTodo picks, among the todo systems keep accepts, the one with the
// smallest dist; ties go to the smaller id. Nil when there is none.
func (r *FrontierRun) nearestTodo(keep func(*FrontierSystem) bool, dist func(*FrontierSystem) float64) *FrontierSystem {
	var best *FrontierSystem
	for _, id := range slices.Sorted(maps.Keys(r.Systems)) {
		s := r.Systems[id]
		if s.Status != FrontierTodo || !keep(s) {
			continue
		}
		if best == nil || dist(s) < dist(best) {
			best = s
		}
	}
	if best == nil {
		return nil
	}
	cp := *best
	return &cp
}

// NextFrontierTarget is the todo system nearest (x, y): in wedge when one is left
// there, else anywhere. Nil when nothing is todo.
func NextFrontierTarget(path, wedge string, x, y float64) (*FrontierSystem, error) {
	var got *FrontierSystem
	err := withFrontierRun(path, func(r *FrontierRun) (bool, error) {
		dist := func(s *FrontierSystem) float64 { return math.Hypot(s.X-x, s.Y-y) }
		got = r.nearestTodo(func(s *FrontierSystem) bool { return s.Wedge == wedge }, dist)
		if got == nil {
			got = r.nearestTodo(func(*FrontierSystem) bool { return true }, dist)
		}
		return false, nil
	})
	return got, err
}

// StartFrontierTarget is the todo system in wedge nearest the galaxy centre, which is
// the one nearest charted space. Nil when the wedge has nothing todo.
func StartFrontierTarget(path, wedge string) (*FrontierSystem, error) {
	var got *FrontierSystem
	err := withFrontierRun(path, func(r *FrontierRun) (bool, error) {
		got = r.nearestTodo(func(s *FrontierSystem) bool { return s.Wedge == wedge },
			func(s *FrontierSystem) float64 { return math.Hypot(s.X, s.Y) })
		return false, nil
	})
	return got, err
}

// OwnClaim is the system agent has claimed and not finished that lies nearest
// (x, y); ties go to the smaller id. Nil when it holds none. A ship returns
// to its own claims before taking new ground, so none is left orphaned.
func OwnClaim(path, agent string, x, y float64) (*FrontierSystem, error) {
	var got *FrontierSystem
	err := withFrontierRun(path, func(r *FrontierRun) (bool, error) {
		var best *FrontierSystem
		for _, id := range slices.Sorted(maps.Keys(r.Systems)) {
			s := r.Systems[id]
			if s.Status != FrontierClaimed || s.Claimant != agent {
				continue
			}
			if best == nil || math.Hypot(s.X-x, s.Y-y) < math.Hypot(best.X-x, best.Y-y) {
				best = s
			}
		}
		if best != nil {
			cp := *best
			got = &cp
		}
		return false, nil
	})
	return got, err
}

// ReleaseAgent returns every system agent has claimed and not finished to todo.
func ReleaseAgent(path, agent string, now time.Time) error {
	return withFrontierRun(path, func(r *FrontierRun) (bool, error) {
		changed := false
		for _, s := range r.Systems {
			if s.Status == FrontierClaimed && s.Claimant == agent {
				s.Status, s.Claimant, s.UpdatedUTC = FrontierTodo, "", now.UTC()
				changed = true
			}
		}
		return changed, nil
	})
}
