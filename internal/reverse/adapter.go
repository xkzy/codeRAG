package reverse

import (
	"context"
)

// Adapter is the interface that all binary analysis tool adapters must implement.
type Adapter interface {
	// Name returns the adapter's tool name (e.g., "ghidra", "ida", "binja", "objdump", "gdb", "lldb").
	Name() string

	// Convert takes raw tool output and converts it to NormalizedBinary.
	// The input format varies by tool; the adapter handles parsing and transformation.
	Convert(ctx context.Context, input any) (*NormalizedBinary, error)
}

// AdapterRegistry holds all registered adapters.
type AdapterRegistry struct {
	adapters map[string]Adapter
}

// NewAdapterRegistry creates a new registry with default adapters.
func NewAdapterRegistry() *AdapterRegistry {
	return &AdapterRegistry{
		adapters: make(map[string]Adapter),
	}
}

// Register adds an adapter to the registry.
func (r *AdapterRegistry) Register(a Adapter) {
	r.adapters[a.Name()] = a
}

// Get retrieves an adapter by name.
func (r *AdapterRegistry) Get(name string) (Adapter, bool) {
	a, ok := r.adapters[name]
	return a, ok
}

// List returns all registered adapter names.
func (r *AdapterRegistry) List() []string {
	names := make([]string, 0, len(r.adapters))
	for n := range r.adapters {
		names = append(names, n)
	}
	return names
}
