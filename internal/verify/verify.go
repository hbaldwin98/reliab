// Package verify runs one tier of the evidence stack: it resolves the tier's
// commands from the profile, runs them under the tier's budget, converts
// their output into findings, and writes the report.
package verify

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/hbaldwin98/reliab/internal/adapters"
	"github.com/hbaldwin98/reliab/internal/gitx"
	"github.com/hbaldwin98/reliab/internal/mutscore"
	"github.com/hbaldwin98/reliab/internal/profile"
	"github.com/hbaldwin98/reliab/internal/report"
	"github.com/hbaldwin98/reliab/internal/version"
)

// OutDir holds command logs and tool output files, relative to the repo root.
const OutDir = ".reliab/out"

// Options configure one verify run.
type Options struct {
	Root    string // repository root
	Profile string // profile path, relative to Root unless absolute
	Tier    string
	Seed    string    // empty means derive one from the start time
	Log     io.Writer // progress lines; nil discards them
	Now     func() time.Time
	// BinDir goes first on PATH for profile commands, so the adapters that
	// ship beside reliab resolve by name. Empty means reliab's own directory.
	BinDir string
	// Only, when set, runs these commands instead of the tier's, under the
	// tier's budget, and writes an on-demand report.
	Only []string
	// Scope narrows an on-demand run to these repo-relative paths. It
	// requires Only.
	Scope []string
	// Base, when set, overrides the profile's base for changed_only commands
	// and {base}.
	Base string
	// Characterize is required to run the characterize command, which only
	// reliab characterize runs (knowledge/decisions/on-demand-characterization.md).
	Characterize *Characterize
}

// BuildID is the profile command that builds the code; when it fails the
// tier's later commands are skipped.
const BuildID = "build"

// CharacterizeID is the profile command that writes transcripts of an
// area's behavior into {capture}.
const CharacterizeID = "characterize"

// Characterize sets up a run of the characterize command.
type Characterize struct {
	// Dir is the absolute directory the command writes into, given to it as
	// {capture} and RELIAB_CAPTURE_DIR.
	Dir string
	// Judge turns the command's exit code and log tail into findings, in
	// place of an output format.
	Judge func(exitCode int, logTail string) []report.Finding
}

