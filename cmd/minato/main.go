// minato is the command line interface of the minato PaaS.
package main

import (
	"fmt"
	"os"

	"github.com/KchaiI/slipway/internal/cli"
)

func main() {
	if err := cli.NewRootCommand().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
