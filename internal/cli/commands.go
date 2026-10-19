package cli

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/hbaldwin98/reliab/internal/doctor"
	"github.com/hbaldwin98/reliab/internal/gate"
	"github.com/hbaldwin98/reliab/internal/gitx"
	"github.com/hbaldwin98/reliab/internal/profile"
	"github.com/hbaldwin98/reliab/internal/report"
	"github.com/hbaldwin98/reliab/internal/setup"
	"github.com/hbaldwin98/reliab/internal/verify"
	"github.com/hbaldwin98/reliab/internal/version"
)

// The v0 command surface, per knowledge/decisions/v0-scope.md.

func newProfileCmd() *cobra.Command {
	return group("profile", "Validate or initialise the stack profile",
		newProfileValidateCmd(),
		newProfileInitCmd(),
	)
}

func newProfileInitCmd() *cobra.Command {
	var opt setup.Options
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Write the profile skeleton, deny rules, agent settings, and AGENTS.md rules",
		Long: "Sets up the repository side of reliab. Safe to re-run: it keeps existing content and\n" +
			"refreshes deny rules after protected_paths change. It never writes CI configuration.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := repoRoot()
			if err != nil {
				return err
			}
			done, p, err := setup.Init(root, opt)
			if err != nil {
				return withCode(ExitConfig, err)
			}
			out := cmd.OutOrStdout()
			if len(done) == 0 {
				fmt.Fprintln(out, "already set up; nothing changed")
			}
			for _, d := range done {
				fmt.Fprintln(out, "- "+d)
			}
			fmt.Fprintln(out)
			fmt.Fprintln(out, "CI is yours to wire (reliab writes no CI files):")
			fmt.Fprintln(out, "  every push:  reliab verify --tier prepr && reliab gate protected --base <target branch>")
			fmt.Fprintln(out, "  nightly:     reliab verify --tier nightly")
			if len(p.ProtectedPaths) > 0 {
				fmt.Fprintln(out, "Require a human approval on your code host for changes to:")
				for _, g := range p.ProtectedPaths {
					fmt.Fprintln(out, "  "+g)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&opt.Condition, "starting-condition", "", "greenfield, maintained, or legacy (required for a new profile)")
	cmd.Flags().StringVar(&opt.Name, "name", "", "profile name for a new profile (default: the directory name)")
	return cmd
}

func newProfileValidateCmd() *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate reliability-profile.yaml against its schema",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := profile.Load(file)
			if err != nil {
				return withCode(ExitConfig, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "ok: %s (profile %s, %s)\n", file, p.Name, p.StartingCondition)
			return nil
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", profile.DefaultPath, "profile to validate")
	return cmd
}

func newVerifyCmd() *cobra.Command {
	var tier, file, seed, base string
	var only, scope []string
	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Run the evidence layers for a tier within its budget",
		Long: "Runs the tier's profile commands under its budget and writes .reliab/reports/<tier>-latest.json.\n" +
			"With --only it runs just the named commands, under the tier's budget, and writes\n" +
			".reliab/reports/on-demand-latest.json; --scope narrows {impacted}, {scope}, and RELIAB_SCOPE\n" +
			"to the given paths. An on-demand report never satisfies the stop or push hooks.\n" +
			"Exit codes: 0 passed and complete, 1 open findings, 2 config error, 3 incomplete (budget).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := gitx.Root(".")
			if err != nil {
				return withCode(ExitConfig, err)
			}
			// Scope paths are given relative to where the command runs.
			for i, p := range scope {
				if scope[i], err = filepath.Abs(p); err != nil {
					return withCode(ExitConfig, err)
				}
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()
			out := cmd.OutOrStdout()
			rep, path, err := verify.Run(ctx, verify.Options{
				Root: root, Profile: file, Tier: tier, Seed: seed, Log: out, Only: only, Scope: scope, Base: base,
			})
			if err != nil {
				return withCode(ExitConfig, err)
			}
			rel, _ := filepath.Rel(root, path)
			open := 0
			for _, f := range rep.Findings {
				if f.Status == report.StatusOpen {
					open++
				}
			}
			fmt.Fprintf(out, "%d open finding(s), %d skipped, complete=%t, %.1fs\nreport: %s\n",
				open, len(rep.Run.Skipped), rep.Run.Complete, rep.Run.ElapsedSec, filepath.ToSlash(rel))
			switch {
			case open > 0:
				return withCode(ExitFindings, fmt.Errorf("%d open finding(s); run reliab report route", open))
			case !rep.Run.Complete:
				return withCode(ExitIncomplete, fmt.Errorf("incomplete: the %s budget ran out", tier))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&tier, "tier", "inner", "tier to run: inner, prepr, or nightly")
	cmd.Flags().StringVarP(&file, "file", "f", profile.DefaultPath, "profile, relative to the repo root")
	cmd.Flags().StringVar(&seed, "seed", "", "seed for property tests (default: derived from the start time)")
	cmd.Flags().StringSliceVar(&only, "only", nil, "run only these profile commands, e.g. mutation_full (repeat or comma-separate)")
	cmd.Flags().StringSliceVar(&scope, "scope", nil, "with --only, narrow the run to these paths (repeat or comma-separate)")
	cmd.Flags().StringVar(&base, "base", "", "ref for changed_only commands and {base} (default: the profile's base, then origin/HEAD, origin/main, main, master)")
	return cmd
}

func newGateCmd() *cobra.Command {
	var base string
	protected := &cobra.Command{
		Use:   "protected",
		Short: "Fail if protected paths changed since a base ref without a person's approval",
		Long: "Lists protected files changed between the merge base of --base and the working tree,\n" +
			"and exits 1 if any lacks a local approval (reliab approve) of its current content. Approvals\n" +
			"live in .reliab/, so on CI every change is unapproved: make your code host require a human\n" +
			"approval when it fails.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := repoRoot()
			if err != nil {
				return err
			}
			p, err := profile.Load(filepath.Join(root, profile.DefaultPath))
			if err != nil {
				return withCode(ExitConfig, err)
			}
			changes, err := gate.Protected(root, p, base)
			if err != nil {
				return withCode(ExitConfig, err)
			}
			out := cmd.OutOrStdout()
			if len(changes) == 0 {
				fmt.Fprintf(out, "no protected paths changed since %s\n", base)
				return nil
			}
			unapproved := gate.Unapproved(changes)
			for _, c := range unapproved {
				fmt.Fprintf(out, "%s (protected by %s)\n", c.Path, c.Glob)
			}
			if len(unapproved) < len(changes) {
				fmt.Fprintln(out, "approved by a person (local approvals; CI and the code host's review still decide):")
				for _, c := range changes {
					if a := c.Approval; a != nil {
						fmt.Fprintf(out, "  %s (protected by %s) approved by %s at %s\n", c.Path, c.Glob, a.By, a.At)
					}
				}
			}
			if len(unapproved) > 0 {
				return withCode(ExitFindings, fmt.Errorf("%d protected file(s) changed without a person's approval (R2)", len(unapproved)))
			}
			return nil
		},
	}
	protected.Flags().StringVar(&base, "base", "", "git ref to compare against, e.g. origin/main")
	_ = protected.MarkFlagRequired("base")

	var tier string
	mutation := &cobra.Command{
		Use:   "mutation",
		Short: "Fail if a mutation score is below its threshold or baseline",
		Long: "Re-scores the tier's latest report (default prepr) from its mutant counts and holds every\n" +
			"mutation command the tier runs to its profile threshold and the baseline file's scopes. Exits 1\n" +
			"when one is below, reports no counts, did not run, or the report is stale; a command skipped as\n" +
			"not relevant passes. Run it after reliab verify, in CI.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := repoRoot()
			if err != nil {
				return err
			}
			p, err := profile.Load(filepath.Join(root, profile.DefaultPath))
			if err != nil {
				return withCode(ExitConfig, err)
			}
			lines, err := gate.Mutation(root, p, tier)
			if err != nil {
				return withCode(ExitConfig, err)
			}
			out := cmd.OutOrStdout()
			if len(lines) == 0 {
				fmt.Fprintf(out, "no mutation command in the %s tier has a threshold or baseline\n", tier)
				return nil
			}
			failed := 0
			for _, l := range lines {
				mark := "ok  "
				if l.Fail {
					mark = "FAIL"
					failed++
				}
				if l.Command == "" {
					fmt.Fprintf(out, "%s %s\n", mark, l.Text)
				} else {
					fmt.Fprintf(out, "%s %s: %s\n", mark, l.Command, l.Text)
				}
			}
			if failed > 0 {
				return withCode(ExitFindings, fmt.Errorf("%d mutation check(s) failed", failed))
			}
			return nil
		},
	}
	mutation.Flags().StringVar(&tier, "tier", "prepr", "tier whose latest report to judge")
	return group("gate", "Agent-free CI gates", protected, mutation)
}