// Run executes the tier and writes its report. It returns the report and the
// path it was written to. Command failures are findings, not errors; an
// error means the run itself could not happen.
func Run(ctx context.Context, opt Options) (*report.Report, string, error) {
	if opt.Now == nil {
		opt.Now = time.Now
	}
	if opt.Log == nil {
		opt.Log = io.Discard
	}
	profPath := opt.Profile
	if !filepath.IsAbs(profPath) {
		profPath = filepath.Join(opt.Root, profPath)
	}
	prof, err := profile.Load(profPath)
	if err != nil {
		return nil, "", err
	}
	tier, ok := ResolveTier(prof, opt.Tier)
	if !ok {
		return nil, "", fmt.Errorf("unknown tier %q (want one of %s)", opt.Tier, strings.Join(TierNames, ", "))
	}
	reportTier := tier.Name
	if len(opt.Only) > 0 {
		if err := checkOnly(prof, opt.Only); err != nil {
			return nil, "", err
		}
		tier.Run = opt.Only
		reportTier = report.OnDemand
	}
	if slices.Contains(tier.Run, CharacterizeID) && opt.Characterize == nil {
		return nil, "", errors.New("characterize runs only through reliab characterize capture and compare")
	}
	scope, err := cleanScope(opt.Root, opt.Scope)
	if err != nil {
		return nil, "", err
	}
	if len(scope) > 0 && len(opt.Only) == 0 {
		return nil, "", errors.New("--scope narrows an on-demand run; name its commands with --only")
	}
	shell, err := FindShell()
	if err != nil {
		return nil, "", err
	}
	baseline, err := mutscore.LoadBaseline(opt.Root, prof)
	if err != nil {
		return nil, "", err
	}

	start := opt.Now()
	// The report records the tree it ran against. reliab's own output never
	// makes a report stale.
	tree, err := gitx.TreeHash(opt.Root, prof.TreeExclude())
	if err != nil {
		return nil, "", err
	}
	impacted, err := impactedArgs(opt.Root, prof, tier.Name, scope)
	if err != nil {
		return nil, "", err
	}
	seed := opt.Seed
	if seed == "" {
		seed = strconv.FormatInt(start.Unix(), 10)
	}
	if err := os.MkdirAll(filepath.Join(opt.Root, OutDir), 0o755); err != nil {
		return nil, "", err
	}

	rep := &report.Report{
		SchemaVersion: report.SchemaVersion,
		Tool:          report.Tool{Name: "reliab", Version: version.Version},
		Run: report.Run{
			Tier:          reportTier,
			Only:          opt.Only,
			Scope:         scope,
			Profile:       prof.Name,
			StartedAt:     report.Now(start),
			BudgetSec:     tier.BudgetSeconds,
			Complete:      true,
			TreeHash:      tree,
			Head:          gitx.Head(opt.Root),
			Seed:          seed,
			PropertyCases: tier.PropertyCases,
			Commands:      []report.CommandResult{},
			Skipped:       []report.Skip{},
		},
		Findings: []report.Finding{},
	}

	runCtx := ctx
	if tier.BudgetSeconds > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithDeadline(ctx, start.Add(time.Duration(tier.BudgetSeconds)*time.Second))
		defer cancel()
	}
	// The base is resolved once, and only when a command uses it, so a repo
	// without one can still run every other command.
	var ch *changes
	if needsBase(prof, tier.Run) {
		base, err := gitx.ResolveBase(opt.Root, opt.Base, prof.Base)
		if err != nil {
			return nil, "", err
		}
		lines, err := gitx.ChangedLines(opt.Root, base)
		if err != nil {
			return nil, "", err
		}
		ch = &changes{base: base, lines: lines}
		rep.Run.Base = base
	}
	rel := judgeRelevance(opt.Root, opt, prof, tier, rep.Run.Base)
	if rel.base != "" {
		rep.Run.Base = rel.base
	}
	vars := map[string]string{
		"{impacted}": impacted, "{cases}": strconv.Itoa(tier.PropertyCases), "{seed}": seed,
		"{scope}": strings.Join(scope, " "), "{base}": rep.Run.Base,
	}
	captureDir := ""
	if opt.Characterize != nil {
		captureDir = filepath.ToSlash(opt.Characterize.Dir)
		vars["{capture}"] = captureDir
	}
	binDir := opt.BinDir
	if binDir == "" {
		if exe, err := os.Executable(); err == nil {
			binDir = filepath.Dir(exe)
		}
	}
	env := append(prependPath(os.Environ(), binDir),
		"RELIAB_TIER="+tier.Name,
		"RELIAB_SEED="+seed,
		"RELIAB_PROPERTY_CASES="+strconv.Itoa(tier.PropertyCases),
		"RELIAB_SCOPE="+strings.Join(scope, " "),
		"RELIAB_CAPTURE_DIR="+captureDir,
		"RELIAB_BASE="+rep.Run.Base,
	)

	what := "--tier " + tier.Name
	if reportTier == report.OnDemand {
		what = "--only " + strings.Join(opt.Only, ",")
		if len(scope) > 0 {
			what += " --scope " + strings.Join(scope, ",")
		}
	}
	fmt.Fprintf(opt.Log, "reliab verify %s (profile %s, budget %s)\n", what, prof.Name, budgetText(tier.BudgetSeconds))
	buildFailed := false
	for _, id := range tier.Run {
		cmd, ok := prof.Commands[id]
		if !ok {
			rep.Run.Skipped = append(rep.Run.Skipped, missingSkip(prof, id))
			continue
		}
		ran, notRelevant := rel.check(cmd)
		if notRelevant != "" {
			rep.Run.Skipped = append(rep.Run.Skipped, report.Skip{ID: id, Reason: report.SkipNotRelevant, Detail: notRelevant})
			fmt.Fprintf(opt.Log, "  %-17s skipped: not relevant: %s\n", id, notRelevant)
			continue
		}
		// After a failed build every later command would test stale
		// binaries; the build failure is already a finding.
		if buildFailed {
			rep.Run.Skipped = append(rep.Run.Skipped, report.Skip{ID: id, Reason: report.SkipBuildFailed})
			fmt.Fprintf(opt.Log, "  %-17s skipped: the build failed\n", id)
			continue
		}
		if !rep.Run.Complete || runCtx.Err() != nil {
			rep.Run.Complete = false
			rep.Run.Skipped = append(rep.Run.Skipped, report.Skip{ID: id, Reason: report.SkipBudget})
			continue
		}
		res, findings, killed := runCommand(runCtx, opt, shell, env, reportTier, id, cmd, vars, ch)
		if killed {
			rep.Run.Complete = false
			rep.Run.Skipped = append(rep.Run.Skipped, report.Skip{
				ID: id, Reason: report.SkipBudget, Detail: fmt.Sprintf("killed after %.1fs", res.ElapsedSec),
			})
			fmt.Fprintf(opt.Log, "  %-17s killed: budget exhausted\n", id)
			continue
		}
		res.Relevance = ran
		rep.Run.Commands = append(rep.Run.Commands, res)
		rep.Findings = append(rep.Findings, findings...)
		if id == BuildID && len(findings) > 0 {
			buildFailed = true
		}
		status := "ok"
		if len(findings) > 0 {
			status = fmt.Sprintf("%d finding(s)", len(findings))
		}
		fmt.Fprintf(opt.Log, "  %-17s %s (%.1fs)\n", id, status, res.ElapsedSec)
	}
	if ctx.Err() != nil {
		return nil, "", ctx.Err()
	}

	prev := decisions(opt.Root)
	carryForward(rep, prev, opt.Root)
	// Scores read the carried classifications: a survivor a person confirmed
	// equivalent leaves the count. A shortfall then keeps a person's
	// acknowledgement only while its numbers are the same.
	mutscore.Apply(rep, prof, baseline)
	carryForward(rep, prev, opt.Root)
	for _, c := range rep.Run.Commands {
		for _, ch := range mutscore.Checks(c.ID, c.Score) {
			if ch.Unmeasured {
				fmt.Fprintf(opt.Log, "  %-17s baseline %s not scored: the tool reported no per-file counts\n", c.ID, ch.Scope)
			}
		}
	}
	rep.Run.ElapsedSec = round(opt.Now().Sub(start).Seconds())
	path, err := report.Write(opt.Root, rep)
	if err != nil {
		return nil, "", err
	}
	return rep, path, nil
}

