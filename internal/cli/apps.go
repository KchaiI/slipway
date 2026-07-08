package cli

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func newAppsCommand() *cobra.Command {
	apps := &cobra.Command{
		Use:   "apps",
		Short: "Manage apps",
	}

	create := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new app (namespace + git repository)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			app, err := client().CreateApp(args[0])
			if err != nil {
				return err
			}
			fmt.Printf("Created app %q.\n", app.Name)
			fmt.Printf("  git remote:  %s\n", app.GitURL)
			fmt.Printf("  url:         %s\n", app.URL)
			fmt.Printf("\nTo deploy:\n  git remote add minato %s\n  git push minato main\n", app.GitURL)
			return nil
		},
	}

	list := &cobra.Command{
		Use:   "list",
		Short: "List apps",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			apps, err := client().ListApps()
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tURL")
			for _, a := range apps {
				fmt.Fprintf(w, "%s\t%s\n", a.Name, a.URL)
			}
			return w.Flush()
		},
	}

	destroy := &cobra.Command{
		Use:   "destroy <name>",
		Short: "Delete an app and all of its resources",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if err := client().DestroyApp(args[0]); err != nil {
				return err
			}
			fmt.Printf("Destroyed app %q.\n", args[0])
			return nil
		},
	}

	apps.AddCommand(create, list, destroy)
	return apps
}
