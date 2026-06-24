package worker

import "strings"

// SubstituteParams replaces every $KEY$ occurrence in each line with
// params[KEY], for keys present in params. Keys absent from params (e.g. live
// state tokens like $ASTEROID_BELT$) are left untouched so they pass through to
// ResolveTokens. Returns a new slice; the input is not mutated. A nil/empty
// params map is a no-op.
func SubstituteParams(lines []string, params map[string]string) []string {
	out := make([]string, len(lines))
	copy(out, lines)
	if len(params) == 0 {
		return out
	}
	for k, v := range params {
		token := "$" + k + "$"
		for i := range out {
			if strings.Contains(out[i], token) {
				out[i] = strings.ReplaceAll(out[i], token, v)
			}
		}
	}
	return out
}
