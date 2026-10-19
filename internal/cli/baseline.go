package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/hbaldwin98/reliab/internal/gitx"
	"github.com/hbaldwin98/reliab/internal/mutscore"
	"github.com/hbaldwin98/reliab/internal/profile"
	"github.com/hbaldwin98/reliab/internal/report"
)

// baselineOptions select the report a baseline is recorded from.
type baselineOptions struct {
	tier, command string
	scopes        []string
	allowLower    bool
}

func newBaselineCmd() *cobra.Command {
	var opt baselineOptions
	propose := &cobra.Command{
		Use:   "propose",
		Short: "Print the mutation baseline a report would record",
		Long: "Scores each --scope (default: the scopes already recorded) from a mutation command's per-file\n" +
			"counts in a fresh, complete report, and prints the baseline file it would write and what changes.\n" +
			"It writes nothing; a person runs reliab baseline write and commits the file.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, _, next, changes, err := proposeBaseline(opt)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			describe(out, changes)
			raw, err := next.Marshal()
			if err != nil {
				return withCode(ExitConfig, err)
			}
			_, err = out.Write(raw)
			return err
		},
	}
	write := &cobra.Command{
		Use:   "write",
		Short: "Write the mutation baseline (a person's decision; agents are denied it)",
		Long: "Writes the baseline reliab baseline propose prints to the profile's mutation_baseline, a\n" +
			"protected path, for a person to review and commit (R2). It refuses to lower a recorded score\n" +
			"unless --allow-lower: the baseline is a ratchet. Hooks deny this command to agents.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, p, next, changes, err := proposeBaseline(opt)
			if err != nil {
				return err
			}
			var lower []string
			for _, c := range changes {
				if c.Lower() {
					lower = append(lower, fmt.Sprintf("%s %.1f%% -> %.1f%%", c.Path, *c.Old, c.New.Score))
				}
			}
			if len(lower) > 0 && !opt.allowLower {
				return withCode(ExitConfig, fmt.Errorf("this lowers %s; the baseline is a ratchet: pass --allow-lower to lower it on purpose", strings.Join(lower, ", ")))
			}
			raw, err := next.Marshal()
			if err != nil {
				return withCode(ExitConfig, err)
			}
			path := filepath.Join(root, filepath.FromSlash(p.MutationBaseline))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(path, raw, 0o644); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			describe(out, changes)
			fmt.Fprintf(out, "wrote %s; review and commit it\n", p.MutationBaseline)
			return nil
		},
	}
	for _, c := range []*cobra.Command{propose, write} {
		c.Flags().StringVar(&opt.tier, "tier", report.OnDemand, "report to record from: on-demand (verify --only mutation_full --scope ...), inner, prepr, or nightly")
		c.Flags().StringVar(&opt.command, "command", "mutation_full", "mutation command whose counts to record")
		c.Flags().StringSliceVar(&opt.scopes, "scope", nil, "glob of the files a scope covers, e.g. src/Billing/** (repeat or comma-separate)")
	}
	write.Flags().BoolVar(&opt.allowLower, "allow-lower", false, "allow lowering a recorded score")
	return group("baseline", "Propose or write the mutation baseline", propose, write)
}

// proposeBaseline loads the profile, the baseline, and the report, and
// returns the proposed baseline.
func proposeBaseline(opt baselineOptions) (string, *profile.Profile, *mutscore.Baseline, []mutscore.Change, error) {
	root, err := repoRoot()
	if err != nil {
		return "", nil, nil, nil, err
	}
	p, err := profile.Load(filepath.Join(root, profile.DefaultPath))
	if err != nil {
		return "", nil, nil, nil, withCode(ExitConfig, err)
	}
	if p.MutationBaseline == "" {
		return "", nil, nil, nil, withCode(ExitConfig, errors.New("the profile names no mutation_baseline file; add one under protected_paths"))
	}
	cur, err := mutscore.LoadBaseline(root, p)
	if err != nil {
		return "", nil, nil, nil, withCode(ExitConfig, err)
	}
	rep, err := report.Read(report.LatestPath(root, opt.tier))
	if err != nil {
		return "", nil, nil, nil, withCode(ExitConfig, fmt.Errorf("no readable %s report: %w", opt.tier, err))
	}
	if !rep.Run.Complete {
		return "", nil, nil, nil, withCode(ExitConfig, fmt.Errorf("the %s report is incomplete; a baseline needs a complete run", opt.tier))
	}
	tree, err := gitx.TreeHash(root, p.TreeExclude())
	if err != nil {
		return "", nil, nil, nil, withCode(ExitConfig, err)
	}
	if tree != rep.Run.TreeHash {
		return "", nil, nil, nil, withCode(ExitConfig, fmt.Errorf("the %s report is stale (tree %s, now %s); re-run it first", opt.tier, rep.Run.TreeHash, tree))
	}
	next, changes, err := mutscore.Propose(cur, rep, opt.command, opt.scopes, time.Now())
	if err != nil {
		return "", nil, nil, nil, withCode(ExitConfig, err)
	}
	return root, p, next, changes, nil
}

func describe(w io.Writer, changes []mutscore.Change) {
	for _, c := range changes {
		was := "new"
		if c.Old != nil {
			was = fmt.Sprintf("was %.1f%%", *c.Old)
		}
		fmt.Fprintf(w, "# %s: %.1f%% (%d killed of %d counted), %s\n", c.Path, c.New.Score, c.New.Killed, c.New.Counted, was)
	}
}
