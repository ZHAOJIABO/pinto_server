package ai_generation

import (
	"fmt"
	"sort"
)

// Registry resolves a style's configured provider by name. Selection is driven
// by bb_ai_style.provider rather than by ordered pattern matching, so which
// vendor served a task is explicit and auditable.
type Registry struct {
	providers map[string]Provider
	fallback  string
}

func NewRegistry(fallback string, providers ...Provider) *Registry {
	registry := &Registry{providers: make(map[string]Provider, len(providers)), fallback: fallback}
	for _, provider := range providers {
		if provider != nil {
			registry.providers[provider.Name()] = provider
		}
	}
	return registry
}

func (r *Registry) Get(name string) (Provider, bool) {
	provider, ok := r.providers[name]
	return provider, ok
}

// Resolve returns the provider for a style, falling back to the configured
// default when the style leaves it unset. It returns an error rather than a nil
// provider so the submit path can reject the request before charging credits.
func (r *Registry) Resolve(styleProvider string) (Provider, error) {
	name := styleProvider
	if name == "" {
		name = r.fallback
	}
	provider, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("ai provider %q is not configured", name)
	}
	return provider, nil
}

func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
