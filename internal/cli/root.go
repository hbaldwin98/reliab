// Package cli wires reliab's subcommands. Commands stay thin; the work
// lives in their own internal packages.
package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/hbaldwin98/reliab/internal/version"
)

// Execute runs the CLI with the given arguments and streams and returns the
// process exit code.
func Execute(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	root := newRoot()
	root.SetArgs(args)
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)
	err := root.Execute()
	if err != nil {
		fmt.Fprintln(stderr, "reliab:", err)
	}
	return codeOf(err)
}

func newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "reliab",
		Short:         "Run the evidence stack and enforce the agent contract",
		Version:       version.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(
		newProfileCmd(),
		newVerifyCmd(),
		newReportCmd(),
		newFindingsCmd(),
		newCharacterizeCmd(),
		newGateCmd(),
		newBaselineCmd(),
		newApproveCmd(),
		newHookCmd(),
		newDoctorCmd(),
		newVersionCmd(),
		newSnapdiffCmd(),
		newChangedLinesCmd(),
		newDocsCmd(),
	)
	return root
}

// group returns a parent command that only holds subcommands.
func group(use, short string, children ...*cobra.Command) *cobra.Command {
	cmd := &cobra.Command{Use: use, Short: short, Args: cobra.NoArgs}
	cmd.AddCommand(children...)
	return cmd
}
