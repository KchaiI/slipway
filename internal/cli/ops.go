package cli

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

func newScaleCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "scale web=<replicas>",
		Short: "Change the number of web replicas and wait until they are ready",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			value, ok := strings.CutPrefix(args[0], "web=")
			if !ok {
				return fmt.Errorf("argument must look like web=3")
			}
			n, err := strconv.ParseInt(value, 10, 32)
			if err != nil {
				return fmt.Errorf("invalid replica count %q", value)
			}
			app, err := resolveApp()
			if err != nil {
				return err
			}
			st, err := client().Scale(app, int32(n))
			if err != nil {
				return err
			}
			fmt.Printf("Scaled %s: web=%d (%d/%d ready)\n", app, n, st.ReadyReplicas, st.Replicas)
			return nil
		},
	}
}

func newRollbackCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "rollback",
		Short: "Redeploy the previous release as a new release",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			app, err := resolveApp()
			if err != nil {
				return err
			}
			rel, err := client().Rollback(app)
			if err != nil {
				return err
			}
			fmt.Printf("Rolled back %s: released v%d (%s)\n", app, rel.Version, rel.Description)
			return nil
		},
	}
}

func newLogsCommand() *cobra.Command {
	var follow bool
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Print app logs from all pods ([pod] prefixed)",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			app, err := resolveApp()
			if err != nil {
				return err
			}
			rc, err := client().Logs(app, follow)
			if err != nil {
				return err
			}
			defer rc.Close()
			_, err = io.Copy(os.Stdout, rc)
			return err
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "stream logs in real time")
	return cmd
}
