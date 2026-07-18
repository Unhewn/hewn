package provider

import "fmt"

var registry = map[string]func() (Provider, error){}

// Register adds a provider constructor under name. Called from a provider
// subpackage's init(), e.g. internal/provider/anthropic.
func Register(name string, ctor func() (Provider, error)) {
	registry[name] = ctor
}

// New constructs the named provider.
func New(name string) (Provider, error) {
	ctor, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("provider: unknown provider %q", name)
	}
	return ctor()
}
