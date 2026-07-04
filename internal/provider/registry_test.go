package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/raftechio/qbconf/internal/config"
)

type stubProvider struct{ name string }

func (s stubProvider) Name() string { return s.name }
func (s stubProvider) GetCluster(context.Context, string) (*ClusterInfo, error) {
	return &ClusterInfo{}, nil
}
func (s stubProvider) Token(context.Context, string) (string, error) { return "", nil }

func TestRegistry(t *testing.T) {
	r := NewRegistry()
	r.Register("aws", func(context.Context, *config.Config, zerolog.Logger) (Provider, error) {
		return stubProvider{name: "aws"}, nil
	})
	r.Register("gcp", func(context.Context, *config.Config, zerolog.Logger) (Provider, error) {
		return stubProvider{name: "gcp"}, nil
	})

	p, err := r.New(context.Background(), "aws", &config.Config{}, zerolog.Nop())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if p.Name() != "aws" {
		t.Errorf("Name() = %q, want %q", p.Name(), "aws")
	}

	names := r.Names()
	if len(names) != 2 || names[0] != "aws" || names[1] != "gcp" {
		t.Errorf("Names() = %v, want [aws gcp]", names)
	}
}

func TestRegistryUnknownProvider(t *testing.T) {
	r := NewRegistry()
	r.Register("aws", func(context.Context, *config.Config, zerolog.Logger) (Provider, error) {
		return stubProvider{name: "aws"}, nil
	})

	_, err := r.New(context.Background(), "azure", &config.Config{}, zerolog.Nop())
	if err == nil || !strings.Contains(err.Error(), `unknown provider "azure"`) {
		t.Errorf("New() error = %v, want unknown provider", err)
	}
	if !strings.Contains(err.Error(), "aws") {
		t.Errorf("New() error = %v, want available providers listed", err)
	}
}
