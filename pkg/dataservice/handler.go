package dataservice

import (
	"context"

	"github.com/rsned/spacemolt/pkg/galaxy"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// Format is the reply format expected by a requester.
type Format int

const (
	// FormatPlaintext produces styled human-readable replies.
	FormatPlaintext Format = iota
	// FormatJSON produces machine-readable JSON replies.
	FormatJSON
)

// Deps bundles the runtime dependencies that concrete handlers may need.
// Passed to each handler's Execute method. Handlers must not mutate
// any field on Deps.
type Deps struct {
	KB    knowledge.Base
	Graph *galaxy.GalaxyGraph
	// Tick returns the current game tick or 0 if no clock is available.
	Tick func() int64
}

// Handler is a single data-query handler registered with the service.
// Handlers must be concurrency-safe; the service may dispatch queries
// from multiple goroutines in the future.
type Handler interface {
	// Name returns the handler's query keyword (e.g. "nearest").
	Name() string

	// ShortHelp returns a one-line description shown in `help` output.
	ShortHelp() string

	// PlaintextUsages returns one or more grammar lines shown in `help`
	// output, e.g. "nearest <poi_type> from <system_id>". A handler with
	// multiple supported grammars should return one entry per form.
	PlaintextUsages() []string

	// JSONExamples returns one or more request examples for `help` output.
	// Each example is a fully-formed JSON request body as a map.
	JSONExamples() []map[string]any

	// HandlePlaintext parses the tail of a plaintext request (tokens after
	// the query keyword) and returns the styled reply.
	HandlePlaintext(ctx context.Context, deps Deps, args []string) (string, error)

	// HandleJSON parses a JSON `params` object and returns a JSON reply
	// as a map the registry will marshal.
	HandleJSON(ctx context.Context, deps Deps, params map[string]any) (map[string]any, error)
}
