// Package aws implements the qbconf provider for AWS EKS clusters.
package aws

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/rs/zerolog"

	"github.com/raftechio/qbconf/internal/config"
	"github.com/raftechio/qbconf/internal/provider"
	"github.com/raftechio/qbconf/internal/retry"
)

// Name is the provider key used on the CLI.
const Name = "aws"

// identityRetryPolicy retries the initial STS identity check, which is the
// first network call of a run and the most likely to hit transient failures
// (e.g. OIDC token endpoints warming up).
var identityRetryPolicy = retry.Policy{
	MaxAttempts: 3,
	BaseDelay:   2 * time.Second,
	MaxDelay:    10 * time.Second,
}

// Provider generates EKS kubeconfig inputs. All dependencies are injected so
// the type is fully testable without AWS access.
type Provider struct {
	eks      EKSDescriber
	presign  STSPresigner
	identity IdentityGetter
	log      zerolog.Logger
}

// Factory adapts New to the provider.Factory signature.
func Factory(ctx context.Context, cfg *config.Config, log zerolog.Logger) (provider.Provider, error) {
	return New(ctx, cfg, log)
}

// New builds an AWS provider: it loads the SDK configuration, applies the
// configured credential strategy, and verifies the resulting identity.
func New(ctx context.Context, cfg *config.Config, log zerolog.Logger) (*Provider, error) {
	base, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.Region))
	if err != nil {
		return nil, fmt.Errorf("load AWS configuration: %w", err)
	}

	strategy, err := StrategyFromConfig(cfg)
	if err != nil {
		return nil, err
	}
	log.Debug().Str("auth", cfg.Auth).Msg("resolving AWS credentials")

	creds, err := strategy.Resolve(ctx, base)
	if err != nil {
		return nil, fmt.Errorf("resolve AWS credentials: %w", err)
	}
	base.Credentials = creds

	stsClient := sts.NewFromConfig(base)
	p := &Provider{
		eks:      eks.NewFromConfig(base),
		presign:  sts.NewPresignClient(stsClient),
		identity: stsClient,
		log:      log,
	}

	if err := p.verifyIdentity(ctx); err != nil {
		return nil, err
	}
	return p, nil
}

// Name implements provider.Provider.
func (p *Provider) Name() string { return Name }

// GetCluster implements provider.Provider.
func (p *Provider) GetCluster(ctx context.Context, clusterName string) (*provider.ClusterInfo, error) {
	out, err := p.eks.DescribeCluster(ctx, &eks.DescribeClusterInput{
		Name: aws.String(clusterName),
	})
	if err != nil {
		return nil, fmt.Errorf("describe EKS cluster %q: %w", clusterName, err)
	}

	cluster := out.Cluster
	if cluster == nil {
		return nil, fmt.Errorf("describe EKS cluster %q: empty response", clusterName)
	}

	endpoint := aws.ToString(cluster.Endpoint)
	caBase64 := ""
	if cluster.CertificateAuthority != nil {
		caBase64 = aws.ToString(cluster.CertificateAuthority.Data)
	}
	if endpoint == "" || caBase64 == "" {
		return nil, fmt.Errorf("EKS cluster %q is not ready (status %q): endpoint or certificate authority missing", clusterName, cluster.Status)
	}

	caData, err := base64.StdEncoding.DecodeString(caBase64)
	if err != nil {
		return nil, fmt.Errorf("decode certificate authority data for cluster %q: %w", clusterName, err)
	}

	return &provider.ClusterInfo{
		Name:     aws.ToString(cluster.Name),
		Endpoint: endpoint,
		CAData:   caData,
	}, nil
}

// Token implements provider.Provider.
func (p *Provider) Token(ctx context.Context, clusterName string) (string, error) {
	return presignedToken(ctx, p.presign, clusterName)
}

// verifyIdentity fails fast with a clear error when credentials are missing
// or invalid, instead of surfacing an opaque failure mid-generation.
func (p *Provider) verifyIdentity(ctx context.Context) error {
	var out *sts.GetCallerIdentityOutput
	err := retry.Do(ctx, identityRetryPolicy, func(ctx context.Context) error {
		var callErr error
		out, callErr = p.identity.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
		if callErr != nil {
			p.log.Debug().Err(callErr).Msg("STS GetCallerIdentity failed, may retry")
		}
		return callErr
	})
	if err != nil {
		return fmt.Errorf("verify AWS identity: %w", err)
	}

	p.log.Debug().
		Str("arn", aws.ToString(out.Arn)).
		Str("account", aws.ToString(out.Account)).
		Msg("verified AWS caller identity")
	return nil
}
