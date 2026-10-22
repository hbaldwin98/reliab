package dap

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	godap "github.com/google/go-dap"

	"github.com/hbaldwin98/reliab/internal/dap/daptest"
)

// The behavior under test is knowledge/decisions/debug-adapter-seam.md,
// "reliab debug run" and "The transcript", unless a test names another
// source.

func TestMain(m *testing.M) {
	daptest.Main(childScripts)
	os.Exit(m.Run())
}

// childScripts are what the test binary plays when started as an adapter.
var childScripts = map[string]*daptest.Script{
	"basic": basicScript(),
}

// root is an absolute path on every OS; it need not exist.
var root = filepath.Join(os.TempDir(), "reliab-dap-repo")

func frames(paths ...string) []godap.StackFrame {
	out := make([]godap.StackFrame, len(paths))
	for i, p := range paths {
		file, fn, _ := strings.Cut(p, " ")
		j := strings.LastIndex(file, ":")
		path := file[:j]
		n, err := strconv.Atoi(file[j+1:])
		if err != nil {
			panic(p)
		}
		out[i] = godap.StackFrame{Id: 100 + i, Name: fn, Line: n, Source: &godap.Source{Path: path}}
	}
	return out
}

func basicScript() *daptest.Script {
	return &daptest.Script{
		Capabilities: godap.Capabilities{
			SupportsConfigurationDoneRequest: true, SupportsConditionalBreakpoints: true,
			ExceptionBreakpointFilters: []godap.ExceptionBreakpointsFilter{
				{Filter: "panic", Label: "Panics", Default: true},
				{Filter: "all", Label: "All exceptions"},
			},
		},
		Steps: []daptest.Step{
			{Output: "=== RUN   TestX\n"},
			{Stop: &daptest.Stop{Reason: "breakpoint", Thread: 1, Frames: frames(
				root+"/pkg/x.go:12 pkg.Sum",
				root+"/pkg/x_test.go:30 pkg.TestX",
				"/usr/local/go/src/testing/testing.go:1690 testing.tRunner",
			)}},
			{Output: "middle\n", Category: "stderr"},
			{Stop: &daptest.Stop{Reason: "breakpoint", Thread: 1, Frames: frames(root + "/pkg/x.go:12 pkg.Sum")}},
			{Output: "--- PASS: TestX\n"},
		},
		Variables: map[int][]godap.Variable{
			1000: {{Name: "n", Value: "3", Type: "int"}, {Name: "items", Value: "[]int len: 3", Type: "[]int", VariablesReference: 7}},
			7:    {{Name: "[0]", Value: "1"}, {Name: "[1]", Value: "2"}, {Name: "[2]", Value: "3"}},
		},
		Eval: map[string]godap.EvaluateResponseBody{
			"len(items)": {Result: "3", Type: "int"},
		},
		EvalErr: map[string]string{"nope": "could not find symbol value for nope"},
	}
}

func baseOptions() Options {
	return Options{
		Mode:      ModeRun,
		Arguments: map[string]any{"mode": "test", "program": "./pkg"},
		Breaks:    []Break{{Spec: "pkg/x.go:12", File: filepath.Join(root, "pkg", "x.go"), Line: 12}},
		Dumps:     []string{"len(items)", "nope"},
		Paths:     PathMapper{Root: root, Base: root},
		Timeout:   10 * time.Second,
	}
}

// runFake plays sc through Session over a pipe.
func runFake(t *testing.T, ctx context.Context, sc *daptest.Script, opt Options, tweak func(*Client)) (*Transcript, *daptest.Adapter, error) {
	t.Helper()
	conn, a := daptest.Pipe(sc)
	c := NewClient(conn)
	if tweak != nil {
		tweak(c)
	}
	tr := &Transcript{SchemaVersion: TranscriptVersion}
	err := Session(ctx, c, tr, opt)
	_ = c.Close()
	select {
	case <-a.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the fake adapter did not end")
	}
	return tr, a, err
}

