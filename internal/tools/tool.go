// Package tools implements the tool-calling layer for the SQL dialect
// conversion agent. Each tool is a plain Go struct implementing Tool; a
// Registry makes them dispatchable by name, and the JSONSchema method
// produces an OpenAI function-calling parameter schema so the future LLM
// loop can request tool calls.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
)

// Tool is a single capability exposed to the agent. Invoke receives raw JSON
// arguments (validated against JSONSchema) and returns a compact textual
// result suitable for pasting back into an LLM context window.
type Tool interface {
	Name() string
	Description() string
	JSONSchema() json.RawMessage
	Invoke(ctx context.Context, args json.RawMessage) (string, error)
}

// Registry dispatches tools by name.
type Registry struct {
	tools map[string]Tool
	order []string
}

// NewRegistry creates a registry populated with the given tools.
func NewRegistry(tools ...Tool) *Registry {
	r := &Registry{tools: map[string]Tool{}}
	for _, t := range tools {
		r.Register(t)
	}
	return r
}

// Register adds a tool, replacing any tool with the same name.
func (r *Registry) Register(t Tool) {
	if _, exists := r.tools[t.Name()]; !exists {
		r.order = append(r.order, t.Name())
	}
	r.tools[t.Name()] = t
}

// Get returns the tool registered under name.
func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// All returns the registered tools in registration order.
func (r *Registry) All() []Tool {
	out := make([]Tool, 0, len(r.order))
	for _, name := range r.order {
		out = append(out, r.tools[name])
	}
	return out
}

// Names returns the registered tool names in registration order.
func (r *Registry) Names() []string {
	names := append([]string(nil), r.order...)
	sort.Strings(names)
	return names
}

// Invoke dispatches args to the named tool.
func (r *Registry) Invoke(ctx context.Context, name string, args json.RawMessage) (string, error) {
	t, ok := r.Get(name)
	if !ok {
		return "", fmt.Errorf("unknown tool %q (available: %v)", name, r.Names())
	}
	return t.Invoke(ctx, args)
}

// unmarshalArgs parses JSON args into a struct, tolerating a missing body.
func unmarshalArgs(args json.RawMessage, v any) error {
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	if err := json.Unmarshal(args, v); err != nil {
		return fmt.Errorf("invalid arguments: %w", err)
	}
	return nil
}
