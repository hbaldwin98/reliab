// Package guard answers hook events with reminders, never denials: when an
// edit or command touches what a person approves (protected oracles, the
// person-only reliab commands, the profile's protected commands and edits),
// before a push, once when the agent stops with unverified changes, and when
// a session starts. It stays quiet when the repository has no usable
// profile; CI gates remain authoritative
// (knowledge/decisions/light-guardrails.md).
package guard

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/hbaldwin98/reliab/internal/gitx"
	"github.com/hbaldwin98/reliab/internal/hookio"
	"github.com/hbaldwin98/reliab/internal/profile"
	"github.com/hbaldwin98/reliab/internal/report"
)

// Repo is a repository with a valid profile.
type Repo struct {
	Root    string
	Profile *profile.Profile
	edits   *profile.EditSet
}

// Open finds the repository containing dir and loads its profile. ok is
// false when dir is not in a reliab repository; err is set when the profile
// exists but is invalid.
func Open(dir string) (repo *Repo, ok bool, err error) {
	root, err := gitx.Root(dir)
	if err != nil {
		return nil, false, nil
	}
	path := filepath.Join(root, profile.DefaultPath)
	if _, statErr := os.Stat(path); statErr != nil {
		return nil, false, nil
	}
	p, err := profile.Load(path)
	if err != nil {
		return nil, false, err
	}
	return &Repo{Root: root, Profile: p}, true, nil
}

// editSet is what no agent edits here (profile.EditSet), made once.
func (r *Repo) editSet() *profile.EditSet {
	if r.edits == nil {
		r.edits = profile.NewEditSet(r.Root, r.Profile)
	}
	return r.edits
}

