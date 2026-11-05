package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/hbaldwin98/reliab/internal/dap"
	"github.com/hbaldwin98/reliab/internal/gitx"
	"github.com/hbaldwin98/reliab/internal/runner"
)

func newProfileCmd() *cobra.Command {
	return group("profile", "Check reliab.yaml, or migrate a reliability-profile.yaml to it",
		newProfileCheckCmd(), newProfileMigrateCmd())
}

func newProfileCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "Load reliab.yaml and report every problem in it",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := gitx.Root(".")
			if err != nil {
				return withCode(ExitConfig, err)
			}
			cfg, err := runner.Load(filepath.Join(root, runner.DefaultPath))
			if err != nil {
				return withCode(ExitConfig, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s: %d commands, ok\n", runner.DefaultPath, len(cfg.Commands))
			return nil
		},
	}
}

func newProfileMigrateCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Write reliab.yaml (and reliab-debug.yaml) from reliability-profile.yaml",
		Long: `Converts a v1 reliability-profile.yaml: tiers become contexts (inner ->
change, prepr -> pr, nightly -> audit), mutation_changed and mutation_full
become one mutation command, and {impacted} becomes {projects} in change.
Roles, budgets, protected paths, relevance rules and corrections have no
reliab.yaml form and are dropped. The debug section moves to
reliab-debug.yaml. It prints what to check by hand, and leaves the old file
for you to delete.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := gitx.Root(".")
			if err != nil {
				return withCode(ExitConfig, err)
			}
			raw, err := os.ReadFile(filepath.Join(root, runner.OldProfilePath))
			if err != nil {
				return withCode(ExitConfig, err)
			}
			m, err := runner.Migrate(raw)
			if err != nil {
				return withCode(ExitConfig, err)
			}
			files := map[string]string{runner.DefaultPath: m.Config}
			if m.Debug != "" {
				files[dap.ConfigPath] = m.Debug
			}
			for name := range files {
				if _, err := os.Stat(filepath.Join(root, name)); err == nil && !force {
					return withCode(ExitConfig, fmt.Errorf("%s exists; pass --force to overwrite it", name))
				} else if err != nil && !errors.Is(err, os.ErrNotExist) {
					return withCode(ExitConfig, err)
				}
			}
			out := cmd.OutOrStdout()
			for _, name := range []string{runner.DefaultPath, dap.ConfigPath} {
				text, ok := files[name]
				if !ok {
					continue
				}
				if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0o644); err != nil {
					return withCode(ExitConfig, err)
				}
				fmt.Fprintf(out, "wrote %s\n", name)
			}
			fmt.Fprintln(out, "check:")
			for _, n := range m.Notes {
				fmt.Fprintf(out, "  - %s\n", n)
			}
			fmt.Fprintf(out, "then: reliab run change --plan, and delete %s\n", runner.OldProfilePath)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing reliab.yaml or reliab-debug.yaml")
	return cmd
}
