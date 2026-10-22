// Package doctor checks that reliab can run in a repository and that the
// repository is set up the way reliab profile init leaves it.
package doctor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/hbaldwin98/reliab/internal/dap"
	"github.com/hbaldwin98/reliab/internal/gitx"
	"github.com/hbaldwin98/reliab/internal/profile"
	"github.com/hbaldwin98/reliab/internal/verify"
	"github.com/hbaldwin98/reliab/internal/version"
)

// Status of one check.
const (
	OK   = "ok"
	Warn = "warn"
	Fail = "FAIL"
)

// Check is one diagnostic.
type Check struct {
	Name, Status, Detail string
}

// Run performs every check from dir.
func Run(dir string) []Check {
	var out []Check
	add := func(name, status, detail string) { out = append(out, Check{name, status, detail}) }

	if _, err := exec.LookPath("git"); err != nil {
		add("git", Fail, "git is not on PATH")
		return out
	}
	root, err := gitx.Root(dir)
	if err != nil {
		add("repository", Fail, "not inside a git repository")
		return out
	}
	add("repository", OK, root)

	if sh, err := verify.FindShell(); err != nil {
		add("shell", Fail, err.Error())
	} else {
		add("shell", OK, sh)
	}

	p, err := profile.Load(filepath.Join(root, profile.DefaultPath))
	if err != nil {
		add("profile", Fail, err.Error())
		return out
	}
	add("profile", OK, fmt.Sprintf("%s (%s)", p.Name, p.StartingCondition))
	// The profile holds the thresholds, relevance rules, and baseline a
	// change must pass, so it is an oracle too
	// (knowledge/decisions/protected-path-approval.md).
	if !profile.Protects(p, profile.DefaultPath) {
		add("profile protection", Warn, profile.DefaultPath+" is not in protected_paths, so an agent can edit its thresholds, relevance rules, and baseline; run reliab profile init to add it")
	}

	ignored := exec.Command("git", "-C", root, "check-ignore", "-q", ".reliab/reports/x.json").Run() == nil
	if ignored {
		add(".gitignore", OK, ".reliab/ is ignored")
	} else {
		add(".gitignore", Fail, ".reliab/ is not ignored; reports would be committed and make every report stale")
	}

	out = append(out, versionPin(root)...)
	out = append(out, tools(p)...)
	out = append(out, debugAdapter(p)...)
	out = append(out, missingRoles(p)...)
	return out
}

// missingRoles warns about each role the codebase needs but has no tool for
// yet: its commands are skipped as role-missing until a person adds one.
func missingRoles(p *profile.Profile) []Check {
	ids := make([]string, 0, len(p.Roles))
	for id, r := range p.Roles {
		if r.Missing {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return roleNumber(ids[i]) < roleNumber(ids[j]) })
	var out []Check
	for _, id := range ids {
		out = append(out, Check{"role " + id, Warn, "needed but missing: " + p.Roles[id].Reason})
	}
	return out
}

// roleNumber orders T2 before T10.
func roleNumber(id string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(id, "T"))
	return n
}

func versionPin(root string) []Check {
	raw, err := os.ReadFile(filepath.Join(root, ".reliab-version"))
	if err != nil {
		return []Check{{"version pin", Warn, ".reliab-version is missing; CI cannot install a pinned reliab"}}
	}
	pin := strings.TrimSpace(string(raw))
	switch {
	case version.IsDev():
		return []Check{{"version pin", OK, pin + " (this binary is a dev build)"}}
	case pin != version.Version:
		return []Check{{"version pin", Warn, fmt.Sprintf(".reliab-version is %s but this binary is %s", pin, version.Version)}}
	}
	return []Check{{"version pin", OK, pin}}
}

// tools checks that the first word of every profile command is on PATH. A
// missing tool is what stage S0 is for, so it warns rather than fails.
func tools(p *profile.Profile) []Check {
	ids := make([]string, 0, len(p.Commands))
	for id := range p.Commands {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var out []Check
	for _, id := range ids {
		bin, ok := commandWord(p.Commands[id].Run)
		if !ok {
			continue
		}
		if shellBuiltins[bin] {
			out = append(out, Check{"command " + id, OK, bin + " (shell builtin)"})
			continue
		}
		if _, err := exec.LookPath(bin); err != nil {
			out = append(out, Check{"command " + id, Warn, bin + " is not on PATH"})
		} else {
			out = append(out, Check{"command " + id, OK, bin})
		}
	}
	return out
}

// debugAdapter checks that the debug adapter's first word is on PATH when
// the profile has a debug section (knowledge/decisions/debug-adapter-seam.md).
// Like a missing tool, a missing adapter warns.
func debugAdapter(p *profile.Profile) []Check {
	if p.Debug == nil {
		return nil
	}
	a := p.Debug.Adapter
	// if statements, not case expressions, so gremlins sees them covered.
	words, err := dap.SplitCommand(a.Run)
	if err != nil {
		return []Check{{"debug adapter", Fail, "debug.adapter.run: " + err.Error()}}
	}
	if len(words) == 0 {
		return []Check{{"debug adapter", Fail, "debug.adapter.run is empty"}}
	}
	if strings.Contains(words[0], "{") {
		return []Check{{"debug adapter", OK, words[0] + " (" + a.Transport + "; a placeholder, checked when reliab debug runs)"}}
	}
	bin := words[0]
	if _, err := exec.LookPath(bin); err != nil {
		return []Check{{"debug adapter", Warn, bin + " is not on PATH; reliab debug run, attach, and probe need it"}}
	}
	return []Check{{"debug adapter", OK, bin + " (" + a.Transport + "); reliab debug probe checks it answers"}}
}

// shellBuiltins are words a POSIX sh runs itself, so they are never on PATH.
var shellBuiltins = map[string]bool{
	"cd": true, "export": true, "set": true, "unset": true, ".": true, "source": true,
	"exec": true, "eval": true, "test": true, "[": true, ":": true, "true": true, "false": true,
	"echo": true, "printf": true, "if": true, "for": true, "while": true, "until": true,
	"case": true, "{": true, "(": true, "!": true, "trap": true, "umask": true, "ulimit": true,
}

// commandWord returns the program a command line runs: its first word after
// any leading NAME=value assignments. ok is false when that cannot be told
// without running a shell (an expansion, or nothing to run).
func commandWord(run string) (string, bool) {
	for _, f := range strings.Fields(run) {
		if name, _, isAssign := strings.Cut(f, "="); isAssign && name != "" && !strings.ContainsAny(name, "$`/") {
			continue
		}
		if strings.ContainsAny(f, "$`") {
			return "", false
		}
		return f, true
	}
	return "", false
}

// Failed reports whether any check failed.
func Failed(cs []Check) bool {
	for _, c := range cs {
		if c.Status == Fail {
			return true
		}
	}
	return false
}
