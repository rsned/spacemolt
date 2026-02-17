package main

import (
	"net/http"
	"strings"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// systemJSON is the JSON representation of a system for API responses.
type systemJSON struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Position    game.Position `json:"position"`
	PoliceLevel int           `json:"police_level"`
	Empire      string        `json:"empire"`
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

// facilityJSON is the JSON representation of a facility for API responses.
type facilityJSON struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Category    string `json:"category"`
	Level       int    `json:"level"`
	LastUpdated int64  `json:"last_updated"`
}

// baseJSON is the JSON representation of a base for API responses.
type baseJSON struct {
	ID           string         `json:"id"`
	POIID        string         `json:"poi_id"`
	Name         string         `json:"name"`
	Description  string         `json:"description"`
	Empire       string         `json:"empire"`
	DefenseLevel int            `json:"defense_level"`
	HasDrones    bool           `json:"has_drones"`
	PublicAccess bool           `json:"public_access"`
	Services     map[string]bool `json:"services"`
	Facilities   []facilityJSON  `json:"facilities"`
}

// baseDetailResponse is the response shape for GET /api/bases/{id}.
type baseDetailResponse struct {
	Base baseJSON `json:"base"`
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
		Empire:      sys.Empire,
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
		// Try to find system by name as a fallback
		allSystems := s.kb.GetSystems()
		for _, s := range allSystems {
			if strings.EqualFold(s.Name, id) {
				sys = &s
				break
			}
		}
		if sys == nil {
			s.logger.Printf("system not found: %s", id)
			http.Error(w, `{"error":"system not found"}`, http.StatusNotFound)
			return
		}
	}

	// Use sys.ID for POI lookup (may differ from request id if found by name)
	pois, err := s.kb.GetPOIs(ctx, sys.ID)
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

// HandleGetBase returns a single base with its facilities.
// GET /api/bases/{id}
func (s *ObserverServer) HandleGetBase(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, `{"error":"base id is required"}`, http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	base, err := s.kb.GetBase(ctx, id)
	if err != nil {
		s.logger.Printf("error getting base %s: %v", id, err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}
	if base == nil {
		http.Error(w, `{"error":"base not found"}`, http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, baseDetailResponse{
		Base: baseToJSON(*base),
	})
}

// HandleGetBaseByPOI returns a base by its POI ID.
// GET /api/pois/{id}/base
func (s *ObserverServer) HandleGetBaseByPOI(w http.ResponseWriter, r *http.Request) {
	poiID := r.PathValue("id")
	if poiID == "" {
		http.Error(w, `{"error":"poi id is required"}`, http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	base, err := s.kb.GetBaseByPOI(ctx, poiID)
	if err != nil {
		s.logger.Printf("error getting base for poi %s: %v", poiID, err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}
	if base == nil {
		http.Error(w, `{"error":"base not found"}`, http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, baseDetailResponse{
		Base: baseToJSON(*base),
	})
}

func baseToJSON(base knowledge.SpaceBase) baseJSON {
	services := base.Services
	if services == nil {
		services = make(map[string]bool)
	}

	facilities := make([]facilityJSON, 0, len(base.Facilities))
	for _, f := range base.Facilities {
		facilities = append(facilities, facilityJSON{
			ID:          f.ID,
			Name:        f.Name,
			Category:    f.Category,
			Level:       f.Level,
			LastUpdated: f.LastUpdated,
		})
	}

	return baseJSON{
		ID:           base.ID,
		POIID:        base.POIID,
		Name:         base.Name,
		Description:  base.Description,
		Empire:       base.Empire,
		DefenseLevel: base.DefenseLevel,
		HasDrones:    base.HasDrones,
		PublicAccess: base.PublicAccess,
		Services:     services,
		Facilities:   facilities,
	}
}