// runCommand runs one profile command and converts its result. killed is
// true when the budget deadline stopped it.
func runCommand(ctx context.Context, opt Options, shell string, env []string, tier, id string, c profile.Command, vars map[string]string, ch *changes) (report.CommandResult, []report.Finding, bool) {
	line := c.Run
	for k, v := range vars {
		line = strings.ReplaceAll(line, k, v)
	}
	logRel := filepath.ToSlash(filepath.Join(OutDir, tier+"-"+id+".log"))
	logPath := filepath.Join(opt.Root, logRel)
	res := report.CommandResult{ID: id, Format: c.Format, Output: c.Output, Log: logRel}
	if c.Format == "none" {
		res.Format = ""
	}

	// Never parse a previous run's results: remove every match first.
	if c.Output != "" {
		for _, p := range outputs(opt.Root, c.Output) {
			if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
				res.ExitCode = -1
				return res, []report.Finding{genericFinding(id, "cannot remove stale output: "+err.Error())}, false
			}
		}
	}
	logFile, err := os.Create(logPath)
	if err != nil {
		res.ExitCode = -1
		return res, []report.Finding{genericFinding(id, "cannot create log: "+err.Error())}, false
	}
	started := opt.Now()
	cmd := exec.CommandContext(ctx, shell, "-c", line)
	cmd.Dir = opt.Root
	// RELIAB_COMMAND_START lets an adapter ignore files an earlier run left.
	cmd.Env = append(env, "RELIAB_COMMAND_START="+started.UTC().Format(time.RFC3339Nano))
	cmd.Stdout, cmd.Stderr = logFile, logFile
	cmd.WaitDelay = 5 * time.Second
	afterStart, cleanup := killTree(cmd)
	runErr := cmd.Start()
	if runErr == nil {
		if err := afterStart(); err != nil {
			fmt.Fprintf(logFile, "reliab: cannot track process tree: %v\n", err)
		}
		runErr = cmd.Wait()
	}
	cleanup()
	_ = logFile.Close()
	res.ElapsedSec = round(opt.Now().Sub(started).Seconds())

	if ctx.Err() != nil {
		return res, nil, true
	}
	var exitErr *exec.ExitError
	switch {
	case runErr == nil:
	case errors.As(runErr, &exitErr):
		res.ExitCode = exitErr.ExitCode()
	default:
		res.ExitCode = -1
		return res, []report.Finding{genericFinding(id, "cannot run: "+runErr.Error())}, false
	}
	if id == CharacterizeID {
		return res, opt.Characterize.Judge(res.ExitCode, tail(logPath, 30)), false
	}
	in := adapters.Input{
		Command: id, Format: c.Format, Root: opt.Root, Display: c.Output, Declared: c.Output != "",
		ExitCode: res.ExitCode, LogTail: tail(logPath, 30),
	}
	if c.Output != "" {
		in.Outputs = outputs(opt.Root, c.Output)
	}
	if c.ChangedOnly && ch != nil {
		in.Changed, in.Base = ch.lines, ch.base
	}
	out := adapters.Findings(in)
	res.Mutants, res.MutantFiles, res.Coverage = out.Mutants, out.MutantFiles, out.Coverage
	return res, out.Findings, false
}

