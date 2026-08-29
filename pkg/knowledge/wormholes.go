package knowledge

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Wormholes come in two populations, and the id says which:
//
//   - TRANSIENT — "wh_entrance_40a24840": a random hex suffix, short-lived,
//     paired with a "wh_intro_<empire>_<same hex>" Anomalous Readings mission.
//     These collapse on their own and the id is never reused.
//   - PERMANENT — named ("Frying Pan", "Leap of Faith"). These always exist and
//     are what the difficulty 8-9 smuggling chain sends you through.
//
// Every wormhole row the KB holds is transient. That matters because the only
// way we ever learn a transient one collapsed is an agent revisiting the system
// and seeing the marker — so a wormhole nobody goes back to stays typed "live"
// forever. On 2026-08-17 five rows were still typed live at 69-123 days old,
// including a hidden one in Goldcrest that had been gone for months and that
// sent an investigation looking for a wormhole route into the system.
const (
	POITypeWormholeEntrance  = "wormhole_entrance"
	POITypeWormholeExit      = "wormhole_exit"
	POITypeWormholeCollapsed = "wormhole_collapsed"
)

// transientWormholeID matches the generated ids: wh_entrance_ or wh_exit_
// followed by a hex blob. A named permanent wormhole never matches.
var transientWormholeID = regexp.MustCompile(`^wh_(entrance|exit)_[0-9a-f]{6,}$`)

// IsTransientWormhole reports whether a POI id is one of the generated,
// short-lived wormholes.
func IsTransientWormhole(poiID string) bool {
	return transientWormholeID.MatchString(strings.ToLower(strings.TrimSpace(poiID)))
}

// WormholeIntroMissionKey returns the hex suffix a transient wormhole shares
// with its "Anomalous Readings" mission ids (wh_intro_<empire>_<suffix>), and
// false for anything that is not a transient wormhole. This is the join between
// a mission board entry and the POI it wants traversed.
func WormholeIntroMissionKey(poiID string) (string, bool) {
	if !IsTransientWormhole(poiID) {
		return "", false
	}
	i := strings.LastIndex(poiID, "_")

	return poiID[i+1:], true
}

// TransientWormholeTrustWindow is how long a transient wormhole observation is
// worth acting on.
//
// Originally inferred from the collapse record -- of 38 rows, the freshest
// collapsed sightings were 2-6 days old -- and since confirmed: the devs have
// stated a wormhole lasts 7 days before collapsing. So this is the real
// lifetime, not an approximation of one, and a week of silence means the row is
// almost certainly describing something that no longer exists.
//
// This is now the FALLBACK. Since 2026-08-28 worker.GetPOI converts
// wormhole_expires_in into an absolute ExpiresAt, so a row captured through
// get_poi carries the server's own answer and does not need this. The window
// still governs every row observed some other way, and every row captured
// before the conversion existed.
//
// The two figures measure DIFFERENT things and must not be compared.
// wormhole_expires_in is the time REMAINING on one hole, not its lifetime -- a
// hole reporting "12h" is not short-lived, it is ~6.5 days old and nearly dead.
// Prefer that figure wherever it exists, because it is specific to the hole;
// use this window only to age a row that never carried one.
const TransientWormholeTrustWindow = 7 * 24 * time.Hour

// WormholeStatus is what the KB can honestly say about one wormhole row.
type WormholeStatus struct {
	POIID     string
	SystemID  string
	Name      string
	Type      string
	Hidden    bool
	Transient bool
	// ExpiresAt is the server's own expiry when we captured one.
	//
	// Empty for every row recorded before 2026-08-28, because get_poi states a
	// wormhole's life as a RELATIVE duration on the response
	// ("wormhole_expires_in":"12h") rather than as the absolute expires_at the
	// POI object carries, and nothing bridged the two. worker.GetPOI now
	// converts it, so rows captured since carry a real expiry and the trust
	// window below is their fallback rather than their only evidence.
	ExpiresAt string
	// LastSeenTick is the tick an agent last observed this POI.
	LastSeenTick int64
	// StaleFor is how long ago that was, given the current tick.
	StaleFor time.Duration
	// Live reports whether the row is worth acting on: typed as an open
	// wormhole, and either carrying an unexpired server expiry or observed
	// recently enough to trust.
	Live bool
	// Reason states why, for a log line or a report.
	Reason string
}

