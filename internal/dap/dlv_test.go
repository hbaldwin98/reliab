package dap

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The integration test of knowledge/decisions/debug-adapter-seam.md: a
// small Go test in a temporary module, debugged with the real dlv dap.
// Skipped without dlv on PATH; reliab's CI installs delve so it runs there.

const tinySum = `package tiny

func Sum(xs []int) int {
	total := 0
	for _, x := range xs {
		total += x
	}
	return total // line 8
}

func Boom(m map[string]int) int {
	return m["a"] / m["b"]
}
`

const tinyTest = `package tiny

import "testing"

func TestSum(t *testing.T) {
	if got := Sum([]int{1, 2, 3}); got != 6 {
		t.Fatalf("Sum = %d", got)
	}
}

func TestBoom(t *testing.T) {
	Boom(map[string]int{"a": 1})
}
`

func tinyModule(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{"go.mod": "module example.com/tiny\n\ngo 1.21\n", "sum.go": tinySum, "sum_test.go": tinyTest} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func dlvSession(t *testing.T, dir, test string, opt Options) *Transcript {
	t.Helper()
	if _, err := exec.LookPath("dlv"); err != nil {
		t.Skip("dlv is not on PATH")
	}
	port, err := FreePort()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	c, err := Start(ctx, AdapterConfig{
		Argv:      []string{"dlv", "dap", "--listen=127.0.0.1:" + strconv.Itoa(port)},
		Transport: TransportTCP, Port: port, Ready: regexp.MustCompile("DAP server listening"), Dir: dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	opt.Mode = ModeRun
	opt.Arguments = map[string]any{
		"mode": "test", "program": ".", "args": []any{"-test.run", "^" + test + "$"},
		"outputMode": "remote", "output": filepath.Join(dir, "__debug_bin"),
	}
	opt.Paths = NewPathMapper(dir, ".")
	opt.Timeout = time.Minute
	tr := &Transcript{SchemaVersion: TranscriptVersion}
	if err := Session(ctx, c, tr, opt); err != nil {
		t.Fatalf("%v\nadapter output:\n%s", err, c.AdapterOutput())
	}
	return tr
}

func TestDelveRun(t *testing.T) {
	dir := tinyModule(t)
	m := NewPathMapper(dir, ".")
	b, err := ParseBreak("sum.go:8", m)
	if err != nil {
		t.Fatal(err)
	}
	tr := dlvSession(t, dir, "TestSum", Options{Breaks: []Break{b}, Dumps: []string{"total", "len(xs)", "nosuch"}})
	if len(tr.Breakpoints) != 1 || !tr.Breakpoints[0].Verified || tr.Breakpoints[0].Path != "sum.go" {
		t.Fatalf("breakpoints = %+v, warnings %q", tr.Breakpoints, tr.Warnings)
	}
	stops := tr.Stops()
	if len(stops) != 1 {
		t.Fatalf("stops = %+v", tr.Entries)
	}
	s := stops[0]
	if s.Reason != "breakpoint" || len(s.Frames) < 2 {
		t.Fatalf("stop = %+v", s)
	}
	// Adapter paths come out repo-relative with slashes; on Windows dlv
	// reports absolute paths that may use backslashes and short names.
	if s.Frames[0] != (Frame{Path: "sum.go", Line: 8, Function: "tiny.Sum"}) {
		t.Errorf("top frame = %+v", s.Frames[0])
	}
	if s.Frames[1].Path != "sum_test.go" || s.Frames[1].Function != "tiny.TestSum" {
		t.Errorf("caller frame = %+v", s.Frames[1])
	}
	for _, f := range s.Frames {
		if strings.Contains(f.Path, `\`) {
			t.Errorf("a frame path has a backslash: %q", f.Path)
		}
	}
	locals := map[string]string{}
	for _, v := range s.Locals {
		locals[v.Name] = v.Value
	}
	if locals["total"] != "6" || !strings.Contains(locals["xs"], "len: 3") {
		t.Errorf("locals = %+v", s.Locals)
	}
	if len(s.Dumps) != 3 || s.Dumps[0].Value != "6" || s.Dumps[1].Value != "3" || s.Dumps[2].Error == "" {
		t.Errorf("dumps = %+v", s.Dumps)
	}
	var out strings.Builder
	for _, e := range tr.Entries {
		if e.Kind == KindOutput {
			out.WriteString(e.Text)
		}
	}
	if !strings.Contains(out.String(), "PASS") {
		t.Errorf("program output = %q", out.String())
	}
	if tr.End.Reason != EndExited || tr.End.ExitCode == nil || *tr.End.ExitCode != 0 || tr.ExitCode != 0 {
		t.Errorf("end = %+v, exit %d", tr.End, tr.ExitCode)
	}
}

// --catch with delve's default filters stops at an unrecovered panic, and
// the run exits 1.
func TestDelveCatchesAPanic(t *testing.T) {
	tr := dlvSession(t, tinyModule(t), "TestBoom", Options{Catch: true, Dumps: []string{`m["a"]`}})
	if len(tr.ExceptionFilters) == 0 {
		t.Fatalf("no exception filters set; warnings %q", tr.Warnings)
	}
	stops := tr.Stops()
	if len(stops) == 0 || stops[0].Reason != "exception" {
		t.Fatalf("stops = %+v", tr.Entries)
	}
	if !strings.Contains(stops[0].Description, "divide by zero") && !strings.Contains(stops[0].Description, "panic") {
		t.Errorf("description = %q", stops[0].Description)
	}
	if tr.ExitCode != 1 {
		t.Errorf("exit %d, end %+v", tr.ExitCode, tr.End)
	}
}
