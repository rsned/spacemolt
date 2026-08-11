package worker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Player-built stations can refuse an outsider's dock, and there is no way to
// know which will until someone tries. Live on 2026-08-11 the shuttle booked a
// passenger for Fortress Blackthorn, flew there, got `Access denied` on dock,
// and then held the undeliverable passenger while circling the system -- it had
// to be recovered by hand.
//
// The server exposes no access flag we can read ahead of time. `bases` carries
// public_access, but it is populated only for the seven pirate strongholds, and
// 18 of the 21 known player stations have no bases row at all. So access is
// LEARNED: proven by a dock that worked, disproven by one that was refused, and
// unknown until then.
//
// A blanket block would be wrong. Two player stations are provably open (Hex
// Star in dheneb, where nine agents sit docked, and The Obsidian Well in arneb),
// so refusing all of them would decline real fares to avoid one bad station.

// playerStationIDLen is the length of the hex id the server mints for a
// player-built station.
const playerStationIDLen = 32

// playerStationID reports whether a station or base id belongs to a PLAYER-built
// station rather than an NPC one.
//
// The discriminator is the id shape, verified against the whole knowledge base
// on 2026-08-11: 21 POIs carry a 32-char lowercase hex id and every one of them
// is type=station, while all 2332 slug-id POIs (including the 44 NPC stations)
// carry a readable id like `treasure_cache_trading_post`. There are no false
// positives in either direction.
//
// Both forms of id are accepted because a station is dual-named -- a base id and
// a poi id, each its own hash (Fortress Blackthorn is base 566fbb2d... and poi
// 9d742ead...). Callers pass whichever they hold.
func playerStationID(id string) bool {
	if len(id) != playerStationIDLen {
		return false
	}
	for _, r := range id {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}

	return true
}

// StationAccess is the learned player-station access map, shared across passes
// via a small JSON file. The zero value is usable but does not persist.
//
// Entries are only ever added, never expired: a station that admitted us is
// evidence that stands, and one that refused us is a fact worth remembering
// until an operator says otherwise. Access could in principle be reconfigured by
// its owner, which is why a denial blocks BOARDING rather than permanently
// blacklisting the station for every purpose.
type StationAccess struct {
	mu     sync.Mutex
	path   string
	open   map[string]bool
	denied map[string]bool
}

// stationAccessFile is the on-disk shape. Sorted-set semantics, list encoding:
// a human reads this file when a shuttle refuses a fare and needs to know why.
type stationAccessFile struct {
	Open   []string `json:"open"`
	Denied []string `json:"denied"`
}

// LoadStationAccess reads the access map at path. A missing or unreadable file
// yields an empty map rather than an error: an absent map must degrade to
// "nothing known yet", never to a worker that refuses to run.
func LoadStationAccess(path string) *StationAccess {
	a := &StationAccess{path: path, open: map[string]bool{}, denied: map[string]bool{}}
	b, err := os.ReadFile(path)
	if err != nil {
		return a
	}
	var f stationAccessFile
	if err := json.Unmarshal(b, &f); err != nil {
		return a
	}
	for _, id := range f.Open {
		a.open[id] = true
	}
	for _, id := range f.Denied {
		a.denied[id] = true
	}

	return a
}

// Deliverable reports whether a shuttle should accept a fare bound for this
// station.
//
// NPC stations are always deliverable. A player station is deliverable only once
// a dock has proven it: unknown reads as NOT deliverable, because the cost of
// guessing wrong is a passenger that can never be dropped and a shuttle that
// circles until an operator intervenes, while the cost of being wrong the other
// way is one declined fare.
//
// A nil map is permissive -- it means the feature is not wired up (tests, older
// callers), and it must not silently disable the shuttle.
func (a *StationAccess) Deliverable(stationID string) bool {
	if a == nil || !playerStationID(stationID) {
		return true
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.open[stationID] && !a.denied[stationID]
}

// Denied reports whether this station has refused us. Distinct from
// !Deliverable, which is also true for a player station we have simply never
// tried: only a station that actually turned us away justifies dumping
// passengers already aboard.
func (a *StationAccess) Denied(stationID string) bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.denied[stationID]
}

// accessDeniedMarker is the server's refusal text. Matched case-insensitively on
// a substring because it arrives wrapped in the client's own error context.
const accessDeniedMarker = "access denied"

// RecordDock learns from a dock attempt: nil error proves access, an
// `Access denied` error proves refusal, and any other failure (no route, a
// transient disconnect) teaches nothing and is ignored -- misreading a network
// blip as a refusal would blacklist a good station forever.
//
// Only player stations are recorded; NPC access is not in question and storing
// them would grow the file without informing any decision.
func (a *StationAccess) RecordDock(stationID string, dockErr error) {
	if a == nil || !playerStationID(stationID) {
		return
	}
	a.mu.Lock()
	switch {
	case dockErr == nil:
		a.open[stationID] = true
		delete(a.denied, stationID) // re-opened by its owner
	case strings.Contains(strings.ToLower(dockErr.Error()), accessDeniedMarker):
		a.denied[stationID] = true
		delete(a.open, stationID) // closed by its owner
	default:
		a.mu.Unlock()

		return
	}
	a.mu.Unlock()
	a.save()
}

// Seed marks stations as open from outside evidence -- chiefly the asset ledger,
// where any agent holding a non-empty docked_at_base at a player station has
// proven that station admits us. That cross-agent evidence is what makes the map
// useful on day one: the shuttle alone would need to gamble a fare per station
// to learn what the fleet already collectively knows.
//
// Seeding never marks anything denied: a docked_at_base that is EMPTY proves
// nothing (the field is a player field that is routinely blank even while
// docked), so absence of evidence must not become evidence of refusal.
func (a *StationAccess) Seed(stationIDs []string) {
	if a == nil {
		return
	}
	a.mu.Lock()
	changed := false
	for _, id := range stationIDs {
		if playerStationID(id) && !a.open[id] && !a.denied[id] {
			a.open[id] = true
			changed = true
		}
	}
	a.mu.Unlock()
	if changed {
		a.save()
	}
}

// save best-effort persists the map. A write failure is not fatal: the in-memory
// map still guards this pass, and the next successful dock rewrites the file.
func (a *StationAccess) save() {
	if a.path == "" {
		return
	}
	a.mu.Lock()
	f := stationAccessFile{Open: sortedKeys(a.open), Denied: sortedKeys(a.denied)}
	a.mu.Unlock()
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return
	}
	if dir := filepath.Dir(a.path); dir != "" && dir != "." {
		_ = os.MkdirAll(dir, 0o755) //nolint:errcheck
	}
	// Write via a temp file and rename: several workers share this path, and a
	// partially written map would be read as "nothing known" by the next loader.
	tmp := a.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil { //nolint:gosec // operator-readable state
		return
	}
	_ = os.Rename(tmp, a.path) //nolint:errcheck
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	// Deterministic file content keeps diffs meaningful when an operator
	// inspects why a fare was refused.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}

	return out
}
