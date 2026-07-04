package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/raftechio/qbconf/internal/config"
	"github.com/raftechio/qbconf/internal/provider"
)

type fakeProvider struct {
	info  provider.ClusterInfo
	token string
}

func (f *fakeProvider) Name() string { return "aws" }
func (f *fakeProvider) GetCluster(context.Context, string) (*provider.ClusterInfo, error) {
	return &f.info, nil
}
func (f *fakeProvider) Token(context.Context, string) (string, error) { return f.token, nil }

// newTestRegistry registers a fake aws provider and captures the config the
// factory receives.
func newTestRegistry(captured **config.Config) *provider.Registry {
	r := provider.NewRegistry()
	r.Register("aws", func(_ context.Context, cfg *config.Config, _ zerolog.Logger) (provider.Provider, error) {
		if captured != nil {
			*captured = cfg
		}
		return &fakeProvider{
			info: provider.ClusterInfo{
				Name:     "demo",
				Endpoint: "https://example.eks.amazonaws.com",
				CAData:   []byte("ca"),
			},
			token: "k8s-aws-v1.dummy",
		}, nil
	})
	return r
}

func run(t *testing.T, registry *provider.Registry, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	root := newRootCmd(registry, "test-version")
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs(args)
	err = root.ExecuteContext(context.Background())
	return out.String(), errOut.String(), err
}

func TestGenerateAWSWritesKubeconfig(t *testing.T) {
	output := filepath.Join(t.TempDir(), "kubeconfig.yaml")

	stdout, _, err := run(t, newTestRegistry(nil),
		"generate", "aws", "--cluster-name", "demo", "--output-file", output)
	if err != nil {
		t.Fatalf("execute error = %v", err)
	}
	if !strings.Contains(stdout, output) {
		t.Errorf("stdout = %q, want mention of %q", stdout, output)
	}

	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read kubeconfig: %v", err)
	}
	for _, want := range []string{"k8s-aws-v1.dummy", "https://example.eks.amazonaws.com", "current-context: demo"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("kubeconfig missing %q:\n%s", want, data)
		}
	}
}

func TestGenerateAWSRequiresClusterName(t *testing.T) {
	_, _, err := run(t, newTestRegistry(nil), "generate", "aws")
	if err == nil || !strings.Contains(err.Error(), "cluster-name is required") {
		t.Errorf("execute error = %v, want cluster-name validation error", err)
	}
}

func TestGenerateAWSRejectsInvalidAuthMode(t *testing.T) {
	_, _, err := run(t, newTestRegistry(nil),
		"generate", "aws", "--cluster-name", "demo", "--auth", "bogus")
	if err == nil || !strings.Contains(err.Error(), "invalid auth mode") {
		t.Errorf("execute error = %v, want invalid auth mode error", err)
	}
}

func TestGenerateAWSAuthModeRequiresRoleARN(t *testing.T) {
	_, _, err := run(t, newTestRegistry(nil),
		"generate", "aws", "--cluster-name", "demo", "--auth", config.AuthAssumeRole)
	if err == nil || !strings.Contains(err.Error(), "role-arn is required") {
		t.Errorf("execute error = %v, want role-arn validation error", err)
	}
}

func TestGenerateAWSDeprecatedFlagMapsToAuth(t *testing.T) {
	output := filepath.Join(t.TempDir(), "kubeconfig.yaml")

	var captured *config.Config
	_, _, err := run(t, newTestRegistry(&captured),
		"generate", "aws",
		"--cluster-name", "demo",
		"--output-file", output,
		"--with-assume-role",
		"--role-arn", "arn:aws:iam::1:role/x")
	if err != nil {
		t.Fatalf("execute error = %v", err)
	}
	if captured == nil {
		t.Fatal("provider factory was never called")
	}
	if captured.Auth != config.AuthAssumeRole {
		t.Errorf("Auth = %q, want %q (deprecated --with-assume-role must map onto --auth)", captured.Auth, config.AuthAssumeRole)
	}
}

func TestGenerateAWSExplicitAuthWinsOverDeprecatedFlag(t *testing.T) {
	output := filepath.Join(t.TempDir(), "kubeconfig.yaml")

	var captured *config.Config
	_, _, err := run(t, newTestRegistry(&captured),
		"generate", "aws",
		"--cluster-name", "demo",
		"--output-file", output,
		"--auth", config.AuthDefault,
		"--with-assume-role")
	if err != nil {
		t.Fatalf("execute error = %v", err)
	}
	if captured.Auth != config.AuthDefault {
		t.Errorf("Auth = %q, want %q (explicit --auth must win)", captured.Auth, config.AuthDefault)
	}
}

func TestGenerateAWSReadsEnvironmentVariables(t *testing.T) {
	t.Setenv("QBCONF_CLUSTER_NAME", "from-env")
	output := filepath.Join(t.TempDir(), "kubeconfig.yaml")

	var captured *config.Config
	_, _, err := run(t, newTestRegistry(&captured),
		"generate", "aws", "--output-file", output)
	if err != nil {
		t.Fatalf("execute error = %v", err)
	}
	if captured.ClusterName != "from-env" {
		t.Errorf("ClusterName = %q, want %q", captured.ClusterName, "from-env")
	}
}

func TestGenerateAWSHonorsAWSRegionEnvVar(t *testing.T) {
	t.Setenv("AWS_REGION", "us-west-2")
	output := filepath.Join(t.TempDir(), "kubeconfig.yaml")

	var captured *config.Config
	_, _, err := run(t, newTestRegistry(&captured),
		"generate", "aws", "--cluster-name", "demo", "--output-file", output)
	if err != nil {
		t.Fatalf("execute error = %v", err)
	}
	if captured.Region != "us-west-2" {
		t.Errorf("Region = %q, want %q (AWS_REGION must stay honored)", captured.Region, "us-west-2")
	}
}

func TestVersionCommand(t *testing.T) {
	stdout, _, err := run(t, newTestRegistry(nil), "version")
	if err != nil {
		t.Fatalf("execute error = %v", err)
	}
	if strings.TrimSpace(stdout) != "test-version" {
		t.Errorf("stdout = %q, want %q", stdout, "test-version")
	}
}
