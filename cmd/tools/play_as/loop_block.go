package main

import (
	"fmt"
	"strings"
)

// scanBraceDepth reports the net brace depth of s and whether the scan
// ended inside a quoted string. Braces inside '"..."' or "'...'" are
// ignored.
func scanBraceDepth(s string) (depth int, inQuote bool, err error) {
	var quoteRune rune
	for _, r := range s {
		if quoteRune != 0 {
			if r == quoteRune {
				quoteRune = 0
			}
			continue
		}
		switch r {
		case '"', '\'':
			quoteRune = r
		case '{':
			depth++
		case '}':
			depth--
		}
	}
	return depth, quoteRune != 0, nil
}

// Statement is one top-level command inside a loop body. Raw is the
// original string (with surrounding whitespace trimmed); Tokens is the
// splitArgs result over Raw.
type Statement struct {
	Raw    string
	Tokens []string
}

// parseStatements splits body into top-level statements, separating on
// ';' and '\n' at brace-depth 0 outside quotes. '#' begins a line
// comment (to end-of-line) outside quotes. Nested '{...}' content is
// preserved verbatim in the enclosing Statement's Raw.
func parseStatements(body string) ([]Statement, error) {
	var out []Statement
	var cur strings.Builder
	var quoteRune rune
	depth := 0
	inComment := false

	flush := func() {
		raw := strings.TrimSpace(cur.String())
		cur.Reset()
		if raw == "" {
			return
		}
		out = append(out, Statement{Raw: raw, Tokens: splitArgs(raw)})
	}

	for _, r := range body {
		if inComment {
			if r == '\n' {
				inComment = false
				flush()
			}
			continue
		}
		if quoteRune != 0 {
			cur.WriteRune(r)
			if r == quoteRune {
				quoteRune = 0
			}
			continue
		}
		switch r {
		case '"', '\'':
			quoteRune = r
			cur.WriteRune(r)
		case '#':
			if depth == 0 {
				inComment = true
			} else {
				cur.WriteRune(r)
			}
		case '{':
			depth++
			cur.WriteRune(r)
		case '}':
			depth--
			if depth < 0 {
				return nil, fmt.Errorf("unexpected '}' with no matching '{'")
			}
			cur.WriteRune(r)
		case ';', '\n':
			if depth == 0 {
				flush()
			} else {
				cur.WriteRune(r)
			}
		default:
			cur.WriteRune(r)
		}
	}
	if quoteRune != 0 {
		return nil, fmt.Errorf("unterminated quoted string")
	}
	if depth != 0 {
		return nil, fmt.Errorf("unbalanced braces (depth=%d at end of input)", depth)
	}
	flush()
	return out, nil
}
