package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// catalogBundle is the top-level shape of the unified catalog download
// (GET /api/catalog.json). Each section is kept as raw JSON so entries pass
// through to the split files byte-for-byte.
type catalogBundle struct {
	Version    string            `json:"version"`
	Ships      []json.RawMessage `json:"ships"`
	Skills     []json.RawMessage `json:"skills"`
	Recipes    []json.RawMessage `json:"recipes"`
	Items      []json.RawMessage `json:"items"`
	Modules    []json.RawMessage `json:"modules"`
	Facilities []json.RawMessage `json:"facilities"`
}

const (
	// catalogCacheFile holds the most recent raw catalog body, reused on a 304
	// (or when the network is unavailable / rate-limited) to split into the
	// current run's output directory without refetching.
	catalogCacheFile = ".catalog.json"
	// catalogETagFile holds the ETag of the cached body for If-None-Match.
	catalogETagFile = ".catalog.etag"
)

// scrapeCatalogBundle downloads the unified catalog (no auth/WebSocket needed),
// caches it with an ETag, and splits it into the legacy per-category envelope
// files that downstream importers and the kb site generator consume. It
// replaces the old per-command catalog pagination and the per-facility WS
// detail fetches.
func (s *Scraper) scrapeCatalogBundle() error {
	body, version, err := s.fetchCatalog(context.Background())
	if err != nil {
		return err
	}

	var bundle catalogBundle
	if err := json.Unmarshal(body, &bundle); err != nil {
		return fmt.Errorf("failed to parse catalog bundle: %w", err)
	}
	s.logger.Printf("  📦 Catalog v%s: %d ships, %d skills, %d recipes, %d items, %d modules, %d facilities",
		version, len(bundle.Ships), len(bundle.Skills), len(bundle.Recipes),
		len(bundle.Items), len(bundle.Modules), len(bundle.Facilities))

	// Items and modules are split apart in the unified catalog but the legacy
	// catalog_items.json (and import-catalog-items) expects them combined.
	combinedItems := make([]json.RawMessage, 0, len(bundle.Items)+len(bundle.Modules))
	combinedItems = append(combinedItems, bundle.Items...)
	combinedItems = append(combinedItems, bundle.Modules...)

	sections := []struct {
		typ      string
		filename string
		entries  []json.RawMessage
	}{
		{"ships", "catalog_ships.json", bundle.Ships},
		{"skills", "catalog_skills.json", bundle.Skills},
		{"recipes", "catalog_recipes.json", bundle.Recipes},
		{"items", "catalog_items.json", combinedItems},
		{"modules", "catalog_modules.json", bundle.Modules},
		{"facilities", "catalog_facilities.json", bundle.Facilities},
	}

	for _, sec := range sections {
		envelope := map[string]any{
			"items":       sec.entries,
			"page":        1,
			"page_size":   len(sec.entries),
			"total":       len(sec.entries),
			"total_pages": 1,
			"type":        sec.typ,
			"version":     version,
			"message":     fmt.Sprintf("%s: showing all %d items", sec.typ, len(sec.entries)),
		}
		out, err := json.MarshalIndent(envelope, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal %s envelope: %w", sec.typ, err)
		}
		if err := s.saveJSON(sec.filename, out); err != nil {
			return err
		}
	}

	return nil
}

// fetchCatalog performs the HTTP GET with ETag/If-None-Match handling and a
// local body cache. On a 304 (or recoverable network/rate-limit failure with a
// warm cache) it returns the cached body. It returns the raw body and the
// catalog's top-level version string.
func (s *Scraper) fetchCatalog(ctx context.Context) ([]byte, string, error) {
	cachePath := filepath.Join(baseOutputDir, catalogCacheFile)
	etagPath := filepath.Join(baseOutputDir, catalogETagFile)

	if err := os.MkdirAll(baseOutputDir, 0755); err != nil {
		return nil, "", fmt.Errorf("failed to create cache dir: %w", err)
	}

	cachedETag, _ := os.ReadFile(etagPath)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, game.DefaultCatalogURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to build catalog request: %w", err)
	}
	if len(cachedETag) > 0 {
		req.Header.Set("If-None-Match", string(cachedETag))
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		if body, verr := s.cachedCatalog(cachePath); verr == nil {
			s.logger.Printf("  ⚠️  Catalog fetch failed (%v); using cached copy", err)
			return body, catalogVersion(body), nil
		}
		return nil, "", fmt.Errorf("catalog fetch failed and no cache available: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusNotModified:
		body, verr := s.cachedCatalog(cachePath)
		if verr != nil {
			return nil, "", fmt.Errorf("server returned 304 but cache is missing: %w", verr)
		}
		s.logger.Printf("  ♻️  Catalog unchanged (304); using cached copy")
		return body, catalogVersion(body), nil
	case http.StatusOK:
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, "", fmt.Errorf("failed to read catalog body: %w", err)
		}
		if err := os.WriteFile(cachePath, body, 0644); err != nil {
			s.logger.Printf("  ⚠️  Failed to write catalog cache: %v", err)
		}
		if etag := resp.Header.Get("ETag"); etag != "" {
			if err := os.WriteFile(etagPath, []byte(etag), 0644); err != nil {
				s.logger.Printf("  ⚠️  Failed to write catalog etag: %v", err)
			}
		}
		return body, catalogVersion(body), nil
	default:
		// Rate-limited or other transient error: fall back to cache if warm.
		if body, verr := s.cachedCatalog(cachePath); verr == nil {
			s.logger.Printf("  ⚠️  Catalog fetch returned %s; using cached copy", resp.Status)
			return body, catalogVersion(body), nil
		}
		return nil, "", fmt.Errorf("catalog fetch returned %s and no cache available", resp.Status)
	}
}

// cachedCatalog reads the locally cached catalog body.
func (s *Scraper) cachedCatalog(cachePath string) ([]byte, error) {
	body, err := os.ReadFile(cachePath)
	if err != nil {
		return nil, err
	}
	return body, nil
}

// catalogVersion extracts the top-level version string from a catalog body,
// returning "unknown" if it cannot be parsed.
func catalogVersion(body []byte) string {
	var v struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(body, &v); err != nil || v.Version == "" {
		return "unknown"
	}
	return v.Version
}
