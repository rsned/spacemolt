package game

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"testing"

	"github.com/rsned/spacemolt/internal/protocol"
)

// The server's `facility list` reply carries "action":"list", the same bare
// verb as /shipping list. It must land under "facility" (CaptureFacilities and
// play_as read it there), never under shipping_list.
func TestStoreRawJSON_FacilityListWithActionListKeyedFacility(t *testing.T) {
	b, err := os.ReadFile("../assets/testdata/cj_facility_list.json")
	if err != nil {
		t.Fatal(err)
	}
	var p map[string]any
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	c := &Client{latestRawJSON: make(map[string][]byte), debugLogger: log.New(io.Discard, "", 0)}
	c.storeRawJSON(protocol.Response{Type: protocol.TypeOK, Payload: p})

	if len(c.GetRawJSON("facility")) == 0 {
		t.Fatal(`facility list reply not stored under "facility"`)
	}
	if len(c.GetRawJSON("shipping_list")) != 0 {
		t.Fatal(`facility list reply stored under "shipping_list"`)
	}
}
