package dataservice

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// stubHandler is a minimal Handler for registry tests.
type stubHandler struct {
	name   string
	reply  string
	jsonOK map[string]any
	err    error
}

func (s *stubHandler) Name() string                { return s.name }
func (s *stubHandler) ShortHelp() string           { return "stub help for " + s.name }
func (s *stubHandler) PlaintextUsages() []string { return []string{s.name + " <arg>"} }
func (s *stubHandler) JSONExamples() []map[string]any {
	return []map[string]any{{"query": s.name}}
}
func (s *stubHandler) HandlePlaintext(ctx context.Context, deps Deps, args []string) (string, error) {
	return s.reply, s.err
}
func (s *stubHandler) HandleJSON(ctx context.Context, deps Deps, params map[string]any) (map[string]any, error) {
	return s.jsonOK, s.err
}

func TestRegistry_DispatchPlaintext(t *testing.T) {
	r := NewRegistry(Deps{})
	r.Register(&stubHandler{name: "echo", reply: "hello"})

	got, err := r.Dispatch(context.Background(), "echo world")
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if got != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
}

func TestRegistry_DispatchJSON(t *testing.T) {
	r := NewRegistry(Deps{})
	r.Register(&stubHandler{name: "echo", jsonOK: map[string]any{"status": "ok", "msg": "hi"}})

	got, err := r.Dispatch(context.Background(), `{"query":"echo","params":{}}`)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("reply is not JSON: %v\n%s", err, got)
	}
	if parsed["status"] != "ok" {
		t.Errorf("status: got %v", parsed["status"])
	}
	if parsed["query"] != "echo" {
		t.Errorf("query echo should be injected, got %v", parsed["query"])
	}
}

func TestRegistry_UnknownPlaintext(t *testing.T) {
	r := NewRegistry(Deps{})
	got, err := r.Dispatch(context.Background(), "wat")
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !strings.Contains(got, "unknown") {
		t.Errorf("expected 'unknown' in plaintext error, got %q", got)
	}
	if !strings.Contains(got, "help") {
		t.Errorf("expected 'help' hint in plaintext error, got %q", got)
	}
}

func TestRegistry_UnknownJSON(t *testing.T) {
	r := NewRegistry(Deps{})
	got, err := r.Dispatch(context.Background(), `{"query":"wat"}`)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if parsed["status"] != "error" {
		t.Errorf("status: got %v", parsed["status"])
	}
}

func TestRegistry_MalformedJSON(t *testing.T) {
	r := NewRegistry(Deps{})
	got, err := r.Dispatch(context.Background(), `{broken`)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("error reply should itself be JSON: %v", err)
	}
	if parsed["status"] != "error" {
		t.Errorf("status: got %v", parsed["status"])
	}
}

func TestRegistry_HelpPlaintext(t *testing.T) {
	r := NewRegistry(Deps{})
	r.Register(&stubHandler{name: "alpha"})
	r.Register(&stubHandler{name: "beta"})

	got, err := r.Dispatch(context.Background(), "help")
	if err != nil {
		t.Fatalf("Dispatch help: %v", err)
	}
	if !strings.Contains(got, "alpha") || !strings.Contains(got, "beta") {
		t.Errorf("help missing handlers: %q", got)
	}
	if !strings.Contains(got, "help") {
		t.Errorf("help self-entry missing")
	}
}

func TestRegistry_HelpPlaintext_MultiUsage(t *testing.T) {
	r := NewRegistry(Deps{})
	r.Register(&multiUsageStub{})

	got, err := r.Dispatch(context.Background(), "help")
	if err != nil {
		t.Fatalf("Dispatch help: %v", err)
	}
	if !strings.Contains(got, "multi form-a") || !strings.Contains(got, "multi form-b") {
		t.Errorf("help missing all grammar forms: %q", got)
	}
	if len(got) > MaxReplyChars {
		t.Errorf("help reply exceeds MaxReplyChars: %d > %d", len(got), MaxReplyChars)
	}
}

type multiUsageStub struct{}

func (multiUsageStub) Name() string { return "multi" }
func (multiUsageStub) ShortHelp() string {
	return "multi-form handler"
}
func (multiUsageStub) PlaintextUsages() []string {
	return []string{"multi form-a <x>", "multi form-b <y>"}
}
func (multiUsageStub) JSONExamples() []map[string]any {
	return []map[string]any{{"query": "multi", "params": map[string]any{"x": 1}}}
}
func (multiUsageStub) HandlePlaintext(ctx context.Context, deps Deps, args []string) (string, error) {
	return "", nil
}
func (multiUsageStub) HandleJSON(ctx context.Context, deps Deps, params map[string]any) (map[string]any, error) {
	return nil, nil
}

func TestRegistry_HelpJSON(t *testing.T) {
	r := NewRegistry(Deps{})
	r.Register(&stubHandler{name: "alpha"})

	got, err := r.Dispatch(context.Background(), `{"query":"help"}`)
	if err != nil {
		t.Fatalf("Dispatch help json: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if parsed["query"] != "help" {
		t.Errorf("query: got %v", parsed["query"])
	}
	handlers, ok := parsed["handlers"].([]any)
	if !ok {
		t.Fatalf("handlers should be an array, got %T", parsed["handlers"])
	}
	if len(handlers) == 0 {
		t.Error("expected at least one handler")
	}
	first, ok := handlers[0].(map[string]any)
	if !ok {
		t.Fatalf("handler entry wrong shape: %T", handlers[0])
	}
	if _, ok := first["usages"].([]any); !ok {
		t.Errorf("usages missing or not an array: %v", first["usages"])
	}
	if _, ok := first["examples"].([]any); !ok {
		t.Errorf("examples missing or not an array: %v", first["examples"])
	}
}

func TestRegistry_HandlerError_Plaintext(t *testing.T) {
	r := NewRegistry(Deps{})
	r.Register(&stubHandler{name: "bad", err: ErrParse("missing required field: from_system")})

	got, err := r.Dispatch(context.Background(), "bad foo")
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !strings.Contains(got, "missing required field: from_system") {
		t.Errorf("expected error message surfaced, got %q", got)
	}
}
