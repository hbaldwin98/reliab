package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	godap "github.com/google/go-dap"
	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/hbaldwin98/reliab/internal/dap"
	"github.com/hbaldwin98/reliab/internal/dap/daptest"
	"github.com/hbaldwin98/reliab/internal/testrepo"
	"github.com/hbaldwin98/reliab/schemas"
)

// The debug commands' flags, exit codes, and rendering are
// knowledge/decisions/debug-adapter-seam.md. The adapter is this test
// binary playing a daptest script; DAPTEST_ROOT tells it the repository's
// path, which it reports in native form (backslashes on Windows).

func TestMain(m *testing.M) {
	daptest.Main(debugScripts(os.Getenv("DAPTEST_ROOT")))
	os.Exit(m.Run())
}

func debugScripts(root string) map[string]*daptest.Script {
	caps := godap.Capabilities{
		SupportsConfigurationDoneRequest: true, SupportsConditionalBreakpoints: true, SupportsLogPoints: true,
		ExceptionBreakpointFilters: []godap.ExceptionBreakpointsFilter{{Filter: "panic", Label: "Panics", Default: true}},
	}
	at := func(line int, fn string) godap.StackFrame {
		return godap.StackFrame{Id: 1, Name: fn, Line: line, Source: &godap.Source{Path: filepath.Join(root, "src", "a", "a.go")}}
	}
	vars := map[int][]godap.Variable{1000: {{Name: "n", Value: "3", Type: "int"}}}
	eval := map[string]godap.EvaluateResponseBody{"n * 2": {Result: "6"}}
	return map[string]*daptest.Script{
		"ok": {
			Capabilities: caps, Variables: vars, Eval: eval,
			Unverified: map[int]string{99: "no code at line 99"},
			Steps: []daptest.Step{
				{Output: "=== RUN   TestA\n"},
				{Stop: &daptest.Stop{Reason: "breakpoint", Thread: 1, Frames: []godap.StackFrame{at(3, "a.Sum")}}},
				{Output: "--- PASS: TestA\n"},
			},
		},
		"panic": {
			Capabilities: caps, Variables: vars,
			Steps: []daptest.Step{{Stop: &daptest.Stop{Reason: "exception", Description: "panic: boom", Thread: 1, Frames: []godap.StackFrame{at(7, "a.Boom")}}}},
		},
		"loop": {
			Capabilities: caps, Variables: vars,
			Steps: []daptest.Step{
				{Stop: &daptest.Stop{Reason: "breakpoint", Thread: 1, Frames: []godap.StackFrame{at(3, "a.Sum")}}},
				{Stop: &daptest.Stop{Reason: "breakpoint", Thread: 1, Frames: []godap.StackFrame{at(3, "a.Sum")}}},
				{Stop: &daptest.Stop{Reason: "breakpoint", Thread: 1, Frames: []godap.StackFrame{at(3, "a.Sum")}}},
			},
		},
		"hang": {Capabilities: caps, Hang: true},
	}
}

// debugRepo is a repository whose profile's adapter plays script.
func debugRepo(t *testing.T, script string) string {
	t.Helper()
	return debugRepoWith(t, script+` "DAP server listening"`, `{ mode: test, program: "{program}", args: ["-test.run", "^{test}$"] }`)
}

// debugRepoWith gives the fake adapter's arguments after its port, and the
// launch block.
func debugRepoWith(t *testing.T, adapterArgs, launch string) string {
	t.Helper()
	exe, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	root := testrepo.New(t, `commands:
  build: "true"
debug:
  adapter:
    run: '"`+filepath.ToSlash(exe)+`" `+daptest.ChildArg+` tcp {port} `+adapterArgs+`'
    transport: tcp
    ready: DAP server listening
  launch: `+launch+`
  attach: { mode: local, processId: "{pid}" }
`)
	t.Setenv("DAPTEST_ROOT", root)
	t.Chdir(root)
	return root
}

