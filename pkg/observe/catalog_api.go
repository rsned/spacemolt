package observe

import (
	"net/http"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// CatalogItemJSON is the JSON representation of a catalog item for API responses.
type CatalogItemJSON struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Rarity      string `json:"rarity"`
	Size        int    `json:"size"`
	BaseValue   int    `json:"base_value"`
	Stackable   bool   `json:"stackable"`
	Tradeable   bool   `json:"tradeable"`
}

// HandleGetCatalogItems returns all catalog items from the knowledge base.
// GET /api/catalog/items
func (s *ObserverServer) HandleGetCatalogItems(w http.ResponseWriter, r *http.Request) {
	items, err := s.kb.GetItems(r.Context())
	if err != nil {
		s.logger.Printf("error getting catalog items: %v", err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}
	result := make([]CatalogItemJSON, 0, len(items))
	for _, item := range items {
		result = append(result, catalogItemToJSON(item))
	}
	WriteJSON(w, http.StatusOK, result)
}

func catalogItemToJSON(item knowledge.CatalogItem) CatalogItemJSON {
	return CatalogItemJSON{
		ID:          item.ID,
		Name:        item.Name,
		Description: item.Description,
		Category:    item.Category,
		Rarity:      item.Rarity,
		Size:        item.Size,
		BaseValue:   item.BaseValue,
		Stackable:   item.Stackable,
		Tradeable:   item.Tradeable,
	}
}
