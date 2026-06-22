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
