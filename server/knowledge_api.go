package main

import (
	"net/http"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// systemJSON is the JSON representation of a system for API responses.
type systemJSON struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Position    game.Position `json:"position"`
	PoliceLevel int           `json:"police_level"`
	Faction     string        `json:"faction"`
	Connections []string      `json:"connections"`
}

// poiJSON is the JSON representation of a POI for API responses.
type poiJSON struct {
	ID        string             `json:"id"`
	Name      string             `json:"name"`
	Type      string             `json:"type"`
	Position  game.Position      `json:"position"`
	Resources []game.POIResource `json:"resources"`
}

// connectedSystemJSON is a minimal system reference for connection endpoints.
type connectedSystemJSON struct {
	ID       string        `json:"id"`
	Name     string        `json:"name"`
	Position game.Position `json:"position"`
}

// systemDetailResponse is the response shape for GET /api/systems/{id}.
type systemDetailResponse struct {
	System      systemJSON            `json:"system"`
	POIs        []poiJSON             `json:"pois"`
	Connections []connectedSystemJSON `json:"connections"`
}

func systemToJSON(sys knowledge.System) systemJSON {
	conns := sys.Connections
	if conns == nil {
		conns = []string{}
	}
	return systemJSON{
		ID:          sys.ID,
		Name:        sys.Name,
		Position:    sys.Position,
		PoliceLevel: sys.PoliceLevel,
		Faction:     sys.Faction,
		Connections: conns,
	}
}

func poiToJSON(poi knowledge.POI) poiJSON {
	resources := poi.Resources
	if resources == nil {
		resources = []game.POIResource{}
	}
	return poiJSON{
		ID:        poi.ID,
		Name:      poi.Name,
		Type:      poi.Type,
		Position:  poi.Position,
		Resources: resources,
	}
}

// HandleGetSystems returns all known systems for the galaxy map.
func (s *ObserverServer) HandleGetSystems(w http.ResponseWriter, _ *http.Request) {
	systems := s.kb.GetSystems()
	result := make([]systemJSON, 0, len(systems))
	for _, sys := range systems {
		result = append(result, systemToJSON(sys))
	}
	writeJSON(w, http.StatusOK, result)
}

// HandleGetSystem returns a single system with its POIs and connected system positions.
func (s *ObserverServer) HandleGetSystem(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, `{"error":"system id is required"}`, http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	sys, err := s.kb.GetSystem(ctx, id)
	if err != nil {
		s.logger.Printf("error getting system %s: %v", id, err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}
	if sys == nil {
		http.Error(w, `{"error":"system not found"}`, http.StatusNotFound)
		return
	}

	pois, err := s.kb.GetPOIs(ctx, id)
	if err != nil {
		s.logger.Printf("error getting POIs for system %s: %v", id, err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	// Build connected systems list by looking up each connection's position/name.
	allSystems := s.kb.GetSystems()
	systemIndex := make(map[string]knowledge.System, len(allSystems))
	for _, s := range allSystems {
		systemIndex[s.ID] = s
	}

	connections := make([]connectedSystemJSON, 0, len(sys.Connections))
	for _, connID := range sys.Connections {
		if connSys, ok := systemIndex[connID]; ok {
			connections = append(connections, connectedSystemJSON{
				ID:       connSys.ID,
				Name:     connSys.Name,
				Position: connSys.Position,
			})
		}
	}

	poisJSON := make([]poiJSON, 0, len(pois))
	for _, p := range pois {
		poisJSON = append(poisJSON, poiToJSON(p))
	}

	writeJSON(w, http.StatusOK, systemDetailResponse{
		System:      systemToJSON(*sys),
		POIs:        poisJSON,
		Connections: connections,
	})
}

// HandleGetSystemPOIs returns POIs for a system (lighter endpoint).
func (s *ObserverServer) HandleGetSystemPOIs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, `{"error":"system id is required"}`, http.StatusBadRequest)
		return
	}

	pois, err := s.kb.GetPOIs(r.Context(), id)
	if err != nil {
		s.logger.Printf("error getting POIs for system %s: %v", id, err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	result := make([]poiJSON, 0, len(pois))
	for _, p := range pois {
		result = append(result, poiToJSON(p))
	}
	writeJSON(w, http.StatusOK, result)
}
