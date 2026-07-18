package tool

import "fmt"

// Registry is the set of tools available to a session, in registration
// order.
type Registry struct {
	tools map[string]Tool
	order []string
}

// NewRegistry builds an empty Registry.
func NewRegistry() *Registry {
	return &Registry{tools: map[string]Tool{}}
}

// Register adds t, replacing any existing tool of the same name in place.
func (r *Registry) Register(t Tool) {
	name := t.Name()
	if _, exists := r.tools[name]; !exists {
		r.order = append(r.order, name)
	}
	r.tools[name] = t
}

// Get looks up a tool by name.
func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// List returns every registered tool, in registration order.
func (r *Registry) List() []Tool {
	out := make([]Tool, 0, len(r.order))
	for _, name := range r.order {
		out = append(out, r.tools[name])
	}
	return out
}

// NewSubset builds a new Registry containing only the named tools from
// base, in the given order. It returns an error naming the first tool not
// found in base rather than silently dropping it.
func NewSubset(base *Registry, names []string) (*Registry, error) {
	sub := NewRegistry()
	for _, name := range names {
		t, ok := base.Get(name)
		if !ok {
			return nil, fmt.Errorf("tool: unknown tool %q", name)
		}
		sub.Register(t)
	}
	return sub, nil
}
