package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/hbaldwin98/reliab/docs"
	"github.com/hbaldwin98/reliab/internal/adapters"
	"github.com/hbaldwin98/reliab/internal/gitx"
	"github.com/hbaldwin98/reliab/internal/profile"
	"github.com/hbaldwin98/reliab/pkg/snapdiff"
)

// Helpers expose rules adapters share, for adapters in any language
// (knowledge/decisions/external-adapters.md). Go adapters can import
// pkg/snapdiff and pkg/findings instead.

func newSnapdiffCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "snapdiff <accepted> <pending>",
		Short: "Label a pending snapshot against its accepted file: additive-only or values-changed",
		Long: "Compares a pending snapshot with the accepted one it would replace and prints the label,\n" +
			"the added and removed line counts, and the labelled diff. A missing accepted file is a new\n" +
			"snapshot (additive-only). Equal files print \"equal\". Line endings and a byte-order mark\n" +
			"do not count. Exit 0 whatever the label; 2 when a file cannot be read.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := snapdiff.CompareFiles(args[0], args[1])
			if err != nil {
				return withCode(ExitConfig, err)
			}
			out := cmd.OutOrStdout()
			if asJSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(struct {
					Label   string `json:"label"`
					Added   int    `json:"added"`
					Removed int    `json:"removed"`
					New     bool   `json:"new"`
					Binary  bool   `json:"binary"`
					Diff    string `json:"diff"`
				}{r.Label, r.Added, r.Removed, r.New, r.Binary, r.Diff})
			}
			if r.Label == "" {
				fmt.Fprintln(out, "equal")
				return nil
			}
			fmt.Fprintf(out, "%s +%d -%d\n", r.Label, r.Added, r.Removed)
			if r.Diff != "" {
				fmt.Fprintln(out, r.Diff)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON: label, added, removed, new, binary, diff")
	return cmd
}

func newChangedLinesCmd() *cobra.Command {
	var base string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "changed-lines",
		Short: "Print the lines the working tree changed since the base, per file",
		Long: "Prints the line ranges the branch adds or changes relative to the merge base with the base\n" +
			"ref, committed or not; untracked files count whole. The base is --base, else the profile's\n" +
			"base, else the first of origin/HEAD, origin/main, main, master. Adapters use it to keep\n" +
			"findings on changed code (R7). Text: one \"file: 3-5,9\" line per file. JSON:\n" +
			"{\"base\": ref, \"files\": {file: [[start, end], ...]}}.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := gitx.Root(".")
			if err != nil {
				return withCode(ExitConfig, err)
			}
			profBase := ""
			if p, err := profile.Load(filepath.Join(root, profile.DefaultPath)); err == nil {
				profBase = p.Base
			}
			ref, err := gitx.ResolveBase(root, base, profBase)
			if err != nil {
				return withCode(ExitConfig, err)
			}
			lines, err := gitx.ChangedLines(root, ref)
			if err != nil {
				return withCode(ExitConfig, err)
			}
			files := make([]string, 0, len(lines))
			ranges := map[string][][2]int{}
			for f, ls := range lines {
				files = append(files, f)
				ranges[f] = toRanges(ls)
			}
			sort.Strings(files)
			out := cmd.OutOrStdout()
			if asJSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(struct {
					Base  string              `json:"base"`
					Files map[string][][2]int `json:"files"`
				}{ref, ranges})
			}
			for _, f := range files {
				parts := make([]string, 0, len(ranges[f]))
				for _, r := range ranges[f] {
					if r[0] == r[1] {
						parts = append(parts, fmt.Sprint(r[0]))
					} else {
						parts = append(parts, fmt.Sprintf("%d-%d", r[0], r[1]))
					}
				}
				fmt.Fprintf(out, "%s: %s\n", f, strings.Join(parts, ","))
			}
			if len(files) == 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "no lines changed since %s\n", ref)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&base, "base", "", "ref to compare with (default: the profile's base, then origin/HEAD, origin/main, main, master)")
	cmd.Flags().BoolVar(&asJSON, "json", false, `print JSON: {"base": ref, "files": {file: [[start, end], ...]}}`)
	return cmd
}

// toRanges collapses a set of line numbers into sorted inclusive ranges.
func toRanges(lines map[int]bool) [][2]int {
	ns := make([]int, 0, len(lines))
	for n := range lines {
		ns = append(ns, n)
	}
	sort.Ints(ns)
	var out [][2]int
	for _, n := range ns {
		if k := len(out); k > 0 && out[k-1][1] == n-1 {
			out[k-1][1] = n
			continue
		}
		out = append(out, [2]int{n, n})
	}
	return out
}

func newFindingsCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check <findings.json>",
		Short: "Check an adapter's findings file before a profile command uses it",
		Long: "Reads a findings file (schemas/findings.schema.json) as reliab verify would and prints\n" +
			"each finding's test and location, the mutant counts, and warnings about what would make\n" +
			"findings unstable or misleading in a report: a test that names a line or a path from this\n" +
			"machine, a location or mutantFiles path that is not a file in the repository, and counts\n" +
			"that do not match the findings. Paths are checked against the repository holding the\n" +
			"working directory. Exit 0 clean, 1 warnings, 2 a file verify would reject.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := os.ReadFile(args[0])
			if err != nil {
				return withCode(ExitConfig, err)
			}
			root, _ := gitx.Root(".")
			c, err := adapters.CheckFindings(raw, root)
			if err != nil {
				return withCode(ExitConfig, fmt.Errorf("%s: reliab verify would reject it: %w", args[0], err))
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%s: %d findings\n", args[0], len(c.Findings))
			for _, f := range c.Findings {
				if f.Location != "" {
					fmt.Fprintf(out, "  %s  (%s)\n", f.Test, f.Location)
				} else {
					fmt.Fprintf(out, "  %s\n", f.Test)
				}
			}
			if m := c.Mutants; m != nil {
				fmt.Fprintf(out, "mutants: %d killed, %d survived, %d not covered; %d files\n", m.Killed, m.Survived, m.NotCovered, len(c.MutantFiles))
			}
			if root == "" {
				fmt.Fprintln(out, "paths not checked: the working directory is not in a git repository")
			}
			for _, w := range c.Warnings {
				fmt.Fprintln(out, "warning:", w)
			}
			if len(c.Warnings) > 0 {
				return withCode(ExitFindings, fmt.Errorf("%d warnings", len(c.Warnings)))
			}
			return nil
		},
	}
}

func newDocsCmd() *cobra.Command {
	topics := make([]string, 0, len(docs.Pages))
	for t := range docs.Pages {
		topics = append(topics, t)
	}
	sort.Strings(topics)
	return &cobra.Command{
		Use:   "docs <topic>",
		Short: "Print a reference page for the installed version: " + strings.Join(topics, ", "),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			page, ok := docs.Pages[args[0]]
			if !ok {
				return withCode(ExitConfig, fmt.Errorf("no page %q; topics: %s", args[0], strings.Join(topics, ", ")))
			}
			_, err := fmt.Fprint(cmd.OutOrStdout(), page)
			return err
		},
	}
}