// outputs returns the absolute paths of the files matching a profile output
// path or glob, relative to root. The profile has already checked the
// pattern.
func outputs(root, output string) []string {
	pattern := filepath.ToSlash(output)
	if !hasMeta(pattern) {
		p := filepath.Join(root, filepath.FromSlash(pattern))
		if _, err := os.Stat(p); err != nil {
			return nil
		}
		return []string{p}
	}
	matches, _ := doublestar.Glob(os.DirFS(root), pattern, doublestar.WithFilesOnly())
	sort.Strings(matches)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, filepath.Join(root, filepath.FromSlash(m)))
	}
	return out
}

func hasMeta(p string) bool { return strings.ContainsAny(p, "*?[{") }

// changes are the lines changed since the base, for changed_only commands.
type changes struct {
	base  string
	lines map[string]map[int]bool
}

// needsBase reports whether a tier's commands use the base: a changed_only
// command or one that names {base}.
func needsBase(p *profile.Profile, ids []string) bool {
	for _, id := range ids {
		if c, ok := p.Commands[id]; ok && (c.ChangedOnly || strings.Contains(c.Run, "{base}")) {
			return true
		}
	}
	return false
}

// prependPath puts dir first on the PATH in env. Windows spells the
// variable in any case.
func prependPath(env []string, dir string) []string {
	if dir == "" {
		return env
	}
	out := make([]string, 0, len(env)+1)
	found := false
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		if k == "PATH" || (runtime.GOOS == "windows" && strings.EqualFold(k, "PATH")) {
			if !found {
				out = append(out, k+"="+dir+string(os.PathListSeparator)+v)
				found = true
			}
			continue
		}
		out = append(out, kv)
	}
	if !found {
		out = append(out, "PATH="+dir)
	}
	return out
}

func genericFinding(id, msg string) report.Finding {
	return report.Finding{ID: adapters.ID(id, ""), Kind: report.KindUnclassified, Command: id, Message: msg, Status: report.StatusOpen}
}

