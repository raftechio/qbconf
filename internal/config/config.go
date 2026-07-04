// Package config defines qbconf's runtime configuration and its validation.
// Values are resolved by viper with the precedence flags > environment >
// config file > defaults.
package config

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/viper"
)

// Authentication modes for the generate command.
const (
	AuthDefault    = "default"
	AuthAssumeRole = "assume-role"
	AuthGHAOIDC    = "gha-oidc"
	AuthGitLabOIDC = "gitlab-oidc"
)

// AuthModes lists every valid value of Config.Auth.
var AuthModes = []string{AuthDefault, AuthAssumeRole, AuthGHAOIDC, AuthGitLabOIDC}

// Config carries all settings for a generate run.
type Config struct {
	Region          string        `mapstructure:"region"`
	ClusterName     string        `mapstructure:"cluster-name"`
	RoleARN         string        `mapstructure:"role-arn"`
	RoleSessionName string        `mapstructure:"role-session-name"`
	Auth            string        `mapstructure:"auth"`
	OutputFile      string        `mapstructure:"output-file"`
	Timeout         time.Duration `mapstructure:"timeout"`
}

// Load unmarshals the resolved viper state into a Config.
func Load(v *viper.Viper) (*Config, error) {
	var c Config
	if err := v.Unmarshal(&c); err != nil {
		return nil, fmt.Errorf("load configuration: %w", err)
	}
	return &c, nil
}

// Validate reports every configuration problem at once so users can fix
// them in a single pass.
func (c *Config) Validate() error {
	var errs []error

	if c.ClusterName == "" {
		errs = append(errs, errors.New("cluster-name is required"))
	}
	if c.Region == "" {
		errs = append(errs, errors.New("region is required"))
	}
	if c.OutputFile == "" {
		errs = append(errs, errors.New("output-file must not be empty"))
	}
	if c.Timeout <= 0 {
		errs = append(errs, errors.New("timeout must be positive"))
	}

	switch c.Auth {
	case AuthDefault:
	case AuthAssumeRole, AuthGHAOIDC, AuthGitLabOIDC:
		if c.RoleARN == "" {
			errs = append(errs, fmt.Errorf("role-arn is required when auth is %q", c.Auth))
		}
	default:
		errs = append(errs, fmt.Errorf("invalid auth mode %q (valid: %v)", c.Auth, AuthModes))
	}

	return errors.Join(errs...)
}
