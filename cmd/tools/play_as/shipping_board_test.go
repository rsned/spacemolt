package main

import (
	"context"
	"testing"
)

// v0.724.0: list_all reads every charted station's board from one dock.
func TestShippingListAllSendsFilters(t *testing.T) {
	c := &shippingRecorder{}
	err := boardCommand(context.Background(), c, []string{"shipping", "list_all", "distance",
		"--filter_origin", "sol_central", "--include_ineligible", "true"})
	if err != nil {
		t.Fatalf("list_all: %v", err)
	}
	if c.action != "list_all" {
		t.Errorf("action = %q, want list_all", c.action)
	}
	if c.payload["sort"] != "distance" {
		t.Errorf("sort = %v, want distance", c.payload["sort"])
	}
	if c.payload["filter_origin"] != "sol_central" {
		t.Errorf("filter_origin = %v, want sol_central", c.payload["filter_origin"])
	}
	if c.payload["include_ineligible"] != true {
		t.Errorf("include_ineligible = %v, want true", c.payload["include_ineligible"])
	}
}

// Unset options stay off the wire: a present key is a choice to the server.
func TestShippingListSendsNothingUnasked(t *testing.T) {
	c := &shippingRecorder{}
	if err := boardCommand(context.Background(), c, []string{"shipping", "list"}); err != nil {
		t.Fatalf("list: %v", err)
	}
	if c.action != "list" || len(c.payload) != 0 {
		t.Errorf("got action %q payload %v, want list with an empty payload", c.action, c.payload)
	}
}

// filter_origin is list_all's alone; the docked board has one origin.
func TestShippingListRejectsFilterOrigin(t *testing.T) {
	c := &shippingRecorder{}
	err := boardCommand(context.Background(), c, []string{"shipping", "list", "--filter_origin", "sol_central"})
	if err == nil {
		t.Fatal("list accepted --filter_origin")
	}
}
