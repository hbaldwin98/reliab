package docs

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hbaldwin98/reliab/internal/profile"
)

// python returns a working Python 3, or skips: the example is checked
// wherever one is installed.
func python(t *testing.T) string {
	t.Helper()
	for _, name := range []string{"python3", "python"} {
		if out, err := exec.Command(name, "--version").Output(); err == nil && strings.HasPrefix(string(out), "Python 3") {
			return name
		}
	}
	t.Skip("no Python 3 to run the example adapter")
	return ""
}

// The example adapter in adapters.md follows the contract on the same page:
// a file for a clean and a failing run, ids that survive a line shift,
// repeats numbered, and no file when the tool did not run.
func TestExampleAdapter(t *testing.T) {
	py := python(t)
	start := strings.Index(Adapters, "```python\n")
	end := strings.Index(Adapters[start+1:], "\n```\n")
	if start < 0 || end < 0 {
		t.Fatal("adapters.md has no python example")
	}
	dir := t.TempDir()
	adapter := filepath.Join(dir, "reliab-adapter-specrun")
	write(t, adapter, Adapters[start+len("```python\n"):start+1+end+1])
	write(t, filepath.Join(dir, "clean.json"), `{"results":[{"suite":"s","name":"a","status":"passed"}]}`)
	failing := `{"results":[{"suite":"s","name":"a","status":"failed","file":"src/a.py","line":42,"message":"m"},` +
		`{"suite":"s","name":"a","status":"failed","file":"src/a.py","line":50}]}`
	write(t, filepath.Join(dir, "failing.json"), failing)
	write(t, filepath.Join(dir, "moved.json"), strings.Replace(failing, `"line":42`, `"line":45`, 1))

	convert := func(code, report string) ([]map[string]string, bool) {
		out := filepath.Join(dir, report+".out")
		_ = os.Remove(out)
		err := exec.Command(py, "-I", adapter, code, filepath.Join(dir, report), out).Run()
		raw, readErr := os.ReadFile(out)
		if err != nil || readErr != nil {
			return nil, false
		}
		var doc struct {
			SchemaVersion string              `json:"schemaVersion"`
			Findings      []map[string]string `json:"findings"`
		}
		if json.Unmarshal(raw, &doc) != nil || doc.SchemaVersion == "" || doc.Findings == nil {
			t.Fatalf("%s: not a findings file: %s", report, raw)
		}
		return doc.Findings, true
	}
	if fs, ok := convert("0", "clean.json"); !ok || len(fs) != 0 {
		t.Errorf("clean: %v %v", fs, ok)
	}
	fs, ok := convert("0", "failing.json")
	if !ok || len(fs) != 2 || fs[0]["test"] != "s > a" || fs[1]["test"] != "s > a (#2)" || fs[0]["location"] != "src/a.py:42" {
		t.Errorf("failing: %v %v", fs, ok)
	}
	if moved, ok := convert("0", "moved.json"); !ok || len(moved) != 2 || moved[0]["test"] != fs[0]["test"] || moved[0]["location"] != "src/a.py:45" {
		t.Errorf("moved: %v %v", moved, ok)
	}
	if fs, ok := convert("2", "failing.json"); ok {
		t.Errorf("tool did not run: wrote %v", fs)
	}
	if fs, ok := convert("0", "missing.json"); ok {
		t.Errorf("no report: wrote %v", fs)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Every adapter block on the debug page is a profile section reliab
// accepts, so a person can paste it (knowledge/decisions/debug-adapter-seam.md:
// the consumer check runs "from the docs page's block alone").
func TestDebugBlocksValidate(t *testing.T) {
	const minimal = "schemaVersion: 0.1.0\nname: docs\nstarting_condition: greenfield\n" +
		"paths: { product: [\"src/**\"], tests: [\"test/**\"] }\nroles:\n" +
		"  T1: { tool: x }\n  T2: { tool: x }\n  T3: { tool: x }\n  T4: { tool: x }\n  T5: { tool: x }\n" +
		"  T6: { tool: x }\n  T7: { tool: x }\n  T8: { tool: x }\n  T9: { tool: x }\n  T10: { tool: x }\n" +
		"  T11: { tool: x }\n  T12: { tool: x }\n  T13: { tool: x }\n  T14: { tool: x }\n" +
		"commands:\n  build: \"true\"\n"
	n := 0
	for _, block := range strings.Split(Debug, "```yaml\n")[1:] {
		body, _, ok := strings.Cut(block, "\n```")
		if !ok || !strings.HasPrefix(body, "debug:") {
			continue
		}
		n++
		p, err := profile.Parse([]byte(minimal + body + "\n"))
		if err != nil {
			t.Errorf("block %d does not validate: %v\n%s", n, err, body)
			continue
		}
		if p.Debug == nil || p.Debug.Launch == nil || p.Debug.Attach == nil {
			t.Errorf("block %d lacks launch or attach:\n%s", n, body)
		}
	}
	// The general block and delve, debugpy, netcoredbg, lldb-dap.
	if n != 5 {
		t.Errorf("found %d debug blocks, want 5", n)
	}
}
