package kubeconfig

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/raftechio/qbconf/internal/provider"
)

var update = flag.Bool("update", false, "update golden files")

func TestBuildMatchesGolden(t *testing.T) {
	info := provider.ClusterInfo{
		Name:     "demo-cluster",
		Endpoint: "https://ABCDEF0123456789.gr7.eu-west-1.eks.amazonaws.com",
		CAData:   []byte("-----BEGIN CERTIFICATE-----\ndummy\n-----END CERTIFICATE-----\n"),
	}

	got, err := Build(info, "k8s-aws-v1.dGVzdC10b2tlbg")
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	golden := filepath.Join("testdata", "eks-basic.golden.yaml")
	if *update {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatalf("update golden file: %v", err)
		}
	}

	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden file: %v (run `go test ./internal/kubeconfig -update` to create it)", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("Build() output differs from %s:\n--- got ---\n%s\n--- want ---\n%s", golden, got, want)
	}
}

func TestBuildSetsCurrentContext(t *testing.T) {
	info := provider.ClusterInfo{Name: "c1", Endpoint: "https://example", CAData: []byte("ca")}
	got, err := Build(info, "token")
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if !bytes.Contains(got, []byte("current-context: c1")) {
		t.Errorf("Build() output missing current-context:\n%s", got)
	}
}
