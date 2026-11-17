package worker

import (
	"cmp"
	"errors"
	"fmt"

	"github.com/rsned/spacemolt/pkg/game"
)

// ErrGateTollUnaffordable refuses a route whose jump-gate tolls exceed the
// ship's credits (v0.720.0: find_route plans through open player gates and the
// toll is charged on the jump). The ship stays where it is, like the fuel
// refusal; a caller that can should re-plan or wait for credits.
var ErrGateTollUnaffordable = errors.New("gate tolls exceed credits")

// routeGateTolls sums the tolls on route's gate hops and names each one
// ("Bravo (400 cr)", "Free (free)"). route excludes the origin step.
func routeGateTolls(route []game.RouteStep) (total int, hops []string) {
	for _, s := range route {
		if !s.ViaGate {
			continue
		}
		total += s.GateToll
		hops = append(hops, fmt.Sprintf("%s (%s)", cmp.Or(s.Name, s.SystemID), gateFee(s)))
	}

	return total, hops
}

// gateFee is a gate hop's toll for a log line: "400 cr" or "free".
func gateFee(s game.RouteStep) string {
	if s.GateToll > 0 {
		return fmt.Sprintf("%d cr", s.GateToll)
	}

	return "free"
}
