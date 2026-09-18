package knowledge

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// onlyAvailableAt matches the accept_mission refusal that names a mission's
// sole giver:
//
//	mission_not_available: This mission is only available at Alpha Centauri Colonial Station.
//
// This refusal is the only source we have found for the giver of a mission
// that has never appeared on an observed board -- mission_template_locations
// is written from board captures, so such a mission has no location row at
// all. The station arrives as a DISPLAY NAME, never an id.
var onlyAvailableAt = regexp.MustCompile(`(?i)only available at\s+(.+?)\s*\.?\s*$`)

// ParseMissionOnlyAvailableAt returns the station display name named by a
// mission_not_available refusal, or "" when the message is a different
// refusal. Matching is on the phrasing, so a wording change fails closed:
// no name, no write.
func ParseMissionOnlyAvailableAt(message string) string {
	m := onlyAvailableAt.FindStringSubmatch(strings.TrimSpace(message))
	if len(m) != 2 {
		return ""
	}
	return strings.TrimSpace(m[1])
}

// ErrStationUnknown is returned when a display name matches no known station.
var ErrStationUnknown = errors.New("station name not found")

// ResolveStationName maps a station's display name to its id and system.
//
// Station ids are dual-named -- find_item hands back POI ids while
// docked_at_base hands back BASE ids -- so a display name must be resolved
// before it is stored anywhere that claims to hold an id. Errors rather than
// returning an empty id, which a caller could otherwise persist as if it had
// resolved.
func (kb *SQLiteKB) ResolveStationName(ctx context.Context, name string) (baseID, systemID string, err error) {
	needle := strings.ToLower(strings.TrimSpace(name))
	if needle == "" {
		return "", "", fmt.Errorf("%w: empty name", ErrStationUnknown)
	}

	// POIs first: they carry the system, which is what routing needs.
	err = kb.db.QueryRowContext(ctx, `
		SELECT id, COALESCE(system_id, '') FROM pois
		WHERE LOWER(TRIM(name)) = ? ORDER BY id LIMIT 1
	`, needle).Scan(&baseID, &systemID)
	if err == nil {
		return baseID, systemID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", "", fmt.Errorf("resolve station: %w", err)
	}

	// Then bases, for a station known only under its base id. bases carries
	// poi_id and NO system_id (the same shape that blocks resolving a pin to
	// its system), so the system has to come from the POI it sits on.
	err = kb.db.QueryRowContext(ctx, `
		SELECT b.id, COALESCE(p.system_id, '')
		FROM bases b LEFT JOIN pois p ON p.id = b.poi_id
		WHERE LOWER(TRIM(b.name)) = ? ORDER BY b.id LIMIT 1
	`, needle).Scan(&baseID, &systemID)
	if err == nil {
		return baseID, systemID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", "", fmt.Errorf("resolve station: %w", err)
	}
	return "", "", fmt.Errorf("%w: %q", ErrStationUnknown, name)
}

// RecordMissionRefusal turns an accept_mission refusal into a stored binding:
// parse the display name, resolve it to an id, then write. Reports
// (recorded=false, nil) when the message is not an only-available-at refusal,
// so a caller can hand it every error without filtering first.
func (kb *SQLiteKB) RecordMissionRefusal(ctx context.Context, missionID, message string, tick int64) (bool, error) {
	stationName := ParseMissionOnlyAvailableAt(message)
	if stationName == "" {
		return false, nil
	}
	baseID, systemID, err := kb.ResolveStationName(ctx, stationName)
	if err != nil {
		return false, err
	}
	if err := kb.SetMissionExclusiveBaseInSystem(
		ctx, missionID, baseID, systemID, "accept_mission_refusal", tick,
	); err != nil {
		return false, err
	}
	return true, nil
}
