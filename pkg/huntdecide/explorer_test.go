package huntdecide

import (
	"context"
	"testing"
)

type stubDecider struct{}

func (stubDecider) Decide(_ context.Context, _ Decision) Choice {
	return Choice{OptionID: "b", Reason: "stub"}
}

func opts(noExplore ...string) []Option {
	skip := map[string]bool{}
	for _, id := range noExplore {
		skip[id] = true
	}
	var out []Option
	for _, id := range []string{"a", "b", "c"} {
		out = append(out, Option{ID: id, NoExplore: skip[id]})
	}
	return out
}

func fixed(f float64, n int) Explorer {
	return Explorer{
		Inner:   stubDecider{},
		Epsilon: map[Kind]float64{KindProvision: 1},
		Float:   func() float64 { return f },
		Intn:    func(int) int { return n },
	}
}

func TestExplorerZeroEpsilonReturnsRule(t *testing.T) {
	e := Explorer{Inner: stubDecider{}}
	c := e.Decide(context.Background(), Decision{Kind: KindProvision, Options: opts()})
	if c.OptionID != "b" || c.RuleOptionID != "b" || c.Source != SourceRule || c.Epsilon != 0 {
		t.Fatalf("got %+v", c)
	}
}

func TestExplorerPicksAmongExplorable(t *testing.T) {
	c := fixed(0, 0).Decide(context.Background(), Decision{Kind: KindProvision, Options: opts("c")})
	if c.OptionID != "a" || c.RuleOptionID != "b" || c.Source != SourceExplore || c.Epsilon != 1 {
		t.Fatalf("got %+v", c)
	}
}

func TestExplorerNeverOverridesNoExploreRulePick(t *testing.T) {
	c := fixed(0, 0).Decide(context.Background(), Decision{Kind: KindProvision, Options: opts("b")})
	if c.OptionID != "b" || c.Source != SourceRule {
		t.Fatalf("got %+v", c)
	}
}

func TestExplorerNeedsTwoExplorable(t *testing.T) {
	c := fixed(0, 0).Decide(context.Background(), Decision{Kind: KindProvision, Options: opts("a", "c")})
	if c.Source != SourceRule || c.OptionID != "b" {
		t.Fatalf("got %+v", c)
	}
}

func TestExplorerAboveEpsilonReturnsRule(t *testing.T) {
	e := fixed(0.15, 0)
	e.Epsilon = map[Kind]float64{KindProvision: 0.15}
	c := e.Decide(context.Background(), Decision{Kind: KindProvision, Options: opts()})
	if c.Source != SourceRule || c.OptionID != "b" {
		t.Fatalf("got %+v", c)
	}
}
