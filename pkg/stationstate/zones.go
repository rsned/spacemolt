package stationstate

import "strings"

// Zone is one region of the station console.
type Zone string

// The console zones, in display order.
const (
	Core       Zone = "core"
	Life       Zone = "life"
	Fuel       Zone = "fuel"
	Production Zone = "production"
	Tourism    Zone = "tourism"
	Shipyard   Zone = "shipyard"
	Defense    Zone = "defense"
	Commerce   Zone = "commerce"
)

// Zones lists every zone in display order.
var Zones = []Zone{Core, Life, Fuel, Production, Tourism, Shipyard, Defense, Commerce}

// Label is the zone's console heading.
func (z Zone) Label() string {
	switch z {
	case Core:
		return "CORE · POWER"
	case Life:
		return "LIFE SUPPORT"
	case Fuel:
		return "FUEL"
	case Production:
		return "PRODUCTION"
	case Tourism:
		return "TOURISM"
	case Shipyard:
		return "SHIPYARD"
	case Defense:
		return "DEFENSE"
	case Commerce:
		return "COMMERCE · ADMIN"
	}
	return strings.ToUpper(string(z))
}

// ZoneOf places a facility in a console zone. Unknown types land in Commerce so
// a new facility is still shown (and its maintenance still counts).
func ZoneOf(facilityType, category string) Zone {
	t := facilityType
	switch {
	case t == "point_defense_battery":
		return Defense
	case t == "stellar_collector_grid", t == "bunker_fed_reactor",
		strings.Contains(t, "reactor"), strings.Contains(t, "battery"):
		return Core
	case t == "atmospheric_recycler", strings.HasPrefix(t, "hydroponic"), strings.Contains(t, "plants"):
		return Life
	case t == "fuel_tank_expansion", t == "refueling_dock", t == h2Combustor:
		return Fuel
	case t == "fabrication_yard", t == "faction_ship_garage":
		return Shipyard
	case strings.HasSuffix(t, "_lounge"), strings.Contains(t, "dining"):
		return Tourism
	case category == "defense":
		return Defense
	case category == "production":
		return Production
	}
	return Commerce
}

const h2Combustor = "h2_fuel_combustor"
