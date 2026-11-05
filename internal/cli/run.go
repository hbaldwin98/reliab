package cli

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/spf13/cobra"

	"github.com/hbaldwin98/reliab/internal/gitx"
	"github.com/hbaldwin98/reliab/internal/runner"
	"github.com/hbaldwin98/reliab/internal/verify"
)

func newRunCmd() *cobra.Command {
	var (
		targets  []string
		plan     bool
		base     string
		parallel int
	)
	cmd := &cobra.Command{
		Use:   "run <change|pr|audit>",
		Short: "Run the reliab.yaml commands for a context, on a target",
		Long: `Runs each command in reliab.yaml that has a line for the context:

  change  the inner loop, after an edit
  pr      a branch about to merge
  audit   existing code

The target is what to look at. change and pr default to the files changed
since the base; audit defaults to everything. --target narrows it, and may
repeat:

  --target src/Api/Api.csproj        a project
  --target src/Api/Data/             a folder
  --target src/Api/Users.cs          a file
  --target src/Api/Users.cs#GetUser  a function (Type.Method also works)

A line may use {files}, {projects} and {tests}: the target's source files,
the projects that own them (a .csproj, a Go package, a package.json dir),
and their tests. With --target, a command's targeted line, when it has one,
runs instead of its context line. Findings outside the target are dropped.

Run with --plan first: it prints what would run, in order, with each
command's line and what its failure means, and runs nothing.`,
		Args:      cobra.ExactArgs(1),
		ValidArgs: runner.Contexts,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := gitx.Root(".")
			if err != nil {
				return withCode(ExitConfig, err)
			}
			cfg, err := runner.Load(filepath.Join(root, runner.DefaultPath))
			if err != nil {
				return withCode(ExitConfig, err)
			}
			context := args[0]
			if !slices.Contains(runner.Contexts, context) {
				return withCode(ExitConfig, fmt.Errorf("unknown context %q: use change, pr or audit", context))
			}
			var t *runner.Target
			ref := ""
			switch {
			case len(targets) > 0:
				t, err = runner.Resolve(root, targets)
			case context == "audit":
				t = runner.ResolveAll()
			default:
				ref, err = gitx.ResolveBase(root, base, cfg.Base)
				if err == nil {
					t, err = runner.ResolveChanged(root, ref)
				}
			}
			if err != nil {
				return withCode(ExitConfig, err)
			}
			p, err := runner.Build(cfg, context, t, len(targets) > 0, ref)
			if err != nil {
				return withCode(ExitConfig, err)
			}
			out := cmd.OutOrStdout()
			if plan {
				p.Print(out)
				return nil
			}
			if !t.All && len(t.Files) == 0 {
				fmt.Fprintf(out, "nothing changed since %s; nothing to run\n", ref)
				return nil
			}
			shell, err := verify.FindShell()
			if err != nil {
				return withCode(ExitConfig, err)
			}
			results := runner.Run(cmd.Context(), p, runner.Options{
				Root: root, Shell: shell, Parallel: parallel, Progress: cmd.ErrOrStderr(),
			})
			if runner.PrintResults(out, results) {
				return withCode(ExitFindings, errors.New("checks failed"))
			}
			return nil
		},
	}
	cmd.Flags().StringArrayVar(&targets, "target", nil, "a project file, folder, file, or file#Function to look at (repeatable)")
	cmd.Flags().BoolVar(&plan, "plan", false, "print what would run, in order, and run nothing")
	cmd.Flags().StringVar(&base, "base", "", "ref that changed files are measured from (default: reliab.yaml's base, then origin/HEAD, origin/main, main, master)")
	cmd.Flags().IntVar(&parallel, "parallel", 0, "most commands running at once (default: CPU count)")
	return cmd
}
