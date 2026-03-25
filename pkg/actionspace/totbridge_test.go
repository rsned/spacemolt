package actionspace

import "testing"

func TestToActionOptions(t *testing.T) {
	as := ActionSpace{
		Actions: []ActionResult{
			{
				Action: Action{Name: "travel", Summary: "Travel to a POI"},
				Valid: true, Targets: []string{"belt_1", "station_1"}, BranchingCount: 2,
			},
			{
				Action: Action{Name: "mine", Summary: "Mine resources"},
				Valid: true, BranchingCount: 1,
			},
			{
				Action: Action{Name: "buy", Summary: "Buy items"},
				Valid: false,
			},
		},
	}

	opts := as.ToActionOptions()
	if len(opts) != 2 {
		t.Fatalf("expected 2 options, got %d", len(opts))
	}
	if opts[0].Action != "travel" || len(opts[0].Targets) != 2 {
		t.Errorf("travel: got %s with %d targets", opts[0].Action, len(opts[0].Targets))
	}
	if opts[1].Action != "mine" {
		t.Errorf("expected mine, got %s", opts[1].Action)
	}
}
