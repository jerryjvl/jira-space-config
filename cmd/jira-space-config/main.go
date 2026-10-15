// Command jira-space-config describes the configuration of one Jira Cloud
// company-managed space as YAML. See README.md.
package main

import (
	"os"

	"github.com/jerryjvl/jira-space-config/internal/cli"
)

func main() {
	if err := cli.Execute(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		os.Exit(1)
	}
}
