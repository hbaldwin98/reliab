package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/hbaldwin98/reliab/internal/export"
	"github.com/hbaldwin98/reliab/internal/gitx"
	"github.com/hbaldwin98/reliab/internal/guard"
	"github.com/hbaldwin98/reliab/internal/hookio"
	"github.com/hbaldwin98/reliab/internal/report"
	summarypkg "github.com/hbaldwin98/reliab/internal/summary"
	"github.com/hbaldwin98/reliab/internal/triage"
	"github.com/hbaldwin98/reliab/internal/verdict"
)

// warnNotRescored says, on stderr, that a recorded change left the mutation
// scores as they were. The change itself stands, so it is not an error.
func warnNotRescored(cmd *cobra.Command, e *triage.NotRescored) {
	if e != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), "reliab: warning: "+e.Error())
	}
}

func repoRoot() (string, error) {
	root, err := gitx.Root(".")
	if err != nil {
		return "", withCode(ExitConfig, err)
	}
	return root, nil
}

func newReportCmd() *cobra.Command {
	route := &cobra.Command{
		Use:   "route",
		Short: "Say what to do next for each open finding",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := repoRoot()
			if err != nil {
				return err
			}
			_, err = triage.Route(cmd.OutOrStdout(), root)
			return err
		},
	}

	var format, tier, out string
	exp := &cobra.Command{
		Use:   "export",
		Short: "Export the latest report as SARIF or JUnit",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := repoRoot()
			if err != nil {
				return err
			}
			var r *report.Report
			if tier == "" {
				r = report.Latest(root, "inner", "prepr", "nightly")
				if r == nil {
					return withCode(ExitConfig, fmt.Errorf("no report yet: run reliab verify"))
				}
			} else if r, err = report.Read(report.LatestPath(root, tier)); err != nil {
				return withCode(ExitConfig, err)
			}
			w := cmd.OutOrStdout()
			if out != "" {
				if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
					return err
				}
				f, err := os.Create(out)
				if err != nil {
					return err
				}
				if err := export.Write(f, r, format); err != nil {
					_ = f.Close()
					return withCode(ExitConfig, err)
				}
				return f.Close()
			}
			if err := export.Write(w, r, format); err != nil {
				return withCode(ExitConfig, err)
			}
			return nil
		},
	}
	exp.Flags().StringVar(&format, "format", "sarif", "export format: "+strings.Join(export.Formats, " or "))
	exp.Flags().StringVar(&tier, "tier", "", "tier to export (default: the most recent report)")
	exp.Flags().StringVarP(&out, "output", "o", "", "write to this file instead of stdout")

	var sumTier, sumFormat, base string
	summary := &cobra.Command{
		Use:   "summary",
		Short: "Render a report as the evidence section of a PR description",
		Long: "Renders the tier's latest report (default prepr) as Markdown for a PR body: what ran, what was\n" +
			"skipped and why, mutation results, findings fixed during the change and still open, and the\n" +
			"questions for a human. It states what the reports say and nothing else, and says at the top\n" +
			"when the report is stale or incomplete. Fixed findings come from earlier runs on this branch:\n" +
			"reports whose commit lies between the merge base with --base and HEAD.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := repoRoot()
			if err != nil {
				return err
			}
			s, err := summarypkg.Build(summarypkg.Options{Root: root, Tier: sumTier, Base: base})
			if err != nil {
				return withCode(ExitConfig, err)
			}
			if err := summarypkg.Write(cmd.OutOrStdout(), s, sumFormat); err != nil {
				return withCode(ExitConfig, err)
			}
			return nil
		},
	}
	summary.Flags().StringVar(&sumTier, "tier", "prepr", "tier whose latest report to summarize")
	summary.Flags().StringVar(&sumFormat, "format", "markdown", "output format: "+strings.Join(summarypkg.Formats, " or "))
	summary.Flags().StringVar(&base, "base", "", "ref the branch forked from (default: origin/HEAD, origin/main, main, or master)")

	var outcome, by, notes, worktree string
	verdictCmd := &cobra.Command{
		Use:   "verdict",
		Short: "Record the reviewer's verdict on the pre-PR report (reviewer agent only)",
		Long: "Writes a verdict into .reliab/reports/prepr-latest.json, bound to its tree hash, so any later\n" +
			"edit voids it. A pass needs --worktree: a clean worktree of this repository where the reviewer\n" +
			"ran reliab verify --tier prepr on the same tree, with the same commands run and skipped and no\n" +
			"open finding the author's report lacks. Hooks deny this command to everyone but the\n" +
			guard.ReviewerAgent + " agent.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := repoRoot()
			if err != nil {
				return err
			}
			v, err := verdict.Record(verdict.Options{
				Root: root, Outcome: outcome, By: by, Notes: notes, Worktree: worktree, Now: time.Now(),
			})
			if err != nil {
				return withCode(ExitConfig, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "verdict %s by %s recorded on the prepr report for tree %s\n", v.Outcome, v.By, v.TreeHash)
			return nil
		},
	}
	verdictCmd.Flags().StringVar(&outcome, "outcome", "", "pass or fail")
	verdictCmd.Flags().StringVar(&by, "by", "", "who reviewed, e.g. "+guard.ReviewerAgent)
	verdictCmd.Flags().StringVar(&notes, "notes", "", "what was checked and what was found")
	verdictCmd.Flags().StringVar(&worktree, "worktree", "", "the clean worktree holding the reviewer's own prepr run (required for pass)")
	_ = verdictCmd.MarkFlagRequired("outcome")
	_ = verdictCmd.MarkFlagRequired("by")
	_ = verdictCmd.MarkFlagRequired("notes")

	return group("report", "Read the latest report", route, exp, summary, verdictCmd)
}

