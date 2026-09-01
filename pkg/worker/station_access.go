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
// public_access, but of the 23 known player stations only 3 have a bases row at
// all -- and those 3 are exactly the ones the fleet has proven open, each with
// public_access=1. Two independent sources agreeing that precisely is suggestive
// but not proof, so a bases row is treated as corroboration and access is still
// LEARNED: proven by a dock that worked, disproven by one that was refused, and
// unknown until then.
//
// A blanket block would be wrong. Three player stations are provably open (Hex
// Star in dheneb, where nine agents sit docked; The Obsidian Well in arneb; and
// The Veil Anchor in bd20_2457), so refusing all of them would decline real
// fares to avoid the closed ones.

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
//
// Access and fuel are INDEPENDENT facts: a station that admits you is not
// thereby a station that can send you home. `no_fuel_source` shows up in the
// fleet logs against seven distinct 32-hex ids -- every one a PLAYER station --
// so a dry desk is a real and specifically player-station hazard, and a router
// needs both facts before it commits a long leg.
//
// Hex Star, incidentally, does sell fuel: johnny_cab refuelled there on
// 2026-08-12. Access and fuel really are orthogonal, and neither is guessable.
type StationAccess struct {
	mu            sync.Mutex
	path          string
	open          map[string]bool
	denied        map[string]bool
	fuel          map[string]bool
	noStationFuel map[string]bool
	gone          map[string]bool
}

// stationAccessFile is the on-disk shape. Sorted-set semantics, list encoding:
// a human reads this file when a shuttle refuses a fare and needs to know why.
type stationAccessFile struct {
	Open          []string `json:"open"`
	Denied        []string `json:"denied"`
	Fuel          []string `json:"fuel"`
	NoStationFuel []string `json:"no_station_fuel"`
	Gone          []string `json:"gone"`
}

