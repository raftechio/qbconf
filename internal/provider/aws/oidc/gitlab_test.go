package oidc

import (
	"context"
	"errors"
	"testing"
)

func TestGitLabToken(t *testing.T) {
	t.Run("prefers ID_TOKEN", func(t *testing.T) {
		t.Setenv("ID_TOKEN", "new-style")
		t.Setenv("CI_JOB_JWT_V2", "legacy")
		token, err := GitLab{}.Token(context.Background(), "aud")
		if err != nil {
			t.Fatalf("Token() error = %v", err)
		}
		if token != "new-style" {
			t.Errorf("token = %q, want %q", token, "new-style")
		}
	})

	t.Run("falls back to CI_JOB_JWT_V2", func(t *testing.T) {
		t.Setenv("ID_TOKEN", "")
		t.Setenv("CI_JOB_JWT_V2", "legacy")
		token, err := GitLab{}.Token(context.Background(), "aud")
		if err != nil {
			t.Fatalf("Token() error = %v", err)
		}
		if token != "legacy" {
			t.Errorf("token = %q, want %q", token, "legacy")
		}
	})

	t.Run("errors when neither variable is set", func(t *testing.T) {
		t.Setenv("ID_TOKEN", "")
		t.Setenv("CI_JOB_JWT_V2", "")
		_, err := GitLab{}.Token(context.Background(), "aud")
		var missing *MissingEnvVarError
		if !errors.As(err, &missing) {
			t.Errorf("error = %v, want MissingEnvVarError", err)
		}
	})
}
