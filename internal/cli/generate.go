package cli

import (
	"github.com/spf13/cobra"
)

func newGenerateCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate a kubeconfig file for a Kubernetes cluster",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newGenerateAWSCmd(a))

	return cmd
}
