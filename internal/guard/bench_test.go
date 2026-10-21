package guard

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hbaldwin98/reliab/internal/hookio"
	"github.com/hbaldwin98/reliab/internal/testrepo"
)

// PreBash runs on every Bash and PowerShell call, so its cost is paid on
// each command an agent runs (knowledge/decisions/go-cli.md: fast hook
// startup). These are the lines an agent types most.
func BenchmarkPreBash(b *testing.B) {
	r := benchRepo(b)
	msg := strings.Repeat("Guard: read the line once; a person runs reliab approve. ", 20)
	lines := map[string]string{
		"go test":    "go test ./...",
		"commit":     `git add -A && git commit -m "` + msg + `"`,
		"pipeline":   "go test ./internal/... 2>&1 | grep -v '^ok' | head -50",
		"cd chain":   "cd src && go vet ./... && cd .. && git status --short",
		"write":      "echo x > src/a/out.txt",
		"powershell": `Get-ChildItem -Recurse src | Select-String -Pattern "TODO"`,
	}
	for name, line := range lines {
		tool := "Bash"
		if name == "powershell" {
			tool = "PowerShell"
		}
		in := hookio.Input{ToolName: tool, Cwd: r.Root, ToolInput: hookio.ToolInput{Command: line}}
		b.Run(name, func(b *testing.B) {
			for b.Loop() {
				if d := r.PreBash(in); d.Deny != "" {
					b.Fatalf("%q denied: %s", line, d.Deny)
				}
			}
		})
	}
}

// benchRepo makes a committed repository as testrepo.New does, for a
// benchmark (testrepo takes a *testing.T).
func benchRepo(b *testing.B) *Repo {
	b.Helper()
	dir := b.TempDir()
	files := map[string]string{
		"reliability-profile.yaml": "schemaVersion: 0.1.0\nname: test\nstarting_condition: greenfield\n" +
			"paths: { product: [\"src/**\"], tests: [\"test/**\"], ignore: [\"docs/**\"] }\n" + testrepo.Roles + passing,
		".gitignore":      ".reliab/\n",
		"src/a/a.txt":     "a",
		"snapshots/s.txt": "s",
	}
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"add", "-A"}, {"-c", "user.name=t", "-c", "user.email=t@example.invalid", "commit", "-qm", "init"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			b.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	r, ok, err := Open(dir)
	if err != nil || !ok {
		b.Fatalf("Open: ok=%v err=%v", ok, err)
	}
	return r
}
