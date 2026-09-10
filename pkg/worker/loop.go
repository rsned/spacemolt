package worker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/rsned/spacemolt/pkg/game"
)

// ExecuteLoop runs count iterations of body. For each statement whose
// first token is "loop", ParseLoopHeader + ParseStatements is applied
// and ExecuteLoop recurses; otherwise runStatement is called. Each loop
// enforces errors according to its own force flag: a loop with force
// continues past errors and returns nil; a loop without force returns
// the first error. depth controls indentation of status lines.
func ExecuteLoop(
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
		iterErrored := false
		for _, stmt := range body {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			var err error
			isLoop := len(stmt.Tokens) > 0 && strings.ToLower(stmt.Tokens[0]) == "loop"
			if isLoop {
				innerCount, innerForce, innerBody, isBlock, perr := ParseLoopHeader(stmt)
				if perr != nil {
					err = perr
				} else {
					var innerStmts []Statement
					if isBlock {
						innerStmts, err = ParseStatements(innerBody)
					} else {
						innerStmts = []Statement{{Raw: innerBody, Tokens: SplitArgs(innerBody)}}
					}
					if err == nil {
						err = ExecuteLoop(ctx, out, innerCount, innerForce, innerStmts, depth+1, runStatement)
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
				// A rate limit is fatal even under -f. Force tolerates
				// FAILURES; this is the server telling us to stop, and a
				// refused command costs no game tick, so retrying past it
				// spins the loop at full speed — the faster we are throttled,
				// the harder we hammer. That feedback loop is what earned the
				// 2026-09-10 IP block, and continuing only escalates the next
				// one (the limiter ladders 2min -> 30min on violations).
				if isRateLimit(err) {
					fmt.Fprintf(out, "%s⛔ rate limited after %d/%d iterations → aborting loop: %v\n", indent, i+1, count, err) //nolint:errcheck
					return fmt.Errorf("%w: %w", ErrRateLimited, err)
				}
				// A *TokenError is fatal: an unresolved $TOKEN$ aborts the entire
				// loop immediately, even under -f (which only tolerates ordinary
				// errors). Return it so every enclosing loop level aborts too.
				var tokErr *TokenError
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
				iterErrored = true
				fmt.Fprintf(out, "%s❌ %v\n", indent, err) //nolint:errcheck
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
		// Pace an iteration that failed and was tolerated by -f. A SUCCESSFUL
		// command already cost a server tick, so it self-paces and is left
		// alone; a REFUSED one costs nothing and returns instantly, which is
		// how `loop -f 100 mine` turns into a burst. Observed 2026-09-09:
		// fighter-7 issued 51 mine attempts in one minute against "Another
		// action is already pending" — a real round-trip each — and crossed
		// the server's 30/min game_mutation cap for its session. Waiting a
		// tick is also the only thing that can CLEAR that particular error.
		if iterErrored && i+1 < count {
			if serr := sleepFunc(ctx, game.SleepTick); serr != nil {
				return serr
			}
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
