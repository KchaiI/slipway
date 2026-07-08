package cli

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func newStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the app's release, pods, and URL",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			app, err := resolveApp()
			if err != nil {
				return err
			}
			st, err := client().AppStatus(app)
			if err != nil {
				return err
			}
			fmt.Printf("=== %s\n", st.Name)
			fmt.Printf("url:      %s\n", st.URL)
			fmt.Printf("git:      %s\n", st.GitURL)
			if st.Release != nil {
				fmt.Printf("release:  v%d (%s) — %s\n", st.Release.Version, st.Release.Status, st.Release.Description)
			} else {
				fmt.Printf("release:  (none deployed yet)\n")
			}
			fmt.Printf("web:      %d/%d ready\n\n", st.ReadyReplicas, st.Replicas)
			if len(st.Pods) > 0 {
				w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
				fmt.Fprintln(w, "POD\tPHASE\tREADY\tRELEASE")
				for _, p := range st.Pods {
					fmt.Fprintf(w, "%s\t%s\t%v\t%s\n", p.Name, p.Phase, p.Ready, p.Release)
				}
				return w.Flush()
			}
			return nil
		},
	}
}

func newReleasesCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "releases",
		Short: "Show the app's release history",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			app, err := resolveApp()
			if err != nil {
				return err
			}
			rels, err := client().Releases(app)
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "VERSION\tSTATUS\tCREATED\tDESCRIPTION")
			for i := len(rels) - 1; i >= 0; i-- {
				r := rels[i]
				fmt.Fprintf(w, "v%d\t%s\t%s\t%s\n",
					r.Version, r.Status, r.CreatedAt.Local().Format("2006-01-02 15:04:05"), r.Description)
			}
			return w.Flush()
		},
	}
}
