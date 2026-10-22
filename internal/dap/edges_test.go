package dap

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	godap "github.com/google/go-dap"
	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/hbaldwin98/reliab/internal/dap/daptest"
	"github.com/hbaldwin98/reliab/schemas"
)

// Edge cases of knowledge/decisions/debug-adapter-seam.md and
// docs/debug.md, and of the DAP specification
// (https://microsoft.github.io/debug-adapter-protocol/specification),
// where a test cites it.

// The transcript a session writes follows schemas/debug-transcript.schema.json
// even when nothing was set and nothing stopped: its lists are empty, not null.
func TestSessionTranscriptValidatesWhenEmpty(t *testing.T) {
	sc := basicScript()
	sc.Steps = nil
	opt := baseOptions()
	opt.Breaks, opt.Dumps = nil, nil
	tr, _, err := runFake(t, context.Background(), sc, opt, nil)
	if err != nil {
		t.Fatal(err)
	}
	tr.StartedAt, tr.Adapter = "2026-10-21T15:30:00Z", AdapterInfo{Run: "x", Transport: TransportStdio}
	raw, _ := json.Marshal(tr)
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if err := schemas.Validate(schemas.DebugTranscript, doc); err != nil {
		t.Errorf("%v\n%s", err, raw)
	}
}

// DAP lines start at 1 (initialize sends linesStartAt1).
func TestParseBreakLineOne(t *testing.T) {
	b, err := ParseBreak("a.go:1", NewPathMapper(root, ""))
	if err != nil || b.Line != 1 {
		t.Errorf("a.go:1 = %+v, %v", b, err)
	}
}

// Zero options mean the documented defaults: --timeout 120s, --frames 10.
func TestZeroOptionsUseDefaults(t *testing.T) {
	sc := basicScript()
	var fs []godap.StackFrame
	for i := 0; i < 12; i++ {
		fs = append(fs, frames(fmt.Sprintf("%s/pkg/x.go:%d f%d", root, i+1, i))...)
	}
	sc.Steps = []daptest.Step{{Stop: &daptest.Stop{Reason: "breakpoint", Thread: 1, Frames: fs}}}
	sc.IgnoreLevels = true
	opt := baseOptions()
	opt.Timeout, opt.Frames, opt.MaxStops = 0, 0, 0
	tr, a, err := runFake(t, context.Background(), sc, opt, nil)
	if err != nil {
		t.Fatal(err)
	}
	if tr.End.Reason != EndExited {
		t.Errorf("a zero timeout ended the run: %+v", tr.End)
	}
	if n := len(tr.Entries[0].Frames); n != DefaultFrames {
		t.Errorf("recorded %d frames from an adapter that ignores levels, want %d", n, DefaultFrames)
	}
	var st godap.StackTraceArguments
	_ = json.Unmarshal(a.Args("stackTrace")[0], &st)
	if st.Levels != DefaultFrames {
		t.Errorf("asked for %d levels", st.Levels)
	}
}

