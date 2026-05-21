package main

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// slugifyTag converts a faction tag into a filesystem- and URL-safe slug,
// keeping only [A-Za-z0-9_-] and replacing any other rune with '_'. Returns
// "untagged" if the result is empty.
func slugifyTag(tag string) string {
	var b strings.Builder
	for _, r := range tag {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	s := b.String()
	if s == "" {
		return "untagged"
	}
	return s
}

// renderFactionHTML renders one faction's dashboard page to a self-contained
// HTML string.
func renderFactionHTML(v *knowledge.FactionView) (string, error) {
	var buf bytes.Buffer
	if err := factionTemplate.Execute(&buf, v); err != nil {
		return "", fmt.Errorf("execute faction template: %w", err)
	}
	return buf.String(), nil
}
