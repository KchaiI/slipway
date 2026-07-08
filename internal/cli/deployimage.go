package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// deploy-image is a maintenance command (hidden from help): it deploys a
// prebuilt image directly, bypassing the git build pipeline. Used by
// acceptance tests and for debugging.
func newDeployImageCommand() *cobra.Command {
	return &cobra.Command{
		Use:    "deploy-image <image>",
		Short:  "Deploy a prebuilt container image (bypasses the git pipeline)",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			app, err := resolveApp()
			if err != nil {
				return err
			}
			rel, err := client().DeployImage(app, args[0])
			if err != nil {
				return err
			}
			fmt.Printf("Released v%d (%s) — %s\n", rel.Version, rel.Status, rel.Image)
			return nil
		},
	}
}
