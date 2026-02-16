package tool

import "context"

// Executor is the interface for a tool that can be executed.
type Executor interface {
	Name() string
	Execute(ctx context.Context, args map[string]any) Result
}

// Result holds the output of a tool execution.
type Result struct {
	Output  string
	IsError bool
}

// Registry maps tool names to executors.
type Registry struct {
	tools map[string]Executor
}

func NewRegistry(executors ...Executor) *Registry {
	r := &Registry{tools: make(map[string]Executor)}
	for _, e := range executors {
		r.tools[e.Name()] = e
	}
	return r
}

func (r *Registry) Get(name string) (Executor, bool) {
	e, ok := r.tools[name]
	return e, ok
}

func (r *Registry) Has(name string) bool {
	_, ok := r.tools[name]
	return ok
}

// Names returns all registered tool names (for building tool definitions).
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	return names
}
