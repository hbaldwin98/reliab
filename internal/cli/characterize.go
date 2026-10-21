package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/hbaldwin98/reliab/internal/characterize"
	"github.com/hbaldwin98/reliab/internal/profile"
	"github.com/hbaldwin98/reliab/internal/report"
	"github.com/hbaldwin98/reliab/internal/verify"
)

func newCharacterizeCmd() *cobra.Command {
	var tier, file string
	var scope []string
	opts := func(cmd *cobra.Command) (verify.Options, error) {
		root, err := repoRoot()
		if err != nil {
			return verify.Options{}, err
		}
		abs := make([]string, len(scope))
		for i, p := range scope {
			if abs[i], err = filepath.Abs(p); err != nil {
				return verify.Options{}, withCode(ExitConfig, err)
			}
		}
		return verify.Options{Root: root, Profile: file, Tier: tier, Scope: abs, Log: cmd.OutOrStdout()}, nil
	}
	run := func(cmd *cobra.Command, f func(context.Context, verify.Options) (*report.Report, string, error)) (*report.Report, error) {
		opt, err := opts(cmd)
		if err != nil {
			return nil, err
		}
		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
		defer stop()
		rep, _, err := f(ctx, opt)
		if err != nil {
			return nil, withCode(ExitConfig, err)
		}
		return rep, nil
	}
	budget := func(c *cobra.Command) {
		c.Flags().StringVar(&tier, "tier", "inner", "tier whose budget the capture runs under: inner, prepr, or nightly")
		c.Flags().StringVarP(&file, "file", "f", profile.DefaultPath, "profile, relative to the repo root")
	}

	capture := &cobra.Command{
		Use:   "capture",
		Short: "Capture an area's behavior before a refactor or migration",
		Long: "Runs the profile's characterize command with {capture} and RELIAB_CAPTURE_DIR naming\n" +
			characterize.Dir + "/baseline, replacing any earlier capture. --scope narrows {scope},\n" +
			"{impacted}, and RELIAB_SCOPE as for verify --only. Nothing is committed or approved.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rep, err := run(cmd, characterize.Capture)
			if err != nil {
				return err
			}
			if rep.HasOpen() {
				return withCode(ExitFindings, fmt.Errorf("the capture failed; run reliab report route"))
			}
			root, _ := repoRoot()
			m, err := characterize.ReadMeta(root)
			if err != nil {
				return withCode(ExitConfig, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "captured %d file(s) into %s/baseline%s\nmake the change, then run reliab characterize compare\n",
				m.Files, characterize.Dir, scopeText(m.Scope))
			return nil
		},
	}
	budget(capture)
	capture.Flags().StringSliceVar(&scope, "scope", nil, "paths whose behavior to capture (repeat or comma-separate)")

	compare := &cobra.Command{
		Use:   "compare",
		Short: "Capture the same area again and report what changed",
		Long: "Re-runs the characterize command over the captured scope into " + characterize.Dir + "/current\n" +
			"and reports each transcript that differs as a finding labelled additive-only or\n" +
			"values-changed, in the on-demand report. Run it as often as needed; the capture stays.\n" +
			"Exit codes: 0 no differences, 1 differences or a failed capture, 2 config error.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rep, err := run(cmd, characterize.Compare)
			if err != nil {
				return err
			}
			open := 0
			for _, f := range rep.Findings {
				if f.Status == report.StatusOpen {
					open++
				}
			}
			if open > 0 {
				return withCode(ExitFindings, fmt.Errorf("%d difference(s) from the capture; run reliab report route", open))
			}
			if !rep.Run.Complete {
				why := incompleteWhy(rep, tier)
				if len(rep.NeedsInput()) > 0 {
					// characterize has no --tests flag to name them with.
					why += "; characterize names no tests, so its command must not need them"
				}
				return withCode(ExitIncomplete, fmt.Errorf("incomplete: %s", why))
			}
			fmt.Fprintln(cmd.OutOrStdout(), "no differences from the capture")
			return nil
		},
	}
	budget(compare)

	discard := &cobra.Command{
		Use:   "discard",
		Short: "Throw the captures away",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := repoRoot()
			if err != nil {
				return err
			}
			if err := characterize.Discard(root); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "removed %s\n", characterize.Dir)
			return nil
		},
	}

	return group("characterize", "Capture behavior before a refactor, compare after, then discard", capture, compare, discard)
}

func scopeText(scope []string) string {
	if len(scope) == 0 {
		return ""
	}
	return " (scope " + strings.Join(scope, ", ") + ")"
}
