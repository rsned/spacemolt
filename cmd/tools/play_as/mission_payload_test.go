package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadMissionPayload(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	good := write("good.json", `{"title":"Meat","type":"delivery","objectives":[{"type":"deliver_item","item_id":"raw_xeno_meat","quantity":1}],"rewards":{"credits":100}}`)
	p, err := readMissionPayload(good)
	if err != nil {
		t.Fatalf("good payload: %v", err)
	}
	if p["title"] != "Meat" {
		t.Errorf("title = %v", p["title"])
	}
	for name, body := range map[string]string{
		"notitle.json": `{"objectives":[{"type":"deliver_item"}]}`,
		"noobj.json":   `{"title":"x","objectives":[]}`,
		"array.json":   `[1,2]`,
	} {
		if _, err := readMissionPayload(write(name, body)); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if _, err := readMissionPayload(filepath.Join(dir, "missing.json")); err == nil {
		t.Error("missing file: want an error")
	}
}
