// Action Visualizer — interactive web tool for exploring the game action space.
// Run with: go run ./cmd/tools/action-visualizer/
// With OpenAPI spec: go run ./cmd/tools/action-visualizer/ -spec server_docs/openapi.json
package main

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os/exec"
	"runtime"

	"github.com/rsned/spacemolt/pkg/actionspace"
)

//go:embed static/*
var staticFiles embed.FS

var registry *actionspace.Registry

func main() {
	specPath := flag.String("spec", "", "path to OpenAPI spec (e.g. server_docs/openapi.json)")
	addr := flag.String("addr", "localhost:8077", "listen address")
	flag.Parse()

	// Load registry.
	if *specPath != "" {
		var err error
		registry, err = actionspace.LoadFromOpenAPI(*specPath)
		if err != nil {
			log.Fatalf("failed to load OpenAPI spec: %v", err)
		}
		registry.LogWarnings()
		log.Printf("Loaded %d actions from %s", len(registry.Actions), *specPath)
	} else {
		registry = actionspace.DefaultRegistry()
		log.Printf("Using default registry (%d actions)", len(registry.Actions))
	}

	// Serve static files from embedded FS.
	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Fatalf("failed to create sub FS: %v", err)
	}
	http.Handle("/", http.FileServer(http.FS(staticFS)))

	// API endpoints.
	http.HandleFunc("POST /api/evaluate", handleEvaluate)
	http.HandleFunc("GET /api/actions", handleActions)

	fmt.Printf("Action Visualizer running at http://%s\n", *addr)
	openBrowser("http://" + *addr)

	if err := http.ListenAndServe(*addr, nil); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}

func handleEvaluate(w http.ResponseWriter, r *http.Request) {
	var gc actionspace.GameContext
	if err := json.NewDecoder(r.Body).Decode(&gc); err != nil {
		http.Error(w, fmt.Sprintf("invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	as := registry.Evaluate(gc)

	// Build tree structure for D3.
	tree := buildTree(as)

	resp := evaluateResponse{
		Tree:  tree,
		Stats: as.Stats,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func handleActions(w http.ResponseWriter, _ *http.Request) {
	type actionInfo struct {
		Name     string `json:"name"`
		Summary  string `json:"summary"`
		Category string `json:"category"`
	}

	actions := make([]actionInfo, len(registry.Actions))
	for i, a := range registry.Actions {
		actions[i] = actionInfo{
			Name:     a.Name,
			Summary:  a.Summary,
			Category: a.Category,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(actions)
}

// evaluateResponse is the JSON response for /api/evaluate.
type evaluateResponse struct {
	Tree  treeNode          `json:"tree"`
	Stats actionspace.Stats `json:"stats"`
}

// treeNode represents a node in the D3 hierarchy.
type treeNode struct {
	Name           string     `json:"name"`
	Category       string     `json:"category,omitempty"`
	Summary        string     `json:"summary,omitempty"`
	Valid          *bool      `json:"valid,omitempty"`
	FailedChecks   []string   `json:"failedChecks,omitempty"`
	Targets        []string   `json:"targets,omitempty"`
	BranchingCount int        `json:"branchingCount,omitempty"`
	ValidCount     int        `json:"validCount,omitempty"`
	TotalCount     int        `json:"totalCount,omitempty"`
	Children       []treeNode `json:"children,omitempty"`
}

func buildTree(as actionspace.ActionSpace) treeNode {
	// Group actions by category.
	categories := make(map[string][]actionspace.ActionResult)
	categoryOrder := make([]string, 0)
	for _, r := range as.Actions {
		cat := r.Action.Category
		if _, exists := categories[cat]; !exists {
			categoryOrder = append(categoryOrder, cat)
		}
		categories[cat] = append(categories[cat], r)
	}

	// Build root label.
	rootLabel := "In Space"
	if as.Context.Docked {
		rootLabel = "Docked"
	}
	if as.Context.InCombat || as.Context.InBattle {
		rootLabel = "In Combat"
	}
	if as.Context.InTransit {
		rootLabel = "In Transit"
	}

	root := treeNode{
		Name:     rootLabel,
		Children: make([]treeNode, 0, len(categoryOrder)),
	}

	for _, cat := range categoryOrder {
		actions := categories[cat]
		catNode := treeNode{
			Name:       cat,
			Category:   cat,
			TotalCount: len(actions),
			Children:   make([]treeNode, 0, len(actions)),
		}

		validCount := 0
		for _, r := range actions {
			valid := r.Valid
			leaf := treeNode{
				Name:           r.Action.Name,
				Category:       cat,
				Summary:        r.Action.Summary,
				Valid:          &valid,
				FailedChecks:   r.FailedChecks,
				Targets:        r.Targets,
				BranchingCount: r.BranchingCount,
			}
			catNode.Children = append(catNode.Children, leaf)
			if r.Valid {
				validCount++
			}
		}

		catNode.ValidCount = validCount
		root.Children = append(root.Children, catNode)
	}

	return root
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		return
	}
	_ = cmd.Start()
}
