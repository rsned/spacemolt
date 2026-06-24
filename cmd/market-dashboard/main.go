// Command market-dashboard serves a read-only web dashboard over data/market.db
// for validating market collection. See docs/superpowers/specs/2026-06-22-market-dashboard-design.md.
package main

import (
	"embed"
	"encoding/json"
	"flag"
	"io/fs"
	"log"
	"net/http"

	"github.com/rsned/spacemolt/pkg/market"
)

//go:embed all:web
var webFS embed.FS

func main() {
	addr := flag.String("addr", ":8090", "HTTP listen address")
	dbPath := flag.String("market-db-path", "data/market.db", "Path to the market SQLite database")
	flag.Parse()

	collector, err := market.Open(market.Config{DBPath: *dbPath})
	if err != nil {
		log.Fatalf("market-dashboard: open %s: %v", *dbPath, err)
	}
	defer func() { _ = collector.Close() }()

	srv := &server{col: collector}

	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatalf("market-dashboard: embed sub: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/stats", srv.statsHandler)
	mux.HandleFunc("GET /api/matrix", srv.matrixHandler)
	mux.HandleFunc("GET /api/station/{id}/orders", srv.stationOrdersHandler)
	mux.HandleFunc("GET /api/item/{id}/history", srv.itemHistoryHandler)
	mux.HandleFunc("GET /api/captures", srv.capturesHandler)
	mux.HandleFunc("GET /api/opportunities", srv.opportunitiesHandler)
	mux.Handle("GET /", http.FileServer(http.FS(sub)))

	log.Printf("market-dashboard: serving %s on %s", *dbPath, *addr)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatalf("market-dashboard: %v", err)
	}
}

// server holds shared dependencies for HTTP handlers.
type server struct {
	col *market.Collector
}

// writeJSON encodes v as JSON with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