// LoadStationAccess reads the access map at path. A missing or unreadable file
// yields an empty map rather than an error: an absent map must degrade to
// "nothing known yet", never to a worker that refuses to run.
func LoadStationAccess(path string) *StationAccess {
	a := &StationAccess{
		path: path,
		open: map[string]bool{}, denied: map[string]bool{},
		fuel: map[string]bool{}, noStationFuel: map[string]bool{},
		gone: map[string]bool{},
	}
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
	for _, id := range f.Fuel {
		a.fuel[id] = true
	}
	for _, id := range f.NoStationFuel {
		a.noStationFuel[id] = true
	}
	for _, id := range f.Gone {
		a.gone[id] = true
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

	// gone is checked as well as denied because `open` is never expired: a
	// station we proved open and that was later dismantled would otherwise stay
	// deliverable forever, and the fare would be booked for somewhere that no
	// longer exists.
	return a.open[stationID] && !a.denied[stationID] && !a.gone[stationID]
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

// noFuelSourceMarker is the server's refusal when a station sells no fuel at all.
// Deliberately NOT matched against "no_fuel_cells" / "no fuel cells in cargo",
// which is the opposite situation: the STATION is fine and the SHIP is out of
// fuel cells to burn. Both appear in the fleet logs in the hundreds of
// thousands, so a substring match loose enough to catch one would silently
// mislabel every station that saw the other. engineer-5's 2026-07-29 strand was
// the SHIP-dry one ("Fuel low (1%) and no fuel cells in cargo") -- it says
// nothing about the station it happened at, which is exactly the confusion this
// narrow match exists to prevent.
const noFuelSourceMarker = "no_fuel_source"

// RecordRefuel learns whether a station can actually sell fuel: a refuel that
// succeeded proves it, `no_fuel_source` disproves it, and every other failure
// teaches nothing.
//
// Recorded for NPC stations too, unlike access: access is only ever in question
// at a player station, but a fuel desk is not guaranteed anywhere, and knowing
// which NPC stations can refuel is what makes a long leg plannable.
func (a *StationAccess) RecordRefuel(stationID string, refuelErr error) {
	if a == nil || stationID == "" {
		return
	}
	a.mu.Lock()
	switch {
	case refuelErr == nil:
		a.fuel[stationID] = true
		delete(a.noStationFuel, stationID)
	case strings.Contains(strings.ToLower(refuelErr.Error()), noFuelSourceMarker):
		a.noStationFuel[stationID] = true
		delete(a.fuel, stationID)
	default:
		a.mu.Unlock()

		return
	}
	a.mu.Unlock()
	a.save()
}

// Fuel reports what is known about a station's fuel desk. known=false means
// nobody has tried, which a router must treat differently from a proven "no":
// unknown is worth a look on the way past, a proven no must be routed around.
func (a *StationAccess) Fuel(stationID string) (sells, known bool) {
	if a == nil {
		return false, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.fuel[stationID] {
		return true, true
	}
	if a.noStationFuel[stationID] {
		return false, true
	}

	return false, false
}

// A station can be DISMANTLED by its owner, and the knowledge base goes on
// serving its POI row indefinitely: `pois` rows are refreshed only when an agent
// visits that system, so a station somewhere nobody has flown to in weeks still
// reads as good data. The server's answer is unambiguous when you finally get
// there -- `{"code":"invalid_poi","message":"Unknown destination: <id>"}`.
//
// Both markers are matched because the code is the durable contract while the
// message is what survives the client's error wrapping; either alone is proof.
const (
	invalidPOICodeMarker     = "invalid_poi"
	unknownDestinationMarker = "unknown destination"
)

// RecordTransit learns whether a station still exists, from an attempt to fly to
// it. Arriving proves it does; `invalid_poi` / `Unknown destination` proves it
// does not; every other failure -- a dropped socket, an unplannable route, a
// timeout -- teaches nothing and is ignored, exactly as for dock and refuel.
//
// This is what stops the survey re-flying the same ghost every pass. The
// 2026-08-12 run spent seven jumps reaching Veilwatch Shoal in oakridge and
// three more to ENDL Kitalpha Cache; both answered `Unknown destination`, and
// both were the stalest rows in the POI table (ticks 1,429,376 and 1,468,302
// against a clock of 1,599,600). Without this the next run would spend the same
// fuel to learn the same thing.
//
// Player stations only. NPC ids are slugs the server has always known, so an
// `Unknown destination` against one is a bug in our own routing, and recording
// it would bury that bug under a permanent skip.
func (a *StationAccess) RecordTransit(stationID string, transitErr error) {
	if a == nil || !playerStationID(stationID) {
		return
	}
	a.mu.Lock()
	switch {
	case transitErr == nil:
		if !a.gone[stationID] {
			a.mu.Unlock()

			return // already known to exist; nothing to write
		}
		delete(a.gone, stationID) // rebuilt, or the id was reused
	case containsAny(strings.ToLower(transitErr.Error()), invalidPOICodeMarker, unknownDestinationMarker):
		a.gone[stationID] = true
	default:
		a.mu.Unlock()

		return
	}
	a.mu.Unlock()
	a.save()
}

// Gone reports whether a station has been proven not to exist. Distinct from
// Denied: one refused us, the other is not there to refuse anyone, and an
// operator reading this map to find out why a fare was declined needs to see
// which.
func (a *StationAccess) Gone(stationID string) bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.gone[stationID]
}

// containsAny reports whether s contains any of the given substrings.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}

	return false
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
	f := stationAccessFile{
		Open: sortedKeys(a.open), Denied: sortedKeys(a.denied),
		Fuel: sortedKeys(a.fuel), NoStationFuel: sortedKeys(a.noStationFuel),
		Gone: sortedKeys(a.gone),
	}
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

// IsPlayerStationID reports whether an id belongs to a player-built station.
// Exported for the survey tool, which must pick its targets out of the full POI
// catalogue before any of them are in the access map.
func IsPlayerStationID(id string) bool { return playerStationID(id) }
