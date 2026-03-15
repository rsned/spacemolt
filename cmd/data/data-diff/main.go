package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"slices"
)

type DiffResult struct {
	Additions []string
	Deletions []string
	Changes   []Change
}

type Change struct {
	ID     string
	Field  string
	OldVal any
	NewVal any
}

func main() {
	var showChanges bool
	flag.BoolVar(&showChanges, "changes", false, "Show field-level changes (not just additions/deletions)")
	flag.BoolVar(&showChanges, "c", false, "Show field-level changes (shorthand)")

	flag.Usage = func() {
		fmt.Println("Usage: data-diff [options] <old-file> <new-file>")
		fmt.Println()
		fmt.Println("Compares two JSON files from data-scraper and reports differences.")
		fmt.Println()
		fmt.Println("Supports:")
		fmt.Println("  - catalog_*.json files (compares by item ID)")
		fmt.Println("  - get_map.json files (compares by system_id)")
		fmt.Println()
		fmt.Println("Options:")
		fmt.Println("  -changes, -c   Show field-level changes (not just additions/deletions)")
		fmt.Println()
		fmt.Println("Examples:")
		fmt.Println("  data-diff craftsman-1.prev/catalog_skills.json craftsman-1/catalog_skills.json")
		fmt.Println("  data-diff -changes craftsman-1.bak/get_map.json craftsman-1/get_map.json")
		fmt.Println("  data-diff data/game-api/craftsman-1.prev/catalog_ships.json \\")
		fmt.Println("            data/game-api/craftsman-1/catalog_ships.json")
		os.Exit(0)
	}

	flag.Parse()

	args := flag.Args()
	if len(args) != 2 {
		flag.Usage()
		os.Exit(1)
	}

	oldFile := args[0]
	newFile := args[1]

	logger := log.New(os.Stdout, "[DIFF] ", log.LstdFlags)

	logger.Printf("Comparing files:")
	logger.Printf("  OLD: %s", oldFile)
	logger.Printf("  NEW: %s\n", newFile)

	// Read both files
	oldData, err := os.ReadFile(oldFile)
	if err != nil {
		logger.Fatalf("Failed to read old file: %v", err)
	}

	newData, err := os.ReadFile(newFile)
	if err != nil {
		logger.Fatalf("Failed to read new file: %v", err)
	}

	// Detect file type and compare
	result, err := compareJSON(oldData, newData, showChanges)
	if err != nil {
		logger.Fatalf("Comparison failed: %v", err)
	}

	// Print results
	printResults(result, logger)
}

func compareJSON(oldData, newData []byte, showChanges bool) (*DiffResult, error) {
	// Try to detect the structure
	var oldMap, newMap map[string]any

	if err := json.Unmarshal(oldData, &oldMap); err != nil {
		return nil, fmt.Errorf("failed to parse old JSON: %w", err)
	}

	if err := json.Unmarshal(newData, &newMap); err != nil {
		return nil, fmt.Errorf("failed to parse new JSON: %w", err)
	}

	// Check for catalog format
	if _, hasItems := oldMap["items"]; hasItems {
		return compareCatalogs(oldMap, newMap, showChanges)
	}

	// Check for map format
	if _, hasSystems := oldMap["systems"]; hasSystems {
		return compareMaps(oldMap, newMap, showChanges)
	}

	return nil, fmt.Errorf("unknown JSON format - expected catalog (with 'items') or map (with 'systems')")
}

func compareCatalogs(oldMap, newMap map[string]any, showChanges bool) (*DiffResult, error) {
	oldItemsRaw, ok := oldMap["items"].([]any)
	if !ok {
		return nil, fmt.Errorf("old 'items' is not an array")
	}

	newItemsRaw, ok := newMap["items"].([]any)
	if !ok {
		return nil, fmt.Errorf("new 'items' is not an array")
	}

	// Convert to map by ID
	oldItems := make(map[string]map[string]any)
	for _, item := range oldItemsRaw {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id, ok := itemMap["id"].(string)
		if !ok {
			continue
		}
		oldItems[id] = itemMap
	}

	newItems := make(map[string]map[string]any)
	for _, item := range newItemsRaw {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id, ok := itemMap["id"].(string)
		if !ok {
			continue
		}
		newItems[id] = itemMap
	}

	result := &DiffResult{
		Additions: []string{},
		Deletions: []string{},
		Changes:   []Change{},
	}

	// Find deletions and changes
	for id, oldItem := range oldItems {
		newItem, exists := newItems[id]
		if !exists {
			name := getName(oldItem)
			result.Deletions = append(result.Deletions, fmt.Sprintf("%s (%s)", name, id))
			continue
		}

		if showChanges {
			// Check for field-level changes
			changes := findFieldChanges(id, oldItem, newItem)
			result.Changes = append(result.Changes, changes...)
		}
	}

	// Find additions
	for id, newItem := range newItems {
		if _, exists := oldItems[id]; !exists {
			name := getName(newItem)
			result.Additions = append(result.Additions, fmt.Sprintf("%s (%s)", name, id))
		}
	}

	// Sort results for consistent output
	slices.Sort(result.Additions)
	slices.Sort(result.Deletions)

	return result, nil
}

