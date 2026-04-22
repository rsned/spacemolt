package main

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/peterh/liner"
)

// scanBraceDepth reports the net brace depth of s and whether the scan
// ended inside a quoted string. Braces inside '"..."' or "'...'" are
// ignored. A negative depth indicates more '}' than '{' in s.
func scanBraceDepth(s string) (depth int, inQuote bool) {
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
	return depth, quoteRune != 0
}

// hasTopLevelOpenBrace reports whether s contains a '{' outside of
// any '"..."' or "'...'" quoted string.
func hasTopLevelOpenBrace(s string) bool {
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
			return true
		}
	}
	return false
}

// Statement is one top-level command inside a loop body. Raw is the
// original string (with surrounding whitespace trimmed); Tokens is the
// splitArgs result over Raw.
type Statement struct {
	Raw    string
	Tokens []string
}

// blockPreview returns a short, single-line summary of a block body
// suitable for inclusion in a "🔁 Repeating {...}" status message.
// Newlines within a statement's Raw are collapsed to spaces; statements
// are joined with '; '; the result is truncated to roughly 60 runes
// with a trailing ellipsis if longer.
func blockPreview(stmts []Statement) string {
	const maxLen = 60
	parts := make([]string, len(stmts))
	for i, s := range stmts {
		parts[i] = strings.Join(strings.Fields(s.Raw), " ")
	}
	joined := strings.Join(parts, "; ")
	if len([]rune(joined)) <= maxLen {
		return joined
	}
	runes := []rune(joined)
	return string(runes[:maxLen]) + "…"
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
	// If the user wrote `loop {` (forgetting the count), say so explicitly —
	// "invalid count \"{\"" is confusing because the brace is the block opener,
	// not an attempted count.
	if tokens[idx] == "{" {
		return 0, false, "", false, fmt.Errorf("loop: missing count before '{' (expected 'loop [-f] <count> { ... }')")
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

// executeLoop runs count iterations of body. For each statement whose
// first token is "loop", parseLoopHeader + parseStatements is applied
// and executeLoop recurses; otherwise runStatement is called. Each loop
// enforces errors according to its own force flag: a loop with force
// continues past errors and returns nil; a loop without force returns
// the first error. depth controls indentation of status lines.
func executeLoop(
	ctx context.Context,
	out io.Writer,
	count int,
	force bool,
	body []Statement,
	depth int,
	runStatement func(tokens []string) error,
) error {
	indent := strings.Repeat("  ", depth)
	var firstErr error
	errCount := 0

	for i := range count {
		fmt.Fprintf(out, "%s── [%d/%d]\n", indent, i+1, count) //nolint:errcheck
		iterFailed := false
		for _, stmt := range body {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			var err error
			isLoop := len(stmt.Tokens) > 0 && strings.ToLower(stmt.Tokens[0]) == "loop"
			if isLoop {
				innerCount, innerForce, innerBody, isBlock, perr := parseLoopHeader(stmt)
				if perr != nil {
					err = perr
				} else {
					var innerStmts []Statement
					if isBlock {
						innerStmts, err = parseStatements(innerBody)
					} else {
						innerStmts = []Statement{{Raw: innerBody, Tokens: splitArgs(innerBody)}}
					}
					if err == nil {
						err = executeLoop(ctx, out, innerCount, innerForce, innerStmts, depth+1, runStatement)
					}
				}
			} else {
				err = runStatement(stmt.Tokens)
			}
			if err != nil {
				errCount++
				fmt.Fprintf(out, "%s❌ %v\n", indent, err)               //nolint:errcheck
				if !force {
					fmt.Fprintf(out, "%sStopping loop after %d/%d iterations\n", indent, i+1, count) //nolint:errcheck
					return err
				}
				if firstErr == nil {
					firstErr = err
				}
				// Inner loop failures abort the remaining statements in this
				// outer iteration; plain statement failures continue to the
				// next statement within the same iteration.
				if isLoop {
					iterFailed = true
					break
				}
			}
		}
		if !iterFailed {
			fmt.Fprintf(out, "%s✓ [%d/%d]\n", indent, i+1, count) //nolint:errcheck
		}
	}
	if force && errCount > 0 {
		fmt.Fprintf(out, "%s🔁 Loop finished with %d error(s) out of %d iterations\n", indent, errCount, count) //nolint:errcheck
	}
	if force {
		return nil
	}
	return firstErr
}

// readLogicalCommand reads a command from liner. If the first line has
// unbalanced '{' at top level (outside quotes), it continues reading
// additional lines with a "... " prompt until brace depth returns to 0,
// joining lines with '\n'. Returns the assembled script. On Ctrl-C
// during continuation, returns (combined-so-far, liner.ErrPromptAborted).
func readLogicalCommand(line *liner.State) (string, error) {
	first, err := line.Prompt("$ ")
	if err != nil {
		return "", err
	}
	depth, inQuote := scanBraceDepth(first)
	if depth <= 0 && !inQuote {
		return first, nil
	}
	combined := first
	for depth > 0 || inQuote {
		more, perr := line.Prompt("... ")
		if perr != nil {
			return combined, perr
		}
		combined += "\n" + more
		depth, inQuote = scanBraceDepth(combined)
		if depth < 0 {
			return combined, fmt.Errorf("unbalanced braces")
		}
	}
	return combined, nil
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
