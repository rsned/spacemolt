package main

import (
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"github.com/rsned/spacemolt/pkg/assets"
	"github.com/rsned/spacemolt/pkg/stationstate"
)

//go:embed web
var webFS embed.FS

const (
	stationName  = "Conjunction Junction"
	defaultHours = 48
	maxHours     = 168
)

// server answers the console's JSON and model-file requests.
type server struct {
	store     *assets.Store
	baseID    string
	modelsDir string
	now       func() time.Time
}

// stateResponse is Status (flattened, keys are the Go field names) plus the
// station extras the popups show.
type stateResponse struct {
	stationstate.Status
	Station        string                    `json:"station"`
	Passengers     *assets.PassengerSnapshot `json:"passengers"`
	TourismPerHour *float64                  `json:"tourism_per_hour"`
	Base           assets.BaseSnapshot       `json:"base"`
}

func (s *server) routes() http.Handler {
	web, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatalf("embed web: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, web, "index.html")
	})
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(web)))
	mux.HandleFunc("GET /api/state", s.handleState)
	mux.HandleFunc("GET /api/history", s.handleHistory)
	mux.HandleFunc("GET /api/models", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, stationModels) })
	mux.HandleFunc("GET /models/{id}/mesh.glb", s.modelFile("mesh.glb", "model/gltf-binary"))
	mux.HandleFunc("GET /models/{id}/keyed.png", s.modelFile("keyed.png", "image/png"))
	mux.HandleFunc("GET /zones/{file}", func(w http.ResponseWriter, r *http.Request) {
		// Only {id}.json, and only ids that are bare names, are looked up.
		file := r.PathValue("file")
		if filepath.Base(file) != file || filepath.Ext(file) != ".json" {
			http.NotFound(w, r)
			return
		}
		data, err := fs.ReadFile(web, "zones/"+file)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	})
	return mux
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("write json: %v", err)
	}
}

func (s *server) handleState(w http.ResponseWriter, r *http.Request) {
	reading, ok, err := s.store.LatestStation(r.Context(), s.baseID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, stateResponse{
		Status:         stationstate.Evaluate(reading, ok, s.now()),
		Station:        stationName,
		Passengers:     reading.Passengers,
		TourismPerHour: reading.TourismPerHour,
		Base:           reading.Base,
	})
}

func (s *server) handleHistory(w http.ResponseWriter, r *http.Request) {
	hours := defaultHours
	if v := r.URL.Query().Get("hours"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			http.Error(w, "hours must be an integer", http.StatusBadRequest)
			return
		}
		hours = max(1, min(n, maxHours))
	}
	pts, err := s.store.StationHistory(r.Context(), s.baseID, s.now().Add(-time.Duration(hours)*time.Hour))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if pts == nil {
		pts = []assets.HistoryPoint{}
	}
	writeJSON(w, pts)
}

// modelFile serves one file of a listed model; unlisted ids are 404.
func (s *server) modelFile(name, contentType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m, ok := findModel(r.PathValue("id"))
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", contentType)
		http.ServeFile(w, r, filepath.Join(s.modelsDir, m.ID, name))
	}
}
