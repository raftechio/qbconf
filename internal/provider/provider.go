// Package provider defines the cloud-agnostic contract every qbconf cloud
// implementation satisfies, plus a registry used by the CLI to dispatch
// `generate <cloud>` to the right implementation.
package provider

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/rs/zerolog"

	"github.com/raftechio/qbconf/internal/config"
)

// ClusterInfo is the cloud-agnostic description of a cluster, sufficient to
// build a kubeconfig.
type ClusterInfo struct {
	Name     string
	Endpoint string
	// CAData is the decoded certificate authority bundle (PEM, not base64).
	CAData []byte
}

// Provider produces the two artifacts a kubeconfig needs: cluster metadata
// and a bearer token.
type Provider interface {
	// Name returns the provider key used on the CLI, e.g. "aws".
	Name() string
	// GetCluster resolves cluster connection metadata.
	GetCluster(ctx context.Context, clusterName string) (*ClusterInfo, error)
	// Token produces a short-lived bearer token for the cluster.
	Token(ctx context.Context, clusterName string) (string, error)
}

// Factory constructs a Provider from resolved configuration.
type Factory func(ctx context.Context, cfg *config.Config, log zerolog.Logger) (Provider, error)

// Registry maps provider names to factories. It is populated once in main
// (explicit registration, no init side effects) and read-only afterwards.
type Registry struct {
	factories map[string]Factory
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{factories: make(map[string]Factory)}
}

// Register adds a provider factory under the given name, replacing any
// existing registration.
func (r *Registry) Register(name string, f Factory) {
	r.factories[name] = f
}

// New instantiates the named provider.
func (r *Registry) New(ctx context.Context, name string, cfg *config.Config, log zerolog.Logger) (Provider, error) {
	f, ok := r.factories[name]
	if !ok {
		return nil, fmt.Errorf("unknown provider %q (available: %s)", name, strings.Join(r.Names(), ", "))
	}
	return f(ctx, cfg, log)
}

// Names returns the registered provider names, sorted.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.factories))
	for name := range r.factories {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
