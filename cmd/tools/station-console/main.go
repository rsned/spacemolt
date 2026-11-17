// Command station-console serves the station hologram console for our
// stations (consoleStations): status JSON from the shared assets DB, plus the
// 3D model files. The page switches stations with ?station=<base id>.
//
//	station-console --addr :8094
package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/assets"
)

func main() {
	addr := flag.String("addr", ":8094", "listen address")
	dbPath := flag.String("db", "data/assets.db", "assets database")
	models := flag.String("models", "~/spacemolt/kb/data/mesh_bakeoff/out-stations", "directory of station model folders")
	base := flag.String("base", "c976fa7df53c87f9616165d7d411b41a", "default station base id (one of consoleStations)")
	flag.Parse()
	if _, ok := findStation(*base); !ok {
		log.Fatalf("--base %s is not a console station", *base)
	}

	cfg := assets.DefaultConfig()
	cfg.DBPath = *dbPath
	store, err := assets.Open(cfg)
	if err != nil {
		log.Fatalf("open assets db %s: %v", *dbPath, err)
	}
	defer func() { _ = store.Close() }()

	s := &server{store: store, baseID: *base, modelsDir: expandHome(*models), now: time.Now}
	log.Printf("station-console listening on %s", *addr)
	srv := &http.Server{Addr: *addr, Handler: s.routes(), ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(srv.ListenAndServe())
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}
