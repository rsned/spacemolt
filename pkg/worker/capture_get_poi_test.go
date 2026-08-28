package worker

import (
	"context"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

// poiFakeClient answers get_poi from a canned raw payload and records what was
// asked for, so a test can prove which command the capture path issues.
type poiFakeClient struct {
	game.GameClient // embedded; anything unimplemented panics if called
	calls           []string
	raw             map[string][]byte
	state           *game.State
}

func (f *poiFakeClient) GetPOI(ctx context.Context) error {
	f.calls = append(f.calls, "get_poi")
	return nil
}

func (f *poiFakeClient) RawCommand(ctx context.Context, command string, args map[string]any) error {
	f.calls = append(f.calls, "raw:"+command)
	return nil
}

func (f *poiFakeClient) GetRawJSON(key string) []byte { return f.raw[key] }

func (f *poiFakeClient) GetState() *game.State {
	if f.state == nil {
		return &game.State{}
	}
	return f.state
}

const poiReply = `{"poi":{
  "id":"ashford_ice_shelf","system_id":"haven","type":"ice_field","class":"C-type",
  "name":"Ashford Ice Shelf","description":"A shelf of dirty ice.",
  "position":{"x":12.5,"y":-3.25},
  "hidden":true,"reveal_difficulty":40,
  "expires_at":"2026-08-29T00:00:00Z",
  "base_id":"","has_base":false,
  "resources":[
    {"resource_id":"water_ice","richness":0.8,"remaining":1200},
    {"resource_id":"nitrogen_ice","richness":0.4,"remaining":300}
  ]}}`

// The capture path must ask get_poi. get_location is not in the 216-path spec
// (2026-08-27) and carries neither hidden nor reveal_difficulty, so a hidden-POI
// survey recorded every reveal as an ordinary POI.
func TestGetPOI_IssuesGetPOINotGetLocation(t *testing.T) {
	f := &poiFakeClient{raw: map[string][]byte{"poi": []byte(poiReply)}}
	if _, err := GetPOI(context.Background(), f); err != nil {
		t.Fatalf("GetPOI: %v", err)
	}
	joined := strings.Join(f.calls, ",")
	if !strings.Contains(joined, "get_poi") {
		t.Errorf("calls = %v, want a get_poi", f.calls)
	}
	if strings.Contains(joined, "get_location") {
		t.Errorf("calls = %v, must not issue get_location", f.calls)
	}
}

// hidden and reveal_difficulty are the fields a hidden-POI sweep exists to
// record, and they come from the reply itself -- not from cached get_system
// data, which is empty for a POI a survey only just revealed.
func TestGetPOI_CapturesHiddenAndRevealDifficulty(t *testing.T) {
	f := &poiFakeClient{raw: map[string][]byte{"poi": []byte(poiReply)}}
	poi, err := GetPOI(context.Background(), f)
	if err != nil {
		t.Fatalf("GetPOI: %v", err)
	}
	if !poi.Hidden {
		t.Error("Hidden = false, want true")
	}
	if poi.RevealDifficulty != 40 {
		t.Errorf("RevealDifficulty = %d, want 40", poi.RevealDifficulty)
	}
	if poi.ExpiresAt != "2026-08-29T00:00:00Z" {
		t.Errorf("ExpiresAt = %q", poi.ExpiresAt)
	}
}

// Structural fields must come from the reply. GetLocationPOI had to borrow
// them from cached system data because get_location omits them; get_poi does
// not, so the enrichment hack goes away with it.
func TestGetPOI_TakesStructuralFieldsFromTheReply(t *testing.T) {
	f := &poiFakeClient{raw: map[string][]byte{"poi": []byte(poiReply)}} // NOTE: empty state
	poi, err := GetPOI(context.Background(), f)
	if err != nil {
		t.Fatalf("GetPOI: %v", err)
	}
	if poi.ID != "ashford_ice_shelf" || poi.SystemID != "haven" || poi.Type != "ice_field" {
		t.Errorf("identity = %+v", poi)
	}
	if poi.Class != "C-type" {
		t.Errorf("Class = %q, want C-type", poi.Class)
	}
	if poi.Description == "" {
		t.Error("Description is empty; it must come from the reply, not cached system data")
	}
	if poi.Position.X != 12.5 || poi.Position.Y != -3.25 {
		t.Errorf("Position = %+v", poi.Position)
	}
}

func TestGetPOI_CapturesResources(t *testing.T) {
	f := &poiFakeClient{raw: map[string][]byte{"poi": []byte(poiReply)}}
	poi, err := GetPOI(context.Background(), f)
	if err != nil {
		t.Fatalf("GetPOI: %v", err)
	}
	if len(poi.Resources) != 2 {
		t.Fatalf("Resources = %d, want 2: %+v", len(poi.Resources), poi.Resources)
	}
	if poi.Resources[0].ResourceID != "water_ice" || poi.Resources[0].Richness != 0.8 {
		t.Errorf("first resource = %+v", poi.Resources[0])
	}
	if poi.Resources[1].Remaining != 300 {
		t.Errorf("second resource remaining = %v, want 300", poi.Resources[1].Remaining)
	}
}

// In transit there is no current POI. The reply is the transit variant, and
// capturing a half-populated POI from it would corrupt the KB row.
func TestGetPOI_RefusesWhileInTransit(t *testing.T) {
	const transit = `{"kind":"transit","in_transit":true,"from_system":"haven","to_system":"sol","ticks_remaining":3}`
	f := &poiFakeClient{raw: map[string][]byte{"poi": []byte(transit)}}
	if _, err := GetPOI(context.Background(), f); err == nil {
		t.Fatal("GetPOI succeeded while in transit; want an error")
	}
}

func TestGetPOI_ErrorsWhenReplyHasNoPOI(t *testing.T) {
	f := &poiFakeClient{raw: map[string][]byte{}}
	if _, err := GetPOI(context.Background(), f); err == nil {
		t.Fatal("GetPOI succeeded with no cached reply; want an error")
	}
}
