package aws

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/sts"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

const (
	// v1Prefix marks the token as an aws-iam-authenticator v1 bearer token.
	v1Prefix = "k8s-aws-v1."
	// clusterIDHeader binds the presigned request to one EKS cluster.
	clusterIDHeader = "x-k8s-aws-id"
	// requestPresignParam is ignored by STS (presigned GetCallerIdentity URLs
	// are always valid for 15 minutes) but validated to be <= 60 by older
	// aws-iam-authenticator servers, so it stays pinned to 60.
	requestPresignParam = "60"
)

// presignedToken produces the EKS bearer token: a presigned STS
// GetCallerIdentity URL, base64-encoded with the aws-iam-authenticator
// prefix.
func presignedToken(ctx context.Context, presigner STSPresigner, clusterName string) (string, error) {
	req, err := presigner.PresignGetCallerIdentity(ctx, &sts.GetCallerIdentityInput{}, func(po *sts.PresignOptions) {
		po.ClientOptions = append(po.ClientOptions, func(o *sts.Options) {
			o.APIOptions = append(o.APIOptions,
				smithyhttp.SetHeaderValue(clusterIDHeader, clusterName),
				smithyhttp.SetHeaderValue("X-Amz-Expires", requestPresignParam),
			)
		})
	})
	if err != nil {
		return "", fmt.Errorf("presign STS GetCallerIdentity: %w", err)
	}
	return v1Prefix + base64.RawURLEncoding.EncodeToString([]byte(req.URL)), nil
}