// missingSkip explains why a tier command has no profile entry.
func missingSkip(p *profile.Profile, id string) report.Skip {
	if role, ok := commandRole[id]; ok {
		if r, ok := p.Roles[role]; ok && r.Applicable != nil && !*r.Applicable {
			return report.Skip{ID: id, Reason: report.SkipNotApplicable, Detail: role + ": " + r.Reason}
		}
		if r, ok := p.Roles[role]; ok && r.Missing {
			return report.Skip{ID: id, Reason: report.SkipRoleMissing, Detail: role + ": " + r.Reason}
		}
	}
	return report.Skip{ID: id, Reason: report.SkipNoCommand}
}

// checkOnly rejects on-demand command ids the profile does not define: a
// command asked for by name is never silently skipped.
func checkOnly(p *profile.Profile, ids []string) error {
	for _, id := range ids {
		if _, ok := p.Commands[id]; ok {
			continue
		}
		if role, ok := commandRole[id]; ok {
			if r, ok := p.Roles[role]; ok && r.Applicable != nil && !*r.Applicable {
				return fmt.Errorf("--only %s: %s is not applicable here: %s", id, role, r.Reason)
			}
			if r, ok := p.Roles[role]; ok && r.Missing {
				return fmt.Errorf("--only %s: this codebase needs %s but has no tool for it yet: %s", id, role, r.Reason)
			}
		}
		defined := make([]string, 0, len(p.Commands))
		for k := range p.Commands {
			defined = append(defined, k)
		}
		sort.Strings(defined)
		return fmt.Errorf("--only %s: the profile defines no such command (it defines %s)", id, strings.Join(defined, ", "))
	}
	return nil
}

// cleanScope returns scope paths relative to root and slash-separated. Each
// must exist inside the repository.
func cleanScope(root string, paths []string) ([]string, error) {
	var out []string
	for _, p := range paths {
		abs := p
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(root, p)
		}
		rel, ok := gitx.Rel(root, abs)
		if !ok {
			return nil, fmt.Errorf("--scope %s is outside the repository", p)
		}
		if _, err := os.Stat(abs); err != nil {
			return nil, fmt.Errorf("--scope %s: %w", p, err)
		}
		out = append(out, rel)
	}
	return out, nil
}

// impactedArgs expands {impacted}. With a scope it covers the files under
// the scope, in any tier. Otherwise, outside the inner tier, with no
// changes, or with any changed file no map entry covers, it is the
// profile's "all". Ignored-class files do not widen the selection.
func impactedArgs(root string, p *profile.Profile, tier string, scope []string) (string, error) {
	if p.Impacted == nil {
		return "", nil
	}
	all := p.Impacted.All
	// if, not switch: Go coverage does not instrument case expressions, so
	// gremlins would call their mutants not covered.
	if len(scope) == 0 && tier != "inner" {
		return all, nil
	}
	var files []string
	var err error
	if len(scope) > 0 {
		files, err = gitx.Files(root, scope)
	} else {
		files, err = gitx.ChangedFiles(root)
	}
	if err != nil {
		return "", err
	}
	var args []string
	seen := map[string]bool{}
	for _, f := range files {
		if matchAny(p.Paths.Ignore, f) || matchAny(p.FreshnessExclude, f) {
			continue
		}
		matched := false
		for _, m := range p.Impacted.Map {
			if ok, _ := doublestar.Match(m.Glob, f); ok {
				matched = true
				if !seen[m.Args] {
					seen[m.Args] = true
					args = append(args, m.Args)
				}
			}
		}
		if !matched {
			return all, nil
		}
	}
	if len(args) == 0 {
		return all, nil
	}
	sep := p.Impacted.Separator
	if sep == "" {
		sep = " "
	}
	return strings.Join(args, sep), nil
}

func matchAny(globs []string, path string) bool {
	for _, g := range globs {
		if ok, _ := doublestar.Match(g, path); ok {
			return true
		}
	}
	return false
}

// decided is the newest classification and the newest acknowledgement
// recorded for one finding id, each in the finding that holds it.
type decided struct{ classified, acked *report.Finding }

