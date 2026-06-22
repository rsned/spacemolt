package main

import (
	"net/http"
	"strconv"

	"github.com/rsned/spacemolt/pkg/market"
)

func (s *server) statsHandler(w http.ResponseWriter, r *http.Request) {
	stats, err := s.col.GetStats(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (s *server) matrixHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	mq := market.MatrixQuery{
		Category: q.Get("category"),
		Search:   q.Get("q"),
		Page:     page,
		Limit:    limit,
	}
	m, err := s.col.GetMatrix(r.Context(), mq)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *server) stationOrdersHandler(w http.ResponseWriter, r *http.Request) {
	stationID := r.PathValue("id")
	if stationID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing station id"})
		return
	}
	orders, err := s.col.GetStationOrders(r.Context(), stationID, r.URL.Query().Get("item"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if orders == nil {
		orders = []market.Order{}
	}
	writeJSON(w, http.StatusOK, orders)
}

func (s *server) itemHistoryHandler(w http.ResponseWriter, r *http.Request) {
	itemID := r.PathValue("id")
	if itemID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing item id"})
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	pts, err := s.col.GetItemPriceHistory(r.Context(), itemID, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if pts == nil {
		pts = []market.ItemPricePoint{}
	}
	writeJSON(w, http.StatusOK, pts)
}

func (s *server) capturesHandler(w http.ResponseWriter, r *http.Request) {
	health, err := s.col.GetCaptureHealth(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if health == nil {
		health = []market.StationCaptures{}
	}
	writeJSON(w, http.StatusOK, health)
}