// TickDuration is the wall-clock length of one game tick, used to age a
// last_updated_tick into a duration.
const TickDuration = 10 * time.Second

// WormholeLiveness classifies every wormhole POI the KB holds against the
// current game tick.
//
// A collapsed row is reported dead with no further reasoning. A live-typed row
// is trusted only while it is fresh, because nothing removes a transient
// wormhole that no agent revisits — the KB's silence is not evidence it still
// exists.
func (kb *SQLiteKB) WormholeLiveness(ctx context.Context, nowTick int64) ([]WormholeStatus, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT id, system_id, COALESCE(name,''), type, COALESCE(hidden,0),
		       COALESCE(expires_at,''), COALESCE(last_updated_tick,0)
		FROM pois
		WHERE type IN (?, ?, ?)
		ORDER BY last_updated_tick DESC`,
		POITypeWormholeEntrance, POITypeWormholeExit, POITypeWormholeCollapsed)
	if err != nil {
		return nil, fmt.Errorf("wormhole liveness: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []WormholeStatus
	for rows.Next() {
		var (
			w      WormholeStatus
			hidden int
		)
		if err := rows.Scan(&w.POIID, &w.SystemID, &w.Name, &w.Type, &hidden,
			&w.ExpiresAt, &w.LastSeenTick); err != nil {
			return nil, fmt.Errorf("scan wormhole: %w", err)
		}
		w.Hidden = hidden != 0
		w.Transient = IsTransientWormhole(w.POIID)
		if nowTick > w.LastSeenTick {
			w.StaleFor = time.Duration(nowTick-w.LastSeenTick) * TickDuration
		}
		w.Live, w.Reason = wormholeLive(w)
		out = append(out, w)
	}

	return out, rows.Err()
}

// wormholeLive is the liveness rule, split out so it can be tested without a
// database.
func wormholeLive(w WormholeStatus) (bool, string) {
	if w.Type == POITypeWormholeCollapsed {
		return false, "collapsed"
	}
	if w.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, w.ExpiresAt)
		if err == nil {
			if time.Now().After(t) {
				return false, "expired at " + w.ExpiresAt
			}

			return true, "server expiry " + w.ExpiresAt + " not yet reached"
		}
	}
	if !w.Transient {
		// A named wormhole is permanent; age says nothing about it.
		return true, "named wormhole, permanent"
	}
	if w.StaleFor > TransientWormholeTrustWindow {
		return false, fmt.Sprintf("transient and unobserved for %.0f days; presumed collapsed",
			w.StaleFor.Hours()/24)
	}

	return true, fmt.Sprintf("transient, observed %.1f days ago", w.StaleFor.Hours()/24)
}

// PermanentWormhole is a named wormhole that always exists.
//
// These are recorded from the smuggling chain's own objective text rather than
// from a survey, because the KB has never held one: every wormhole POI captured
// so far is transient. AnchorPOI is the landmark the mission names, not the
// wormhole's own POI id — that has to come from an agent actually flying there,
// and is left empty rather than invented.
type PermanentWormhole struct {
	Name       string
	SystemID   string
	AnchorHint string
	MissionID  string
	// POIID is filled in once someone surveys the system and finds it.
	POIID string
}

// KnownPermanentWormholes is what the mission board has told us so far.
//
// Both come from the two-step smuggling chain through_the_fire (difficulty 9,
// 15,000cr) -> leap_of_faith (difficulty 8, 10,000cr), whose objectives are
// type traverse_wormhole and name their targets in prose. Templates seen once,
// 2026-07-07, and not offered since.
var KnownPermanentWormholes = []PermanentWormhole{
	{
		Name:       "Frying Pan",
		SystemID:   "hadar",
		AnchorHint: "near Hadar's star",
		MissionID:  "through_the_fire",
	},
	{
		Name:       "Leap of Faith",
		SystemID:   "alzirr",
		AnchorHint: "near the Maw",
		MissionID:  "leap_of_faith",
	},
}

// PermanentWormholeIn returns the named wormhole known to sit in a system.
func PermanentWormholeIn(systemID string) (PermanentWormhole, bool) {
	for _, w := range KnownPermanentWormholes {
		if strings.EqualFold(w.SystemID, systemID) {
			return w, true
		}
	}

	return PermanentWormhole{}, false
}
