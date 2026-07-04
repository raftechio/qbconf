// Package oidc provides web-identity token sources used to assume AWS IAM
// roles from CI systems.
package oidc

import "context"

// TokenSource fetches a web-identity JWT for the given audience.
type TokenSource interface {
	Token(ctx context.Context, audience string) (string, error)
}

// MissingEnvVarError reports a required environment variable that is not set.
type MissingEnvVarError struct {
	Name string
}

func (e *MissingEnvVarError) Error() string {
	return "missing required environment variable: " + e.Name
}
