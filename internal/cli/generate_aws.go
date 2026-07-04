package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/raftechio/qbconf/internal/config"
	"github.com/raftechio/qbconf/internal/kubeconfig"
	"github.com/raftechio/qbconf/internal/provider/aws"
)

func newGenerateAWSCmd(a *app) *cobra.Command {
	var cfgFile string

	cmd := &cobra.Command{
		Use:   "aws",
		Short: "Generate a kubeconfig file for an AWS EKS cluster",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.runGenerateAWS(cmd, cfgFile)
		},
	}

	flags := cmd.Flags()
	flags.String("cluster-name", "", "name of the EKS cluster (required)")
	flags.String("region", "eu-west-1", "AWS region")
	flags.String("role-arn", "", "ARN of the AWS IAM role to assume")
	flags.String("role-session-name", "qbconf-session", "name of the AWS STS role session to create")
	flags.String("auth", config.AuthDefault, fmt.Sprintf("authentication mode (%s)", strings.Join(config.AuthModes, "|")))
	flags.String("output-file", "kubeconfig.yaml", "path of the kubeconfig file to write")
	flags.Duration("timeout", 2*time.Minute, "overall timeout for the generate operation")
	flags.StringVar(&cfgFile, "config", "", "path to a qbconf config file (default: ./.qbconf.yaml, then $HOME/.config/qbconf/.qbconf.yaml)")

	// Boolean flags from qbconf v1, kept as deprecated aliases for --auth.
	flags.Bool("with-assume-role", false, "enable assuming of IAM role via STS")
	flags.Bool("with-gha-oidc", false, "enable assuming of IAM role via OIDC for GitHub Actions")
	flags.Bool("with-gitlab-oidc", false, "enable assuming of IAM role via OIDC for GitLab CI/CD")
	_ = flags.MarkDeprecated("with-assume-role", "use --auth=assume-role instead")
	_ = flags.MarkDeprecated("with-gha-oidc", "use --auth=gha-oidc instead")
	_ = flags.MarkDeprecated("with-gitlab-oidc", "use --auth=gitlab-oidc instead")

	return cmd
}

func (a *app) runGenerateAWS(cmd *cobra.Command, cfgFile string) error {
	v := viper.New()
	v.SetEnvPrefix("QBCONF")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	v.AutomaticEnv()

	// AWS_* variables stay honored for backward compatibility with qbconf v1;
	// QBCONF_* takes precedence when both are set.
	if err := errors.Join(
		v.BindEnv("region", "QBCONF_REGION", "AWS_REGION"),
		v.BindEnv("role-arn", "QBCONF_ROLE_ARN", "AWS_ROLE_ARN"),
		v.BindEnv("role-session-name", "QBCONF_ROLE_SESSION_NAME", "AWS_ROLE_SESSION_NAME"),
		v.BindPFlags(cmd.Flags()),
	); err != nil {
		return fmt.Errorf("bind configuration: %w", err)
	}

	if err := readConfigFile(v, cfgFile); err != nil {
		return err
	}

	cfg, err := config.Load(v)
	if err != nil {
		return err
	}

	// Map the deprecated v1 boolean flags onto --auth, unless --auth was set
	// explicitly (an explicit --auth always wins).
	if !cmd.Flags().Changed("auth") {
		switch {
		case v.GetBool("with-assume-role"):
			cfg.Auth = config.AuthAssumeRole
		case v.GetBool("with-gha-oidc"):
			cfg.Auth = config.AuthGHAOIDC
		case v.GetBool("with-gitlab-oidc"):
			cfg.Auth = config.AuthGitLabOIDC
		}
	}

	if err := cfg.Validate(); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), cfg.Timeout)
	defer cancel()

	log := a.log.With().Str("provider", aws.Name).Logger()
	log.Debug().
		Str("cluster", cfg.ClusterName).
		Str("region", cfg.Region).
		Str("auth", cfg.Auth).
		Msg("generating kubeconfig")

	p, err := a.registry.New(ctx, aws.Name, cfg, log)
	if err != nil {
		return fmt.Errorf("initialize %s provider: %w", aws.Name, err)
	}

	info, err := p.GetCluster(ctx, cfg.ClusterName)
	if err != nil {
		return err
	}

	token, err := p.Token(ctx, cfg.ClusterName)
	if err != nil {
		return err
	}

	data, err := kubeconfig.Build(*info, token)
	if err != nil {
		return err
	}

	if err := kubeconfig.Write(cfg.OutputFile, data); err != nil {
		return err
	}

	log.Info().Str("file", cfg.OutputFile).Msg("kubeconfig written")
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "kubeconfig written to %s\n", cfg.OutputFile)
	return nil
}

// readConfigFile loads an explicit --config file, or searches the default
// locations and silently skips when no config file exists.
func readConfigFile(v *viper.Viper, cfgFile string) error {
	if cfgFile != "" {
		v.SetConfigFile(cfgFile)
		if err := v.ReadInConfig(); err != nil {
			return fmt.Errorf("read config file: %w", err)
		}
		return nil
	}

	v.SetConfigName(".qbconf")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")
	if home, err := os.UserHomeDir(); err == nil {
		v.AddConfigPath(filepath.Join(home, ".config", "qbconf"))
	}

	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if errors.As(err, &notFound) {
			return nil
		}
		return fmt.Errorf("read config file: %w", err)
	}
	return nil
}
