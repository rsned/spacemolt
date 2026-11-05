package main

import (
	"reflect"
	"strings"
	"testing"
)

// TestParseItemTransferFlags covers the storage-bucket flags on the four item
// transfer commands. A faction storage extension ("bucket") is addressed by
// name or id; an empty dest_bucket means the faction's main store, which is
// the only store station maintenance draws from.
func TestParseItemTransferFlags(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want map[string]any
	}{
		{"bucket to main store, = form",
			[]string{"--source=faction", "--target=faction", "--bucket=Maintainence", "--dest_bucket="},
			map[string]any{"source": "faction", "target": "faction", "bucket": "Maintainence", "dest_bucket": ""}},
		{"bare dest_bucket at end means main store",
			[]string{"--bucket", "Maintainence", "--dest_bucket"},
			map[string]any{"bucket": "Maintainence", "dest_bucket": ""}},
		{"bare dest_bucket before another flag means main store",
			[]string{"--dest_bucket", "--source=faction"},
			map[string]any{"dest_bucket": "", "source": "faction"}},
		{"numeric-looking bucket stays a string",
			[]string{"--bucket=12345"},
			map[string]any{"bucket": "12345"}},
		{"main store into a bucket",
			[]string{"--source=faction", "--target=faction", "--dest_bucket=a6bc64fb3ce3ba7973fa08fe3fbaf3a3"},
			map[string]any{"source": "faction", "target": "faction", "dest_bucket": "a6bc64fb3ce3ba7973fa08fe3fbaf3a3"}},
		{"no flags",
			nil,
			map[string]any{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseItemTransferFlags(c.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %#v, want %#v", got, c.want)
			}
		})
	}
}

// TestParseItemTransferFlags_RejectsUnknownFlag guards against a typo moving
// items from the wrong store: an unrecognised flag such as --buckt used to be
// dropped silently, which turns a bucket withdraw into a main-store withdraw.
func TestParseItemTransferFlags_RejectsUnknownFlag(t *testing.T) {
	_, err := parseItemTransferFlags([]string{"--buckt=Maintainence"})
	if err == nil || !strings.Contains(err.Error(), "buckt") {
		t.Fatalf("want an error naming the unknown flag, got %v", err)
	}
}
