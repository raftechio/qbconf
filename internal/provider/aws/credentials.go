package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	"github.com/raftechio/qbconf/internal/config"
	"github.com/raftechio/qbconf/internal/provider/aws/oidc"
)

// stsAudience is the audience requested for web-identity tokens.
const stsAudience = "sts.amazonaws.com"

// CredentialStrategy layers a credentials provider on top of the base AWS
// configuration. Exactly one strategy is selected per run from Config.Auth.
type CredentialStrategy interface {
	Resolve(ctx context.Context, base aws.Config) (aws.CredentialsProvider, error)
}

// DefaultChain keeps the SDK's default credential resolution untouched.
type DefaultChain struct{}

// Resolve implements CredentialStrategy.
func (DefaultChain) Resolve(_ context.Context, base aws.Config) (aws.CredentialsProvider, error) {
	return base.Credentials, nil
}

// AssumeRole assumes an IAM role via STS using the base credentials.
type AssumeRole struct {
	RoleARN     string
	SessionName string
}

// Resolve implements CredentialStrategy.
func (a AssumeRole) Resolve(_ context.Context, base aws.Config) (aws.CredentialsProvider, error) {
	stsClient := sts.NewFromConfig(base)
	p := stscreds.NewAssumeRoleProvider(stsClient, a.RoleARN, func(o *stscreds.AssumeRoleOptions) {
		o.RoleSessionName = a.SessionName
	})
	return aws.NewCredentialsCache(p), nil
}

// WebIdentity assumes an IAM role via STS AssumeRoleWithWebIdentity using an
// OIDC token source. The SDK provider refreshes credentials on expiry, unlike
// static credentials captured once.
type WebIdentity struct {
	RoleARN     string
	SessionName string
	Source      oidc.TokenSource
}

// Resolve implements CredentialStrategy.
func (w WebIdentity) Resolve(ctx context.Context, base aws.Config) (aws.CredentialsProvider, error) {
	stsClient := sts.NewFromConfig(base)
	retriever := tokenRetriever{ctx: ctx, audience: stsAudience, source: w.Source}
	p := stscreds.NewWebIdentityRoleProvider(stsClient, w.RoleARN, retriever, func(o *stscreds.WebIdentityRoleOptions) {
		o.RoleSessionName = w.SessionName
	})
	return aws.NewCredentialsCache(p), nil
}

// tokenRetriever adapts oidc.TokenSource to the SDK's IdentityTokenRetriever.
// The SDK interface takes no context, so the run's context is captured at
// construction time.
type tokenRetriever struct {
	ctx      context.Context
	audience string
	source   oidc.TokenSource
}

func (t tokenRetriever) GetIdentityToken() ([]byte, error) {
	token, err := t.source.Token(t.ctx, t.audience)
	if err != nil {
		return nil, err
	}
	return []byte(token), nil
}

// StrategyFromConfig maps the validated auth mode to a credential strategy.
func StrategyFromConfig(cfg *config.Config) (CredentialStrategy, error) {
	switch cfg.Auth {
	case config.AuthDefault:
		return DefaultChain{}, nil
	case config.AuthAssumeRole:
		return AssumeRole{RoleARN: cfg.RoleARN, SessionName: cfg.RoleSessionName}, nil
	case config.AuthGHAOIDC:
		source, err := oidc.NewGitHubActionsFromEnv()
		if err != nil {
			return nil, err
		}
		return WebIdentity{RoleARN: cfg.RoleARN, SessionName: cfg.RoleSessionName, Source: source}, nil
	case config.AuthGitLabOIDC:
		return WebIdentity{RoleARN: cfg.RoleARN, SessionName: cfg.RoleSessionName, Source: oidc.GitLab{}}, nil
	default:
		return nil, fmt.Errorf("unsupported auth mode %q", cfg.Auth)
	}
}