// decisions collects, per finding id, the newest classification and
// acknowledgement in any tier's latest report or in history. A decision
// made in one tier holds in another, and through a run that skipped the
// command or ran it on demand (knowledge/decisions/finding-classification.md).
func decisions(root string) map[string]decided {
	reports := report.History(root)
	for _, t := range append(slices.Clone(TierNames), report.OnDemand) {
		if r, err := report.Read(report.LatestPath(root, t)); err == nil {
			reports = append(reports, r)
		}
	}
	out := map[string]decided{}
	for _, r := range reports {
		for i := range r.Findings {
			f := &r.Findings[i]
			d := out[f.ID]
			if f.Classification != nil && (d.classified == nil || newer(f.Classification.At, d.classified.Classification.At)) {
				d.classified = f
			}
			if f.Acknowledged != nil && (d.acked == nil || newer(f.Acknowledged.At, d.acked.Acknowledged.At)) {
				d.acked = f
			}
			out[f.ID] = d
		}
	}
	return out
}

// newer reports whether timestamp a is after b; on a tie, or one that does
// not parse, the later record read wins.
func newer(a, b string) bool {
	ta, errA := time.Parse(time.RFC3339, a)
	tb, errB := time.Parse(time.RFC3339, b)
	if errA != nil || errB != nil {
		return a >= b
	}
	return !ta.Before(tb)
}

// carryForward keeps the agent's classification and any human
// acknowledgement for findings that are present, matched by their stable
// id, from the newest decision recorded for each (decisions).
//
// A command-level finding (no test: the command failed as a whole) has one
// id however it failed, so a decision carries only while its fingerprint is
// the same; otherwise it is judged afresh and says why
// (knowledge/decisions/finding-classification.md).
func carryForward(rep *report.Report, prev map[string]decided, root string) {
	for i, f := range rep.Findings {
		if f.Test == "" {
			rep.Findings[i].Fingerprint = report.Fingerprint(f, root)
		}
	}
	for i, f := range rep.Findings {
		d := prev[f.ID]
		// An environment failure says nothing about the next run: once the
		// machine is fixed, a failure there is judged afresh.
		if c := d.classified; c != nil && c.Kind != report.KindEnvironment {
			if why := carryRefused(*c, f, "classified "+c.Kind); why != "" {
				rep.Findings[i].Reopened = why
			} else {
				rep.Findings[i].Kind = c.Kind
				rep.Findings[i].Classification = c.Classification
			}
		}
		if a := d.acked; a != nil && a.Kind != report.KindEnvironment {
			if why := carryRefused(*a, f, "acknowledged by "+a.Acknowledged.By); why != "" {
				rep.Findings[i].Reopened = why
			} else {
				rep.Findings[i].Acknowledged = a.Acknowledged
				rep.Findings[i].Status = report.StatusAcknowledged
			}
		}
	}
}

// FindShell returns the POSIX sh that profile commands run under.
func FindShell() (string, error) {
	for _, c := range shellCandidates() {
		if p, err := exec.LookPath(c); err == nil {
			return p, nil
		}
	}
	return "", errors.New("no POSIX sh found; profile commands run under sh (on Windows install Git for Windows); run reliab doctor")
}

// tail returns up to n final lines of the file at path.
func tail(path string, n int) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		lines = append(lines, sc.Text())
		if len(lines) > n {
			lines = lines[1:]
		}
	}
	return strings.Join(lines, "\n")
}

func round(s float64) float64 { return float64(int64(s*10+0.5)) / 10 }

func budgetText(s int) string {
	if s == 0 {
		return "none"
	}
	return (time.Duration(s) * time.Second).String()
}

// carryRefused says why a command-level finding's earlier decision (what)
// does not carry to cur, or "" when it does: always for a finding with a
// test, and for a command-level one while the failure is the same. An
// earlier report without a fingerprint cannot show that, so nothing carries
// from it.
func carryRefused(old, cur report.Finding, what string) string {
	if cur.Test != "" {
		return ""
	}
	if old.Fingerprint == "" {
		return "it was " + what + " in a report that recorded no fingerprint of the failure, so that decision was not carried; judge it afresh"
	}
	if old.Fingerprint != cur.Fingerprint {
		return "the failure changed since it was " + what + "; judge it afresh"
	}
	return ""
}
