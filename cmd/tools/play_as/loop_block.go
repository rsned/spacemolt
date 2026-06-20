package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/worker"
)

// executeLoop runs count iterations of body. For each statement whose
// first token is "loop", worker.ParseLoopHeader + worker.ParseStatements is applied
// and executeLoop recurses; otherwise runStatement is called. Each loop
// enforces errors according to its own force flag: a loop with force
// continues past errors and returns nil; a loop without force returns
// the first error. depth controls indentation of status lines.
func executeLoop(
	ctx context.Context,
	out io.Writer,
	count int,
	force bool,
	body []worker.Statement,
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
				innerCount, innerForce, innerBody, isBlock, perr := worker.ParseLoopHeader(stmt)
				if perr != nil {
					err = perr
				} else {
					var innerStmts []worker.Statement
					if isBlock {
						innerStmts, err = worker.ParseStatements(innerBody)
					} else {
						innerStmts = []worker.Statement{{Raw: innerBody, Tokens: worker.SplitArgs(innerBody)}}
					}
					if err == nil {
						err = executeLoop(ctx, out, innerCount, innerForce, innerStmts, depth+1, runStatement)
					}
				}
			} else {
				err = runStatement(stmt.Tokens)
			}
			if err != nil {
				// A *game.GoalReachedError signals "this command's goal is
				// already achieved." Treat it as a positive exit from the
				// innermost enclosing loop: print a 🎯 line and return nil.
				// -f is intentionally ignored — -f tolerates errors, not
				// successes, and re-running a satisfied command is pointless.
				var goal *game.GoalReachedError
				if errors.As(err, &goal) {
					fmt.Fprintf(out, "%s🎯 goal reached: %s → exiting loop\n", indent, goal.Message) //nolint:errcheck
					return nil
				}
				// A *tokenError is fatal: an unresolved $TOKEN$ aborts the entire
				// loop immediately, even under -f (which only tolerates ordinary
				// errors). Return it so every enclosing loop level aborts too.
				var tokErr *tokenError
				if errors.As(err, &tokErr) {
					fmt.Fprintf(out, "%s❌ %v → aborting loop\n", indent, tokErr) //nolint:errcheck
					return err
				}
				// Context cancellation is a Ctrl+C interrupt from the REPL, not
				// a command failure: abort the whole loop cleanly (every
				// enclosing level too) rather than printing a ❌ error line.
				if errors.Is(err, context.Canceled) {
					fmt.Fprintf(out, "%s⛔ interrupted after %d/%d iterations\n", indent, i+1, count) //nolint:errcheck
					return err
				}
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
