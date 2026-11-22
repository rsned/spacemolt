package huntdecide

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func readLines(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return strings.Split(strings.TrimRight(string(b), "\n"), "\n")
}

func TestLogAppendsOneLinePerRecordInDatedFile(t *testing.T) {
	dir := t.TempDir()
	l := NewLog(dir)
	t1 := time.Date(2026, 11, 21, 23, 59, 0, 0, time.UTC)
	t2 := time.Date(2026, 11, 22, 0, 1, 0, 0, time.UTC)
	if err := l.Append(t1, SortieRecord{Type: "sortie_start", At: t1, SortieID: "s1"}); err != nil {
		t.Fatal(err)
	}
	if err := l.Append(t2, OutcomeRecord{Type: "outcome", At: t2, SortieID: "s1", Phase: "provision"}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ file, typ string }{
		{"2026-11-21.jsonl", "sortie_start"},
		{"2026-11-22.jsonl", "outcome"},
	} {
		lines := readLines(t, filepath.Join(dir, DecisionsDir, c.file))
		if len(lines) != 1 {
			t.Fatalf("%s: want 1 line, got %d", c.file, len(lines))
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(lines[0]), &m); err != nil {
			t.Fatalf("%s: invalid JSON: %v", c.file, err)
		}
		if m["type"] != c.typ {
			t.Errorf("%s: type = %v, want %s", c.file, m["type"], c.typ)
		}
	}
}

func TestNilLogAppendIsNoOp(t *testing.T) {
	if err := (*Log)(nil).Append(time.Now(), SortieRecord{}); err != nil {
		t.Fatalf("nil log Append = %v, want nil", err)
	}
}

func unwritableLog(t *testing.T) *Log {
	t.Helper()
	f := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(f, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return NewLog(f)
}

func TestLogAppendErrorWhenDirUnwritable(t *testing.T) {
	if err := unwritableLog(t).Append(time.Now(), SortieRecord{}); err == nil {
		t.Fatal("want error, got nil")
	}
}

type fixedDecider struct{ c Choice }

func (s fixedDecider) Decide(context.Context, Decision) Choice { return s.c }

func TestRecorderWritesDecisionAndReturnsInnerChoice(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	want := Choice{OptionID: "b", RuleOptionID: "a", Source: SourceExplore, Epsilon: 0.1, Reason: "r"}
	r := Recorder{Inner: fixedDecider{want}, Log: NewLog(dir), Now: func() time.Time { return now }}
	d := Decision{
		Kind: KindProvision, SortieID: "s1", Seq: 3,
		State:   State{AgentID: "ag"},
		Options: []Option{{ID: "a"}, {ID: "b"}},
	}
	if got := r.Decide(context.Background(), d); got != want {
		t.Fatalf("choice = %+v, want %+v", got, want)
	}
	lines := readLines(t, filepath.Join(dir, DecisionsDir, "2026-11-21.jsonl"))
	if len(lines) != 1 {
		t.Fatalf("want 1 line, got %d", len(lines))
	}
	var rec DecisionRecord
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatal(err)
	}
	if rec.Type != "decision" || rec.Kind != KindProvision || rec.Seq != 3 ||
		rec.State.AgentID != "ag" || len(rec.Options) != 2 ||
		rec.OptionID != "b" || rec.RuleOptionID != "a" || rec.Source != SourceExplore {
		t.Fatalf("unexpected record: %+v", rec)
	}
	if !rec.At.Equal(now) {
		t.Errorf("at = %v, want %v", rec.At, now)
	}
}

func TestRecorderSurvivesLogFailure(t *testing.T) {
	want := Choice{OptionID: "a", RuleOptionID: "a", Source: SourceRule}
	r := Recorder{Inner: fixedDecider{want}, Log: unwritableLog(t)}
	if got := r.Decide(context.Background(), Decision{Kind: KindReturn}); got != want {
		t.Fatalf("choice = %+v, want %+v", got, want)
	}
}

// jsonTags returns the json tag names of struct type t, flattening
// embedded structs.
func jsonTags(t reflect.Type) []string {
	var tags []string
	for f := range t.Fields() {
		tag, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if f.Anonymous && tag == "" {
			tags = append(tags, jsonTags(f.Type)...)
			continue
		}
		if tag != "" && tag != "-" {
			tags = append(tags, tag)
		}
	}
	return tags
}

func TestSchemaIsDraft202012AndCoversRecordFields(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal(recordSchema, &schema); err != nil {
		t.Fatalf("schema does not parse: %v", err)
	}
	if schema["$schema"] != "https://json-schema.org/draft/2020-12/schema" {
		t.Errorf("$schema = %v", schema["$schema"])
	}
	shapes, _ := schema["oneOf"].([]any)
	if len(shapes) != 3 {
		t.Fatalf("oneOf has %d shapes, want 3", len(shapes))
	}
	defs, _ := schema["$defs"].(map[string]any)
	props := func(m any) map[string]any {
		mm, _ := m.(map[string]any)
		if ref, ok := mm["$ref"].(string); ok {
			mm, _ = defs[strings.TrimPrefix(ref, "#/$defs/")].(map[string]any)
		}
		p, _ := mm["properties"].(map[string]any)
		return p
	}
	byType := map[string]map[string]any{}
	for _, s := range shapes {
		p := props(s)
		typ, _ := p["type"].(map[string]any)
		if c, ok := typ["const"].(string); ok {
			byType[c] = p
		}
		if en, ok := typ["enum"].([]any); ok {
			for _, e := range en {
				byType[e.(string)] = p
			}
		}
	}
	check := func(shape string, p map[string]any, typ reflect.Type) {
		t.Helper()
		if p == nil {
			t.Errorf("no schema shape for %s", shape)
			return
		}
		for _, tag := range jsonTags(typ) {
			if _, ok := p[tag]; !ok {
				t.Errorf("%s: schema lacks property %q", shape, tag)
			}
		}
	}
	check("decision", byType["decision"], reflect.TypeOf(DecisionRecord{}))
	check("outcome", byType["outcome"], reflect.TypeOf(OutcomeRecord{}))
	check("sortie_start", byType["sortie_start"], reflect.TypeOf(SortieRecord{}))
	check("sortie_end", byType["sortie_end"], reflect.TypeOf(SortieRecord{}))
	check("decision.state", props(byType["decision"]["state"]), reflect.TypeOf(State{}))
	check("sortie.state", props(byType["sortie_start"]["state"]), reflect.TypeOf(State{}))
}
