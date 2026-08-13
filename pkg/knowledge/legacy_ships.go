package knowledge

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

// legacyShipClassesJSON holds ship classes the server no longer publishes in its
// catalog but that agents still fly. Recovered from dated pulls under data/game-api,
// where the server itself tagged them `"legacy": true`.
//
// They matter because StoreShipClasses is a full replace — DELETE then INSERT — so a
// class absent from the live catalog is erased on every refresh. That silently left
// 261 of the fleet's 413 hulls (63%, and the four most-flown classes: prospector,
// drillship, excavator, deeprock_harvester) with no catalog row at all, so nothing
// could compute their jump fuel (ceil(scale^1.5 x speed)), their jump time, or their
// bare-hull cargo.
//
//go:embed catalogdata/legacy_ship_classes.json
var legacyShipClassesJSON []byte

// LegacyShipClasses returns the recovered legacy classes. Callers get a fresh slice
// each call, so a mutation cannot poison the embedded set.
func LegacyShipClasses() ([]ShipClassDef, error) {
	var out []ShipClassDef
	if err := json.Unmarshal(legacyShipClassesJSON, &out); err != nil {
		return nil, fmt.Errorf("decode legacy ship classes: %w", err)
	}
	return out, nil
}

// withLegacyShipClasses appends any legacy class the live catalog does not carry.
// A class the server has started publishing again wins: the live definition is
// authoritative and the recovered copy is only a fallback for what is missing.
func withLegacyShipClasses(classes []ShipClassDef) ([]ShipClassDef, error) {
	legacy, err := LegacyShipClasses()
	if err != nil {
		return nil, err
	}
	have := make(map[string]bool, len(classes))
	for _, c := range classes {
		have[c.ID] = true
	}
	for _, c := range legacy {
		if !have[c.ID] {
			classes = append(classes, c)
		}
	}
	return classes, nil
}
