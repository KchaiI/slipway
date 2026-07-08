// Package cli implements the minato command line interface, a thin client of
// the minato-server REST API.
package cli

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
)

var (
	apiBase string
	appFlag string
)

// NewRootCommand builds the `minato` command tree.
func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "minato",
		Short:         "minato — git push して数十秒後に本番 URL で動く、Kubernetes 上のセルフホスト PaaS",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	defaultAPI := os.Getenv("MINATO_API")
	if defaultAPI == "" {
		defaultAPI = "http://minato.localtest.me"
	}
	root.PersistentFlags().StringVar(&apiBase, "api", defaultAPI,
		"minato-server base URL (env: MINATO_API)")
	root.PersistentFlags().StringVarP(&appFlag, "app", "a", "",
		"app name (defaults to the app of the 'minato' git remote)")

	root.AddCommand(
		newAppsCommand(),
		newStatusCommand(),
		newReleasesCommand(),
		newDeployImageCommand(),
	)
	return root
}

func client() *Client { return NewClient(strings.TrimRight(apiBase, "/")) }

var remoteAppRE = regexp.MustCompile(`/git/([a-z0-9-]+)\.git$`)

// resolveApp returns the target app: the -a flag if given, otherwise the app
// name parsed from the current repository's "minato" git remote.
func resolveApp() (string, error) {
	if appFlag != "" {
		return appFlag, nil
	}
	out, err := exec.Command("git", "remote", "get-url", "minato").Output()
	if err == nil {
		if m := remoteAppRE.FindStringSubmatch(strings.TrimSpace(string(out))); m != nil {
			return m[1], nil
		}
	}
	return "", fmt.Errorf("no app specified: pass -a <app> or run inside a repo with a 'minato' git remote")
}
