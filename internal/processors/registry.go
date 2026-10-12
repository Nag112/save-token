package processors

import (
	"fmt"
	"sort"

	"github.com/Nag112/save-token/internal/config"
)

// Registry holds enabled processors in routing order (ports
// registry.ProcessorRegistry). Generic must always be present.
type Registry struct {
	list    []Processor
	byName  map[string]Processor
	generic Processor
}

// NewRegistry builds the registry from processors, applying the
// disabled_processors config filter. Panics if generic is missing after
// filtering — mirrors the Python ValueError and is a build-time bug.
func NewRegistry(all ...Processor) *Registry {
	disabled := map[string]bool{}
	for _, name := range config.Get().DisabledProcessors {
		disabled[name] = true
	}
	var list []Processor
	byName := map[string]Processor{}
	for _, p := range all {
		if disabled[p.Name()] {
			continue
		}
		if _, dup := byName[p.Name()]; dup {
			panic(fmt.Sprintf("duplicate processor name %q", p.Name()))
		}
		list = append(list, p)
		byName[p.Name()] = p
	}
	sort.SliceStable(list, func(i, j int) bool {
		return list[i].Priority() < list[j].Priority()
	})
	generic := byName["generic"]
	if generic == nil {
		panic("generic processor must not be disabled")
	}
	return &Registry{list: list, byName: byName, generic: generic}
}

// DefaultRegistry returns the v1 processor set. Specialized processors are
// added by later steps; generic is always included.
func DefaultRegistry() *Registry {
	return NewRegistry(append(Specialized(), NewGeneric())...)
}

// Specialized returns the specialized v1 processors (without generic).
func Specialized() []Processor {
	return []Processor{NewGit(), NewTest(), NewPackageInstall(), NewBuild(), NewLint()}
}

// List returns processors in routing order.
func (r *Registry) List() []Processor { return r.list }

// Get looks a processor up by name; nil when absent or disabled.
func (r *Registry) Get(name string) Processor { return r.byName[name] }

// Generic returns the fallback processor.
func (r *Registry) Generic() Processor { return r.generic }

// HookPatterns collects every processor's anchored hook patterns, in
// registry order — the input for the gate's compressibility policy.
func (r *Registry) HookPatterns() []string {
	var out []string
	for _, p := range r.list {
		out = append(out, p.HookPatterns()...)
	}
	return out
}
