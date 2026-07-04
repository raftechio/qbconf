package oidc

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewGitHubActionsFromEnv(t *testing.T) {
	t.Run("both variables present", func(t *testing.T) {
		t.Setenv(githubRequestURLEnv, "https://token.example/x")
		t.Setenv(githubRequestTokenEnv, "secret")
		src, err := NewGitHubActionsFromEnv()
		if err != nil {
			t.Fatalf("NewGitHubActionsFromEnv() error = %v", err)
		}
		if src.RequestURL != "https://token.example/x" || src.RequestToken != "secret" {
			t.Errorf("source = %+v", src)
		}
	})

	t.Run("missing url", func(t *testing.T) {
		t.Setenv(githubRequestURLEnv, "")
		t.Setenv(githubRequestTokenEnv, "secret")
		_, err := NewGitHubActionsFromEnv()
		var missing *MissingEnvVarError
		if !errors.As(err, &missing) || missing.Name != githubRequestURLEnv {
			t.Errorf("error = %v, want MissingEnvVarError for %s", err, githubRequestURLEnv)
		}
	})

	t.Run("missing token", func(t *testing.T) {
		t.Setenv(githubRequestURLEnv, "https://token.example/x")
		t.Setenv(githubRequestTokenEnv, "")
		_, err := NewGitHubActionsFromEnv()
		var missing *MissingEnvVarError
		if !errors.As(err, &missing) || missing.Name != githubRequestTokenEnv {
			t.Errorf("error = %v, want MissingEnvVarError for %s", err, githubRequestTokenEnv)
		}
	})
}

func TestGitHubActionsToken(t *testing.T) {
	t.Run("happy path sends bearer token and audience", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if got := r.Header.Get("Authorization"); got != "Bearer runner-token" {
				t.Errorf("Authorization = %q", got)
			}
			if got := r.URL.Query().Get("audience"); got != "sts.amazonaws.com" {
				t.Errorf("audience = %q", got)
			}
			// The original request URL already carries a query parameter,
			// which must survive audience injection.
			if got := r.URL.Query().Get("keep"); got != "1" {
				t.Errorf("keep = %q, want original query preserved", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"value":"the-jwt"}`))
		}))
		defer srv.Close()

		src := &GitHubActions{RequestURL: srv.URL + "?keep=1", RequestToken: "runner-token", HTTPClient: srv.Client()}
		token, err := src.Token(context.Background(), "sts.amazonaws.com")
		if err != nil {
			t.Fatalf("Token() error = %v", err)
		}
		if token != "the-jwt" {
			t.Errorf("token = %q, want %q", token, "the-jwt")
		}
	})

	t.Run("non-2xx status is an error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "forbidden", http.StatusForbidden)
		}))
		defer srv.Close()

		src := &GitHubActions{RequestURL: srv.URL, RequestToken: "t", HTTPClient: srv.Client()}
		_, err := src.Token(context.Background(), "aud")
		if err == nil || !strings.Contains(err.Error(), "403") {
			t.Errorf("Token() error = %v, want 403 status error", err)
		}
	})

	t.Run("empty token value is an error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"value":""}`))
		}))
		defer srv.Close()

		src := &GitHubActions{RequestURL: srv.URL, RequestToken: "t", HTTPClient: srv.Client()}
		_, err := src.Token(context.Background(), "aud")
		if err == nil || !strings.Contains(err.Error(), "empty token") {
			t.Errorf("Token() error = %v, want empty token error", err)
		}
	})

	t.Run("malformed json is an error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`not json`))
		}))
		defer srv.Close()

		src := &GitHubActions{RequestURL: srv.URL, RequestToken: "t", HTTPClient: srv.Client()}
		_, err := src.Token(context.Background(), "aud")
		if err == nil || !strings.Contains(err.Error(), "decode") {
			t.Errorf("Token() error = %v, want decode error", err)
		}
	})
}
