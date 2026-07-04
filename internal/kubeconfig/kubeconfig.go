// Package kubeconfig builds and writes kubeconfig files from cloud-agnostic
// cluster metadata. Serialization intentionally goes through
// k8s.io/client-go's clientcmd so the output always matches upstream
// kubeconfig semantics.
package kubeconfig

import (
	"fmt"

	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"

	"github.com/raftechio/qbconf/internal/provider"
)

// Build renders a kubeconfig for the given cluster, authenticating with the
// supplied bearer token.
func Build(info provider.ClusterInfo, token string) ([]byte, error) {
	cfg := api.Config{
		Clusters: map[string]*api.Cluster{
			info.Name: {
				Server:                   info.Endpoint,
				CertificateAuthorityData: info.CAData,
			},
		},
		Contexts: map[string]*api.Context{
			info.Name: {
				Cluster:   info.Name,
				AuthInfo:  info.Name,
				Namespace: "default",
			},
		},
		AuthInfos: map[string]*api.AuthInfo{
			info.Name: {
				Token: token,
			},
		},
		CurrentContext: info.Name,
	}

	data, err := clientcmd.Write(cfg)
	if err != nil {
		return nil, fmt.Errorf("serialize kubeconfig: %w", err)
	}
	return data, nil
}
