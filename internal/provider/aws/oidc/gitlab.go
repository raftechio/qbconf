package oidc

import (
	"context"
	"os"
	"strings"
)

// gitlabTokenEnvVars are checked in order: ID_TOKEN is the variable name
// conventionally used with GitLab's `id_tokens:` keyword; CI_JOB_JWT_V2 is
// the legacy (deprecated by GitLab) predefined variable.
var gitlabTokenEnvVars = []string{"ID_TOKEN", "CI_JOB_JWT_V2"}

// GitLab reads the web-identity token GitLab CI exposes via the job
// environment. The audience is fixed at token issuance by the job's
// `id_tokens:` configuration, so the audience argument is unused.
type GitLab struct{}

// Token implements TokenSource.
func (GitLab) Token(_ context.Context, _ string) (string, error) {
	for _, name := range gitlabTokenEnvVars {
		if v := os.Getenv(name); v != "" {
			return v, nil
		}
	}
	return "", &MissingEnvVarError{Name: strings.Join(gitlabTokenEnvVars, " or ")}
}
