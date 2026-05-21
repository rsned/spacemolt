package main

import (
	"bytes"
	"fmt"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// renderFactionHTML renders one faction's dashboard page to a self-contained
// HTML string.
func renderFactionHTML(v *knowledge.FactionView) (string, error) {
	var buf bytes.Buffer
	if err := factionTemplate.Execute(&buf, v); err != nil {
		return "", fmt.Errorf("execute faction template: %w", err)
	}
	return buf.String(), nil
}
