package respfmt

// Option configures a Format call. Reserved for per-command toggles that
// would otherwise need package-level state — e.g. a future
// WithFullDescriptions() to replace play_as's missionsShowFull bool.
type Option func(*options)

type options struct {
	fullDescriptions bool
}

// WithFullDescriptions disables description truncation in formatters that
// would otherwise cap long bodies (e.g. missions).
func WithFullDescriptions() Option {
	return func(o *options) { o.fullDescriptions = true }
}

func resolveOptions(opts []Option) options {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// Format dispatches command to the matching formatter and returns the
// styled rendering. Returns "" if no formatter is registered for command,
// letting callers fall back to JSON pretty-print.
//
// Formatters are added incrementally as they migrate from play_as.
func Format(command string, raw []byte, opts ...Option) string {
	_ = resolveOptions(opts) // wired in as formatters that consume options land
	switch command {
	// Empty for now — formatters land here as they migrate.
	}
	return ""
}
