// Package doctor checks that reliab can run in a repository: git, a POSIX
// shell, a valid reliab.yaml, and the tools its commands start.
package doctor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hbaldwin98/reliab/internal/dap"
	"github.com/hbaldwin98/reliab/internal/gitx"
	"github.com/hbaldwin98/reliab/internal/runner"
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

	if sh, err := runner.FindShell(); err != nil {
		add("shell", Fail, err.Error())
	} else {
		add("shell", OK, sh)
	}

	cfg, err := runner.Load(filepath.Join(root, runner.DefaultPath))
	if err != nil {
		add(runner.DefaultPath, Fail, err.Error())
		return out
	}
	add(runner.DefaultPath, OK, fmt.Sprintf("%d commands", len(cfg.Commands)))

	ignored := exec.Command("git", "-C", root, "check-ignore", "-q", ".reliab/run/x.log").Run() == nil
	if ignored {
		add(".gitignore", OK, ".reliab/ is ignored")
	} else {
		add(".gitignore", Warn, ".reliab/ is not ignored; run logs and tool output would show as changes")
	}

	out = append(out, versionPin(root)...)
	out = append(out, tools(cfg)...)
	if dc, err := dap.LoadConfig(filepath.Join(root, dap.ConfigPath)); err == nil {
		out = append(out, debugAdapter(dc)...)
	} else if !os.IsNotExist(err) {
		add("debug adapter", Fail, err.Error())
	}
	return out
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

// tools checks that the program each command line runs is on PATH. A
// missing tool warns: the command fails when it runs, saying so.
func tools(cfg *runner.Config) []Check {
	var out []Check
	for _, name := range cfg.Names() {
		c := cfg.Commands[name]
		seen := map[string]bool{}
		for _, line := range []string{c.Change, c.PR, c.Audit, c.Targeted} {
			bin, ok := commandWord(line)
			if !ok || seen[bin] {
				continue
			}
			seen[bin] = true
			switch _, err := exec.LookPath(bin); {
			case shellBuiltins[bin]:
				out = append(out, Check{"command " + name, OK, bin + " (shell builtin)"})
			case err != nil:
				out = append(out, Check{"command " + name, Warn, bin + " is not on PATH"})
			default:
				out = append(out, Check{"command " + name, OK, bin})
			}
		}
	}
	return out
}

// debugAdapter checks that the debug adapter's program is on PATH
// (knowledge/decisions/debug-adapter-seam.md). A missing adapter warns.
func debugAdapter(c *dap.Config) []Check {
	a := c.Adapter
	words, err := dap.SplitCommand(a.Run)
	if err != nil {
		return []Check{{"debug adapter", Fail, "adapter.run: " + err.Error()}}
	}
	if len(words) == 0 {
		return []Check{{"debug adapter", Fail, "adapter.run is empty"}}
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
