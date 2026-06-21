package worker

import "context"

// CommandRunner executes one already-tokenized command line. tokens[0] is the
// command name; the remaining elements are its arguments. It is the seam shared
// by the play_as REPL (rich dispatch) and the headless worker (lean dispatch).
type CommandRunner interface {
	Run(ctx context.Context, tokens []string) error
}
