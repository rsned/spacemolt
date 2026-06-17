package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// formatGetAchievements renders a get_achievements response: a summary headline
// followed by per-category sections. Within each category earned entries are
// listed first, then locked entries sorted by descending completion so the
// closest-to-done show at the top. Locked entries with a progress counter get a
// current/target column; hidden/secret entries render as "???". Returns "" if
// the payload can't be parsed (caller falls back to JSON). Handles both the flat
// OK payload and an action_result-wrapped frame.
func formatGetAchievements(raw []byte) string {
	var resp serverapi.GetAchievementsResponse
	if err := json.Unmarshal(unwrapActionResult(raw), &resp); err != nil {
		return ""
	}
	if len(resp.Achievements) == 0 && resp.Summary.Total == 0 {
		return ""
	}

	// Group by category, preserving first-seen category order.
	byCat := map[string][]serverapi.Achievement{}
	var catOrder []string
	for _, a := range resp.Achievements {
		if _, seen := byCat[a.Category]; !seen {
			catOrder = append(catOrder, a.Category)
		}
		byCat[a.Category] = append(byCat[a.Category], a)
	}
	sort.Strings(catOrder)

	// Widest name across all entries for column alignment.
	nameW := 0
	for _, a := range resp.Achievements {
		if n := len(achievementName(a)); n > nameW {
			nameW = n
		}
	}

	var b strings.Builder
	b.WriteString("=== Achievements ===\n")
	fmt.Fprintf(&b, "%d / %d earned · %d points\n",
		resp.Summary.Earned, resp.Summary.Total, resp.Summary.Points)

	for _, cat := range catOrder {
		entries := byCat[cat]
		earnedN := 0
		for _, a := range entries {
			if a.Earned {
				earnedN++
			}
		}
		sortAchievements(entries)
		fmt.Fprintf(&b, "\n%s (%d/%d)\n", strings.ToUpper(cat), earnedN, len(entries))
		for _, a := range entries {
			mark := "○"
			if a.Earned {
				mark = "✓"
			} else if a.Hidden {
				mark = "?"
			}
			fmt.Fprintf(&b, "  %s  %-*s  %3d pts", mark, nameW, achievementName(a), a.Points)
			if !a.Earned && a.Progress != nil && a.Progress.Target > 0 {
				pct := 100 * a.Progress.Current / a.Progress.Target
				fmt.Fprintf(&b, "   [%d/%d · %d%%]", a.Progress.Current, a.Progress.Target, pct)
			}
			if desc := truncateDescription(a.Description, 40); desc != "" {
				fmt.Fprintf(&b, "   %s", desc)
			}
			b.WriteByte('\n')
		}
	}

	return b.String()
}

// truncateDescription returns the first max runes of s, appending "…" when it
// was cut. Rune-aware so multi-byte characters aren't split mid-codepoint.
func truncateDescription(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

// achievementName returns the display name for an achievement. Hidden/secret
// entries report Name as the server's "???" sentinel, so fall back to the id
// (which is never masked) tagged "(secret)" to keep each row identifiable.
func achievementName(a serverapi.Achievement) string {
	if a.Hidden && !a.Earned {
		return a.ID + " (secret)"
	}
	return a.Name
}

// sortAchievements orders entries within a category: earned first, then locked
// by descending completion fraction (closest-to-done first), with name as the
// final tiebreaker for stable output.
func sortAchievements(entries []serverapi.Achievement) {
	frac := func(a serverapi.Achievement) float64 {
		if a.Progress == nil || a.Progress.Target == 0 {
			return 0
		}
		return float64(a.Progress.Current) / float64(a.Progress.Target)
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Earned != entries[j].Earned {
			return entries[i].Earned
		}
		if !entries[i].Earned {
			if fi, fj := frac(entries[i]), frac(entries[j]); fi != fj {
				return fi > fj
			}
		}
		return entries[i].Name < entries[j].Name
	})
}
