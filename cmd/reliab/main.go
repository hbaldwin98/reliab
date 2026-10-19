// Command reliab runs the evidence stack for a codebase and answers agent
// hook events. See knowledge/overview.md.
package main

import (
	"os"

	"github.com/hbaldwin98/reliab/internal/cli"
)

func main() {
	os.Exit(cli.Execute(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