func TestDebugRun(t *testing.T) {
	root := debugRepo(t, "ok")
	code, out, errOut := run("debug", "run", "--set", "program=./src/a", "--set", "test=TestA",
		"--break", "src/a/a.go:3", "--break", "src/a/a.go:99", "--dump", "n * 2", "--dump", "missing")
	if code != ExitOK {
		t.Fatalf("exit %d\nstdout %s\nstderr %s", code, out, errOut)
	}
	// Breakpoints and the unverified warning come before the program's
	// output; paths are repo-relative with slashes on every OS.
	for _, want := range []string{
		"breakpoint src/a/a.go:3: verified\n",
		"breakpoint src/a/a.go:99: NOT verified\n",
		"warning: breakpoint src/a/a.go:99 is not verified: no code at line 99",
		"[stdout] === RUN   TestA\n== stop 1: breakpoint, thread 1\n  at src/a/a.go:3 a.Sum\n  locals:\n    n = 3\n  dumps:\n    n * 2 = 6\n    missing = <error: ",
		"[stdout] --- PASS: TestA\n",
		"end: exited, program exit code 0; 1 stop(s)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout lacks %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "warning:") > strings.Index(out, "[stdout]") {
		t.Errorf("the warning printed after the program ran:\n%s", out)
	}
	i := strings.LastIndex(out, "; transcript ")
	if i < 0 {
		t.Fatalf("no transcript path in the tail:\n%s", out)
	}
	path := strings.TrimSpace(out[i+len("; transcript "):])
	if !strings.HasPrefix(path, ".reliab/debug/") {
		t.Fatalf("transcript path %q", path)
	}
	written, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	last, err := os.ReadFile(filepath.Join(root, ".reliab", "debug", "last.json"))
	if err != nil || !bytes.Equal(written, last) {
		t.Fatalf("last.json is not a copy of %s: %v", path, err)
	}
	tr := validTranscript(t, written)
	if got := tr.Request.Arguments.(map[string]any); got["program"] != "./src/a" || got["args"].([]any)[1] != "^TestA$" {
		t.Errorf("launch arguments = %v", got)
	}
	if !strings.Contains(tr.Adapter.Run, " tcp ") || tr.Adapter.Port == 0 || strings.Contains(tr.Adapter.Run, "{port}") {
		t.Errorf("adapter = %+v", tr.Adapter)
	}
}

func validTranscript(t *testing.T, raw []byte) dap.Transcript {
	t.Helper()
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if err := schemas.Validate(schemas.DebugTranscript, doc); err != nil {
		t.Fatalf("transcript does not validate: %v\n%s", err, raw)
	}
	var tr dap.Transcript
	if err := json.Unmarshal(raw, &tr); err != nil {
		t.Fatal(err)
	}
	return tr
}