func kinds(tr *Transcript) string {
	var out []string
	for _, e := range tr.Entries {
		out = append(out, e.Kind)
	}
	return strings.Join(out, ",")
}

func TestRunRecordsStopsAndOutputInOrder(t *testing.T) {
	tr, a, err := runFake(t, context.Background(), basicScript(), baseOptions(), nil)
	if err != nil {
		t.Fatal(err)
	}
	// Program output arrives as output events and is kept in order with
	// the stops.
	if got := kinds(tr); got != "output,stop,output,stop,output" {
		t.Fatalf("entries = %s", got)
	}
	stop := tr.Entries[1]
	if stop.Seq != 1 || stop.Reason != "breakpoint" || stop.Thread != 1 {
		t.Errorf("stop = %+v", stop)
	}
	if len(stop.Frames) != 3 || stop.Frames[0] != (Frame{Path: "pkg/x.go", Line: 12, Function: "pkg.Sum"}) {
		t.Errorf("frames = %+v", stop.Frames)
	}
	if stop.Frames[2].Path != "/usr/local/go/src/testing/testing.go" {
		t.Errorf("a path outside the repository stays absolute: %q", stop.Frames[2].Path)
	}
	// Locals: one level by default; items has children it does not expand.
	if len(stop.Locals) != 2 || stop.Locals[0].Name != "n" || stop.Locals[0].Value != "3" || stop.Locals[0].Scope != "Locals" || stop.Locals[1].Children != nil {
		t.Errorf("locals = %+v", stop.Locals)
	}
	// Each dump is evaluated in the top frame; an evaluation error is
	// recorded as the value, never fatal.
	if len(stop.Dumps) != 2 || stop.Dumps[0].Value != "3" || stop.Dumps[1].Error == "" || stop.Dumps[1].Value != "" {
		t.Errorf("dumps = %+v", stop.Dumps)
	}
	for _, raw := range a.Args("evaluate") {
		var ea godap.EvaluateArguments
		_ = json.Unmarshal(raw, &ea)
		if ea.FrameId != 100 {
			t.Errorf("evaluate in frame %d, want the top frame 100", ea.FrameId)
		}
	}
	if tr.Entries[0].Text != "=== RUN   TestX\n" || tr.Entries[2].Category != "stderr" {
		t.Errorf("output = %+v / %+v", tr.Entries[0], tr.Entries[2])
	}
	if tr.End.Reason != EndExited || tr.End.ExitCode == nil || *tr.End.ExitCode != 0 || tr.ExitCode != 0 {
		t.Errorf("end = %+v exit %d", tr.End, tr.ExitCode)
	}
	if tr.Truncated.Variables != 2 {
		t.Errorf("truncated variables = %d, want items at both stops", tr.Truncated.Variables)
	}
	cmds := strings.Join(a.Commands(), ",")
	if !strings.HasPrefix(cmds, "initialize,launch,setBreakpoints,configurationDone,stackTrace,scopes,variables,evaluate,evaluate,continue,") {
		t.Errorf("requests = %s", cmds)
	}
	if strings.Contains(cmds, "setExceptionBreakpoints") || strings.Contains(cmds, "terminate") {
		t.Errorf("without --catch or a limit: %s", cmds)
	}
	if !strings.HasSuffix(cmds, "disconnect") {
		t.Errorf("the session did not disconnect: %s", cmds)
	}
	if tr.Request.Command != "launch" || tr.Request.Arguments.(map[string]any)["program"] != "./pkg" {
		t.Errorf("request = %+v", tr.Request)
	}
	var la map[string]any
	_ = json.Unmarshal(a.Args("launch")[0], &la)
	if la["mode"] != "test" || la["program"] != "./pkg" {
		t.Errorf("launch sent %v, want the profile's arguments verbatim", la)
	}
}

