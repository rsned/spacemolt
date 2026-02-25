// skill-tree reads skills.json and generates Graphviz DOT files (one per category)
// showing the skill dependency tree. Left-to-right flow: root skills on the left,
// dependents on the right. Cross-category dependencies use gray edges.
//
// Usage:
//
//	skill-tree -input skills.json -out ./diagrams
//	dot -Tsvg -o Engineering.svg diagrams/engineering.dot
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
)

func main() {
	input := flag.String("input", "skills.json", "Path to skills.json")
	outDir := flag.String("out", ".", "Output directory for DOT files")
	flag.Parse()

	payload, err := LoadSkills(*input)
	if err != nil {
		log.Fatalf("load skills: %v", err)
	}

	if err := os.MkdirAll(*outDir, 0755); err != nil {
		log.Fatalf("create output dir: %v", err)
	}

	categories := OrderedCategories(payload)
	written := 0
	for _, cat := range categories {
		g, err := BuildCategoryGraph(payload, cat)
		if err != nil {
			log.Printf("skip %q: %v", cat, err)
			continue
		}
		if g == nil {
			continue
		}
		path, err := EmitDOT(g, *outDir)
		if err != nil {
			log.Fatalf("emit DOT for %q: %v", cat, err)
		}
		fmt.Printf("wrote %s\n", path)
		written++
	}
	fmt.Printf("\nGenerated %d DOT files in %s\n", written, filepath.Clean(*outDir))
	fmt.Println("Convert to SVG: dot -Tsvg -o Engineering.svg engineering.dot")
}