func newApproveCmd() *cobra.Command {
	var base, by string
	cmd := &cobra.Command{
		Use:   "approve <path>...",
		Short: "Record a person's approval of protected-path changes (people only)",
		Long: "Records, in .reliab/approvals.json, that a person approved the current content of each named\n" +
			"protected file the branch changed since its base: its blob hash on the merge base and in the\n" +
			"working tree. reliab gate protected, the PR summary, and the reviewer then accept that change;\n" +
			"any later edit to the file voids the approval. Approvals are local: CI and the code host's\n" +
			"owner review stay authoritative. Hooks deny this command to agents.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := repoRoot()
			if err != nil {
				return err
			}
			p, err := profile.Load(filepath.Join(root, profile.DefaultPath))
			if err != nil {
				return withCode(ExitConfig, err)
			}
			ref, err := gitx.ResolveBase(root, base, p.Base)
			if err != nil {
				return withCode(ExitConfig, err)
			}
			paths := make([]string, len(args))
			for i, a := range args {
				if paths[i], err = repoRel(root, a); err != nil {
					return withCode(ExitConfig, err)
				}
			}
			added, err := gate.Approve(root, p, ref, paths, by, time.Now())
			if err != nil {
				return withCode(ExitConfig, err)
			}
			for _, a := range added {
				fmt.Fprintf(cmd.OutOrStdout(), "%s approved by %s against %s (merge base %s)\n", a.Path, a.By, a.Base, a.MergeBase[:min(len(a.MergeBase), 12)])
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&by, "by", "", "who approves, as human:<name>")
	cmd.Flags().StringVar(&base, "base", "", "ref the branch forked from (default: the profile's base, then origin/HEAD, origin/main, main, master)")
	_ = cmd.MarkFlagRequired("by")
	return cmd
}

// repoRel turns a path given on the command line, relative to where reliab
// runs, into a repo-relative slash path.
func repoRel(root, p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s is outside the repository", p)
	}
	return filepath.ToSlash(rel), nil
}

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
		Short: "Check prerequisites for running reliab here",
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