func TestDepthExpandsChildren(t *testing.T) {
	opt := baseOptions()
	opt.Depth = 2
	tr, _, err := runFake(t, context.Background(), basicScript(), opt, nil)
	if err != nil {
		t.Fatal(err)
	}
	items := tr.Entries[1].Locals[1]
	if len(items.Children) != 3 || items.Children[2].Value != "3" {
		t.Errorf("items = %+v", items)
	}
	if tr.Truncated.Variables != 0 {
		t.Errorf("truncated variables = %d at depth 2", tr.Truncated.Variables)
	}
}

func TestBreakpointsGroupedPerFileWithConditions(t *testing.T) {
	opt := baseOptions()
	opt.Breaks = []Break{
		{Spec: "pkg/x.go:12", File: filepath.Join(root, "pkg", "x.go"), Line: 12},
		{Spec: "pkg/y.go:3", File: filepath.Join(root, "pkg", "y.go"), Line: 3},
		{Spec: `pkg/x.go:20 if n == 2`, File: filepath.Join(root, "pkg", "x.go"), Line: 20, Condition: "n == 2"},
	}
	_, a, err := runFake(t, context.Background(), basicScript(), opt, nil)
	if err != nil {
		t.Fatal(err)
	}
	sent := a.Args("setBreakpoints")
	if len(sent) != 2 {
		t.Fatalf("setBreakpoints sent %d times, want once per file", len(sent))
	}
	var x godap.SetBreakpointsArguments
	_ = json.Unmarshal(sent[0], &x)
	if x.Source.Path != filepath.Join(root, "pkg", "x.go") || len(x.Breakpoints) != 2 || x.Breakpoints[1].Line != 20 || x.Breakpoints[1].Condition != "n == 2" {
		t.Errorf("x.go breakpoints = %+v", x)
	}
}

func TestConditionWithoutAdapterSupportWarns(t *testing.T) {
	sc := basicScript()
	sc.Capabilities.SupportsConditionalBreakpoints = false
	opt := baseOptions()
	opt.Breaks[0].Condition, opt.Breaks[0].Spec = "n > 1", "pkg/x.go:12 if n > 1"
	tr, a, err := runFake(t, context.Background(), sc, opt, nil)
	if err != nil {
		t.Fatal(err)
	}
	var x godap.SetBreakpointsArguments
	_ = json.Unmarshal(a.Args("setBreakpoints")[0], &x)
	if x.Breakpoints[0].Condition != "" {
		t.Errorf("sent a condition the adapter does not support: %+v", x)
	}
	if len(tr.Warnings) != 1 || !strings.Contains(tr.Warnings[0], "conditional") {
		t.Errorf("warnings = %q", tr.Warnings)
	}
}

// Every --break whose setBreakpoints response says verified: false is a
// warning printed before the program runs.
func TestUnverifiedBreakpointWarnsBeforeTheProgramRuns(t *testing.T) {
	sc := basicScript()
	sc.Unverified = map[int]string{40: "could not find file"}
	sc.Moved = map[int]int{12: 13}
	opt := baseOptions()
	opt.Breaks = append(opt.Breaks, Break{Spec: "pkg/x.go:40", File: filepath.Join(root, "pkg", "x.go"), Line: 40})
	var atReady []string
	var warnings []string
	conn, a := daptest.Pipe(sc)
	c := NewClient(conn)
	defer func() { _ = c.Close() }()
	opt.OnReady = func(tr *Transcript) {
		atReady = a.Commands()
		warnings = append(warnings, tr.Warnings...)
	}
	tr := &Transcript{}
	if err := Session(context.Background(), c, tr, opt); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range atReady {
		if cmd == "configurationDone" {
			t.Errorf("OnReady ran after configurationDone: %v", atReady)
		}
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "pkg/x.go:40") || !strings.Contains(warnings[0], "could not find file") {
		t.Errorf("warnings at ready = %q", warnings)
	}
	if b := tr.Breakpoints; len(b) != 2 || !b[0].Verified || b[0].ActualLine != 13 || b[1].Verified || b[1].Path != "pkg/x.go" {
		t.Errorf("breakpoints = %+v", b)
	}
}