func compareMaps(oldMap, newMap map[string]any, showChanges bool) (*DiffResult, error) {
	oldSystemsRaw, ok := oldMap["systems"].([]any)
	if !ok {
		return nil, fmt.Errorf("old 'systems' is not an array")
	}

	newSystemsRaw, ok := newMap["systems"].([]any)
	if !ok {
		return nil, fmt.Errorf("new 'systems' is not an array")
	}

	// Convert to map by system_id
	oldSystems := make(map[string]map[string]any)
	for _, sys := range oldSystemsRaw {
		sysMap, ok := sys.(map[string]any)
		if !ok {
			continue
		}
		id, ok := sysMap["system_id"].(string)
		if !ok {
			continue
		}
		oldSystems[id] = sysMap
	}

	newSystems := make(map[string]map[string]any)
	for _, sys := range newSystemsRaw {
		sysMap, ok := sys.(map[string]any)
		if !ok {
			continue
		}
		id, ok := sysMap["system_id"].(string)
		if !ok {
			continue
		}
		newSystems[id] = sysMap
	}

	result := &DiffResult{
		Additions: []string{},
		Deletions: []string{},
		Changes:   []Change{},
	}

	// Find deletions
	for id, oldSys := range oldSystems {
		if _, exists := newSystems[id]; !exists {
			name := getSystemName(oldSys)
			result.Deletions = append(result.Deletions, fmt.Sprintf("%s (%s)", name, id))
			continue
		}

		if showChanges {
			newSys := newSystems[id]
			changes := findFieldChanges(id, oldSys, newSys)
			result.Changes = append(result.Changes, changes...)
		}
	}

	// Find additions
	for id, newSys := range newSystems {
		if _, exists := oldSystems[id]; !exists {
			name := getSystemName(newSys)
			result.Additions = append(result.Additions, fmt.Sprintf("%s (%s)", name, id))
		}
	}

	// Sort results
	slices.Sort(result.Additions)
	slices.Sort(result.Deletions)

	return result, nil
}

func findFieldChanges(id string, oldItem, newItem map[string]any) []Change {
	var changes []Change

	for key, newVal := range newItem {
		oldVal, exists := oldItem[key]
		if !exists {
			// Field added in new version
			changes = append(changes, Change{
				ID:     id,
				Field:  key,
				OldVal: nil,
				NewVal: newVal,
			})
			continue
		}

		// Normalize and compare values
		oldJSON := normalizeJSON(oldVal)
		newJSON := normalizeJSON(newVal)

		if oldJSON != newJSON {
			changes = append(changes, Change{
				ID:     id,
				Field:  key,
				OldVal: oldVal,
				NewVal: newVal,
			})
		}
	}

	// Check for fields dropped in new version
	for key, oldVal := range oldItem {
		if _, exists := newItem[key]; !exists {
			changes = append(changes, Change{
				ID:     id,
				Field:  key,
				OldVal: oldVal,
				NewVal: nil,
			})
		}
	}

	return changes
}

// normalizeJSON converts a value to a normalized JSON string for comparison
// It sorts object keys and array elements to ensure consistent ordering
func normalizeJSON(v any) string {
	normalized := normalizeValue(v)
	result, _ := json.Marshal(normalized)
	return string(result)
}

