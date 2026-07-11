// Command craft-dashboard serves a live HTML dashboard for Executor B's
// dispatched craft plans: each plan's DAG rendered as a mermaid flowchart
// (done nodes faded grey, dispatched nodes bold/gold, parked nodes red) plus
// a per-item progress table. It re-reads every *.state.json file in
// --plans-dir on every request, so a browser refresh always reflects the
// latest tick the executor has written.
//
//	craft-dashboard --addr :8091 --plans-dir data/overmind/craft-plans --refresh 30
package main

import (
	"embed"
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/rsned/spacemolt/pkg/craftdash"
	"github.com/rsned/spacemolt/pkg/overmind/plans"
)

//go:embed assets/mermaid.min.js
var assetsFS embed.FS

func main() {
	addr := flag.String("addr", ":8091", "listen address for the dashboard")
	plansDir := flag.String("plans-dir", "data/overmind/craft-plans", "directory of *.state.json plan-run files")
	refresh := flag.Int("refresh", 30, "auto-refresh interval in seconds (0 disables)")
	flag.Parse()

	logger := log.New(log.Writer(), "[craft-dashboard] ", log.LstdFlags)

	http.Handle("/assets/", http.FileServer(http.FS(assetsFS)))

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		runs, errs := plans.LoadAllRuns(*plansDir)
		for _, e := range errs {
			logger.Printf("load error: %v", e)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if _, err := w.Write(craftdash.PageWithErrors(runs, errs, *refresh)); err != nil {
			logger.Printf("write response: %v", err)
		}
	})

	logger.Printf("serving craft plan dashboard on %s (plans-dir %s, refresh %ds)", *addr, *plansDir, *refresh)

	srv := &http.Server{
		Addr:              *addr,
		ReadHeaderTimeout: 5 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil {
		logger.Fatalf("server: %v", err)
	}
}