// Every --break whose response does not say verified: true is a warning,
// including one the adapter left out of its response.
func TestMissingBreakpointResponseIsUnverified(t *testing.T) {
	sc := basicScript()
	sc.DropBreakpoints = true
	tr, _, err := runFake(t, context.Background(), sc, baseOptions(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if tr.Breakpoints[0].Verified || len(tr.Warnings) != 1 || !strings.Contains(tr.Warnings[0], "not verified") {
		t.Errorf("breakpoints %+v warnings %q", tr.Breakpoints, tr.Warnings)
	}
}

// The capabilities event updates what the adapter supports
// (DAP: CapabilitiesEvent).
func TestCapabilitiesEventIsApplied(t *testing.T) {
	sc := basicScript()
	sc.Capabilities.SupportsConditionalBreakpoints = false
	sc.Capabilities.ExceptionBreakpointFilters = nil
	sc.CapabilitiesEvent = &godap.Capabilities{
		SupportsConditionalBreakpoints: true,
		ExceptionBreakpointFilters:     []godap.ExceptionBreakpointsFilter{{Filter: "late", Label: "Late", Default: true}},
	}
	opt := baseOptions()
	opt.Catch = true
	opt.Breaks[0].Condition = "n > 1"
	tr, a, err := runFake(t, context.Background(), sc, opt, nil)
	if err != nil {
		t.Fatal(err)
	}
	var x godap.SetBreakpointsArguments
	_ = json.Unmarshal(a.Args("setBreakpoints")[0], &x)
	if x.Breakpoints[0].Condition != "n > 1" || strings.Join(tr.ExceptionFilters, ",") != "late" || len(tr.Warnings) != 0 {
		t.Errorf("condition %q, filters %v, warnings %q", x.Breakpoints[0].Condition, tr.ExceptionFilters, tr.Warnings)
	}
}

// A stop with no frames records no locals and still evaluates the dumps.
func TestStopWithoutFrames(t *testing.T) {
	sc := basicScript()
	sc.Steps = []daptest.Step{{Stop: &daptest.Stop{Reason: "pause", Thread: 1}}}
	opt := baseOptions()
	opt.Dumps = []string{"len(items)"}
	tr, a, err := runFake(t, context.Background(), sc, opt, nil)
	if err != nil {
		t.Fatal(err)
	}
	e := tr.Entries[0]
	if len(e.Frames) != 0 || len(e.Locals) != 0 || len(e.Dumps) != 1 || e.Dumps[0].Value != "3" || len(a.Args("scopes")) != 0 {
		t.Errorf("entry %+v, requests %v", e, a.Commands())
	}
}

// docs/debug.md: variables past the 100-per-scope cap are counted, not
// kept; values are cut at 4096 characters, a value of exactly that length
// is kept whole; --depth expands that many levels and no more.
func TestVariableCaps(t *testing.T) {
	sc := basicScript()
	var many []godap.Variable
	for i := 0; i < MaxVariables+3; i++ {
		many = append(many, godap.Variable{Name: fmt.Sprintf("v%d", i), Value: "1"})
	}
	exact := strings.Repeat("e", MaxValue)
	many[0] = godap.Variable{Name: "exact", Value: exact, VariablesReference: 7}
	sc.Variables[1000] = many
	sc.Variables[7] = []godap.Variable{{Name: "child", Value: "c", VariablesReference: 8}}
	sc.Variables[8] = []godap.Variable{{Name: "grandchild", Value: "g"}}
	sc.Steps = []daptest.Step{{Stop: &daptest.Stop{Reason: "breakpoint", Thread: 1, Frames: frames(root + "/pkg/x.go:12 pkg.Sum")}}}
	opt := baseOptions()
	opt.Dumps = nil
	opt.Depth = 2
	tr, _, err := runFake(t, context.Background(), sc, opt, nil)
	if err != nil {
		t.Fatal(err)
	}
	locals := tr.Entries[0].Locals
	if len(locals) != MaxVariables || locals[0].Value != exact || tr.Truncated.Values != 0 {
		t.Errorf("kept %d locals, first %d chars, values cut %d", len(locals), len(locals[0].Value), tr.Truncated.Values)
	}
	if kids := locals[0].Children; len(kids) != 1 || kids[0].Children != nil {
		t.Errorf("depth 2 expanded %+v", kids)
	}
	// 3 past the cap, and the grandchild past --depth.
	if tr.Truncated.Variables != 4 {
		t.Errorf("truncated variables = %d, want 4", tr.Truncated.Variables)
	}
}

// Exit 3 is the overall budget (knowledge/decisions/tiers-budgets-exit-codes.md),
// also when it passes while reliab waits on a request mid-run, or on the
// launch response.
func TestBudgetDuringARequest(t *testing.T) {
	sc := basicScript()
	sc.Silent = map[string]bool{"continue": true}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	tr, _, err := runFake(t, ctx, sc, baseOptions(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if tr.End.Reason != EndBudget || tr.ExitCode != 3 {
		t.Errorf("end %+v exit %d", tr.End, tr.ExitCode)
	}

	// The held launch response never comes.
	sc = basicScript()
	sc.LaunchAfterConfigurationDone, sc.NeverAnswerLaunch = true, true
	ctx2, cancel2 := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel2()
	_, _, err = runFake(t, ctx2, sc, baseOptions(), nil)
	var se *SetupError
	if !errors.As(err, &se) || !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want a setup error from the budget", err)
	}
}

// DAP ProtocolMessage.seq: the first message is 1 and each next one is 1
// greater, responses to reverse requests included.
func TestSeqIncreasesByOne(t *testing.T) {
	sc := basicScript()
	sc.Reverse = []string{"runInTerminal"}
	_, a, err := runFake(t, context.Background(), sc, baseOptions(), nil)
	if err != nil {
		t.Fatal(err)
	}
	got := a.Received()
	for i, r := range got {
		if r.Seq != i+1 {
			t.Fatalf("message %d (%s %s) has seq %d", i, r.Type, r.Command, r.Seq)
		}
	}
}

// DAP ErrorResponse: the message is the error's format with its variables
// filled, else the response's message.
func TestResponseErr(t *testing.T) {
	body := func(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
	tests := []struct {
		r    Response
		want string
	}{
		{Response{Command: "evaluate", Success: true}, ""},
		{Response{Command: "evaluate", Message: "short"}, "evaluate: short"},
		{Response{Command: "evaluate", Message: "short", Body: body(map[string]any{"error": map[string]any{"format": "no {name} in {where}", "variables": map[string]string{"name": "x", "where": "f"}}})}, "evaluate: no x in f"},
		{Response{Command: "evaluate", Message: "short", Body: body(map[string]any{"error": map[string]any{"format": ""}})}, "evaluate: short"},
		{Response{Command: "launch"}, "launch: failed"},
	}
	for _, tt := range tests {
		err := tt.r.Err()
		got := ""
		if err != nil {
			got = err.Error()
		}
		if got != tt.want {
			t.Errorf("%+v: %q, want %q", tt.r, got, tt.want)
		}
	}
}

// A response body is optional in DAP; a request whose response has none
// decodes to an empty body, not an error.
func TestEmptyResponseBody(t *testing.T) {
	sc := basicScript()
	sc.EmptyBodies = map[string]bool{"threads": true}
	conn, _ := daptest.Pipe(sc)
	c := NewClient(conn)
	defer func() { _ = c.Close() }()
	th, err := c.Threads(context.Background())
	if err != nil || len(th) != 0 {
		t.Errorf("threads %v, %v", th, err)
	}
}

// Everything else the adapter sends is ignored and counted: a response to
// no request, an unknown message type, malformed JSON.
func TestStrayMessagesAreCounted(t *testing.T) {
	sc := basicScript()
	sc.Raw = []string{
		`{"seq":90,"type":"response","request_seq":999,"command":"threads","success":true}`,
		`{"seq":91,"type":"notice"}`,
		`{not json`,
	}
	tr, _, err := runFake(t, context.Background(), sc, baseOptions(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"response threads", "message notice", "malformed message"} {
		if tr.Ignored[k] != 1 {
			t.Errorf("ignored[%q] = %d; all %v", k, tr.Ignored[k], tr.Ignored)
		}
	}
}

func TestEndOfConnectionError(t *testing.T) {
	sc := basicScript()
	sc.Silent = map[string]bool{"threads": true}
	conn, a := daptest.Pipe(sc)
	c := NewClient(conn)
	defer func() { _ = c.Close() }()
	p, err := c.Send("threads", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Send("disconnect", nil); err != nil {
		t.Fatal(err)
	}
	<-a.Done()
	_, err = p.Wait(context.Background(), 5*time.Second)
	if err == nil || !strings.Contains(err.Error(), "EOF") {
		t.Errorf("err = %v, want one naming EOF", err)
	}
	if c.AdapterOutput() != "" {
		t.Errorf("a client reliab did not start has adapter output %q", c.AdapterOutput())
	}
}

// The ready pattern is a regex on the adapter's output lines; a line that
// begins a write still starts a line.
func TestWatchWriterAnchoredReady(t *testing.T) {
	w := &watchWriter{re: regexp.MustCompile(`^listening`), ready: make(chan struct{})}
	_, _ = w.Write([]byte("starting\n"))
	_, _ = w.Write([]byte("\nlistening on 1\n"))
	select {
	case <-w.ready:
	default:
		t.Fatal("an anchored ready line after a newline did not match")
	}
}

// docs/debug.md: on failure reliab shows the tail of what the adapter
// printed; the kept output and the line being matched are bounded.
func TestWatchWriterKeepsATail(t *testing.T) {
	w := &watchWriter{re: regexp.MustCompile(`ready$`), ready: make(chan struct{})}
	_, _ = w.Write(bytes.Repeat([]byte("x"), outputCap+10))
	_, _ = w.Write([]byte("ready"))
	if got := w.String(); len(got) != outputCap || !strings.HasSuffix(got, "xready") {
		t.Errorf("tail is %d bytes, ends %q", len(got), got[len(got)-10:])
	}
	if len(w.line) > outputCap {
		t.Errorf("the line buffer grew to %d", len(w.line))
	}
	select {
	case <-w.ready:
	default:
		t.Error("ready after a long line")
	}
}

func TestHasDrive(t *testing.T) {
	// Windows drive-letter paths: a letter A-Z in either case, a colon,
	// and a separator.
	for _, p := range []string{"a:/", "z:\\x", "A:/x", "Z:\\", "c:/"} {
		if !hasDrive(p) {
			t.Errorf("hasDrive(%q) = false", p)
		}
	}
	for _, p := range []string{"C:", "C:x", "1:/", "@:/", "[:/", "`:/", "{:/", "/c/x", "", "ab:/"} {
		if hasDrive(p) {
			t.Errorf("hasDrive(%q) = true", p)
		}
	}
}

// docs/debug.md's rendering: a stop's description beside its reason when
// it says more; no exception-filter, dumps, ignored, or truncated line when
// there is nothing to say; a value exactly --width long is not cut.
func TestRenderOmitsWhatIsEmpty(t *testing.T) {
	tr := &Transcript{
		Adapter: AdapterInfo{Run: "a", Transport: TransportStdio},
		Entries: []Entry{
			{Kind: KindStop, Seq: 1, Reason: "exception", Description: "panic: boom", Thread: 1,
				Locals: []Variable{{Name: "w", Value: strings.Repeat("w", 12)}}},
			{Kind: KindStop, Seq: 2, Reason: "step", Description: "step", Thread: 1},
		},
		End: End{Reason: EndExited},
	}
	var b bytes.Buffer
	Render(&b, tr, 12, "")
	out := b.String()
	if !strings.Contains(out, "== stop 1: exception (panic: boom), thread 1\n") || !strings.Contains(out, "== stop 2: step, thread 1\n") {
		t.Errorf("stop heads:\n%s", out)
	}
	if !strings.Contains(out, "    w = "+strings.Repeat("w", 12)+"\n") {
		t.Errorf("a value of exactly the width was cut:\n%s", out)
	}
	for _, never := range []string{"exception filters", "dumps:", "ignored", "truncated", "port"} {
		if strings.Contains(out, never) {
			t.Errorf("rendering has %q with nothing to say:\n%s", never, out)
		}
	}
}

// Every unexpanded placeholder is named with where it is, also one after a
// placeholder that has a value.
func TestExpandNamesASecondPlaceholder(t *testing.T) {
	_, err := Expand("launch", map[string]any{"a": "{pid} {nope}"}, map[string]string{"pid": "1"})
	if err == nil || !strings.Contains(err.Error(), "{nope} in launch.a;") {
		t.Errorf("err = %v", err)
	}
}

func TestSplitCommandCarriageReturn(t *testing.T) {
	got, err := SplitCommand("a\r\nb\rc")
	if err != nil || strings.Join(got, "|") != "a|b|c" {
		t.Errorf("got %q, %v", got, err)
	}
}

// An adapter may report the root through a symlink's target; the
// resolved spelling maps too (docs/windows.md: compare paths after
// filepath.EvalSymlinks).
func TestPathMapperThroughASymlink(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("cannot make a symlink here: %v", err)
	}
	resolved, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatal(err)
	}
	m := NewPathMapper(link, ".")
	if got := m.Rel(filepath.Join(resolved, "pkg", "a.go")); got != "pkg/a.go" {
		t.Errorf("Rel through the link's target = %q", got)
	}
}
