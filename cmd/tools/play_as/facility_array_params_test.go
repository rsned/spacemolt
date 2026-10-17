package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// buildFacilityPayload put every --flag value into the payload as a plain
// string, but the facility command declares THREE array-typed parameters --
// items, job_ids and package_ids -- so all three were rejected by the server:
//
//	invalid_payload: Parameter 'package_ids' must be an array, but received a string.
//
// Observed live 2026-10-17 trying to rebuild the Crystal Infusion Still from
// its 42 recovered material packages.
func TestBuildFacilityPayload_ArrayParamsBecomeArrays(t *testing.T) {
	for _, key := range []string{"package_ids", "job_ids", "items"} {
		t.Run(key, func(t *testing.T) {
			payload, _, err := buildFacilityPayload(
				strings.Fields("facility faction_build crystal_infusion_still --" + key + " aaa,bbb,ccc"))
			if err != nil {
				t.Fatal(err)
			}
			got, ok := payload[key].([]string)
			if !ok {
				t.Fatalf("%s is %T (%v), want []string", key, payload[key], payload[key])
			}
			if want := []string{"aaa", "bbb", "ccc"}; !reflect.DeepEqual(got, want) {
				t.Errorf("%s = %v, want %v", key, got, want)
			}
		})
	}
}

// A single id is still an array of one: the server types the parameter, not
// the arity.
func TestBuildFacilityPayload_SingleIDIsStillAnArray(t *testing.T) {
	payload, _, err := buildFacilityPayload(
		strings.Fields("facility faction_build crystal_infusion_still --package_ids onlyone"))
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := payload["package_ids"].([]string); !ok || len(got) != 1 || got[0] != "onlyone" {
		t.Errorf("package_ids = %#v, want []string{\"onlyone\"}", payload["package_ids"])
	}
}

// JSON-array syntax is what anyone copying from a doc or a generated list will
// paste, so accept it rather than sending it verbatim as one string. The
// brace-and-single-quote form that actually got typed is accepted too.
func TestBuildFacilityPayload_AcceptsBracketedAndBracedForms(t *testing.T) {
	for _, raw := range []string{
		`["aaa","bbb"]`,
		`['aaa','bbb']`,
		`{'aaa','bbb'}`,
		`aaa, bbb`,
	} {
		payload, _, err := buildFacilityPayload([]string{"facility", "faction_build", "x", "--package_ids", raw})
		if err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		got, ok := payload["package_ids"].([]string)
		if !ok {
			t.Fatalf("%s: got %T", raw, payload["package_ids"])
		}
		if want := []string{"aaa", "bbb"}; !reflect.DeepEqual(got, want) {
			t.Errorf("%s -> %v, want %v", raw, got, want)
		}
	}
}

// The 'package:<id>' prefix is accepted by the server alongside the raw id, so
// it must survive untouched -- stripping it would be a guess about which form
// the server prefers.
func TestBuildFacilityPayload_KeepsThePackagePrefix(t *testing.T) {
	payload, _, err := buildFacilityPayload([]string{
		"facility", "faction_build", "x", "--package_ids", "package:abc,package:def",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"package:abc", "package:def"}
	if got, _ := payload["package_ids"].([]string); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// Duplicates are rejected locally. The spec says pooled package contents must
// equal the recipe inputs EXACTLY and "any shortage or overage is rejected
// before anything is touched", so a doubled id is a guaranteed server-side
// failure -- and a copy-paste doubling is exactly what happened live, sending
// 44 ids for a 42-package set.
func TestBuildFacilityPayload_RejectsDuplicatePackageIDs(t *testing.T) {
	_, _, err := buildFacilityPayload([]string{
		"facility", "faction_build", "x", "--package_ids", "aaa,bbb,aaa",
	})
	if err == nil {
		t.Fatal("want an error naming the duplicate")
	}
	if !strings.Contains(err.Error(), "aaa") {
		t.Errorf("error should name the duplicate id: %v", err)
	}
}

// An empty list is a mistake worth catching before the wire.
func TestBuildFacilityPayload_RejectsEmptyArrayFlag(t *testing.T) {
	if _, _, err := buildFacilityPayload([]string{
		"facility", "faction_build", "x", "--package_ids", "[]",
	}); err == nil {
		t.Error("want an error for an empty --package_ids")
	}
}

// Forty-two package ids is a 1,400-character command line, which is how a
// copy-paste doubling slipped in live. An @file indirection takes the list from
// disk instead.
func TestBuildFacilityPayload_AtFileSuppliesTheArray(t *testing.T) {
	dir := t.TempDir()

	for name, body := range map[string]string{
		// A JSON array, as produced by tooling.
		"ids.json": "[\n \"package:aaa\",\n \"package:bbb\",\n \"package:ccc\"\n]\n",
		// A plain list, one per line -- what a terminal paste looks like.
		"ids.txt": "package:aaa\npackage:bbb\npackage:ccc\n",
		// Comma-separated on one line, with stray whitespace.
		"ids.csv": " package:aaa , package:bbb ,package:ccc \n",
	} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		payload, _, err := buildFacilityPayload([]string{
			"facility", "faction_build", "crystal_infusion_still", "--package_ids", "@" + path,
		})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		want := []string{"package:aaa", "package:bbb", "package:ccc"}
		if got, _ := payload["package_ids"].([]string); !reflect.DeepEqual(got, want) {
			t.Errorf("%s -> %v, want %v", name, got, want)
		}
	}
}

// A missing file must say so plainly rather than sending "@/path" as an id and
// letting the server reject something that looks nothing like the mistake.
func TestBuildFacilityPayload_AtFileMissingIsNamed(t *testing.T) {
	_, _, err := buildFacilityPayload([]string{
		"facility", "faction_build", "x", "--package_ids", "@/nonexistent/ids.json",
	})
	if err == nil {
		t.Fatal("want an error for a missing @file")
	}
	if !strings.Contains(err.Error(), "ids.json") {
		t.Errorf("error should name the path: %v", err)
	}
}

// Duplicate detection still applies when the list comes from a file -- that is
// precisely where a doubled id hides.
func TestBuildFacilityPayload_AtFileRejectsDuplicates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dupes.json")
	if err := os.WriteFile(path, []byte(`["aaa","bbb","aaa"]`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := buildFacilityPayload([]string{
		"facility", "faction_build", "x", "--package_ids", "@" + path,
	})
	if err == nil || !strings.Contains(err.Error(), "aaa") {
		t.Errorf("want a duplicate error naming aaa, got %v", err)
	}
}
