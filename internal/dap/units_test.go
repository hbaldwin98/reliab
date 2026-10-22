package dap

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/hbaldwin98/reliab/schemas"
)

// Placeholders expand in every string value, at any depth; an unexpanded
// placeholder is an error before the adapter starts
// (knowledge/decisions/debug-adapter-seam.md, "A debug section").
func TestExpand(t *testing.T) {
	vars := map[string]string{"port": "4711", "cwd": "C:/src/x", "root": "C:/src/x", "pid": "42", "program": "./internal/report", "test": "TestRouteKinds"}
	in := map[string]any{
		"mode":      "test",
		"program":   "{program}",
		"args":      []any{"-test.run", "^{test}$"},
		"processId": "{pid}",
		"port":      "{port}",
		"addr":      "127.0.0.1:{port}",
		"env":       map[string]any{"HOME": "{cwd}/home", "n": 3.0, "on": true, "deeper": []any{map[string]any{"at": "{root}"}}},
		"{program}": "keys are left alone",
		"json":      "{}",
	}
	got, err := Expand("launch", in, vars)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"mode":      "test",
		"program":   "./internal/report",
		"args":      []any{"-test.run", "^TestRouteKinds$"},
		"processId": 42,
		"port":      4711,
		"addr":      "127.0.0.1:4711",
		"env":       map[string]any{"HOME": "C:/src/x/home", "n": 3.0, "on": true, "deeper": []any{map[string]any{"at": "C:/src/x"}}},
		"{program}": "keys are left alone",
		"json":      "{}",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %#v\nwant %#v", got, want)
	}
	if in["program"] != "{program}" {
		t.Error("Expand changed its input")
	}

	_, err = Expand("launch", map[string]any{"a": []any{"{test}", map[string]any{"b": "{pid} {nope}"}}}, map[string]string{})
	if err == nil {
		t.Fatal("unexpanded placeholders passed")
	}
	for _, w := range []string{"{nope}", "{pid}", "{test}", "launch.a[0]", "launch.a[1].b", "--pid", "--set"} {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("error %q does not name %s", err, w)
		}
	}
	if _, err := Expand("launch", map[string]any{"p": "{port}"}, map[string]string{}); err == nil || !strings.Contains(err.Error(), "transport tcp") {
		t.Errorf("{port} without tcp: %v", err)
	}
}

func TestParseSets(t *testing.T) {
	got, err := ParseSets([]string{"program=./x", "test=TestA=B", "empty="})
	if err != nil || !reflect.DeepEqual(got, map[string]string{"program": "./x", "test": "TestA=B", "empty": ""}) {
		t.Errorf("got %v, %v", got, err)
	}
	for _, bad := range []string{"noequals", "=x", "a-b=1", "pid=3", "port=1", "cwd=x", "root=x"} {
		if _, err := ParseSets([]string{bad}); err == nil {
			t.Errorf("--set %s accepted", bad)
		}
	}
}

func TestSplitCommand(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"dlv dap --listen=127.0.0.1:{port}", []string{"dlv", "dap", "--listen=127.0.0.1:{port}"}},
		{`  "C:\Program Files\x\a.exe"   --flag 'two words' ""  `, []string{`C:\Program Files\x\a.exe`, "--flag", "two words", ""}},
		{`C:\tools\netcoredbg.exe --interpreter=vscode`, []string{`C:\tools\netcoredbg.exe`, "--interpreter=vscode"}},
		{"a\tb\nc", []string{"a", "b", "c"}},
		{`x"y z"w`, []string{"xy zw"}},
	}
	for _, tt := range tests {
		got, err := SplitCommand(tt.in)
		if err != nil || !reflect.DeepEqual(got, tt.want) {
			t.Errorf("SplitCommand(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
		}
	}
	if _, err := SplitCommand(`a "b`); err == nil {
		t.Error("an unclosed quote passed")
	}
}

