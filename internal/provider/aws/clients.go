package aws

import (
	"context"

	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// The interfaces below describe the minimal AWS SDK surface qbconf consumes.
// The real SDK clients satisfy them implicitly; tests supply small fakes.

// EKSDescriber describes EKS clusters.
type EKSDescriber interface {
	DescribeCluster(ctx context.Context, params *eks.DescribeClusterInput, optFns ...func(*eks.Options)) (*eks.DescribeClusterOutput, error)
}

// STSPresigner presigns STS GetCallerIdentity requests.
type STSPresigner interface {
	PresignGetCallerIdentity(ctx context.Context, params *sts.GetCallerIdentityInput, optFns ...func(*sts.PresignOptions)) (*v4.PresignedHTTPRequest, error)
}

// IdentityGetter resolves the current AWS caller identity.
type IdentityGetter interface {
	GetCallerIdentity(ctx context.Context, params *sts.GetCallerIdentityInput, optFns ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error)
}