// rel turns a path from a tool call into a repo-relative slash path; ok is
// false for a path outside the repository.
func (r *Repo) rel(cwd, p string) (string, bool) {
	p = strings.Trim(p, `"'`)
	if p == "" {
		return "", false
	}
	p = filepath.FromSlash(strings.ReplaceAll(p, `\`, "/"))
	if !filepath.IsAbs(p) {
		base := cwd
		if base == "" {
			base = r.Root
		}
		p = filepath.Join(base, p)
	}
	return gitx.Rel(r.Root, filepath.Clean(p))
}

// caseless says the file system ignores case by default, as Windows' and
// macOS's do, so a path spelled in another case names the same file.
var caseless = caselessOS(runtime.GOOS)

// caselessOS is caseless for an operating system. A function, not the
// variable's initializer: Go coverage does not instrument package-level
// initializers, so gremlins never ran a mutant there.
func caselessOS(goos string) bool { return goos == "windows" || goos == "darwin" }

// match reports whether the repo-relative path matches a protected glob,
// ignoring case where the file system does.
func match(glob, rel string) bool {
	if caseless {
		glob, rel = strings.ToLower(glob), strings.ToLower(rel)
	}
	ok, _ := doublestar.Match(glob, rel)
	return ok
}

// isProtected reports whether the repo-relative path is, or is a directory
// under, a protected path. The profile is not, until the person commits it:
// the profile being onboarded is the agent's to write
// (knowledge/decisions/protected-path-approval.md).
func (r *Repo) isProtected(rel string) (string, bool) {
	if r.editSet().Exempt(rel) {
		return "", false
	}
	for _, g := range r.editSet().Globs() {
		if match(g, rel) || match(g, strings.TrimSuffix(rel, "/")+"/x") {
			return g, true
		}
	}
	return "", false
}

// PreEdit reminds the agent when Edit, Write, or NotebookEdit targets a
// protected path (R2).
func (r *Repo) PreEdit(in hookio.Input) hookio.Decision {
	rel, ok := r.rel(in.Cwd, in.ToolInput.Path())
	if !ok {
		return hookio.Decision{}
	}
	if g, hit := r.isProtected(rel); hit {
		return remind(protectedReason(rel, g))
	}
	return r.protectedEdit(rel, in.ToolInput)
}

// protectedEdit reminds on an edit that adds text matching protected_edits,
// such as a snapshot tool's auto-accept switch: it approves an oracle
// without naming its path (R2). Text the file already had is not counted,
// so editing near an existing match is allowed. A hook cannot see what a
// test opts into at runtime by other means; gate protected is the backstop.
func (r *Repo) protectedEdit(rel string, t hookio.ToolInput) hookio.Decision {
	guards := r.Profile.EditGuards()
	if len(guards) == 0 {
		return hookio.Decision{}
	}
	type change struct{ before, after string }
	// if, not switch: Go coverage does not instrument case expressions, so
	// gremlins would call their mutants not covered.
	changes := []change{{t.OldString, t.NewString}}
	if len(t.Edits) > 0 {
		changes = nil
		for _, e := range t.Edits {
			changes = append(changes, change{e.OldString, e.NewString})
		}
	} else if t.Content != "" {
		old, _ := os.ReadFile(filepath.Join(r.Root, filepath.FromSlash(rel)))
		changes = []change{{string(old), t.Content}}
	} else if t.NewSource != "" {
		changes = []change{{"", t.NewSource}}
	}
	for _, re := range guards {
		for _, c := range changes {
			if len(re.FindAllStringIndex(c.after, -1)) > len(re.FindAllStringIndex(c.before, -1)) {
				return remind(fmt.Sprintf("this edit to %s adds text matching protected_edits %q, which approves or rewrites a protected oracle without naming it. "+
					"Oracles change with a person's approval (R2): unless the user asked for this, leave the diff for them and classify the finding characterization-diff.", rel, re.String()))
			}
		}
	}
	return hookio.Decision{}
}

// protectedEditsRule extends the R2 rule with the profile's protected
// edits, when it has any.
func protectedEditsRule(pats []string) string {
	if len(pats) == 0 {
		return ""
	}
	return "; never add text matching " + strings.Join(pats, ", ")
}

// protectedCommandsRule extends the R2 rule with the profile's protected
// commands, when it has any.
func protectedCommandsRule(pats []string) string {
	if len(pats) == 0 {
		return ""
	}
	return "; never run commands matching " + strings.Join(pats, ", ")
}

// remind is a hook answer the agent reads and the harness never blocks on.
func remind(msg string) hookio.Decision {
	return hookio.Decision{Context: "reliab reminder (not blocked): " + msg}
}

func protectedReason(rel, glob string) string {
	if strings.HasPrefix(glob, ".reliab/") {
		return fmt.Sprintf("%s is written by the reliab binary; reliab verify, reliab findings classify, and reliab characterize discard change it.", rel)
	}
	return fmt.Sprintf("%s is a protected oracle (matches %s), which changes with a person's approval (R2). "+
		"Unless the user asked for this change, report it to them instead.", rel, glob)
}

// personCmd matches reliab run as a command, at the start of the line or
// after a separator, with its binary bare, pathed, or quoted (& "C:\tools\
// reliab.exe"), followed by a person-only subcommand. reliab named inside
// an argument, such as a commit message, is not at a command position and
// passes (knowledge/decisions/light-guardrails.md).
var personCmd = regexp.MustCompile(`(?i)^\s*(?:\w+=\S*\s+)*["']?(?:[^\s"';&|]*[\\/])?reliab(?:\.exe|\.cmd)?["']?\s+(findings\s+ack|approve|report\s+verdict|baseline\s+write)\b`)

// personSubcommand returns the person-only subcommand cmd runs, if any. It
// finds each "reliab" and matches personCmd, anchored, from the command
// position before it: just after the nearest separator, or the line's start.
// Anchoring keeps a long commit message cheap.
func personSubcommand(cmd string) string {
	lower := strings.ToLower(cmd)
	if !strings.Contains(lower, "reliab") {
		return ""
	}
	if strings.Contains(cmd, "<<") {
		cmd = stripHeredocs(cmd)
		lower = strings.ToLower(cmd)
	}
	for off := 0; ; {
		i := strings.Index(lower[off:], "reliab")
		if i < 0 {
			return ""
		}
		i += off
		start := strings.LastIndexAny(cmd[:i], ";&|(`{\n") + 1
		if m := personCmd.FindStringSubmatch(cmd[start:]); m != nil {
			return strings.ToLower(strings.Join(strings.Fields(m[1]), " "))
		}
		off = i + len("reliab")
	}
}

// pushCmd matches a git push or a PR creation, for the pre-push reminder.
var pushCmd = regexp.MustCompile(`\bgit\b[^;&|\n]*\spush\b|\bgh\s+pr\s+create\b`)

// ReviewerAgent is the plugin's reviewer subagent as hooks see it: the only
// caller allowed to record a verdict (knowledge/decisions/reviewer-verdict.md).
const ReviewerAgent = "reliab:reviewer"

// isReviewer accepts the plugin's reviewer and a project copy of it
// (.claude/agents/reviewer.md), which hooks name without the plugin prefix.
func isReviewer(agent string) bool { return agent == ReviewerAgent || agent == "reviewer" }

// PreBash reminds the agent when it runs a person-only reliab command or a
// profile's protected command, and of the pre-PR report's state before a
// push or PR creation. Nothing else is inspected, and nothing is denied.
func (r *Repo) PreBash(in hookio.Input) hookio.Decision {
	cmd := in.ToolInput.Command
	if sub := personSubcommand(cmd); sub != "" {
		if sub == "findings ack" {
			return remind("findings ack records a person's decision. Unless the user asked you to record it, ask them.")
		}
		if sub == "baseline write" {
			return remind("baseline write changes the protected mutation baseline (R2). Unless the user asked for it, show them reliab baseline propose.")
		}
		if sub == "approve" {
			return remind("approve records a person's approval of a protected-path change (R2). Unless the user asked you to record it, list the change as a question for them.")
		}
		if !isReviewer(in.AgentType) {
			return remind("report verdict is the reviewer's to record, not the author's; the " + ReviewerAgent + " agent records it after reviewing.")
		}
	}
	for _, re := range r.Profile.CommandGuards() {
		if re.MatchString(cmd) {
			return remind(fmt.Sprintf("this command approves or rewrites a protected oracle (matches protected_commands %q). "+
				"Oracles change with a person's approval (R2): unless the user asked for this, leave the diff for them and classify the finding characterization-diff.", re.String()))
		}
	}
	// The substring checks keep the regexp off most lines.
	if (strings.Contains(cmd, "push") || strings.Contains(cmd, "pr create")) && pushCmd.MatchString(cmd) {
		return r.PrePush()
	}
	return hookio.Decision{}
}

// heredocStart finds a heredoc operator and its delimiter word.
var heredocStart = regexp.MustCompile(`<<-?\s*['"]?([A-Za-z_][A-Za-z0-9_]*)['"]?`)

// stripHeredocs drops heredoc bodies, which are data (a commit message
// naming reliab approve on a line of its own), keeping the rest of the line.
func stripHeredocs(cmd string) string {
	for {
		loc := heredocStart.FindStringSubmatchIndex(cmd)
		if loc == nil {
			return cmd
		}
		word := cmd[loc[2]:loc[3]]
		nl := strings.IndexByte(cmd[loc[1]:], '\n')
		if nl < 0 {
			return cmd
		}
		bodyStart := loc[1] + nl + 1
		rest := cmd[bodyStart:]
		end := len(cmd)
		for i := 0; i <= len(rest); {
			j := strings.IndexByte(rest[i:], '\n')
			line := rest[i:]
			if j >= 0 {
				line = rest[i : i+j]
			}
			if strings.TrimSpace(line) == word {
				end = bodyStart + i + len(line)
				break
			}
			if j < 0 {
				break
			}
			i += j + 1
		}
		cmd = cmd[:loc[0]] + cmd[end:]
	}
}

// PrePush reminds the agent, before a push or PR, when the latest pre-PR
// report is missing, stale, incomplete, has open findings, or lacks a
// verdict the profile requires. CI is the gate.
func (r *Repo) PrePush() hookio.Decision {
	if problem := r.prePushProblem(); problem != "" {
		return remind(problem + " Run reliab verify --tier prepr and resolve what reliab report route lists before this lands.")
	}
	return hookio.Decision{}
}

func (r *Repo) prePushProblem() string {
	rep, err := report.Read(report.LatestPath(r.Root, "prepr"))
	if err != nil {
		return "there is no pre-PR report."
	}
	if fresh, err := r.fresh(rep); err != nil {
		return "cannot check report freshness: " + err.Error() + "."
	} else if !fresh {
		return "the pre-PR report is stale; the code changed while or after it ran."
	}
	if waiting := rep.NeedsInput(); len(waiting) > 0 {
		return "the pre-PR report is incomplete: " + needsInputText(waiting) + "."
	}
	if !rep.Run.Complete {
		return "the pre-PR report is incomplete (budget ran out)."
	}
	if n := countOpen(rep); n > 0 {
		return fmt.Sprintf("the pre-PR report has %d open finding(s).", n)
	}
	if r.Profile.RequireReviewerVerdict {
		v := rep.Verdict
		if v == nil {
			return "the profile asks for a reviewer verdict and the report has none; have the " + ReviewerAgent + " agent review the change."
		}
		if v.Outcome != report.VerdictPass {
			return "the reviewer's verdict is " + v.Outcome + ": " + v.Notes
		}
		if !rep.VerdictCurrent() {
			return "the reviewer's verdict is for another tree; have the " + ReviewerAgent + " agent review the change again."
		}
	}
	return ""
}

// needsInputText lists the commands that wait for tests to be named.
func needsInputText(skips []report.Skip) string {
	parts := make([]string, len(skips))
	for i, s := range skips {
		parts[i] = s.ID + ": " + s.Detail
	}
	return strings.Join(parts, "; ")
}

func (r *Repo) treeHash() (string, error) {
	return gitx.TreeHash(r.Root, r.Profile.TreeExclude())
}

func (r *Repo) fresh(rep *report.Report) (bool, error) {
	h, err := r.treeHash()
	return rep.FreshFor(h), err
}

func countOpen(rep *report.Report) int {
	n := 0
	for _, f := range rep.Findings {
		if f.Status == report.StatusOpen {
			n++
		}
	}
	return n
}

// Stop prods the agent once when product code changed since the latest
// report (R8) or the agent still owes work on open findings. Findings that
// need a human do not prod: stopping to ask is the right move. The harness
// delivers a reminder from a Stop hook only as a block, so this blocks once
// and lets the next stop through (stop_hook_active): a nudge, never a trap.
func (r *Repo) Stop(in hookio.Input) hookio.Decision {
	if in.StopHookActive {
		return hookio.Decision{}
	}
	rep := report.Latest(r.Root, "inner", "prepr")
	changed, err := r.productChangesSince(rep, in.SessionID)
	if err != nil {
		return hookio.Decision{}
	}
	if len(changed) > 0 {
		return hookio.Decision{Block: fmt.Sprintf("reliab reminder: product code changed since the last report (%s). "+
			"Run reliab verify --tier inner and act on reliab report route, or say why not and stop again (R8).", summarize(changed))}
	}
	if rep == nil {
		return hookio.Decision{}
	}
	// A command that needs tests named has not run: the change is not
	// verified until the agent names them (knowledge/decisions/named-tests.md).
	if waiting := rep.NeedsInput(); len(waiting) > 0 {
		return hookio.Decision{Block: fmt.Sprintf("reliab reminder: the %s report is incomplete: %s. "+
			"Name the tests that guard the change and re-run reliab verify --tier %s.", rep.Run.Tier, needsInputText(waiting), rep.Run.Tier)}
	}
	var owed []string
	for _, f := range rep.Findings {
		if report.NeedsAgent(f) {
			owed = append(owed, f.ID+" ["+f.Kind+"]")
		}
	}
	if len(owed) > 0 {
		return hookio.Decision{Block: fmt.Sprintf("reliab reminder: open findings still need you: %s. "+
			"Run reliab report route; classify, fix, and re-run reliab verify.", summarize(owed))}
	}
	return hookio.Decision{}
}

// productChangesSince lists product-class paths that differ between the
// report's tree and the working tree. When there is no report or its tree
// is no longer available, it lists what differs from HEAD plus what the
// session changed since it started, so committing a change does not hide it.
func (r *Repo) productChangesSince(rep *report.Report, session string) ([]string, error) {
	var paths []string
	if rep != nil {
		h, err := r.treeHash()
		if err != nil {
			return nil, err
		}
		if rep.FreshFor(h) {
			return nil, nil
		}
		// Back on the tree the report started on: what changed while it ran.
		to := h
		if h == rep.Run.TreeHash {
			to = rep.Run.TreeHashEnd
		}
		out, err := exec.Command("git", "-C", r.Root, "diff-tree", "-r", "--name-only", rep.Run.TreeHash, to).Output()
		if err == nil {
			paths = strings.Fields(string(out))
		} else {
			rep = nil
		}
	}
	if rep == nil {
		var err error
		if paths, err = gitx.ChangedFiles(r.Root); err != nil {
			return nil, err
		}
		paths = append(paths, r.changedSinceSessionStart(session)...)
	}
	var product []string
	seen := map[string]bool{}
	for _, p := range paths {
		if r.isProduct(p) && !seen[p] {
			seen[p] = true
			product = append(product, p)
		}
	}
	return product, nil
}

// isProduct applies knowledge/decisions/path-classification.md: tests and
// ignored paths are not product; anything else, matched or not, is.
func (r *Repo) isProduct(rel string) bool {
	if strings.HasPrefix(rel, ".reliab/") {
		return false
	}
	return !matchAny(r.Profile.Paths.Tests, rel) && !matchAny(r.Profile.Paths.Ignore, rel) &&
		!matchAny(r.Profile.FreshnessExclude, rel)
}

func matchAny(globs []string, p string) bool {
	for _, g := range globs {
		if ok, _ := doublestar.Match(g, p); ok {
			return true
		}
	}
	return false
}

func summarize(items []string) string {
	if len(items) > 5 {
		return strings.Join(items[:5], ", ") + fmt.Sprintf(" and %d more", len(items)-5)
	}
	return strings.Join(items, ", ")
}

// sessionAnchor is where a session's starting tree is kept.
func (r *Repo) sessionAnchor(session string) string {
	if session == "" || session != filepath.Base(session) || strings.HasPrefix(session, ".") {
		return ""
	}
	return filepath.Join(r.Root, ".reliab", "sessions", session)
}

// anchorSession records the working tree's hash when a session starts. A
// resumed or compacted session keeps its first anchor.
func (r *Repo) anchorSession(session string) {
	path := r.sessionAnchor(session)
	if path == "" {
		return
	}
	if _, err := os.Stat(path); err == nil {
		return
	}
	h, err := r.treeHash()
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(path, []byte(h+"\n"), 0o644)
}

// changedSinceSessionStart lists paths that differ between the session's
// starting tree and the working tree, committed or not; none when the
// session has no anchor.
func (r *Repo) changedSinceSessionStart(session string) []string {
	path := r.sessionAnchor(session)
	if path == "" {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	h, err := r.treeHash()
	if err != nil {
		return nil
	}
	out, err := exec.Command("git", "-C", r.Root, "diff-tree", "-r", "--name-only", strings.TrimSpace(string(raw)), h).Output()
	if err != nil {
		return nil
	}
	return strings.Fields(string(out))
}

// SessionStart briefs the agent: the profile, the testing workflow, and the
// open findings in the latest report. It also anchors the session, so Stop
// can see changes the session commits before any report exists.
func (r *Repo) SessionStart(in hookio.Input) hookio.Decision {
	r.anchorSession(in.SessionID)
	var b strings.Builder
	p := r.Profile
	fmt.Fprintf(&b, "This repository uses reliab (profile %s, starting condition %s).\n", p.Name, p.StartingCondition)
	b.WriteString(`Workflow:
- Run reliab verify --tier inner when a change is done and --tier prepr before it merges; act on reliab report route.
- A failing test is a finding: reproduce it, fix product code (never an assertion or tolerance), or classify it with reliab findings classify.
- Every property cites its source of intent in a comment.
- Protected oracles change only with a person's approval: never edit ` + strings.Join(p.ProtectedPaths, ", ") + protectedCommandsRule(p.ProtectedCommands) + protectedEditsRule(p.ProtectedEdits) + ` (R2).
- On request, the plugin's skills test one area: mutation (how strong its tests are), properties (tests from stated intent), fuzz (parsers and decoders), characterize (around a refactor or migration), adapter (a tool reliab cannot read).
- To explain reliab or what to run when: reliab docs overview; to wire CI: reliab docs ci.
`)
	// The plugin launcher exports its version; the repository pins one for CI.
	if plugin := os.Getenv("RELIAB_PLUGIN_VERSION"); plugin != "" {
		if raw, err := os.ReadFile(filepath.Join(r.Root, ".reliab-version")); err == nil {
			if pin := strings.TrimSpace(string(raw)); pin != "" && pin != plugin {
				fmt.Fprintf(&b, "Warning: the reliab plugin is %s but this repository pins %s for CI; results may differ.\n", plugin, pin)
			}
		}
	}
	rep := report.Latest(r.Root, "inner", "prepr")
	if rep == nil {
		b.WriteString("No report yet.\n")
		return hookio.Decision{Context: b.String()}
	}
	fresh, _ := r.fresh(rep)
	fmt.Fprintf(&b, "Latest report: %s tier at %s, complete=%t, fresh=%t.\n", rep.Run.Tier, rep.Run.StartedAt, rep.Run.Complete, fresh)
	for _, f := range rep.Findings {
		if f.Status == report.StatusOpen {
			subject := f.Test
			if subject == "" {
				subject = "command " + f.Command
			}
			fmt.Fprintf(&b, "- open %s [%s] %s\n", f.ID, f.Kind, subject)
		}
	}
	return hookio.Decision{Context: b.String()}
}
