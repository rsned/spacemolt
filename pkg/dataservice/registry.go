package dataservice

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// ErrParse is returned by handlers when user input is structurally wrong.
// The registry surfaces the error message directly to the requester.
type ErrParse string

// Error implements the error interface.
func (e ErrParse) Error() string { return string(e) }

// Registry is the set of registered handlers. Safe for concurrent reads
// after all handlers are registered at construction time.
type Registry struct {
	mu       sync.RWMutex
	deps     Deps
	handlers map[string]Handler
}

// NewRegistry creates an empty registry backed by the given deps.
func NewRegistry(deps Deps) *Registry {
	return &Registry{
		deps:     deps,
		handlers: make(map[string]Handler),
	}
}

// Register adds a handler. Panics on duplicate names to catch wiring mistakes
// at startup.
func (r *Registry) Register(h Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	name := h.Name()
	if _, exists := r.handlers[name]; exists {
		panic(fmt.Sprintf("dataservice: duplicate handler name %q", name))
	}
	r.handlers[name] = h
}

// Dispatch parses the content, routes to a handler, and returns the
// rendered reply. Errors returned are from the Dispatch mechanism
// itself; handler-level failures are rendered into the reply.
func (r *Registry) Dispatch(ctx context.Context, content string) (string, error) {
	format := DetectFormat(content)

	switch format {
	case FormatJSON:
		return r.dispatchJSON(ctx, content), nil
	default:
		return r.dispatchPlaintext(ctx, content), nil
	}
}

func (r *Registry) dispatchPlaintext(ctx context.Context, content string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return "Error: empty request. Send 'help' for available commands."
	}

	fields := strings.Fields(trimmed)
	query := strings.ToLower(fields[0])
	args := fields[1:]

	if query == "help" {
		return r.helpPlaintext()
	}

	r.mu.RLock()
	h, ok := r.handlers[query]
	r.mu.RUnlock()
	if !ok {
		return fmt.Sprintf("Error: unknown query %q. Send 'help' for available commands.", query)
	}

	reply, err := h.HandlePlaintext(ctx, r.deps, args)
	if err != nil {
		return fmt.Sprintf("Error: %s", err.Error())
	}
	return reply
}

// jsonEnvelope is the top-level JSON request shape.
type jsonEnvelope struct {
	Query  string         `json:"query"`
	Params map[string]any `json:"params"`
}

func (r *Registry) dispatchJSON(ctx context.Context, content string) string {
	var env jsonEnvelope
	if err := json.Unmarshal([]byte(content), &env); err != nil {
		return mustMarshal(map[string]any{
			"status": "error",
			"error":  fmt.Sprintf("invalid JSON: %s", err.Error()),
		})
	}
	query := strings.ToLower(strings.TrimSpace(env.Query))

	if query == "" {
		return mustMarshal(map[string]any{
			"status": "error",
			"error":  "missing required field: query",
		})
	}

	if query == "help" {
		return r.helpJSON()
	}

	r.mu.RLock()
	h, ok := r.handlers[query]
	r.mu.RUnlock()
	if !ok {
		return mustMarshal(map[string]any{
			"query":  query,
			"status": "error",
			"error":  fmt.Sprintf("unknown query %q", query),
		})
	}

	out, err := h.HandleJSON(ctx, r.deps, env.Params)
	if err != nil {
		return mustMarshal(map[string]any{
			"query":  query,
			"status": "error",
			"error":  err.Error(),
		})
	}
	if out == nil {
		out = map[string]any{}
	}
	if _, ok := out["status"]; !ok {
		out["status"] = "ok"
	}
	out["query"] = query
	return mustMarshal(out)
}

// helpPlaintext renders a short command listing.
func (r *Registry) helpPlaintext() string {
	names := r.sortedNames()
	var sb strings.Builder
	sb.WriteString("Dataservice — available queries:\n")
	sb.WriteString("  help — this message\n")
	for _, name := range names {
		h := r.handlers[name]
		fmt.Fprintf(&sb, "  %s — %s\n", h.PlaintextUsage(), h.ShortHelp())
	}
	return sb.String()
}

// helpJSON renders the help response as JSON.
func (r *Registry) helpJSON() string {
	names := r.sortedNames()
	handlers := make([]map[string]any, 0, len(names))
	for _, name := range names {
		h := r.handlers[name]
		handlers = append(handlers, map[string]any{
			"name":            h.Name(),
			"description":     h.ShortHelp(),
			"plaintext_usage": h.PlaintextUsage(),
			"json_example":    h.JSONExample(),
		})
	}
	return mustMarshal(map[string]any{
		"query":    "help",
		"status":   "ok",
		"handlers": handlers,
	})
}

func (r *Registry) sortedNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.handlers))
	for n := range r.handlers {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// mustMarshal marshals to JSON; falls back to a hard-coded error shape
// on failure (no known handler return shapes should ever fail marshal).
func mustMarshal(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return `{"status":"error","error":"internal: marshal failed"}`
	}
	return string(b)
}
