// json-diff normalizes two JSON files so they can be meaningfully diffed.
//
// It recursively sorts object keys and sorts arrays of objects by a
// detected identity field (tries "id", "system_id", "name", etc.).
//
// Usage:
//
//	go run ./cmd/json-diff file1.json file2.json
//	go run ./cmd/json-diff -key system_id file1.json file2.json
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// candidateKeys are tried in order when sorting arrays of objects.
var candidateKeys = []string{
	"id", "system_id", "item_id", "poi_id", "base_id",
	"name", "key", "type", "resource_id",
}

var forceKey = flag.String("key", "", "force this field as the sort key for arrays of objects")

func main() {
	flag.Parse()
	args := flag.Args()
	if len(args) != 2 {
		fmt.Fprintf(os.Stderr, "Usage: json-diff [-key field] file1.json file2.json\n")
		os.Exit(1)
	}

	a, err := loadAndNormalize(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading %s: %v\n", args[0], err)
		os.Exit(1)
	}
	b, err := loadAndNormalize(args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading %s: %v\n", args[1], err)
		os.Exit(1)
	}

	dir, err := os.MkdirTemp("", "json-diff-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating temp dir: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	fileA := filepath.Join(dir, filepath.Base(args[0]))
	fileB := filepath.Join(dir, filepath.Base(args[1]))

	// If both basenames are the same, disambiguate.
	if filepath.Base(args[0]) == filepath.Base(args[1]) {
		fileA = filepath.Join(dir, "a_"+filepath.Base(args[0]))
		fileB = filepath.Join(dir, "b_"+filepath.Base(args[1]))
	}

	if err := os.WriteFile(fileA, a, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "error writing %s: %v\n", fileA, err)
		os.Exit(1)
	}
	if err := os.WriteFile(fileB, b, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "error writing %s: %v\n", fileB, err)
		os.Exit(1)
	}

	// Try diff with color; fall back to plain diff.
	cmd := exec.Command("diff", "-u", "--color=auto",
		"--label", args[0], "--label", args[1],
		fileA, fileB)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Run() // exit code 1 means differences found, which is expected
}

func loadAndNormalize(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	normalized := normalize(v)
	out, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return nil, err
	}
	out = append(out, '\n')
	return out, nil
}

// normalize recursively sorts objects by key and arrays of objects by a
// detected identity field.
func normalize(v any) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, child := range val {
			out[k] = normalize(child)
		}
		return out

	case []any:
		// Normalize children first.
		normed := make([]any, len(val))
		for i, child := range val {
			normed[i] = normalize(child)
		}
		// If all elements are objects, sort by detected key.
		if len(normed) > 0 && allObjects(normed) {
			key := detectSortKey(normed)
			if key != "" {
				slices.SortFunc(normed, func(a, b any) int {
					ka := sortValue(a.(map[string]any)[key])
					kb := sortValue(b.(map[string]any)[key])
					return strings.Compare(ka, kb)
				})
			}
		}
		// If all elements are scalars (strings/numbers), sort them too.
		if len(normed) > 0 && allScalars(normed) {
			slices.SortFunc(normed, func(a, b any) int {
				return strings.Compare(sortValue(a), sortValue(b))
			})
		}
		return normed

	default:
		return v
	}
}

func allObjects(arr []any) bool {
	for _, v := range arr {
		if _, ok := v.(map[string]any); !ok {
			return false
		}
	}
	return true
}

func allScalars(arr []any) bool {
	for _, v := range arr {
		switch v.(type) {
		case string, float64, bool, json.Number:
			// ok
		default:
			return false
		}
	}
	return true
}

// detectSortKey picks the best field to sort an array of objects by.
func detectSortKey(arr []any) string {
	if *forceKey != "" {
		return *forceKey
	}
	first := arr[0].(map[string]any)
	for _, k := range candidateKeys {
		if _, ok := first[k]; ok {
			return k
		}
	}
	// Fall back: first string-valued field alphabetically.
	keys := make([]string, 0, len(first))
	for k := range first {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		if _, ok := first[k].(string); ok {
			return k
		}
	}
	return ""
}

func sortValue(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case float64:
		return fmt.Sprintf("%020.6f", val)
	case bool:
		if val {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", v)
	}
}