func TestCatchSetsDefaultFiltersAndExceptionStopExitsOne(t *testing.T) {
	sc := basicScript()
	sc.Steps = []daptest.Step{{Stop: &daptest.Stop{Reason: "exception", Description: "panic: boom", Thread: 1, Frames: frames(root + "/pkg/x.go:50 pkg.Boom")}}}
	sc.ExitCode = 2
	opt := baseOptions()
	opt.Catch = true
	tr, a, err := runFake(t, context.Background(), sc, opt, nil)
	if err != nil {
		t.Fatal(err)
	}
	var ex godap.SetExceptionBreakpointsArguments
	if sent := a.Args("setExceptionBreakpoints"); len(sent) != 1 || json.Unmarshal(sent[0], &ex) != nil || strings.Join(ex.Filters, ",") != "panic" {
		t.Errorf("exception filters sent = %+v, want the default ones", ex)
	}
	if strings.Join(tr.ExceptionFilters, ",") != "panic" {
		t.Errorf("transcript filters = %v", tr.ExceptionFilters)
	}
	if tr.ExitCode != 1 || tr.Entries[0].Description != "panic: boom" {
		t.Errorf("exit %d, entries %+v", tr.ExitCode, tr.Entries)
	}
}

func TestCatchFilterNamesFilters(t *testing.T) {
	opt := baseOptions()
	opt.CatchFilters = []string{"all", "uncaught"}
	tr, a, err := runFake(t, context.Background(), basicScript(), opt, nil)
	if err != nil {
		t.Fatal(err)
	}
	var ex godap.SetExceptionBreakpointsArguments
	_ = json.Unmarshal(a.Args("setExceptionBreakpoints")[0], &ex)
	if strings.Join(ex.Filters, ",") != "all,uncaught" {
		t.Errorf("filters = %v", ex.Filters)
	}
	if len(tr.Warnings) != 1 || !strings.Contains(tr.Warnings[0], `"uncaught"`) {
		t.Errorf("warnings = %q", tr.Warnings)
	}
}

func TestCatchWithoutFiltersWarns(t *testing.T) {
	sc := basicScript()
	sc.Capabilities.ExceptionBreakpointFilters = nil
	opt := baseOptions()
	opt.Catch = true
	tr, a, err := runFake(t, context.Background(), sc, opt, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Args("setExceptionBreakpoints")) != 0 || len(tr.Warnings) != 1 || !strings.Contains(tr.Warnings[0], "no filters") {
		t.Errorf("warnings = %q", tr.Warnings)
	}
}

