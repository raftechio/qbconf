// Package cli wires the cobra command tree. It contains no business logic:
// commands resolve configuration, then delegate to providers and the
// kubeconfig package.
package cli

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"

	"github.com/raftechio/qbconf/internal/logging"
	"github.com/raftechio/qbconf/internal/provider"
)

// app carries the dependencies shared by all commands.
type app struct {
	registry *provider.Registry
	log      zerolog.Logger
}

// Execute runs the qbconf CLI and returns the first error encountered.
// It is the caller's job (main) to map that error to an exit code.
func Execute(ctx context.Context, registry *provider.Registry, version string) error {
	return newRootCmd(registry, version).ExecuteContext(ctx)
}

func newRootCmd(registry *provider.Registry, version string) *cobra.Command {
	a := &app{registry: registry}

	root := &cobra.Command{
		Use:           "qbconf",
		Short:         "Minimalistic Kubernetes kubeconfig file generator",
		Long:          "qbconf generates kubeconfig files for managed Kubernetes clusters\nusing short-lived cloud credentials (AWS STS and EKS APIs today).",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			level, _ := cmd.Flags().GetString("log-level")
			format, _ := cmd.Flags().GetString("log-format")

			log, err := logging.New(level, format, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			a.log = log.With().Str("run_id", uuid.NewString()).Logger()
			return nil
		},
	}

	root.PersistentFlags().String("log-level", "warn", "log level (trace|debug|info|warn|error)")
	root.PersistentFlags().String("log-format", logging.FormatAuto, "log format (auto|json|console)")

	root.AddCommand(newGenerateCmd(a))
	root.AddCommand(newVersionCmd(version))

	return root
}

func newVersionCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the qbconf version",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), version)
		},
	}
}
