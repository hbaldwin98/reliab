// Package guard decides hook events: it denies edits to protected oracles,
// denies shell commands that would write them or push unverified work, keeps
// the agent from stopping with unverified changes, and briefs new sessions.
// It fails open when the repository has no usable profile, so a broken
// profile never bricks a session; CI gates remain authoritative
// (knowledge/decisions/enforcement-hooks-self-contained.md).
package guard

import (
	"fmt"
	"os"
	"os/exec"
	"path"
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

// alwaysProtected are reliab's own artifacts: only the binary writes them.
var alwaysProtected = []string{".reliab/**"}

func (r *Repo) protected() []string {
	return append(append([]string{}, alwaysProtected...), r.Profile.ProtectedPaths...)
}

// rels turns a path from a tool call into the repo-relative slash paths it
// may name: with .. taken by name, as PowerShell takes it, and as the kernel
// resolves it through symlinks (physical). It returns none for a path
// outside the repository.
func (r *Repo) rels(cwd, p string) []string {
	p = strings.Trim(p, `"'`)
	if p == "" {
		return nil
	}
	// PowerShell commands use backslashes on every OS.
	p = filepath.FromSlash(strings.ReplaceAll(p, `\`, "/"))
	if !filepath.IsAbs(p) {
		base := cwd
		if base == "" {
			base = r.Root
		}
		p = base + string(filepath.Separator) + p
	}
	var out []string
	if rel, ok := gitx.Rel(r.Root, filepath.Clean(p)); ok {
		out = append(out, rel)
	}
	if rel, ok := gitx.Rel(r.Root, physical(p)); ok && (len(out) == 0 || rel != out[0]) {
		out = append(out, rel)
	}
	return out
}

// physical resolves p, an absolute path, as the kernel does
// (path_resolution(7)): each component through symlinks before the next, so
// a .. after a symlink leaves the directory the link leads to, not the one
// it is named in. What does not exist yet is joined as named.
func physical(p string) string {
	vol := filepath.VolumeName(p)
	cur := vol + string(filepath.Separator)
	for _, c := range strings.Split(filepath.ToSlash(p[len(vol):]), "/") {
		if c == "" || c == "." {
			continue
		}
		if c == ".." {
			cur = filepath.Dir(cur)
			continue
		}
		cur = filepath.Join(cur, c)
		if real, err := filepath.EvalSymlinks(cur); err == nil {
			cur = real
		}
	}
	return cur
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
	if rel == profile.DefaultPath && gitx.BlobAt(r.Root, "HEAD", rel) == "" {
		return "", false
	}
	for _, g := range r.protected() {
		if match(g, rel) || match(g, strings.TrimSuffix(rel, "/")+"/x") {
			return g, true
		}
	}
	return "", false
}

// holdsProtected reports whether the repo-relative path is a directory that
// holds a protected file, such as a snapshot directory whose files a glob
// like **/*.verified.* matches. files lists the repository's files, as
// gitx.Files does, so one listing serves every path a command names.
func (r *Repo) holdsProtected(files []string, rel string) (string, bool) {
	dir := strings.TrimSuffix(rel, "/")
	if fi, err := os.Stat(filepath.Join(r.Root, filepath.FromSlash(dir))); err != nil || !fi.IsDir() {
		return "", false
	}
	prefix := dir + "/"
	if dir == "." || dir == "" {
		prefix = ""
	}
	for _, f := range files {
		if caseless && !strings.HasPrefix(strings.ToLower(f), strings.ToLower(prefix)) || !caseless && !strings.HasPrefix(f, prefix) {
			continue
		}
		for _, g := range r.protected() {
			if match(g, f) {
				return g, true
			}
		}
	}
	return "", false
}

// PreEdit denies Edit, Write, and NotebookEdit calls on protected paths (R2).
func (r *Repo) PreEdit(in hookio.Input) hookio.Decision {
	rels := r.rels(in.Cwd, in.ToolInput.Path())
	if len(rels) == 0 {
		return hookio.Decision{}
	}
	for _, rel := range rels {
		if g, hit := r.isProtected(rel); hit {
			return hookio.Decision{Deny: protectedReason(rel, g)}
		}
	}
	return r.protectedEdit(rels[0], in.ToolInput)
}

// protectedEdit denies an edit that adds text matching protected_edits,
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
				return hookio.Decision{Deny: fmt.Sprintf("reliab: this edit to %s adds text matching protected_edits %q, which approves or rewrites a protected oracle without naming it. "+
					"Oracles change only with human approval (R2): leave the diff for a person, classify the finding characterization-diff, and stop.", rel, re.String())}
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

func protectedReason(rel, glob string) string {
	if strings.HasPrefix(glob, ".reliab/") {
		return fmt.Sprintf("reliab: %s is written only by the reliab binary. Use reliab verify, reliab findings classify, reliab report route, or reliab characterize discard instead.", rel)
	}
	return fmt.Sprintf("reliab: %s is a protected oracle (matches %s). Oracles change only with human approval (R2). "+
		"Do not edit it; report the needed change to the user, who decides.", rel, glob)
}

var (
	// writeVerb matches shell and PowerShell commands that write, move, or
	// delete files, with PowerShell's built-in aliases for them (del, ri,
	// copy, sc, and the rest). It is a heuristic; the CI gate is the
	// backstop.
	writeVerb = regexp.MustCompile(`(?i)(^|[\s;&|(])(sed\s+(-[a-z]*i|--in-place)|perl\s+-[a-z]*i|cp|mv|rm|rmdir|tee|truncate|dd|install|ln|touch|chmod|` + gitSub + `(checkout|restore|rm|mv|clean|reset)|set-content|add-content|out-file|remove-item|copy-item|move-item|new-item|rename-item|clear-content|del|erase|ri|rd|copy|cpi|move|mi|sc|ac|ni|ren|rni|clc)\b`)
	// dirVerb matches commands that rewrite or remove a whole directory's
	// files when given one, so a directory holding an oracle is protected
	// too. Other write verbs only write into a directory they name.
	dirVerb = regexp.MustCompile(`(?i)(^|[\s;&|(])(rm|rmdir|` + gitSub + `(checkout|restore|rm|clean|reset)|remove-item|del|erase|ri|rd)\b`)
	// commandSep splits a command line into simple commands.
	commandSep = regexp.MustCompile(`[;&|\n]+`)
	// redirect matches every output redirect, a numbered (2>) or &> one too
	// (GNU Bash manual §3.6); >&1 names no file.
	redirect = regexp.MustCompile(`(^|[^>])>{1,2}\s*([^\s;&|>]+)`)
	// fileDup matches >&word where word names a file, not a descriptor.
	fileDup = regexp.MustCompile(`>&\s*[^\s\d\-;&|<>]`)
	// outputFlags name a file or directory a command writes.
	outputFlags = map[string]bool{"--output": true, "--output-file": true, "--output-directory": true, "-outfile": true}
	// shortOutputs are commands whose short option names the file they
	// write. git's -o names one only in format-patch, which sets it.
	shortOutputs = map[string]string{"curl": "-o", "sort": "-o", "go": "-o", "wget": "-O"}
	pushCmd      = regexp.MustCompile(`(?i)\bgit\b[^;&|\n]*\bpush\b|\bgh\s+pr\s+create\b`)
	// The binary may be quoted: & "C:\tools\reliab.exe" findings ack.
	ackCmd     = regexp.MustCompile(`(?i)\breliab(\.exe|\.cmd)?["']?\s+findings\s+ack\b`)
	approveCmd = regexp.MustCompile(`(?i)\breliab(\.exe|\.cmd)?["']?\s+approve\b`)
	tokenSep   = regexp.MustCompile(`[\s;&|()<>]+`)
	verdictCmd = regexp.MustCompile(`(?i)\breliab(\.exe|\.cmd)?["']?\s+report\s+verdict\b`)
	// baselineWrite changes the protected mutation baseline without naming
	// its path.
	baselineWrite = regexp.MustCompile(`(?i)\breliab(\.exe|\.cmd)?["']?\s+baseline\s+write\b`)
)

// gitSub matches git and the global options before its subcommand (git(1)
// SYNOPSIS): -C <path> and -c <name>=<value>, the path options in either
// spelling, and switches such as --no-pager. -C also moves where relative
// paths resolve (dirFlags).
const gitSub = `git(\s+(-c\s+\S+|--(git-dir|work-tree|namespace|exec-path|config-env)(=|\s+)\S+|-{1,2}[a-z][\w-]*))*\s+`

// ReviewerAgent is the plugin's reviewer subagent as hooks see it: the only
// caller allowed to record a verdict (knowledge/decisions/reviewer-verdict.md).
const ReviewerAgent = "reliab:reviewer"

// PreBash inspects Bash and PowerShell commands. It denies agents the human
// acknowledgement and approval commands, everyone but the reviewer the
// verdict command, writes to protected paths, and pushes or PR creation
// without a fresh, complete pre-PR report.
func (r *Repo) PreBash(in hookio.Input) hookio.Decision {
	cmd := in.ToolInput.Command
	// The person-only commands are judged on the code Bash may run, so their
	// words in a commit message or an echo do not trip them. codeText models
	// Bash only; a PowerShell or any other line is judged whole.
	code := cmd
	if in.ToolName == "Bash" {
		code = codeText(cmd)
	}
	if ackCmd.MatchString(code) {
		return hookio.Decision{Deny: "reliab: findings ack records a human decision; only a person runs it. Ask the user."}
	}
	if baselineWrite.MatchString(code) {
		return hookio.Decision{Deny: "reliab: baseline write changes the protected mutation baseline (R2); only a person runs it. Show the user reliab baseline propose and ask."}
	}
	if approveCmd.MatchString(code) {
		return hookio.Decision{Deny: "reliab: approve records a person's approval of a protected-path change (R2); only a person runs it. " +
			"List the change under questions for a human and stop; the user runs reliab approve <path> --by human:<name>."}
	}
	if verdictCmd.MatchString(code) && in.AgentType != ReviewerAgent {
		return hookio.Decision{Deny: "reliab: report verdict is the reviewer's to record, never the author's. " +
			"After a clean reliab verify --tier prepr, have the " + ReviewerAgent + " agent review the change; it records the verdict."}
	}
	for _, re := range r.Profile.CommandGuards() {
		if re.MatchString(cmd) {
			return hookio.Decision{Deny: fmt.Sprintf("reliab: this command approves or rewrites a protected oracle (matches protected_commands %q). "+
				"Oracles change only with human approval (R2): leave the diff for a person, classify the finding characterization-diff, and stop.", re.String())}
		}
	}
	if d := r.protectedWrite(in.Cwd, cmd, in.ToolName == "PowerShell"); d.Deny != "" {
		return d
	}
	if pushCmd.MatchString(cmd) {
		return r.PrePush()
	}
	return hookio.Decision{}
}

// protectedWrite judges each simple command on its own: a path is a write
// target only as a redirection target, or as an operand after a write verb
// in the same simple command. So a read that names a protected path or
// .reliab/ passes, even beside a write elsewhere in the command line. A
// relative path resolves in each directory its simple command may run in
// (see parts); pwsh says the line is PowerShell.
func (r *Repo) protectedWrite(cwd, cmd string, pwsh bool) hookio.Decision {
	if cwd == "" {
		cwd = r.Root
	}
	type target struct {
		path string
		cwds []string
	}
	var targets []target
	// >| is a redirect that overrides noclobber (GNU Bash manual §3.6.2),
	// not a pipe, and >&word one of stdout and stderr to a file unless word
	// is a number or - (§3.6.4); the same length keeps every offset.
	cmd = strings.ReplaceAll(cmd, ">|", "> ")
	cmd = fileDup.ReplaceAllStringFunc(cmd, func(m string) string { return "> " + m[2:] })
	ps := parts(cwd, cmd, pwsh)
	for _, p := range ps {
		dirs := union(p.cwds, p.may)
		for _, m := range redirect.FindAllStringSubmatch(p.text, -1) {
			targets = append(targets, target{m[2], dirs})
		}
		if loc := writeVerb.FindStringIndex(p.text); loc != nil {
			for _, t := range tokenSep.Split(p.text[loc[1]:], -1) {
				targets = append(targets, target{t, dirs})
			}
		}
		for _, t := range outputs(words(p.text, pwsh)) {
			targets = append(targets, target{t, dirs})
		}
	}
	var files, repoDirs []string
	listed := false
	list := func() []string {
		if !listed {
			files, _ = gitx.Files(r.Root, nil)
			listed = true
		}
		return files
	}
	paths := func(dir, t string) []string {
		if dir != anywhere {
			return r.target(dir, t)
		}
		if repoDirs == nil {
			repoDirs = r.dirs(list())
		}
		return r.fromAnywhere(repoDirs, t)
	}
	for _, t := range targets {
		for _, dir := range t.cwds {
			for _, rel := range paths(dir, t.path) {
				if g, hit := r.isProtected(rel); hit {
					return hookio.Decision{Deny: protectedReason(rel, g)}
				}
			}
		}
	}
	// A directory holding an oracle counts only as an argument of a command
	// that rewrites or removes a whole directory, in its own simple command,
	// so not git -C's directory before the subcommand.
	for _, p := range ps {
		loc := dirVerb.FindStringIndex(p.text)
		if loc == nil {
			continue
		}
		for _, t := range tokenSep.Split(p.text[loc[1]:], -1) {
			for _, dir := range union(p.cwds, p.may) {
				for _, rel := range paths(dir, t) {
					if g, hit := r.holdsProtected(list(), rel); hit {
						return hookio.Decision{Deny: protectedReason(rel, g)}
					}
				}
			}
		}
	}
	return hookio.Decision{}
}

// target turns one command token into the repo-relative paths it may name
// (rels), skipping options and the null device.
func (r *Repo) target(cwd, t string) []string {
	t = strings.Trim(t, `"'`)
	if t == "" || strings.HasPrefix(t, "-") || t == "/dev/null" {
		return nil
	}
	out := r.rels(cwd, t)
	// Git Bash on Windows names drive C: /c, as cdTarget reads it.
	// A drive's root itself (/c) holds no repository's file.
	if runtime.GOOS == "windows" && len(t) > 2 && t[0] == '/' && isLetter(t[1]) && t[2] == '/' {
		out = append(out, r.rels(cwd, t[1:2]+":/"+t[3:])...)
	}
	return out
}

// fromAnywhere turns one relative command token into the repo-relative
// paths it may name from a directory the hook cannot know. Its leading ..s
// reach any directory, so what follows them is judged below each directory
// of the repository (repoDirs) and each directory above it.
func (r *Repo) fromAnywhere(repoDirs []string, t string) []string {
	t = strings.Trim(t, `"'`)
	if t == "" || strings.HasPrefix(t, "-") || t == "/dev/null" {
		return nil
	}
	p := filepath.FromSlash(strings.ReplaceAll(t, `\`, "/"))
	sep := string(filepath.Separator)
	// Absolute, rooted, or on a drive: it names the same file from anywhere
	// on that drive.
	if filepath.IsAbs(p) || filepath.VolumeName(p) != "" || strings.HasPrefix(p, sep) {
		return r.rels(r.Root, p)
	}
	p = filepath.Clean(p)
	for p == ".." || strings.HasPrefix(p, ".."+sep) {
		p = strings.TrimPrefix(strings.TrimPrefix(p, ".."), sep)
	}
	var out []string
	for _, base := range repoDirs {
		if rel, ok := gitx.Rel(r.Root, filepath.Join(base, p)); ok {
			out = append(out, rel)
		}
	}
	return out
}

// dirs lists the repository's root, every directory holding one of its
// files (gitx.Files) but a protected one, and every directory above the
// root. A cd into an oracle's directory is no accident, and counting it
// would deny every relative write after a cd to a variable, such as rm -rf
// build.
func (r *Repo) dirs(files []string) []string {
	out := []string{r.Root}
	for d := r.Root; filepath.Dir(d) != d; {
		d = filepath.Dir(d)
		out = append(out, d)
	}
	seen := map[string]bool{}
	for _, f := range files {
		for d := path.Dir(f); d != "." && !seen[d]; d = path.Dir(d) {
			seen[d] = true
			if _, hit := r.isProtected(d); !hit {
				out = append(out, filepath.Join(r.Root, filepath.FromSlash(d)))
			}
		}
	}
	return out
}

// outputs returns the files a simple command's options name for output:
// --output, --output-file, or --output-directory (git diff and log write
// a patch with --output), PowerShell's -OutFile (also -OutFile:<path>),
// and the short option of a command that writes there (shortOutputs):
// git format-patch -o <dir>, curl -o, sort -o, go build -o, wget -O, with
// the value attached or not.
func outputs(ws []string) []string {
	var out []string
	short := ""
	for k, w := range ws {
		low := strings.ToLower(w)
		if o, ok := shortOutputs[strings.TrimSuffix(path.Base(strings.ReplaceAll(low, `\`, "/")), ".exe")]; ok && short == "" {
			short = o
		}
		if low == "format-patch" {
			short = "-o"
		}
		name, val, eq := strings.Cut(low, "=")
		if !eq && strings.HasPrefix(low, "-outfile:") {
			name, val, eq = "-outfile", low[len("-outfile:"):], true
		}
		// An if chain, not a switch: Go coverage does not instrument case
		// expressions, so gremlins never ran a mutant there.
		if outputFlags[name] && eq {
			out = append(out, w[len(w)-len(val):])
		} else if outputFlags[name] || short != "" && w == short {
			if k+1 < len(ws) {
				out = append(out, ws[k+1])
			}
		} else if short != "" && strings.HasPrefix(w, short) {
			out = append(out, w[len(short):])
		}
	}
	return out
}

// PrePush allows a push or PR only when the latest pre-PR report matches
// the working tree, is complete, has no open findings, and, if the profile
// requires it, carries a passing reviewer verdict.
func (r *Repo) PrePush() hookio.Decision {
	const fix = " Run reliab verify --tier prepr and resolve what reliab report route lists."
	rep, err := report.Read(report.LatestPath(r.Root, "prepr"))
	if err != nil {
		return hookio.Decision{Deny: "reliab: no pre-PR report." + fix}
	}
	if fresh, err := r.fresh(rep); err != nil {
		return hookio.Decision{Deny: "reliab: cannot check report freshness: " + err.Error()}
	} else if !fresh {
		return hookio.Decision{Deny: "reliab: the pre-PR report is stale; the code changed after it ran." + fix}
	}
	if waiting := rep.NeedsInput(); len(waiting) > 0 {
		return hookio.Decision{Deny: "reliab: the pre-PR report is incomplete: " + needsInputText(waiting) + "." + fix}
	}
	if !rep.Run.Complete {
		return hookio.Decision{Deny: "reliab: the pre-PR report is incomplete (budget ran out)." + fix}
	}
	if n := countOpen(rep); n > 0 {
		return hookio.Decision{Deny: fmt.Sprintf("reliab: the pre-PR report has %d open finding(s).", n) + fix}
	}
	if r.Profile.RequireReviewerVerdict {
		// if, not switch: Go coverage does not instrument case expressions.
		v := rep.Verdict
		if v == nil {
			return hookio.Decision{Deny: "reliab: the profile requires a passing reviewer verdict on the pre-PR report, and it has none. Have the " + ReviewerAgent + " agent review the change."}
		}
		if v.Outcome != report.VerdictPass {
			return hookio.Decision{Deny: "reliab: the reviewer's verdict is " + v.Outcome + ": " + v.Notes}
		}
		if v.TreeHash != rep.Run.TreeHash {
			return hookio.Decision{Deny: "reliab: the reviewer's verdict is for another tree. Have the " + ReviewerAgent + " agent review the change again."}
		}
	}
	return hookio.Decision{}
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
	return h == rep.Run.TreeHash, err
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

// Stop keeps the agent working when product code changed since the latest
// report (R8) or the agent still owes work on open findings. Findings that
// need a human do not block: stopping to ask is the right move. It honors
// stop_hook_active so the harness's own block cap is never the backstop.
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
		return hookio.Decision{Block: fmt.Sprintf("reliab: product code changed since the last report (%s). "+
			"Run reliab verify --tier inner and act on reliab report route before stopping (R8).", summarize(changed))}
	}
	if rep == nil {
		return hookio.Decision{}
	}
	// A command that needs tests named has not run: the change is not
	// verified until the agent names them (knowledge/decisions/named-tests.md).
	if waiting := rep.NeedsInput(); len(waiting) > 0 {
		return hookio.Decision{Block: fmt.Sprintf("reliab: the %s report is incomplete: %s. "+
			"Name the tests that guard the change and re-run reliab verify --tier %s before stopping.", rep.Run.Tier, needsInputText(waiting), rep.Run.Tier)}
	}
	var owed []string
	for _, f := range rep.Findings {
		if report.NeedsAgent(f) {
			owed = append(owed, f.ID+" ["+f.Kind+"]")
		}
	}
	if len(owed) > 0 {
		return hookio.Decision{Block: fmt.Sprintf("reliab: open findings still need you: %s. "+
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
		if h == rep.Run.TreeHash {
			return nil, nil
		}
		out, err := exec.Command("git", "-C", r.Root, "diff-tree", "-r", "--name-only", rep.Run.TreeHash, h).Output()
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

// SessionStart briefs the agent: the profile, the rules it works under, and
// the open findings in the latest report. It also anchors the session, so
// Stop can see changes the session commits before any report exists.
func (r *Repo) SessionStart(in hookio.Input) hookio.Decision {
	r.anchorSession(in.SessionID)
	var b strings.Builder
	p := r.Profile
	fmt.Fprintf(&b, "This repository uses reliab (profile %s, starting condition %s).\n", p.Name, p.StartingCondition)
	b.WriteString(`Rules:
- Run reliab verify --tier inner when a change is done (not after each file edit) and --tier prepr before a PR; act on reliab report route.
- A failing test is a finding: fix product code, never an assertion, tolerance, or catch block (R1).
- Never edit protected oracles: ` + strings.Join(p.ProtectedPaths, ", ") + protectedCommandsRule(p.ProtectedCommands) + protectedEditsRule(p.ProtectedEdits) + ` (R2). A property's saved failing case under one is committed with its fix; then a person runs reliab approve, never you.
- Every property cites its source of intent in a comment (R3).
- Reproduce, shrink, and classify every failure before reporting: reliab findings classify (R4).
- Behavior a snapshot pins is escalated to a human, never fixed (R9).
- Before a PR, after a clean prepr, have the ` + ReviewerAgent + ` agent review the change; never run reliab report verdict yourself.
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
				subject = f.Command
			}
			fmt.Fprintf(&b, "- open %s [%s] %s\n", f.ID, f.Kind, subject)
		}
	}
	return hookio.Decision{Context: b.String()}
}
