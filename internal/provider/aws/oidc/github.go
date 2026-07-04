package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Environment variables provided by GitHub Actions when the workflow has
// `id-token: write` permission.
const (
	githubRequestURLEnv   = "ACTIONS_ID_TOKEN_REQUEST_URL"
	githubRequestTokenEnv = "ACTIONS_ID_TOKEN_REQUEST_TOKEN"
)

// GitHubActions fetches OIDC tokens from the GitHub Actions token endpoint.
type GitHubActions struct {
	RequestURL   string
	RequestToken string
	// HTTPClient overrides the default client, mainly for tests.
	HTTPClient *http.Client
}

// NewGitHubActionsFromEnv builds a GitHubActions source from the environment
// variables GitHub Actions injects into OIDC-enabled workflows.
func NewGitHubActionsFromEnv() (*GitHubActions, error) {
	requestURL := os.Getenv(githubRequestURLEnv)
	if requestURL == "" {
		return nil, &MissingEnvVarError{Name: githubRequestURLEnv}
	}
	requestToken := os.Getenv(githubRequestTokenEnv)
	if requestToken == "" {
		return nil, &MissingEnvVarError{Name: githubRequestTokenEnv}
	}
	return &GitHubActions{RequestURL: requestURL, RequestToken: requestToken}, nil
}

// Token implements TokenSource.
func (g *GitHubActions) Token(ctx context.Context, audience string) (string, error) {
	u, err := url.Parse(g.RequestURL)
	if err != nil {
		return "", fmt.Errorf("parse %s: %w", githubRequestURLEnv, err)
	}
	q := u.Query()
	q.Set("audience", audience)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", fmt.Errorf("build GitHub OIDC token request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+g.RequestToken)
	req.Header.Set("Accept", "application/json")

	client := g.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request GitHub OIDC token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return "", fmt.Errorf("GitHub OIDC token endpoint returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var payload struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("decode GitHub OIDC token response: %w", err)
	}
	if payload.Value == "" {
		return "", errors.New("GitHub OIDC token endpoint returned an empty token")
	}
	return payload.Value, nil
}
