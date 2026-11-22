package worker

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/rsned/spacemolt/pkg/huntdecide"
)

// ParseHuntExplore parses the --hunt-explore spec: comma-separated kind=epsilon
// pairs, for example "provision=0.15,return=0.05". An empty spec is an empty
// map (no exploration). An unknown kind, a missing "=" or an epsilon outside
// [0, 1] is an error.
func ParseHuntExplore(spec string) (map[huntdecide.Kind]float64, error) {
	out := map[huntdecide.Kind]float64{}
	if strings.TrimSpace(spec) == "" {
		return out, nil
	}
	for part := range strings.SplitSeq(spec, ",") {
		part = strings.TrimSpace(part)
		name, val, ok := strings.Cut(part, "=")
		if !ok {
			return nil, fmt.Errorf("%q: want kind=epsilon", part)
		}
		kind := huntdecide.Kind(strings.TrimSpace(name))
		if kind != huntdecide.KindProvision && kind != huntdecide.KindReturn {
			return nil, fmt.Errorf("%q: unknown decision kind %q (want %s or %s)",
				part, name, huntdecide.KindProvision, huntdecide.KindReturn)
		}
		eps, err := strconv.ParseFloat(strings.TrimSpace(val), 64)
		if err != nil {
			return nil, fmt.Errorf("%q: bad epsilon: %w", part, err)
		}
		if eps < 0 || eps > 1 {
			return nil, fmt.Errorf("%q: epsilon %v outside [0, 1]", part, eps)
		}
		out[kind] = eps
	}
	return out, nil
}
