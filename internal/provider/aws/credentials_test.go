package aws

import (
	"strings"
	"testing"

	"github.com/raftechio/qbconf/internal/config"
	"github.com/raftechio/qbconf/internal/provider/aws/oidc"
)

func TestStrategyFromConfig(t *testing.T) {
	base := config.Config{
		RoleARN:         "arn:aws:iam::1:role/x",
		RoleSessionName: "sess",
	}

	t.Run("default", func(t *testing.T) {
		cfg := base
		cfg.Auth = config.AuthDefault
		s, err := StrategyFromConfig(&cfg)
		if err != nil {
			t.Fatalf("StrategyFromConfig() error = %v", err)
		}
		if _, ok := s.(DefaultChain); !ok {
			t.Errorf("strategy = %T, want DefaultChain", s)
		}
	})

	t.Run("assume-role", func(t *testing.T) {
		cfg := base
		cfg.Auth = config.AuthAssumeRole
		s, err := StrategyFromConfig(&cfg)
		if err != nil {
			t.Fatalf("StrategyFromConfig() error = %v", err)
		}
		ar, ok := s.(AssumeRole)
		if !ok {
			t.Fatalf("strategy = %T, want AssumeRole", s)
		}
		if ar.RoleARN != cfg.RoleARN || ar.SessionName != cfg.RoleSessionName {
			t.Errorf("AssumeRole = %+v, want role %q session %q", ar, cfg.RoleARN, cfg.RoleSessionName)
		}
	})

	t.Run("gha-oidc requires GitHub Actions environment", func(t *testing.T) {
		t.Setenv("ACTIONS_ID_TOKEN_REQUEST_URL", "")
		t.Setenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN", "")
		cfg := base
		cfg.Auth = config.AuthGHAOIDC
		if _, err := StrategyFromConfig(&cfg); err == nil {
			t.Error("StrategyFromConfig() = nil error, want missing env var error")
		}
	})

	t.Run("gha-oidc", func(t *testing.T) {
		t.Setenv("ACTIONS_ID_TOKEN_REQUEST_URL", "https://token.actions.example/token?x=1")
		t.Setenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN", "runner-token")
		cfg := base
		cfg.Auth = config.AuthGHAOIDC
		s, err := StrategyFromConfig(&cfg)
		if err != nil {
			t.Fatalf("StrategyFromConfig() error = %v", err)
		}
		wi, ok := s.(WebIdentity)
		if !ok {
			t.Fatalf("strategy = %T, want WebIdentity", s)
		}
		if _, ok := wi.Source.(*oidc.GitHubActions); !ok {
			t.Errorf("source = %T, want *oidc.GitHubActions", wi.Source)
		}
	})

	t.Run("gitlab-oidc", func(t *testing.T) {
		cfg := base
		cfg.Auth = config.AuthGitLabOIDC
		s, err := StrategyFromConfig(&cfg)
		if err != nil {
			t.Fatalf("StrategyFromConfig() error = %v", err)
		}
		wi, ok := s.(WebIdentity)
		if !ok {
			t.Fatalf("strategy = %T, want WebIdentity", s)
		}
		if _, ok := wi.Source.(oidc.GitLab); !ok {
			t.Errorf("source = %T, want oidc.GitLab", wi.Source)
		}
	})

	t.Run("unknown mode", func(t *testing.T) {
		cfg := base
		cfg.Auth = "bogus"
		_, err := StrategyFromConfig(&cfg)
		if err == nil || !strings.Contains(err.Error(), "unsupported auth mode") {
			t.Errorf("StrategyFromConfig() error = %v, want unsupported auth mode", err)
		}
	})
}
