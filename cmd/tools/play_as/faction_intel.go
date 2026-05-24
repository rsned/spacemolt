package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// submitFactionIntel reads saved get_poi intel file(s) and submits them via
// faction_submit_intel. The path may be a single get_poi JSON file or a
// directory (walked for *.json); POIs are grouped by system into one
// submission. Files already in {"systems":[...]} intel format pass through.
func submitFactionIntel(client game.GameClient, ctx context.Context, path string, format outputFormat) error {
	resolved, err := resolveIntelPath(path)
	if err != nil {
		return err
	}
	files, err := collectIntelFiles(resolved)
	if err != nil {
		return err
	}

	systems, poiCount, warnings, err := buildIntelSystems(ctx, files)
	if err != nil {
		return err
	}
	for _, w := range warnings {
		fmt.Printf("  Warning: %s\n", w)
	}
	if len(systems) == 0 {
		return fmt.Errorf("no submittable intel found in %s", resolved)
	}

	if format == formatStyled {
		fmt.Printf("\nSubmitting intel: %d POI(s) across %d system(s) from %s\n",
			poiCount, len(systems), resolved)
		for _, sys := range systems {
			name, _ := sys["name"].(string)
			pois, _ := sys["pois"].([]map[string]any)
			fmt.Printf("  • %s (%s): %d POI(s)\n", name, sys["system_id"], len(pois))
		}
	}

	return simpleCommand(client, func(ctx context.Context) error {
		return client.FactionSubmitIntel(ctx, systems)
	}, ctx, 2*time.Second, "faction_submit_intel", format)
}

// resolveIntelPath returns the path as given if it exists, else the path
// relative to globalIntelDir if that exists.
func resolveIntelPath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("no path given")
	}
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	if globalIntelDir != "" {
		alt := filepath.Join(globalIntelDir, path)
		if _, err := os.Stat(alt); err == nil {
			return alt, nil
		}
		return "", fmt.Errorf("not found: %q (also tried %q)", path, alt)
	}
	return "", fmt.Errorf("not found: %q", path)
}

// collectIntelFiles returns the .json files at path: just the file if path is a
// file, or all *.json under it (sorted) if path is a directory.
func collectIntelFiles(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return []string{path}, nil
	}
	var files []string
	err = filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(strings.ToLower(d.Name()), ".json") {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no .json files under %s", path)
	}
	sort.Strings(files)
	return files, nil
}

// buildIntelSystems parses the given files and groups their POIs by system into
// the faction_submit_intel "systems" array. System metadata (name, description,
// empire, police_level, connections) is enriched from the knowledge base when
// available. Returns the systems array, the total POI count, and any per-file
// warnings (unreadable/unrecognized files are skipped, not fatal).
func buildIntelSystems(ctx context.Context, files []string) (systems []map[string]any, poiCount int, warnings []string, err error) {
	grouped := make(map[string]map[string]any)
	var order []string

	add := func(systemID string, poi map[string]any) {
		sys, ok := grouped[systemID]
		if !ok {
			sys = map[string]any{"system_id": systemID, "pois": []map[string]any{}}
			grouped[systemID] = sys
			order = append(order, systemID)
		}
		sys["pois"] = append(sys["pois"].([]map[string]any), poi)
		poiCount++
	}

	for _, f := range files {
		data, readErr := os.ReadFile(f)
		if readErr != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", f, readErr))
			continue
		}

		// Pass-through: file is already in {"systems":[...]} intel format.
		var probe struct {
			Systems json.RawMessage `json:"systems"`
		}
		if json.Unmarshal(data, &probe) == nil && len(probe.Systems) > 0 {
			var syss []map[string]any
			if jerr := json.Unmarshal(probe.Systems, &syss); jerr != nil {
				warnings = append(warnings, fmt.Sprintf("%s: bad systems array: %v", f, jerr))
				continue
			}
			for _, s := range syss {
				sid, _ := s["system_id"].(string)
				if sid == "" {
					warnings = append(warnings, fmt.Sprintf("%s: system entry missing system_id", f))
					continue
				}
				if pois, ok := s["pois"].([]any); ok {
					for _, p := range pois {
						if pm, ok := p.(map[string]any); ok {
							add(sid, pm)
						}
					}
				}
			}
			continue
		}

		// get_poi shape: {"poi":{...}, "resources":[...]}.
		var resp serverapi.GetPOIResponse
		if jerr := json.Unmarshal(data, &resp); jerr != nil || resp.POI.ID == "" {
			warnings = append(warnings, fmt.Sprintf("%s: not a get_poi or intel file", f))
			continue
		}
		if resp.POI.SystemID == "" {
			warnings = append(warnings, fmt.Sprintf("%s: poi %q missing system_id", f, resp.POI.ID))
			continue
		}
		add(resp.POI.SystemID, poiToIntelMap(resp.POI, resp.Resources))
	}

	systems = make([]map[string]any, 0, len(order))
	for _, sid := range order {
		sys := grouped[sid]
		enrichSystemMeta(ctx, sys, sid)
		systems = append(systems, sys)
	}
	return systems, poiCount, warnings, nil
}

