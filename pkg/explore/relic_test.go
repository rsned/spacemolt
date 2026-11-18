package explore

import (
	"context"
	"errors"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

const relicPrompt = `{"poi":{"id":"vault","type":"relic"},"interaction":{"message":"A sealed archive hums.","command":"interact_poi"}}`

// resetRelicsTried forgets which relics this process has attempted.
func resetRelicsTried() {
	relicsTried.Lock()
	defer relicsTried.Unlock()
	relicsTried.seen = map[string]bool{}
}

// relicFixture is a ship parked at one relic POI whose get_poi reply is prompt.
func relicFixture(t *testing.T, prompt string) (*fakeClient, Deps, game.POI) {
	t.Helper()
	resetRelicsTried()
	p := poi("vault", "relic", 0)
	c := newFakeClient(p)
	c.poiReplies = map[string]string{"vault": prompt}
	d, _ := testDeps(c)
	return c, d.withDefaults(), p
}

// only returns the single interaction the step recorded.
func only(t *testing.T, kb knowledge.Base) knowledge.RelicInteraction {
	t.Helper()
	rows, err := kb.RelicInteractions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("recorded %d interactions, want 1: %+v", len(rows), rows)
	}
	return rows[0]
}

func TestRelicStepOutcomes(t *testing.T) {
	cases := []struct {
		name       string
		prompt     string
		reply      string
		replyErr   error
		want       string
		wantSent   bool
		wantPrompt string
		check      func(t *testing.T, r knowledge.RelicInteraction)
	}{
		{
			name:   "a prompt offering a mission is skipped",
			prompt: `{"interaction":{"message":"Help us.","command":"interact_poi","mission":{"id":"m1","title":"Recover"}}}`,
			want:   knowledge.RelicMissionSkipped, wantSent: false, wantPrompt: "Help us.",
			check: func(t *testing.T, r knowledge.RelicInteraction) {
				if r.MissionJSON != `{"id":"m1","title":"Recover"}` {
					t.Errorf("MissionJSON=%q", r.MissionJSON)
				}
			},
		},
		{
			name: "data copied", prompt: relicPrompt,
			reply: `{"result":{"poi_id":"vault","message":"Archive copied.","data_copied":true}}`,
			want:  knowledge.RelicDataCopied, wantSent: true, wantPrompt: "A sealed archive hums.",
			check: func(t *testing.T, r knowledge.RelicInteraction) {
				if !r.DataCopied || r.Message != "Archive copied." {
					t.Errorf("row %+v", r)
				}
			},
		},
		{
			name: "items recovered", prompt: relicPrompt,
			reply: `{"poi_id":"vault","message":"You pry it open.","items_recovered":{"relic_shard":2,"scrap":7}}`,
			want:  knowledge.RelicItems, wantSent: true, wantPrompt: "A sealed archive hums.",
			check: func(t *testing.T, r knowledge.RelicInteraction) {
				if r.Items["relic_shard"] != 2 || r.Items["scrap"] != 7 {
					t.Errorf("Items=%v", r.Items)
				}
			},
		},
		{
			name: "a message only", prompt: relicPrompt,
			reply: `{"poi_id":"vault","message":"Nothing stirs."}`,
			want:  knowledge.RelicNothing, wantSent: true, wantPrompt: "A sealed archive hums.",
		},
		{
			name: "the server refuses", prompt: relicPrompt,
			replyErr: errors.New("already_interacted: nothing more here"),
			want:     knowledge.RelicRefused, wantSent: true, wantPrompt: "A sealed archive hums.",
			check: func(t *testing.T, r knowledge.RelicInteraction) {
				if r.Error != "already_interacted: nothing more here" {
					t.Errorf("Error=%q", r.Error)
				}
			},
		},
		{
			name: "a relic with no interaction block is still tried", prompt: `{"poi":{"id":"vault","type":"relic"}}`,
			reply: `{"poi_id":"vault","message":"Dust.","data_copied":true}`,
			want:  knowledge.RelicDataCopied, wantSent: true, wantPrompt: "",
		},
		{
			name: "no interaction block and refused", prompt: `{"poi":{"id":"vault","type":"relic"}}`,
			replyErr: errors.New("nothing to interact with"),
			want:     knowledge.RelicNoPrompt, wantSent: true, wantPrompt: "",
			check: func(t *testing.T, r knowledge.RelicInteraction) {
				if r.Error != "nothing to interact with" {
					t.Errorf("Error=%q", r.Error)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, d, p := relicFixture(t, tc.prompt)
			c.rawCommand = func(string) (string, error) { return tc.reply, tc.replyErr }

			tried := relicStep(context.Background(), d, p)

			if sent := c.count("interact_poi") == 1; sent != tc.wantSent || tried != tc.wantSent {
				t.Errorf("interact_poi sent=%v tried=%v, want %v", sent, tried, tc.wantSent)
			}
			r := only(t, d.KB)
			if r.Outcome != tc.want {
				t.Errorf("Outcome=%q, want %q", r.Outcome, tc.want)
			}
			if r.Prompt != tc.wantPrompt {
				t.Errorf("Prompt=%q, want %q", r.Prompt, tc.wantPrompt)
			}
			if r.POIID != "vault" || r.SystemID != "far_wick" || r.AgentID != "explorer-1" {
				t.Errorf("row keyed %s/%s/%s", r.POIID, r.SystemID, r.AgentID)
			}
			if tc.check != nil {
				tc.check(t, r)
			}
		})
	}
}

// A POI of another type that prompts an interaction counts as a relic.
func TestRelicStepFollowsAnInteractionPrompt(t *testing.T) {
	c, d, _ := relicFixture(t, relicPrompt)
	c.rawCommand = func(string) (string, error) { return `{"message":"ok","data_copied":true}`, nil }
	if !relicStep(context.Background(), d, poi("vault", "anomaly", 0)) {
		t.Error("a POI with an interaction prompt was not tried")
	}
}

// An ordinary POI is left alone.
func TestRelicStepIgnoresOrdinaryPOIs(t *testing.T) {
	c, d, _ := relicFixture(t, `{"poi":{"id":"vault","type":"asteroid_belt"}}`)
	if relicStep(context.Background(), d, poi("vault", "asteroid_belt", 0)) {
		t.Error("an asteroid belt was treated as a relic")
	}
	if c.count("interact_poi") != 0 {
		t.Error("interact_poi sent at an ordinary POI")
	}
	if rows, _ := d.KB.RelicInteractions(context.Background()); len(rows) != 0 {
		t.Errorf("recorded %d rows for an ordinary POI", len(rows))
	}
}

// One attempt per relic per agent per process, however often the tour passes.
func TestRelicStepTriesARelicOnce(t *testing.T) {
	c, d, p := relicFixture(t, relicPrompt)
	c.rawCommand = func(string) (string, error) { return `{"message":"ok","data_copied":true}`, nil }
	relicStep(context.Background(), d, p)
	if relicStep(context.Background(), d, p) {
		t.Error("second pass tried the relic again")
	}
	if n := c.count("interact_poi"); n != 1 {
		t.Errorf("interact_poi sent %d times, want 1", n)
	}
}

// The tour runs the relic step at each POI when Options.Relics is set.
func TestTourInteractsWithRelics(t *testing.T) {
	resetRelicsTried()
	c := newFakeClient(poi("star", "star", 0), poi("vault", "relic", 1))
	c.poiReplies = map[string]string{"vault": relicPrompt, "star": `{"poi":{"id":"star"}}`}
	c.rawCommand = func(string) (string, error) { return `{"message":"ok","data_copied":true}`, nil }
	d, _ := testDeps(c)
	d.HasScanner = func(*game.State) bool { return false }

	res, err := System(context.Background(), d, Options{Relics: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Relics != 1 || c.count("interact_poi") != 1 {
		t.Errorf("Relics=%d, interact_poi sent %d times; want 1 and 1", res.Relics, c.count("interact_poi"))
	}

	c2 := newFakeClient(poi("star", "star", 0), poi("vault2", "relic", 1))
	d2, _ := testDeps(c2)
	d2.HasScanner = func(*game.State) bool { return false }
	if _, err := System(context.Background(), d2, Options{}); err != nil {
		t.Fatal(err)
	}
	if c2.count("get_poi") != 0 || c2.count("interact_poi") != 0 {
		t.Errorf("relic step ran without Options.Relics: %v", c2.calls)
	}
}