func TestDebugRunJSON(t *testing.T) {
	debugRepo(t, "ok")
	code, out, errOut := run("debug", "run", "--json", "--set", "program=./src/a", "--set", "test=TestA", "--break", "src/a/a.go:99")
	if code != ExitOK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	tr := validTranscript(t, []byte(out))
	if len(tr.Breakpoints) != 1 || tr.Breakpoints[0].Verified || tr.Breakpoints[0].Path != "src/a/a.go" {
		t.Errorf("breakpoints = %+v", tr.Breakpoints)
	}
	// The warning still reaches the agent, on stderr.
	if !strings.Contains(errOut, "warning: breakpoint src/a/a.go:99 is not verified") {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestDebugRunExitCodes(t *testing.T) {
	t.Run("exception stop", func(t *testing.T) {
		debugRepo(t, "panic")
		code, out, errOut := run("debug", "run", "--catch", "--set", "program=x", "--set", "test=T")
		if code != ExitFindings || !strings.Contains(out, "exception filters: panic") || !strings.Contains(errOut, "at an exception") {
			t.Errorf("exit %d\nstdout %s\nstderr %s", code, out, errOut)
		}
	})
	t.Run("max stops", func(t *testing.T) {
		debugRepo(t, "loop")
		code, out, errOut := run("debug", "run", "--max-stops", "2", "--set", "program=x", "--set", "test=T")
		if code != ExitFindings || !strings.Contains(out, "truncated: stops past --max-stops") || !strings.Contains(errOut, "max-stops") {
			t.Errorf("exit %d\nstdout %s\nstderr %s", code, out, errOut)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		debugRepo(t, "hang")
		code, out, _ := run("debug", "run", "--timeout", "300ms", "--set", "program=x", "--set", "test=T")
		if code != ExitFindings || !strings.Contains(out, "end: timeout") {
			t.Errorf("exit %d\nstdout %s", code, out)
		}
	})
	t.Run("budget", func(t *testing.T) {
		debugRepo(t, "hang")
		code, out, _ := run("debug", "run", "--budget", "2s", "--set", "program=x", "--set", "test=T")
		if code != ExitIncomplete || !strings.Contains(out, "end: budget") {
			t.Errorf("exit %d\nstdout %s", code, out)
		}
	})
}

// Errors before the adapter starts are exit 2, and nothing is started.
func TestDebugConfigErrors(t *testing.T) {
	t.Run("unexpanded placeholder", func(t *testing.T) {
		root := debugRepo(t, "ok")
		code, _, errOut := run("debug", "run", "--set", "program=./src/a")
		if code != ExitConfig || !strings.Contains(errOut, "{test}") || !strings.Contains(errOut, "debug.launch.args[1]") {
			t.Errorf("exit %d, stderr %q", code, errOut)
		}
		if _, err := os.Stat(filepath.Join(root, ".reliab", "debug", "adapter.log")); !os.IsNotExist(err) {
			t.Errorf("the adapter started: %v", err)
		}
	})
	t.Run("no debug section", func(t *testing.T) {
		t.Chdir(testrepo.New(t, "commands:\n  build: \"true\"\n"))
		code, _, errOut := run("debug", "run")
		if code != ExitConfig || !strings.Contains(errOut, "no debug section") || !strings.Contains(errOut, "reliab docs debug") {
			t.Errorf("exit %d, stderr %q", code, errOut)
		}
	})
	t.Run("bad break", func(t *testing.T) {
		debugRepo(t, "ok")
		code, _, errOut := run("debug", "run", "--set", "program=x", "--set", "test=T", "--break", "src/a/a.go")
		if code != ExitConfig || !strings.Contains(errOut, "file:line") {
			t.Errorf("exit %d, stderr %q", code, errOut)
		}
	})
	t.Run("set a builtin", func(t *testing.T) {
		debugRepo(t, "ok")
		if code, _, errOut := run("debug", "run", "--set", "port=1"); code != ExitConfig || !strings.Contains(errOut, "reliab's own placeholder") {
			t.Errorf("exit %d, stderr %q", code, errOut)
		}
	})
	t.Run("attach needs a pid", func(t *testing.T) {
		debugRepo(t, "ok")
		if code, _, errOut := run("debug", "attach"); code != ExitConfig || !strings.Contains(errOut, "--pid is required") {
			t.Errorf("exit %d, stderr %q", code, errOut)
		}
	})
	t.Run("adapter fails to start", func(t *testing.T) {
		debugRepo(t, "no-such-script")
		code, _, errOut := run("debug", "run", "--set", "program=x", "--set", "test=T")
		if code != ExitConfig || !strings.Contains(errOut, "adapter output:") || !strings.Contains(errOut, "bad tcp arguments") {
			t.Errorf("exit %d, stderr %q", code, errOut)
		}
	})
}

func TestDebugAttach(t *testing.T) {
	root := debugRepo(t, "ok")
	code, out, errOut := run("debug", "attach", "--pid", "4242", "--break", "src/a/a.go:3")
	if code != ExitOK {
		t.Fatalf("exit %d\nstdout %s\nstderr %s", code, out, errOut)
	}
	raw, err := os.ReadFile(filepath.Join(root, ".reliab", "debug", "last.json"))
	if err != nil {
		t.Fatal(err)
	}
	tr := validTranscript(t, raw)
	// {pid} as a whole value is a number, as adapters take it.
	if tr.Mode != dap.ModeAttach || tr.Request.Command != "attach" || tr.Request.Arguments.(map[string]any)["processId"] != 4242.0 {
		t.Errorf("mode %s request %+v", tr.Mode, tr.Request)
	}
}

func TestDebugProbe(t *testing.T) {
	debugRepo(t, "ok")
	code, out, errOut := run("debug", "probe")
	if code != ExitOK {
		t.Fatalf("exit %d\nstdout %s\nstderr %s", code, out, errOut)
	}
	for _, want := range []string{"adapter: ", " (tcp)\n", "  panic: Panics (default)\n", "conditional breakpoints: yes\n", "log points: yes\n", "terminate request: no\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("probe lacks %q:\n%s", want, out)
		}
	}
	code, out, _ = run("debug", "probe", "--json")
	var caps godap.Capabilities
	if code != ExitOK || json.Unmarshal([]byte(out), &caps) != nil || !caps.SupportsLogPoints {
		t.Errorf("probe --json: exit %d, %s", code, out)
	}

	debugRepo(t, "no-such-script")
	if code, _, errOut := run("debug", "probe"); code != ExitConfig || !strings.Contains(errOut, "bad tcp arguments") {
		t.Errorf("failing probe: exit %d, stderr %q", code, errOut)
	}
}

// {pid} comes only from --pid: a run whose launch names it fails before
// the adapter starts, as any unexpanded placeholder does.
func TestDebugRunPidOnlyFromAttach(t *testing.T) {
	debugRepoWith(t, `ok "DAP server listening"`, `{ processId: "{pid}" }`)
	code, _, errOut := run("debug", "run")
	if code != ExitConfig || !strings.Contains(errOut, "{pid}") || !strings.Contains(errOut, "--pid") {
		t.Errorf("exit %d, stderr %q", code, errOut)
	}
}

func TestDebugRunUnexpandedInAdapterRun(t *testing.T) {
	debugRepoWith(t, `ok "DAP server listening" {flavor}`, `{ program: x }`)
	code, _, errOut := run("debug", "probe")
	if code != ExitConfig || !strings.Contains(errOut, "unexpanded placeholder {flavor} in debug.adapter.run") {
		t.Errorf("exit %d, stderr %q", code, errOut)
	}
}

// With a ready pattern reliab waits for it, beyond the five-second
// connection retry it uses without one.
func TestDebugWaitsForReady(t *testing.T) {
	if testing.Short() {
		t.Skip("waits six seconds")
	}
	debugRepoWith(t, `ok "DAP server listening" 6000`, `{ program: x }`)
	if code, out, errOut := run("debug", "probe"); code != ExitOK {
		t.Errorf("exit %d\nstdout %s\nstderr %s", code, out, errOut)
	}
}

// The transcript records the run's duration, and --json keeps the exit
// codes; a limit's error names what ended the run.
func TestDebugRunJSONExitAndDuration(t *testing.T) {
	debugRepo(t, "panic")
	start := time.Now()
	code, out, _ := run("debug", "run", "--json", "--catch", "--set", "program=x", "--set", "test=T")
	elapsed := time.Since(start).Seconds()
	tr := validTranscript(t, []byte(out))
	if code != ExitFindings || tr.ExitCode != ExitFindings {
		t.Errorf("--json exit %d, transcript %d", code, tr.ExitCode)
	}
	// Starting the adapter alone takes milliseconds, and the session is
	// part of the command.
	if tr.DurationSec < 0.001 || tr.DurationSec > elapsed {
		t.Errorf("durationSec %.1f, the command took %.2fs", tr.DurationSec, elapsed)
	}

	debugRepo(t, "loop")
	if code, _, errOut := run("debug", "run", "--max-stops", "2", "--set", "program=x", "--set", "test=T"); code != ExitFindings || !strings.Contains(errOut, "stopped 2 times") {
		t.Errorf("exit %d, stderr %q", code, errOut)
	}
}
