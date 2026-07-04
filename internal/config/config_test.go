package config

import (
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
)

func validConfig() *Config {
	return &Config{
		Region:          "eu-west-1",
		ClusterName:     "demo",
		RoleSessionName: "qbconf-session",
		Auth:            AuthDefault,
		OutputFile:      "kubeconfig.yaml",
		Timeout:         time.Minute,
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string // substring of the expected error, empty for success
	}{
		{name: "valid default auth", mutate: func(*Config) {}},
		{
			name:   "valid assume-role with role arn",
			mutate: func(c *Config) { c.Auth = AuthAssumeRole; c.RoleARN = "arn:aws:iam::1:role/x" },
		},
		{
			name:    "missing cluster name",
			mutate:  func(c *Config) { c.ClusterName = "" },
			wantErr: "cluster-name is required",
		},
		{
			name:    "missing region",
			mutate:  func(c *Config) { c.Region = "" },
			wantErr: "region is required",
		},
		{
			name:    "assume-role without role arn",
			mutate:  func(c *Config) { c.Auth = AuthAssumeRole },
			wantErr: "role-arn is required",
		},
		{
			name:    "gha-oidc without role arn",
			mutate:  func(c *Config) { c.Auth = AuthGHAOIDC },
			wantErr: "role-arn is required",
		},
		{
			name:    "gitlab-oidc without role arn",
			mutate:  func(c *Config) { c.Auth = AuthGitLabOIDC },
			wantErr: "role-arn is required",
		},
		{
			name:    "unknown auth mode",
			mutate:  func(c *Config) { c.Auth = "magic" },
			wantErr: `invalid auth mode "magic"`,
		},
		{
			name:    "empty output file",
			mutate:  func(c *Config) { c.OutputFile = "" },
			wantErr: "output-file",
		},
		{
			name:    "non-positive timeout",
			mutate:  func(c *Config) { c.Timeout = 0 },
			wantErr: "timeout must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.mutate(cfg)

			err := cfg.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() = %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateReportsAllProblemsAtOnce(t *testing.T) {
	cfg := &Config{Auth: "bogus"}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want error")
	}
	for _, want := range []string{"cluster-name", "region", "output-file", "timeout", "auth mode"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Validate() error %q missing %q", err, want)
		}
	}
}

func TestLoad(t *testing.T) {
	v := viper.New()
	v.Set("region", "us-east-1")
	v.Set("cluster-name", "prod")
	v.Set("role-arn", "arn:aws:iam::1:role/x")
	v.Set("role-session-name", "sess")
	v.Set("auth", AuthAssumeRole)
	v.Set("output-file", "out.yaml")
	v.Set("timeout", "90s")

	cfg, err := Load(v)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	want := Config{
		Region:          "us-east-1",
		ClusterName:     "prod",
		RoleARN:         "arn:aws:iam::1:role/x",
		RoleSessionName: "sess",
		Auth:            AuthAssumeRole,
		OutputFile:      "out.yaml",
		Timeout:         90 * time.Second,
	}
	if *cfg != want {
		t.Errorf("Load() = %+v, want %+v", *cfg, want)
	}
}
