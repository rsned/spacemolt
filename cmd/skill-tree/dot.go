package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// EscapeDotLabel escapes a string for use in a Graphviz label.
func EscapeDotLabel(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

// Slug returns a filesystem-safe name for the category.
func Slug(category string) string {
	return strings.ReplaceAll(strings.ToLower(category), " ", "_")
}

// EmitDOT writes a Graphviz DOT file for the category graph.
func EmitDOT(g *CategoryGraph, outDir string) (string, error) {
	filename := Slug(g.Category) + ".dot"
	path := filepath.Join(outDir, filename)
	f, err := os.Create(path)
	if err != nil {
		return "", fmt.Errorf("create %s: %w", path, err)
	}
	defer f.Close()

	// Header
	fmt.Fprintf(f, "digraph %q {\n", EscapeDotLabel(g.Category))
	fmt.Fprintf(f, "    rankdir=LR;\n")
	fmt.Fprintf(f, "    splines=polyline;\n")
	fmt.Fprintf(f, "    node [shape=box];\n\n")

	// Group nodes by tier for rank=same
	tierToNodes := make(map[int][]NodeInfo)
	for _, n := range g.Nodes {
		tierToNodes[n.Tier] = append(tierToNodes[n.Tier], n)
	}
	maxTier := 0
	for t := range tierToNodes {
		if t > maxTier {
			maxTier = t
		}
	}

	// Emit node definitions
	for _, n := range g.Nodes {
		label := EscapeDotLabel(n.Name)
		fmt.Fprintf(f, "    %q [label=%q];\n", n.ID, label)
	}
	fmt.Fprintln(f)

	// Emit rank=same for each tier (left-to-right flow)
	for tier := 0; tier <= maxTier; tier++ {
		nodes := tierToNodes[tier]
		if len(nodes) == 0 {
			continue
		}
		fmt.Fprintf(f, "    { rank=same;")
		for _, n := range nodes {
			fmt.Fprintf(f, " %q;", n.ID)
		}
		fmt.Fprintf(f, " }\n")
	}
	fmt.Fprintln(f)

	// Emit edges with level labels; cross-category edges use gray50
	for _, e := range g.Edges {
		label := strconv.Itoa(e.RequiredLevel)
		if e.CrossCategory {
			fmt.Fprintf(f, "    %q -> %q [label=%q color=gray50];\n", e.From, e.To, label)
		} else {
			fmt.Fprintf(f, "    %q -> %q [label=%q];\n", e.From, e.To, label)
		}
	}

	fmt.Fprintf(f, "}\n")
	return path, nil
}

// OrderedCategories returns sorted category names from the payload.
func OrderedCategories(payload *SkillsPayload) []string {
	seen := make(map[string]bool)
	for _, s := range payload.Skills {
		seen[s.Category] = true
	}
	var cats []string
	for c := range seen {
		cats = append(cats, c)
	}
	sort.Strings(cats)
	return cats
}
