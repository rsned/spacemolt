package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rsned/spacemolt/pkg/market"
)

func newTestServer(t *testing.T) (*server, *market.Collector) {
	t.Helper()
	c, err := market.Open(market.Config{DBPath: t.TempDir() + "/test.db"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return &server{col: c}, c
}

func TestStatsHandler(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	rec := httptest.NewRecorder()
	srv.statsHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var stats market.Stats
	if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if stats.StationCount != 0 {
		t.Errorf("station_count = %d, want 0", stats.StationCount)
	}
}

func TestMatrixHandlerEmpty(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/matrix", nil)
	rec := httptest.NewRecorder()
	srv.matrixHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var m market.Matrix
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m.TotalItems != 0 || len(m.Items) != 0 {
		t.Errorf("expected empty matrix, got %+v", m)
	}
}
