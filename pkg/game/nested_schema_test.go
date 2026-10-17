package game

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// nestedSchemas pins types that are NOT an action's top-level response and so
// are invisible to TestPassthroughStructsCoverOpenAPISchemas, which only walks
// passthroughSchemas.
//
// Base is the gap that motivated this: it is embedded in several responses and
// nothing checked it, so it had quietly fallen nine fields behind the schema --
// including contraband_items, where a missing tag silently decodes to the zero
// value and inverts a station's contraband policy.
var nestedSchemas = map[string]reflect.Type{
	"Base": reflect.TypeOf(serverapi.Base{}),
}

func TestNestedStructsCoverOpenAPISchemas(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "..", "server_docs", "openapi.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("openapi.json not found: %v", err)
	}

	var doc struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse openapi.json: %v", err)
	}

	for schemaName, typ := range nestedSchemas {
		schema, ok := doc.Components.Schemas[schemaName]
		if !ok {
			t.Errorf("openapi has no schema %q", schemaName)

			continue
		}
		fields := jsonFieldNames(typ)
		for prop := range schema.Properties {
			if !fields[prop] {
				t.Errorf("%s: openapi declares %q but %s has no matching json tag "+
					"(it would silently decode to the zero value)",
					schemaName, prop, typ.Name())
			}
		}
	}
}
