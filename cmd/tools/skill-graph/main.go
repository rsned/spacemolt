// Command skill-graph reads a skill YAML file and outputs a DOT digraph
// representation suitable for rendering with Graphviz.
//
// Usage:
//
//	go run cmd/tools/skill-graph/main.go [flags] <skill.yaml>
//
// Flags:
//
//	-o <file>          output file (default: stdout)
//	-skills-dir <dir>  directory of skill YAMLs for sub-skill resolution
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rsned/spacemolt/pkg/skills"
)

func main() {
	outputFile := flag.String("o", "", "output file (default: stdout)")
	skillsDir := flag.String("skills-dir", "", "directory of skill YAMLs for sub-skill resolution")
	flag.Parse()

	if flag.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: skill-graph [flags] <skill.yaml>")
		os.Exit(1)
	}

	skillPath := flag.Arg(0)

	skill, err := skills.LoadSkill(skillPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading skill %s: %v\n", skillPath, err)
		os.Exit(1)
	}

	// Determine the skills directory for registry loading.
	dir := *skillsDir
	if dir == "" {
		dir = filepath.Dir(skillPath)
	}

	// Attempt to load a registry for sub-skill resolution.
	var dot string
	registry, err := skills.LoadRegistry(dir)
	if err != nil {
		// If registry loading fails, fall back to no registry.
		fmt.Fprintf(os.Stderr, "warning: could not load skills registry from %s: %v\n", dir, err)
		dot = skills.GenerateDOT(skill)
	} else {
		dot = skills.GenerateDOTWithRegistry(skill, registry)
	}

	if *outputFile != "" {
		if err := os.WriteFile(*outputFile, []byte(dot), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "error writing output file: %v\n", err)
			os.Exit(1)
		}
	} else {
		fmt.Print(dot)
	}
}