// normalizeValue recursively normalizes a value for consistent comparison
func normalizeValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		// Convert map to sorted key-value pairs
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		slices.Sort(keys)

		// Build new map with normalized values
		normalized := make(map[string]any, len(val))
		for _, k := range keys {
			normalized[k] = normalizeValue(val[k])
		}
		return normalized

	case []any:
		// Normalize array elements and sort if they're comparable
		normalized := make([]any, len(val))
		for i, item := range val {
			normalized[i] = normalizeValue(item)
		}

		// Try to sort arrays of simple values or objects
		if canSortArray(normalized) {
			slices.SortFunc(normalized, compareNormalized)
		}

		return normalized

	case json.Number:
		// Normalize numbers
		return val.String()

	default:
		return v
	}
}

// canSortArray checks if an array can be sorted for consistent comparison
func canSortArray(arr []any) bool {
	if len(arr) == 0 {
		return false
	}

	// Check if all elements are the same comparable type
	firstType := getType(arr[0])
	for _, item := range arr[1:] {
		if getType(item) != firstType {
			return false
		}
	}

	// Only sort if elements are primitive types or maps
	switch firstType {
	case "string", "number", "bool", "map":
		return true
	default:
		return false
	}
}

// getType returns the type of a value for comparison purposes
func getType(v any) string {
	switch v.(type) {
	case string:
		return "string"
	case json.Number:
		return "number"
	case float64:
		return "number"
	case int, int64:
		return "number"
	case bool:
		return "bool"
	case map[string]any:
		return "map"
	case []any:
		return "array"
	default:
		return "other"
	}
}

// compareNormalized compares two normalized values for sorting
func compareNormalized(a, b any) int {
	aStr, _ := json.Marshal(a)
	bStr, _ := json.Marshal(b)
	aStrStr := string(aStr)
	bStrStr := string(bStr)

	if aStrStr < bStrStr {
		return -1
	} else if aStrStr > bStrStr {
		return 1
	}
	return 0
}

func getName(item map[string]any) string {
	if name, ok := item["name"].(string); ok {
		return name
	}
	return "<unnamed>"
}

func getSystemName(sys map[string]any) string {
	if name, ok := sys["name"].(string); ok {
		return name
	}
	return "<unnamed>"
}

func printResults(result *DiffResult, logger *log.Logger) {
	additions := len(result.Additions)
	deletions := len(result.Deletions)
	changes := len(result.Changes)

	logger.Println("\nSUMMARY")
	logger.Println("━━━━━━━")

	totalChanges := additions + deletions + changes
	if totalChanges == 0 {
		logger.Println("✓ No changes detected")
		return
	}

	fmt.Printf("  Additions:  %s%d%s\n", colorGreen, additions, colorReset)
	fmt.Printf("  Deletions:  %s%d%s\n", colorRed, deletions, colorReset)
	if changes > 0 {
		fmt.Printf("  Modified:   %s%d%s\n", colorYellow, changes, colorReset)
	}

	// Print details
	if additions > 0 {
		fmt.Printf("\n%sADDITIONS%s\n", colorGreen, colorReset)
		fmt.Println("─────────")
		for _, item := range result.Additions {
			fmt.Printf("  + %s\n", item)
		}
	}

	if deletions > 0 {
		fmt.Printf("\n%sDELETIONS%s\n", colorRed, colorReset)
		fmt.Println("─────────")
		for _, item := range result.Deletions {
			fmt.Printf("  - %s\n", item)
		}
	}

	if len(result.Changes) > 0 {
		fmt.Printf("\n%sMODIFIED ITEMS%s\n", colorYellow, colorReset)
		fmt.Println("──────────────")
		// Group changes by ID
		changesByItem := make(map[string][]Change)
		for _, change := range result.Changes {
			changesByItem[change.ID] = append(changesByItem[change.ID], change)
		}
		ids := make([]string, 0, len(changesByItem))
		for id := range changesByItem {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		for _, id := range ids {
			fmt.Printf("  • %s\n", id)
			for _, change := range changesByItem[id] {
				switch {
				case change.OldVal == nil:
					fmt.Printf("    %s+ %s: %v%s\n", colorGreen, change.Field, change.NewVal, colorReset)
				case change.NewVal == nil:
					fmt.Printf("    %s- %s: %v%s\n", colorRed, change.Field, change.OldVal, colorReset)
				default:
					fmt.Printf("    %s:\n", change.Field)
					fmt.Printf("      - %v\n", change.OldVal)
					fmt.Printf("      + %v\n", change.NewVal)
				}
			}
		}
	}

	fmt.Println()
}

const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
)