// Paths from the adapter are made repo-relative with source_root and
// slash-separated, so a transcript reads the same on Windows and CI.
func TestPathMapper(t *testing.T) {
	m := NewPathMapper("C:/src/repo", ".")
	tests := []struct{ in, want string }{
		{`C:\src\repo\internal\report\route.go`, "internal/report/route.go"},
		{`c:\SRC\Repo\internal\x.go`, "internal/x.go"},
		{"C:/src/repo/internal/x.go", "internal/x.go"},
		{`C:\Program Files\Go\src\testing\testing.go`, "C:/Program Files/Go/src/testing/testing.go"},
		{`C:\src\repository\x.go`, "C:/src/repository/x.go"},
		{"internal/x.go", "internal/x.go"},
		{`internal\x.go`, "internal/x.go"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := m.Rel(tt.in); got != tt.want {
			t.Errorf("Rel(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}

	// A source_root inside the repository: the adapter's relative paths
	// start there.
	sub := NewPathMapper("C:/src/repo", "service")
	if got := sub.Rel("api/h.go"); got != "service/api/h.go" {
		t.Errorf("relative to source_root = %q", got)
	}
	// A source_root outside it (an adapter in a container): paths under it
	// become relative to it.
	box := NewPathMapper("/home/me/repo", "/app")
	if got := box.Rel("/app/internal/x.go"); got != "internal/x.go" {
		t.Errorf("container path = %q", got)
	}
	if got := box.Rel("/home/me/repo/internal/x.go"); got != "internal/x.go" {
		t.Errorf("repo path = %q", got)
	}
	if got := box.Rel("/usr/lib/go/x.go"); got != "/usr/lib/go/x.go" {
		t.Errorf("outside both = %q", got)
	}
}

// A temporary directory can come back with symlinks or 8.3 short names
// (docs/windows.md); the resolved spelling maps too.
func TestPathMapperResolvesTheRoot(t *testing.T) {
	dir := t.TempDir()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := NewPathMapper(dir, "")
	for _, p := range []string{filepath.Join(dir, "a", "b.go"), filepath.Join(resolved, "a", "b.go")} {
		if got := m.Rel(p); got != "a/b.go" {
			t.Errorf("Rel(%q) = %q", p, got)
		}
	}
	if got := m.Abs("a/b.go"); got != filepath.Join(dir, "a", "b.go") {
		t.Errorf("Abs = %q", got)
	}
	abs := filepath.Join(dir, "c.go")
	if got := m.Abs(abs); got != abs {
		t.Errorf("Abs(abs) = %q", got)
	}
}

func TestParseBreak(t *testing.T) {
	m := NewPathMapper(filepath.Join(os.TempDir(), "r"), "")
	b, err := ParseBreak(`internal/report/route.go:120 if f.Kind == "below-threshold"`, m)
	if err != nil {
		t.Fatal(err)
	}
	if b.File != filepath.Join(m.Root, "internal", "report", "route.go") || b.Line != 120 || b.Condition != `f.Kind == "below-threshold"` {
		t.Errorf("break = %+v", b)
	}
	if runtime.GOOS == "windows" {
		b, err := ParseBreak(`C:\src\x.go:7`, m)
		if err != nil || b.File != `C:\src\x.go` || b.Line != 7 {
			t.Errorf("drive path: %+v %v", b, err)
		}
	}
	for _, bad := range []string{"x.go", "x.go:", "x.go:abc", "x.go:0", ":3"} {
		if _, err := ParseBreak(bad, m); err == nil {
			t.Errorf("--break %q accepted", bad)
		}
	}
}

func sampleTranscript() *Transcript {
	zero := 0
	return &Transcript{
		SchemaVersion: TranscriptVersion, Mode: ModeRun, StartedAt: "2026-10-21T15:30:00Z", DurationSec: 2.5,
		Adapter: AdapterInfo{Run: "dlv dap --listen=127.0.0.1:4711", Transport: TransportTCP, Port: 4711},
		Request: RequestInfo{Command: "launch", Arguments: map[string]any{"mode": "test"}},
		Breakpoints: []BreakpointRecord{
			{Spec: "pkg/x.go:12", Path: "pkg/x.go", Line: 12, Verified: true},
			{Spec: "pkg/x.go:99", Path: "pkg/x.go", Line: 99, Verified: false, Message: "no code"},
		},
		ExceptionFilters: []string{"panic"},
		Warnings:         []string{"breakpoint pkg/x.go:99 is not verified: no code"},
		Entries: []Entry{
			{Kind: KindOutput, Category: "stdout", Text: "=== RUN   TestX\nline two\n"},
			{Kind: KindStop, Seq: 1, Reason: "breakpoint", Thread: 1,
				Frames: []Frame{{Path: "pkg/x.go", Line: 12, Function: "pkg.Sum"}, {Path: "pkg/x_test.go", Line: 30, Function: "pkg.TestX"}},
				Locals: []Variable{{Name: "n", Value: "3", Scope: "Locals"}, {Name: "s", Value: strings.Repeat("y", 30), Children: []Variable{{Name: "[0]", Value: "a\nb"}}}},
				Dumps:  []Dump{{Expression: "len(items)", Value: "3"}, {Expression: "nope", Error: "could not find symbol"}}},
			{Kind: KindOutput, Category: "stderr", Text: "warn\n"},
		},
		ReverseRequests: []ReverseRequest{{Command: "runInTerminal", Seq: 9}},
		Ignored:         map[string]int{"event module": 2},
		End:             End{Reason: EndExited, ExitCode: &zero},
		Truncated:       Truncated{Variables: 1},
		ExitCode:        0,
	}
}

// The text rendering: one block per stop, top frames as path:line func,
// locals and dumps as name = value lines, output marked by category, and a
// one-line tail with the exit code and the transcript's path. Long values
// are cut at a width with a marker, never silently.
func TestRender(t *testing.T) {
	var b bytes.Buffer
	Render(&b, sampleTranscript(), 20, ".reliab/debug/20261021-153000.json")
	want := `adapter: dlv dap --listen=127.0.0.1:4711 (tcp, port 4711)
breakpoint pkg/x.go:12: verified
breakpoint pkg/x.go:99: NOT verified
exception filters: panic
warning: breakpoint pkg/x.go:99 is not verified: no code
[stdout] === RUN   TestX
[stdout] line two
== stop 1: breakpoint, thread 1
  at pkg/x.go:12 pkg.Sum
     pkg/x_test.go:30 pkg.TestX
  locals:
    n = 3
    s = yyyyyyyyyyyyyyyyyyyy... [cut, 10 more chars]
      [0] = a\nb
  dumps:
    len(items) = 3
    nope = <error: could not fi... [cut, 10 more chars]
[stderr] warn
the adapter asked for runInTerminal (seq 9); reliab declined it
ignored from the adapter: event module x2
truncated: 1 variable(s) not expanded (past --depth or the per-scope cap)
end: exited, program exit code 0; 1 stop(s), 2.5s; exit 0; transcript .reliab/debug/20261021-153000.json
`
	if got := b.String(); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderTailAndTruncation(t *testing.T) {
	tr := sampleTranscript()
	tr.End = End{Reason: EndMaxStops, Message: "stopped 20 times (--max-stops)"}
	tr.ExitCode = 1
	tr.Truncated = Truncated{Stops: true, Values: 2, OutputBytes: 7}
	var b bytes.Buffer
	RenderRun(&b, tr, 0, "")
	out := b.String()
	for _, w := range []string{
		"truncated: stops past --max-stops; 2 value(s) cut at 4096 chars; 7 byte(s) of program output past 1048576\n",
		"end: max-stops: stopped 20 times (--max-stops); 1 stop(s), 2.5s; exit 1\n",
		"    s = " + strings.Repeat("y", 30) + "\n",
	} {
		if !strings.Contains(out, w) {
			t.Errorf("rendering lacks %q:\n%s", w, out)
		}
	}
	if strings.Contains(out, "transcript ") {
		t.Errorf("tail names a transcript it was not given:\n%s", out)
	}
}

func TestComputeExitCode(t *testing.T) {
	tests := []struct {
		reason    string
		exception bool
		want      int
	}{
		{EndExited, false, 0}, {EndTerminated, false, 0},
		{EndExited, true, 1}, {EndMaxStops, false, 1}, {EndTimeout, false, 1},
		{EndAdapterError, false, 1}, {EndAdapterGone, false, 1},
		{EndBudget, false, 3}, {EndBudget, true, 1},
	}
	for _, tt := range tests {
		tr := &Transcript{End: End{Reason: tt.reason}, Entries: []Entry{{Kind: KindStop, Reason: "breakpoint"}}}
		if tt.exception {
			tr.Entries = append(tr.Entries, Entry{Kind: KindStop, Reason: "exception"})
		}
		if got := ComputeExitCode(tr); got != tt.want {
			t.Errorf("%s exception=%t: exit %d, want %d", tt.reason, tt.exception, got, tt.want)
		}
	}
}

// The transcript is written under .reliab/debug/ with last.json as a copy,
// and validates against schemas/debug-transcript.schema.json.
func TestWriteValidates(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 21, 15, 30, 0, 0, time.Local)
	tr := sampleTranscript()
	p1, err := Write(dir, tr, now)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := Write(dir, tr, now)
	if err != nil {
		t.Fatal(err)
	}
	if p1 != ".reliab/debug/20261021-153000.json" || p2 != ".reliab/debug/20261021-153000-2.json" {
		t.Errorf("paths = %s, %s", p1, p2)
	}
	a, _ := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p2)))
	b, err := os.ReadFile(filepath.Join(dir, ".reliab", "debug", "last.json"))
	if err != nil || !bytes.Equal(a, b) || len(a) == 0 {
		t.Fatalf("last.json differs from %s: %v", p2, err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(a))
	if err != nil {
		t.Fatal(err)
	}
	if err := schemas.Validate(schemas.DebugTranscript, doc); err != nil {
		t.Fatalf("transcript does not validate: %v", err)
	}
	var back Transcript
	if err := json.Unmarshal(a, &back); err != nil || back.Entries[1].Dumps[1].Error != "could not find symbol" {
		t.Errorf("round trip: %v %+v", err, back.Entries)
	}
}

func TestTranscriptSchemaRejects(t *testing.T) {
	raw, _ := json.Marshal(sampleTranscript())
	tests := []struct {
		name   string
		mutate func(m map[string]any)
	}{
		{"unknown key", func(m map[string]any) { m["extra"] = 1 }},
		{"major 0", func(m map[string]any) { m["schemaVersion"] = "0.1.0" }},
		{"bad mode", func(m map[string]any) { m["mode"] = "step" }},
		{"bad end", func(m map[string]any) { m["end"].(map[string]any)["reason"] = "done" }},
		{"bad entry kind", func(m map[string]any) { m["entries"].([]any)[0].(map[string]any)["kind"] = "log" }},
		{"stop without seq", func(m map[string]any) { delete(m["entries"].([]any)[1].(map[string]any), "seq") }},
		{"exit code 2", func(m map[string]any) { m["exitCode"] = 2 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			tt.mutate(doc.(map[string]any))
			if err := schemas.Validate(schemas.DebugTranscript, doc); err == nil {
				t.Error("validated")
			}
		})
	}
}
