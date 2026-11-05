package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/hbaldwin98/reliab/internal/doctor"
	"github.com/hbaldwin98/reliab/internal/version"
)

// newVersionCmd prints the version alone, the one .reliab-version pins and
// the install scripts take.
func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print reliab's version",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintln(cmd.OutOrStdout(), version.Version)
		},
	}
}

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check reliab can run here: git, sh, reliab.yaml, and the tools it starts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			checks := doctor.Run(".")
			for _, c := range checks {
				fmt.Fprintf(cmd.OutOrStdout(), "%-4s  %-20s %s\n", c.Status, c.Name, c.Detail)
			}
			if doctor.Failed(checks) {
				return withCode(ExitConfig, fmt.Errorf("some checks failed"))
			}
			return nil
		},
	}
}

func newFindingsCmd() *cobra.Command {
	return group("findings", "Check an adapter's findings file", newFindingsCheckCmd())
}
