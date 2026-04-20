package main

import (
	"fmt"
	"strconv"
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

// parseLoopHeader parses the header of a "loop" statement. It accepts
// the existing single-command form (e.g. "loop 5 mine") and the new
// block form (e.g. "loop 5 { mine; refuel }"). For the block form the
// returned body is the content between the outermost matching braces;
// for the single form it is the concatenation of remaining args.
func parseLoopHeader(stmt Statement) (count int, force bool, body string, isBlock bool, err error) {
	tokens := stmt.Tokens
	if len(tokens) == 0 || strings.ToLower(tokens[0]) != "loop" {
		return 0, false, "", false, fmt.Errorf("not a loop statement")
	}
	idx := 1
	if idx < len(tokens) && tokens[idx] == "-f" {
		force = true
		idx++
	}
	if idx >= len(tokens) {
		return 0, false, "", false, fmt.Errorf("loop: missing count")
	}
	n, cerr := strconv.Atoi(tokens[idx])
	if cerr != nil || n < 1 {
		return 0, false, "", false, fmt.Errorf("loop: invalid count %q (must be a positive integer)", tokens[idx])
	}
	count = n
	idx++
	if idx >= len(tokens) {
		return 0, false, "", false, fmt.Errorf("loop: missing command(s)")
	}

	// Find where the header ends in Raw so we can inspect the remainder.
	rest, ferr := afterTokens(stmt.Raw, tokens[:idx])
	if ferr != nil {
		return 0, false, "", false, ferr
	}
	rest = strings.TrimLeft(rest, " \t")

	if strings.HasPrefix(rest, "{") {
		inner, ok := extractBracedBody(rest)
		if !ok {
			return 0, false, "", false, fmt.Errorf("loop: unclosed '{' in block body")
		}
		return count, force, inner, true, nil
	}
	return count, force, rest, false, nil
}

// afterTokens returns the substring of raw that begins after the last
// character of the final token in tokens. Token boundaries use the same
// whitespace + quote-pair rules as splitArgs (no backslash escapes).
func afterTokens(raw string, tokens []string) (string, error) {
	pos := 0
	for _, want := range tokens {
		for pos < len(raw) && (raw[pos] == ' ' || raw[pos] == '\t') {
			pos++
		}
		start := pos
		var quoteByte byte
		var got strings.Builder
		for pos < len(raw) {
			c := raw[pos]
			if quoteByte != 0 {
				if c == quoteByte {
					quoteByte = 0
				} else {
					got.WriteByte(c)
				}
				pos++
				continue
			}
			if c == '"' || c == '\'' {
				quoteByte = c
				pos++
				continue
			}
			if c == ' ' || c == '\t' {
				break
			}
			got.WriteByte(c)
			pos++
		}
		if got.String() != want {
			return "", fmt.Errorf("afterTokens: expected %q at %d, got %q", want, start, got.String())
		}
	}
	return raw[pos:], nil
}

// extractBracedBody assumes s starts with '{' and returns the content
// between that '{' and its matching '}', respecting quotes and nesting.
func extractBracedBody(s string) (body string, ok bool) {
	if len(s) == 0 || s[0] != '{' {
		return "", false
	}
	depth := 0
	var quoteRune rune
	for i, r := range s {
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
			if depth == 0 {
				return s[1:i], true
			}
		}
	}
	return "", false
}
