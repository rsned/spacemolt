// Command overmind-status serves a live HTML status page for one or more
// overminds. It re-reads each overmind's status JSON file on every request and
// renders an alphabetical worker table per overmind (name, credits, fleet role,
// current task/status, last position), with a table of contents across all
// overminds at the top.
//
// The page auto-refreshes on a configurable interval (default 5 minutes) and a
// "Refresh now" button reloads immediately, so a manual refresh always reflects
// the newest snapshot the overminds have written (~every 30s).
//
//	overmind-status --addr :8087 --refresh 300 \
//	  --overmind "Haul=data/overmind/fleet-status.json" \
//	  --overmind "Marketbots=data/overmind/mb-status.json" \
//	  --overmind "Shuttle=data/overmind/shuttle-status.json"
//
// With no --overmind flags the three defaults above are used.
package main

import (
	"flag"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/ovstatus"
)

// sourceList is a repeatable "Name=path" flag accumulating overmind sources.
type sourceList []ovstatus.Source

func (s *sourceList) String() string {
	parts := make([]string, 0, len(*s))
	for _, src := range *s {
		parts = append(parts, src.Name+"="+src.Path)
	}
	return strings.Join(parts, ",")
}

func (s *sourceList) Set(v string) error {
	name, path, ok := strings.Cut(v, "=")
	name, path = strings.TrimSpace(name), strings.TrimSpace(path)
	if !ok || name == "" || path == "" {
		return errBadSource
	}
	*s = append(*s, ovstatus.Source{Name: name, Path: path})
	return nil
}

var errBadSource = flagError("overmind must be in the form Name=path")

type flagError string

func (e flagError) Error() string { return string(e) }

func defaultSources() []ovstatus.Source {
	return []ovstatus.Source{
		{Name: "Haul", Path: "data/overmind/fleet-status.json"},
		{Name: "Marketbots", Path: "data/overmind/mb-status.json"},
		{Name: "Shuttle", Path: "data/overmind/shuttle-status.json"},
	}
}

func main() {
	var sources sourceList
	addr := flag.String("addr", ":8087", "Listen address for the status page")
	refresh := flag.Int("refresh", 300, "Default auto-refresh interval in seconds (0 disables)")
	flag.Var(&sources, "overmind", "Overmind source as Name=path (repeatable); defaults to Haul/Marketbots/Shuttle")
	flag.Parse()

	if len(sources) == 0 {
		sources = defaultSources()
	}

	logger := log.New(log.Writer(), "[overmind-status] ", log.LstdFlags)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		// A ?refresh=N query overrides the default so the operator can watch
		// faster (or pause auto-refresh with ?refresh=0) without a restart.
		rf := *refresh
		if q := r.URL.Query().Get("refresh"); q != "" {
			if n, err := strconv.Atoi(q); err == nil && n >= 0 {
				rf = n
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if _, err := w.Write([]byte(ovstatus.Render(sources, rf, time.Now()))); err != nil {
			logger.Printf("write response: %v", err)
		}
	})

	names := make([]string, 0, len(sources))
	for _, s := range sources {
		names = append(names, s.Name)
	}
	logger.Printf("serving status for [%s] on %s (refresh %ds)", strings.Join(names, ", "), *addr, *refresh)

	srv := &http.Server{
		Addr:              *addr,
		ReadHeaderTimeout: 5 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil {
		logger.Fatalf("server: %v", err)
	}
}
