package main

import (
	"encoding/json"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

// The output must load as a data/agents/<id>/credentials.json file.
func TestCredentialsJSONMatchesTheFileFormat(t *testing.T) {
	out, err := credentialsJSON("miner-1", "pw", "solarian", "pid1")
	if err != nil {
		t.Fatal(err)
	}
	var got game.Credentials
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, out)
	}
	want := game.Credentials{Username: "miner-1", Password: "pw", Empire: "solarian", PlayerID: "pid1"}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}
