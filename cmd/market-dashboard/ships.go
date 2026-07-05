package main

import (
	"database/sql"
	"net/http"
)

// shipClassSummary is one row of the Ships tab: a hull class aggregated
// across every station snapshot in the knowledge base.
type shipClassSummary struct {
	ClassID             string `json:"class_id"`
	ShipName            string `json:"ship_name"`
	Category            string `json:"category"`
	Tier                int    `json:"tier"`
	ListingCount        int    `json:"listing_count"`
	MinPrice            int    `json:"min_price"`
	MaxPrice            int    `json:"max_price"`
	StationCount        int    `json:"station_count"`
	CheapestStationID   string `json:"cheapest_station_id"`
	CheapestStationName string `json:"cheapest_station_name"`
	CapturedAt          string `json:"captured_at"`
}

// shipListingRow is one (station, price, config, seller) group in the
// per-class drill-down; Qty is how many identical hulls are for sale.
type shipListingRow struct {
	StationID    string `json:"station_id"`
	StationName  string `json:"station_name"`
	SystemID     string `json:"system_id"`
	SystemName   string `json:"system_name"`
	Price        int    `json:"price"`
	Qty          int    `json:"qty"`
	Hull         int    `json:"hull"` // -1 = not reported
	MaxHull      int    `json:"max_hull"`
	Shield       int    `json:"shield"`
	ModulesCount int    `json:"modules_count"`
	Tier         int    `json:"tier"`
	Seller       string `json:"seller"`
	CapturedAt   string `json:"captured_at"`
}

func (s *server) shipsHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := s.kb.QueryContext(r.Context(), `
		SELECT sl.class_id,
			COALESCE(MAX(sl.ship_name), '')  AS ship_name,
			COALESCE(MAX(sl.category), '')   AS category,
			COALESCE(MAX(sl.tier), 0)        AS tier,
			COUNT(*)                         AS listing_count,
			MIN(sl.price)                    AS min_price,
			MAX(sl.price)                    AS max_price,
			COUNT(DISTINCT sl.station_id)    AS station_count,
			(SELECT s2.station_id FROM ship_listings s2
				WHERE s2.class_id = sl.class_id ORDER BY s2.price, s2.station_id LIMIT 1),
			(SELECT s3.station_name FROM ship_listings s3
				WHERE s3.class_id = sl.class_id ORDER BY s3.price, s3.station_id LIMIT 1),
			MAX(sl.captured_at)              AS captured_at
		FROM ship_listings sl
		GROUP BY sl.class_id
		ORDER BY listing_count DESC, sl.class_id
	`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer func() { _ = rows.Close() }()

	summaries := []shipClassSummary{}
	for rows.Next() {
		var c shipClassSummary
		var cheapID, cheapName sql.NullString
		if err := rows.Scan(&c.ClassID, &c.ShipName, &c.Category, &c.Tier,
			&c.ListingCount, &c.MinPrice, &c.MaxPrice, &c.StationCount,
			&cheapID, &cheapName, &c.CapturedAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		c.CheapestStationID = cheapID.String
		c.CheapestStationName = cheapName.String
		summaries = append(summaries, c)
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, summaries)
}

func (s *server) shipClassHandler(w http.ResponseWriter, r *http.Request) {
	classID := r.PathValue("id")
	if classID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing class id"})
		return
	}
	rows, err := s.kb.QueryContext(r.Context(), `
		SELECT station_id, station_name, system_id, system_name, price,
			COUNT(*) AS qty,
			COALESCE(hull, -1) AS hull, COALESCE(max_hull, 0) AS max_hull,
			COALESCE(shield, 0) AS shield,
			COALESCE(modules_count, 0) AS modules_count, COALESCE(tier, 0) AS tier,
			COALESCE(seller, '') AS seller, MAX(captured_at)
		FROM ship_listings
		WHERE class_id = ?
		GROUP BY station_id, station_name, system_id, system_name, price,
			hull, max_hull, shield, modules_count, tier, seller
		ORDER BY price, station_id
	`, classID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer func() { _ = rows.Close() }()

	listings := []shipListingRow{}
	for rows.Next() {
		var l shipListingRow
		if err := rows.Scan(&l.StationID, &l.StationName, &l.SystemID, &l.SystemName,
			&l.Price, &l.Qty, &l.Hull, &l.MaxHull, &l.Shield, &l.ModulesCount, &l.Tier,
			&l.Seller, &l.CapturedAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		listings = append(listings, l)
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, listings)
}
