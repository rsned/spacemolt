package huntdecide

import (
	"context"
	"math/rand/v2"
)

// Explorer wraps an inner Decider and, with probability Epsilon[kind],
// replaces its pick with a uniformly random explorable option.
type Explorer struct {
	Inner   Decider
	Epsilon map[Kind]float64
	Float   func() float64  // nil -> math/rand/v2 Float64
	Intn    func(n int) int // nil -> math/rand/v2 IntN
}

// Decide asks the inner decider, then possibly explores instead. It fills
// RuleOptionID, Source and Epsilon on every choice. It explores only when
// epsilon > 0, Float() < epsilon, the rule's pick is not NoExplore, and at
// least two options are explorable.
func (e Explorer) Decide(ctx context.Context, d Decision) Choice {
	c := e.Inner.Decide(ctx, d)
	eps := e.Epsilon[d.Kind]
	c.RuleOptionID = c.OptionID
	c.Source = SourceRule
	c.Epsilon = eps
	if eps <= 0 {
		return c
	}

	float := e.Float
	if float == nil {
		float = rand.Float64
	}
	if float() >= eps {
		return c
	}

	var explorable []string
	for _, o := range d.Options {
		if o.ID == c.OptionID && o.NoExplore {
			return c
		}
		if !o.NoExplore {
			explorable = append(explorable, o.ID)
		}
	}
	if len(explorable) < 2 {
		return c
	}

	intn := e.Intn
	if intn == nil {
		intn = rand.IntN
	}
	c.OptionID = explorable[intn(len(explorable))]
	c.Source = SourceExplore
	return c
}