// poiToIntelMap maps a get_poi POI (and its display resources) into the POI
// schema faction_submit_intel expects.
func poiToIntelMap(poi serverapi.POI, resources []serverapi.ResourceDisplay) map[string]any {
	m := map[string]any{
		"id":       poi.ID,
		"type":     poi.Type,
		"name":     poi.Name,
		"position": map[string]any{"x": poi.Position.X, "y": poi.Position.Y},
	}
	if poi.Description != "" {
		m["description"] = poi.Description
	}
	if poi.Class != "" {
		m["class"] = poi.Class
	}
	if poi.BaseID != "" {
		m["base_id"] = poi.BaseID
	}
	if poi.BaseName != "" {
		m["base_name"] = poi.BaseName
	}

	var res []map[string]any
	for _, r := range resources {
		res = append(res, map[string]any{
			"resource_id": r.ResourceID,
			"richness":    r.Richness,
			"remaining":   r.Remaining,
		})
	}
	if len(res) == 0 {
		for _, r := range poi.Resources {
			res = append(res, map[string]any{
				"resource_id": r.ResourceID,
				"richness":    r.Richness,
				"remaining":   r.Remaining,
			})
		}
	}
	if len(res) > 0 {
		m["resources"] = res
	}
	return m
}

// enrichSystemMeta fills in system-level fields from the knowledge base when
// they are not already present. Always ensures a non-empty "name" (falling back
// to the system id) since the server requires it.
func enrichSystemMeta(ctx context.Context, sys map[string]any, systemID string) {
	if globalKB != nil {
		if ks, err := globalKB.GetSystem(ctx, systemID); err == nil && ks != nil {
			if name, _ := sys["name"].(string); name == "" && ks.Name != "" {
				sys["name"] = ks.Name
			}
			if _, ok := sys["description"]; !ok && ks.Description != "" {
				sys["description"] = ks.Description
			}
			if _, ok := sys["empire"]; !ok && ks.Empire != "" {
				sys["empire"] = ks.Empire
			}
			if _, ok := sys["police_level"]; !ok && ks.PoliceLevel != 0 {
				sys["police_level"] = ks.PoliceLevel
			}
			if _, ok := sys["connections"]; !ok && len(ks.Connections) > 0 {
				conns := make([]map[string]any, 0, len(ks.Connections))
				for _, c := range ks.Connections {
					conns = append(conns, map[string]any{
						"system_id": c.SystemID,
						"distance":  c.Distance,
					})
				}
				sys["connections"] = conns
			}
		}
	}
	if name, _ := sys["name"].(string); name == "" {
		sys["name"] = systemID
	}
}