// --max-stops ends a breakpoint in a hot loop and terminates the debuggee.
func TestMaxStopsEndsTheRun(t *testing.T) {
	sc := basicScript()
	sc.Capabilities.SupportsTerminateRequest = true
	stop := daptest.Step{Stop: &daptest.Stop{Reason: "breakpoint", Thread: 1, Frames: frames(root + "/pkg/x.go:12 pkg.Sum")}}
	sc.Steps = []daptest.Step{stop, stop, stop, stop, stop}
	opt := baseOptions()
	opt.MaxStops = 2
	tr, a, err := runFake(t, context.Background(), sc, opt, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(tr.Stops()); n != 2 || !tr.Truncated.Stops || tr.End.Reason != EndMaxStops || tr.ExitCode != 1 {
		t.Errorf("stops %d, truncated %+v, end %+v, exit %d", n, tr.Truncated, tr.End, tr.ExitCode)
	}
	cmds := a.Commands()
	if n := strings.Count(strings.Join(cmds, ","), "continue"); n != 1 {
		t.Errorf("continued %d times, want once before the second stop ended the run", n)
	}
	var d godap.DisconnectArguments
	_ = json.Unmarshal(a.Args("disconnect")[0], &d)
	if len(a.Args("terminate")) != 1 || !d.TerminateDebuggee {
		t.Errorf("run must end the debuggee: %v, disconnect %+v", cmds, d)
	}
}

func TestTimeoutEndsTheRun(t *testing.T) {
	sc := basicScript()
	sc.Steps = []daptest.Step{{Output: "working\n"}}
	sc.Hang = true
	opt := baseOptions()
	opt.Timeout = 200 * time.Millisecond
	start := time.Now()
	tr, _, err := runFake(t, context.Background(), sc, opt, nil)
	if err != nil {
		t.Fatal(err)
	}
	if tr.End.Reason != EndTimeout || tr.ExitCode != 1 || kinds(tr) != "output" {
		t.Errorf("end %+v exit %d entries %s", tr.End, tr.ExitCode, kinds(tr))
	}
	if time.Since(start) > 8*time.Second {
		t.Errorf("took %s", time.Since(start))
	}
}

// Exit 3 is the overall budget (knowledge/decisions/tiers-budgets-exit-codes.md).
func TestBudgetEndsTheRunWithThree(t *testing.T) {
	sc := basicScript()
	sc.Steps = nil
	sc.Hang = true
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	tr, a, err := runFake(t, ctx, sc, baseOptions(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if tr.End.Reason != EndBudget || tr.ExitCode != 3 {
		t.Errorf("end %+v exit %d", tr.End, tr.ExitCode)
	}
	if len(a.Args("disconnect")) != 1 {
		t.Errorf("no disconnect after the budget: %v", a.Commands())
	}
}

// The client owns a per-request timeout.
func TestRequestTimeoutEndsTheRun(t *testing.T) {
	sc := basicScript()
	sc.Silent = map[string]bool{"stackTrace": true}
	tr, _, err := runFake(t, context.Background(), sc, baseOptions(), func(c *Client) { c.RequestTimeout = 200 * time.Millisecond })
	if err != nil {
		t.Fatal(err)
	}
	if tr.End.Reason != EndAdapterError || !strings.Contains(tr.End.Message, "stackTrace: no response within") || tr.ExitCode != 1 {
		t.Errorf("end %+v exit %d", tr.End, tr.ExitCode)
	}
}

// runInTerminal and startDebugging get an error response, and the
// transcript records that the adapter asked.
func TestReverseRequestsAreDeclinedAndRecorded(t *testing.T) {
	sc := basicScript()
	sc.Reverse = []string{"runInTerminal", "startDebugging", "somethingElse"}
	tr, a, err := runFake(t, context.Background(), sc, baseOptions(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.ReverseRequests) != 2 || tr.ReverseRequests[0].Command != "runInTerminal" || tr.ReverseRequests[1].Command != "startDebugging" {
		t.Errorf("reverse requests = %+v", tr.ReverseRequests)
	}
	if tr.Ignored["request somethingElse"] != 1 {
		t.Errorf("ignored = %v", tr.Ignored)
	}
	answered := map[string]bool{}
	for _, r := range a.Received() {
		if r.Type == "response" {
			if r.Success {
				t.Errorf("%s answered with success", r.Command)
			}
			answered[r.Command] = true
		}
	}
	if !answered["runInTerminal"] || !answered["startDebugging"] || !answered["somethingElse"] {
		t.Errorf("answered = %v", answered)
	}
	if tr.ExitCode != 0 {
		t.Errorf("exit %d", tr.ExitCode)
	}
}

// Everything else the adapter sends is ignored and counted.
func TestUnhandledEventsAreCounted(t *testing.T) {
	sc := basicScript()
	sc.Noise = []string{"module", "module", "loadedSource"}
	tr, _, err := runFake(t, context.Background(), sc, baseOptions(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if tr.Ignored["event module"] != 2 || tr.Ignored["event loadedSource"] != 1 {
		t.Errorf("ignored = %v", tr.Ignored)
	}
}

func TestLaunchFailureIsASetupError(t *testing.T) {
	sc := basicScript()
	sc.LaunchError = "build failed: undefined: Foo"
	_, _, err := runFake(t, context.Background(), sc, baseOptions(), nil)
	var se *SetupError
	if !errors.As(err, &se) || !strings.Contains(err.Error(), "undefined: Foo") {
		t.Fatalf("err = %v, want a setup error with the adapter's message", err)
	}
}

// debugpy answers launch only after configurationDone.
func TestLaunchAnsweredAfterConfigurationDone(t *testing.T) {
	sc := basicScript()
	sc.LaunchAfterConfigurationDone = true
	tr, _, err := runFake(t, context.Background(), sc, baseOptions(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Stops()) != 2 || tr.ExitCode != 0 {
		t.Errorf("stops %d exit %d", len(tr.Stops()), tr.ExitCode)
	}
}

func TestStopWithoutThreadAsksForThreads(t *testing.T) {
	sc := basicScript()
	sc.Steps = []daptest.Step{{Stop: &daptest.Stop{Reason: "pause", Frames: frames(root + "/pkg/x.go:12 pkg.Sum")}}}
	tr, a, err := runFake(t, context.Background(), sc, baseOptions(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if tr.Entries[0].Thread != 1 || len(a.Args("threads")) != 1 {
		t.Errorf("thread %d, requests %v", tr.Entries[0].Thread, a.Commands())
	}
}

func TestAttachLeavesTheProcessRunning(t *testing.T) {
	sc := basicScript()
	sc.Capabilities.SupportsTerminateRequest = true
	sc.Hang = true
	opt := baseOptions()
	opt.Mode = ModeAttach
	opt.Arguments = map[string]any{"mode": "local", "processId": 4242}
	opt.Timeout = 200 * time.Millisecond
	tr, a, err := runFake(t, context.Background(), sc, opt, nil)
	if err != nil {
		t.Fatal(err)
	}
	if tr.Mode != ModeAttach || tr.Request.Command != "attach" || len(a.Args("attach")) != 1 || len(a.Args("launch")) != 0 {
		t.Errorf("mode %s request %s, sent %v", tr.Mode, tr.Request.Command, a.Commands())
	}
	var d godap.DisconnectArguments
	_ = json.Unmarshal(a.Args("disconnect")[0], &d)
	if d.TerminateDebuggee || len(a.Args("terminate")) != 0 {
		t.Errorf("attach without --terminate ended the process: %+v %v", d, a.Commands())
	}

	opt.Terminate = true
	_, a, err = runFake(t, context.Background(), sc, opt, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(a.Args("disconnect")[0], &d)
	if !d.TerminateDebuggee {
		t.Errorf("attach --terminate: disconnect %+v", d)
	}
}

func TestExitedWithoutTerminatedStillEnds(t *testing.T) {
	sc := basicScript()
	sc.Steps = []daptest.Step{{Output: "ok\n"}}
	sc.NoTerminated = true
	sc.ExitCode = 1
	tr, _, err := runFake(t, context.Background(), sc, baseOptions(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if tr.End.Reason != EndExited || tr.End.ExitCode == nil || *tr.End.ExitCode != 1 || tr.ExitCode != 0 {
		t.Errorf("end %+v exit %d", tr.End, tr.ExitCode)
	}
}

func TestCapsLongValuesAndOutput(t *testing.T) {
	sc := basicScript()
	long := strings.Repeat("x", MaxValue+10)
	sc.Variables[1000] = []godap.Variable{{Name: "s", Value: long}}
	sc.Steps = []daptest.Step{
		{Output: strings.Repeat("o", MaxOutput-5)},
		{Stop: &daptest.Stop{Reason: "breakpoint", Thread: 1, Frames: frames(root + "/pkg/x.go:12 pkg.Sum")}},
		{Output: "0123456789"},
		{Output: "dropped", Category: "stderr"},
	}
	opt := baseOptions()
	opt.Dumps = nil
	tr, _, err := runFake(t, context.Background(), sc, opt, nil)
	if err != nil {
		t.Fatal(err)
	}
	v := tr.Entries[1].Locals[0].Value
	if !strings.HasPrefix(v, strings.Repeat("x", MaxValue)+"...") || !strings.Contains(v, "+10 chars") || tr.Truncated.Values != 1 {
		t.Errorf("value %q..., truncated %+v", v[len(v)-20:], tr.Truncated)
	}
	if tr.Truncated.OutputBytes != 5+len("dropped") || kinds(tr) != "output,stop,output" || tr.Entries[2].Text != "01234" {
		t.Errorf("truncated %+v, entries %s, last %q", tr.Truncated, kinds(tr), tr.Entries[len(tr.Entries)-1].Text)
	}
}

func TestProbeReturnsCapabilities(t *testing.T) {
	conn, a := daptest.Pipe(basicScript())
	c := NewClient(conn)
	defer func() { _ = c.Close() }()
	caps, err := Probe(context.Background(), c, "fake")
	if err != nil {
		t.Fatal(err)
	}
	if !caps.SupportsConditionalBreakpoints || len(caps.ExceptionBreakpointFilters) != 2 {
		t.Errorf("caps = %+v", caps)
	}
	if got := strings.Join(a.Commands(), ","); got != "initialize,disconnect" {
		t.Errorf("probe sent %s", got)
	}
}

// The client implements the stepping requests phase 2 builds on.
func TestClientRequests(t *testing.T) {
	sc := basicScript()
	sc.Steps = []daptest.Step{{Stop: &daptest.Stop{Reason: "breakpoint", Thread: 1, Frames: frames(root+"/pkg/x.go:12 pkg.Sum", root+"/pkg/x_test.go:3 pkg.TestX")}}}
	conn, a := daptest.Pipe(sc)
	c := NewClient(conn)
	defer func() { _ = c.Close() }()
	ctx := context.Background()
	if _, err := Initialize(ctx, c, "fake"); err != nil {
		t.Fatal(err)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(c.Do(ctx, "launch", map[string]any{}, nil))
	must(c.ConfigurationDone(ctx))
	th, err := c.Threads(ctx)
	must(err)
	st, err := c.StackTrace(ctx, 1, 1)
	must(err)
	sc2, err := c.Scopes(ctx, st[0].Id)
	must(err)
	vs, err := c.Variables(ctx, sc2[0].VariablesReference)
	must(err)
	ev, err := c.Evaluate(ctx, "len(items)", st[0].Id)
	must(err)
	if len(th) != 2 || len(st) != 1 || st[0].Name != "pkg.Sum" || len(vs) != 2 || ev.Result != "3" {
		t.Errorf("threads %v stack %v vars %v eval %v", th, st, vs, ev)
	}
	must(c.Next(ctx, 1))
	must(c.StepIn(ctx, 1))
	must(c.StepOut(ctx, 1))
	must(c.Pause(ctx, 1))
	must(c.Continue(ctx, 1))
	must(c.Terminate(ctx))
	if _, err := c.Evaluate(ctx, "nope", 0); err == nil || !strings.Contains(err.Error(), "could not find symbol") {
		t.Errorf("evaluate error = %v", err)
	}
	must(c.Disconnect(ctx, true))
	want := "initialize,launch,configurationDone,threads,stackTrace,scopes,variables,evaluate,next,stepIn,stepOut,pause,continue,terminate,evaluate,disconnect"
	if got := strings.Join(a.Commands(), ","); got != want {
		t.Errorf("sent %s\nwant %s", got, want)
	}
	<-a.Done()
	select {
	case <-c.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the client did not see the connection end")
	}
	if _, err := c.Send("threads", nil); err == nil {
		t.Error("send after the adapter left succeeded")
	}
}

func TestPendingFailsWhenTheAdapterLeaves(t *testing.T) {
	sc := basicScript()
	sc.Silent = map[string]bool{"threads": true}
	conn, a := daptest.Pipe(sc)
	c := NewClient(conn)
	defer func() { _ = c.Close() }()
	p, err := c.Send("threads", nil)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		_, _ = c.Send("disconnect", nil)
	}()
	<-a.Done()
	if _, err := p.Wait(context.Background(), 5*time.Second); err == nil || !strings.Contains(err.Error(), "closed the connection") {
		t.Errorf("err = %v", err)
	}
}