func newFindingsCmd() *cobra.Command {
	var kind, reason string
	classify := &cobra.Command{
		Use:   "classify <id>",
		Short: "Record the kind of a finding (agent)",
		Long:  "Kinds: " + strings.Join(triage.ClassifiableKinds, ", ") + ".",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := repoRoot()
			if err != nil {
				return err
			}
			tiers, err := triage.Classify(root, args[0], kind, reason, time.Now())
			var stale *triage.NotRescored
			if err != nil && !errors.As(err, &stale) {
				return withCode(ExitConfig, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s classified %s in the %s report(s)\n", args[0], kind, strings.Join(tiers, " and "))
			warnNotRescored(cmd, stale)
			return nil
		},
	}
	classify.Flags().StringVar(&kind, "kind", "", "finding kind")
	classify.Flags().StringVar(&reason, "reason", "", "one-line reason for the classification")
	_ = classify.MarkFlagRequired("kind")
	_ = classify.MarkFlagRequired("reason")

	var by string
	ack := &cobra.Command{
		Use:   "ack <id>",
		Short: "Record a human decision on a human-kind finding: legacy-defect-or-quirk, characterization-diff, equivalent-mutant, requirement-silent, or environment (people only)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := repoRoot()
			if err != nil {
				return err
			}
			tiers, err := triage.Acknowledge(root, args[0], by, time.Now())
			var stale *triage.NotRescored
			if err != nil && !errors.As(err, &stale) {
				return withCode(ExitConfig, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s acknowledged by %s in the %s report(s)\n", args[0], by, strings.Join(tiers, " and "))
			warnNotRescored(cmd, stale)
			return nil
		},
	}
	ack.Flags().StringVar(&by, "by", "", "who decided, e.g. human:<name>")
	_ = ack.MarkFlagRequired("by")

	return group("findings", "Manage findings in the latest reports", classify, ack)
}

func newHookCmd() *cobra.Command {
	type handler func(r *guard.Repo, in hookio.Input) hookio.Decision
	mk := func(use, short, event string, h handler) *cobra.Command {
		return &cobra.Command{
			Use:   use,
			Short: short,
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				in, err := hookio.Read(cmd.InOrStdin())
				if err != nil {
					// Exit 1, never 2: Claude Code treats exit 2 from a hook as
					// a blocking decision. A hook error must not block.
					return withCode(exitHookError, err)
				}
				dir := in.Cwd
				if dir == "" {
					dir = "."
				}
				repo, ok, err := guard.Open(dir)
				if err != nil {
					// Fail open on a broken profile, but say so where it matters.
					if event == "SessionStart" {
						return hookio.Write(cmd.OutOrStdout(), event, hookio.Decision{
							Context: "reliab: the profile is invalid, so hooks are not enforcing: " + err.Error(),
						})
					}
					fmt.Fprintln(cmd.ErrOrStderr(), "reliab: profile invalid, allowing:", err)
					return nil
				}
				if !ok {
					return nil
				}
				return hookio.Write(cmd.OutOrStdout(), event, h(repo, in))
			},
		}
	}
	return group("hook", "Answer an agent hook event read from stdin",
		mk("pre-edit", "Deny edits to protected paths", "PreToolUse",
			func(r *guard.Repo, in hookio.Input) hookio.Decision { return r.PreEdit(in) }),
		mk("pre-bash", "Deny shell commands that write protected paths, ack findings, or push unverified work", "PreToolUse",
			func(r *guard.Repo, in hookio.Input) hookio.Decision { return r.PreBash(in) }),
		mk("pre-push", "Deny pushes without a fresh, complete pre-PR report", "PreToolUse",
			func(r *guard.Repo, _ hookio.Input) hookio.Decision { return r.PrePush() }),
		mk("stop", "Block stopping while product code changed since the last report", "Stop",
			func(r *guard.Repo, in hookio.Input) hookio.Decision { return r.Stop(in) }),
		mk("session-start", "Inject the profile summary, rules, and open findings", "SessionStart",
			func(r *guard.Repo, in hookio.Input) hookio.Decision { return r.SessionStart(in) }),
	)
}
