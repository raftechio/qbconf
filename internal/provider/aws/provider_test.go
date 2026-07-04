package aws

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/rs/zerolog"
)

type fakeEKS struct {
	out *eks.DescribeClusterOutput
	err error
}

func (f fakeEKS) DescribeCluster(context.Context, *eks.DescribeClusterInput, ...func(*eks.Options)) (*eks.DescribeClusterOutput, error) {
	return f.out, f.err
}

type fakePresigner struct {
	url string
	err error
}

func (f fakePresigner) PresignGetCallerIdentity(context.Context, *sts.GetCallerIdentityInput, ...func(*sts.PresignOptions)) (*v4.PresignedHTTPRequest, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &v4.PresignedHTTPRequest{Method: "GET", URL: f.url}, nil
}

func readyCluster(name string) *ekstypes.Cluster {
	ca := base64.StdEncoding.EncodeToString([]byte("pem-ca-data"))
	return &ekstypes.Cluster{
		Name:                 awssdk.String(name),
		Endpoint:             awssdk.String("https://example.eks.amazonaws.com"),
		Status:               ekstypes.ClusterStatusActive,
		CertificateAuthority: &ekstypes.Certificate{Data: awssdk.String(ca)},
	}
}

func TestGetCluster(t *testing.T) {
	tests := []struct {
		name    string
		eks     fakeEKS
		wantErr string
	}{
		{
			name: "happy path",
			eks:  fakeEKS{out: &eks.DescribeClusterOutput{Cluster: readyCluster("demo")}},
		},
		{
			name:    "api error",
			eks:     fakeEKS{err: errors.New("AccessDeniedException")},
			wantErr: "describe EKS cluster",
		},
		{
			name:    "empty response",
			eks:     fakeEKS{out: &eks.DescribeClusterOutput{}},
			wantErr: "empty response",
		},
		{
			name: "cluster still creating has no endpoint",
			eks: fakeEKS{out: &eks.DescribeClusterOutput{Cluster: &ekstypes.Cluster{
				Name:   awssdk.String("demo"),
				Status: ekstypes.ClusterStatusCreating,
			}}},
			wantErr: "not ready",
		},
		{
			name: "malformed certificate authority",
			eks: fakeEKS{out: &eks.DescribeClusterOutput{Cluster: &ekstypes.Cluster{
				Name:                 awssdk.String("demo"),
				Endpoint:             awssdk.String("https://example"),
				Status:               ekstypes.ClusterStatusActive,
				CertificateAuthority: &ekstypes.Certificate{Data: awssdk.String("%%% not base64 %%%")},
			}}},
			wantErr: "decode certificate authority",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Provider{eks: tt.eks, log: zerolog.Nop()}

			info, err := p.GetCluster(context.Background(), "demo")
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("GetCluster() error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("GetCluster() error = %v", err)
			}
			if info.Name != "demo" {
				t.Errorf("Name = %q, want %q", info.Name, "demo")
			}
			if info.Endpoint != "https://example.eks.amazonaws.com" {
				t.Errorf("Endpoint = %q", info.Endpoint)
			}
			if string(info.CAData) != "pem-ca-data" {
				t.Errorf("CAData = %q, want decoded PEM", info.CAData)
			}
		})
	}
}

func TestTokenEncodesPresignedURL(t *testing.T) {
	const presignedURL = "https://sts.eu-west-1.amazonaws.com/?Action=GetCallerIdentity&X-Amz-Signature=abc"
	p := &Provider{presign: fakePresigner{url: presignedURL}, log: zerolog.Nop()}

	token, err := p.Token(context.Background(), "demo")
	if err != nil {
		t.Fatalf("Token() error = %v", err)
	}

	if !strings.HasPrefix(token, "k8s-aws-v1.") {
		t.Fatalf("token %q missing k8s-aws-v1. prefix", token)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(token, "k8s-aws-v1."))
	if err != nil {
		t.Fatalf("token payload is not raw-url base64: %v", err)
	}
	if string(decoded) != presignedURL {
		t.Errorf("decoded token = %q, want %q", decoded, presignedURL)
	}
}

func TestTokenPropagatesPresignError(t *testing.T) {
	p := &Provider{presign: fakePresigner{err: errors.New("no credentials")}, log: zerolog.Nop()}
	if _, err := p.Token(context.Background(), "demo"); err == nil {
		t.Error("Token() = nil, want error")
	}
}
