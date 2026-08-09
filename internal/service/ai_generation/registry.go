package ai_generation

import (
	"fmt"
	"sort"
)

// Registry maps a configured model key to the adapter that serves it. Selection
// is driven by that key (bb_config, then bb_ai_style.provider, then the YAML
// default) rather than by ordered pattern matching, so which model served a task
// is explicit and auditable.
type Registry struct {
	providers map[string]Provider
}

func NewRegistry(providers ...Provider) *Registry {
	registry := &Registry{providers: make(map[string]Provider, len(providers))}
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

// Resolve returns an error rather than a nil provider so the submit path can
// reject the request before charging credits.
func (r *Registry) Resolve(name string) (Provider, error) {
	provider, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("ai model %q is not configured", name)
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
