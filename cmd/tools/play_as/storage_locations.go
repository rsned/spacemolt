package main

import (
	"fmt"
	"slices"
	"strings"
)

// storageLocation is one station in the galaxy-wide storage index that
// view_storage returns when the agent is NOT docked.
type storageLocation struct {
	BaseID     string `json:"base_id"`
	BaseName   string `json:"base_name"`
	ItemCount  int64  `json:"item_count"`
	ShipCount  int64  `json:"ship_count"`
	System     string `json:"system"`
	SystemName string `json:"system_name"`
}

// writeStorageLocations renders the undocked storage index: one row per station
// holding anything, biggest first.
//
// This variant carries an EMPTY base_id with empty items and ships, which is
// also exactly what a "you must be docked to view storage" error looks like.
// The frames are told apart by the presence of locations, so the index is
// formatted and the error still falls through to the raw frame.
//
// base_id is rendered as its own column rather than folded into the name,
// because it is the argument you pass back as station_id to read one station's
// contents -- and some are opaque hashes with a display name that looks nothing
// like them ("The Veil Anchor" / 59b10227...).
func writeStorageLocations(b *strings.Builder, locs []storageLocation) {
	sorted := slices.Clone(locs)
	slices.SortFunc(sorted, func(x, y storageLocation) int {
		if x.ItemCount != y.ItemCount {
			if x.ItemCount > y.ItemCount {
				return -1
			}

			return 1
		}

		return strings.Compare(x.BaseID, y.BaseID)
	})

	var totalItems, totalShips int64
	for _, l := range sorted {
		totalItems += l.ItemCount
		totalShips += l.ShipCount
	}

	fmt.Fprintf(b, "Storage across %d locations — %s items", len(sorted), commaInt(totalItems))
	if totalShips > 0 {
		fmt.Fprintf(b, ", %s ships", commaInt(totalShips))
	}
	b.WriteString("\n")

	nameW, sysW, itemW, shipW := len("STATION"), len("SYSTEM"), len("ITEMS"), len("SHIPS")
	for _, l := range sorted {
		nameW = max(nameW, len(l.BaseName))
		sysW = max(sysW, len(l.SystemName))
		itemW = max(itemW, len(commaInt(l.ItemCount)))
		shipW = max(shipW, len(commaInt(l.ShipCount)))
	}

	fmt.Fprintf(b, "  %-*s  %-*s  %*s  %*s  %s\n",
		nameW, "STATION", sysW, "SYSTEM", itemW, "ITEMS", shipW, "SHIPS", "STATION ID")
	for _, l := range sorted {
		ships := commaInt(l.ShipCount)
		if l.ShipCount == 0 {
			ships = "-"
		}
		fmt.Fprintf(b, "  %-*s  %-*s  %*s  %*s  %s\n",
			nameW, l.BaseName, sysW, l.SystemName,
			itemW, commaInt(l.ItemCount), shipW, ships, l.BaseID)
	}

	b.WriteString("\n  Dock, or pass a STATION ID, to read one station's contents.\n")
}
