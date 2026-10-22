package dap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	godap "github.com/google/go-dap"
)

// Defaults for the run and attach flags.
const (
	DefaultFrames   = 10
	DefaultDepth    = 1
	DefaultMaxStops = 20
	DefaultTimeout  = 120 * time.Second
)

// Caps on what a transcript keeps.
const (
	// MaxVariables is how many variables one scope or container keeps.
	MaxVariables = 100
	// MaxValue is how many characters of one value the transcript keeps.
	MaxValue = 4096
	// MaxOutput is how many bytes of program output the transcript keeps.
	MaxOutput = 1 << 20
	// teardownTimeout bounds terminate and disconnect at the end.
	teardownTimeout = 5 * time.Second
)

// Break is one parsed --break.
type Break struct {
	Spec      string
	File      string // absolute, native
	Line      int
	Condition string
}

// ParseBreak reads "file:line" or "file:line if condition". The file is
// relative to the repository root unless absolute.
func ParseBreak(spec string, m PathMapper) (Break, error) {
	loc, cond, _ := strings.Cut(spec, " if ")
	loc = strings.TrimSpace(loc)
	i := strings.LastIndex(loc, ":")
	if i <= 0 {
		return Break{}, fmt.Errorf("--break %q: want file:line or 'file:line if condition'", spec)
	}
	line, err := strconv.Atoi(loc[i+1:])
	if err != nil || line < 1 {
		return Break{}, fmt.Errorf("--break %q: %q is not a line number", spec, loc[i+1:])
	}
	return Break{Spec: spec, File: m.Abs(loc[:i]), Line: line, Condition: strings.TrimSpace(cond)}, nil
}

// Options drive one run or attach session.
type Options struct {
	Mode         string
	Arguments    map[string]any
	Breaks       []Break
	Dumps        []string
	Catch        bool
	CatchFilters []string
	Frames       int
	Depth        int
	MaxStops     int
	// Timeout bounds the program's run, from configurationDone.
	Timeout time.Duration
	// Terminate ends an attached process at the end (attach only).
	Terminate bool
	Paths     PathMapper
	AdapterID string
	// OnReady is called once breakpoints and exception filters are set,
	// before the program runs, so warnings print first.
	OnReady func(*Transcript)
	Now     func() time.Time
}

// SetupError is a failure before the program ran: exit code 2.
type SetupError struct{ Err error }

func (e *SetupError) Error() string { return e.Err.Error() }
func (e *SetupError) Unwrap() error { return e.Err }

// Initialize sends initialize and returns the adapter's capabilities.
func Initialize(ctx context.Context, c *Client, adapterID string) (godap.Capabilities, error) {
	var caps godap.Capabilities
	err := c.Do(ctx, "initialize", godap.InitializeRequestArguments{
		ClientID: "reliab", ClientName: "reliab", AdapterID: adapterID, Locale: "en-US",
		LinesStartAt1: true, ColumnsStartAt1: true, PathFormat: "path",
		SupportsVariableType: true,
	}, &caps)
	return caps, err
}

// Session runs the non-interactive loop on t: initialize, launch or
// attach, breakpoints, then record each stop and continue until the
// program ends or a limit does. A *SetupError means the program never ran;
// ctx ending records the budget as the end.
func Session(ctx context.Context, c *Client, t *Transcript, opt Options) error {
	if opt.Now == nil {
		opt.Now = time.Now
	}
	s := &session{c: c, t: t, opt: opt}
	t.Mode = opt.Mode
	t.Request = RequestInfo{Command: requestFor(opt.Mode), Arguments: opt.Arguments}
	if t.Breakpoints == nil {
		t.Breakpoints = []BreakpointRecord{}
	}
	if t.ExceptionFilters == nil {
		t.ExceptionFilters = []string{}
	}
	if t.Entries == nil {
		t.Entries = []Entry{}
	}
	err := s.run(ctx)
	t.ReverseRequests = c.ReverseRequests()
	if ig := c.Ignored(); len(ig) > 0 {
		t.Ignored = ig
	}
	if err != nil {
		return err
	}
	t.ExitCode = ComputeExitCode(t)
	return nil
}

func requestFor(mode string) string {
	if mode == ModeAttach {
		return "attach"
	}
	return "launch"
}

type session struct {
	c       *Client
	t       *Transcript
	opt     Options
	caps    godap.Capabilities
	outKept int
	ended   bool // the debuggee exited or the adapter ended the session
}

func (s *session) run(ctx context.Context) error {
	caps, err := Initialize(ctx, s.c, s.opt.AdapterID)
	if err != nil {
		return &SetupError{err}
	}
	s.caps = caps
	launch, err := s.c.Send(requestFor(s.opt.Mode), s.opt.Arguments)
	if err != nil {
		return &SetupError{err}
	}
	// Some adapters answer launch only after configurationDone, so wait for
	// initialized and watch the launch response at the same time.
	launched, lch := false, launch.ch
	for initialized := false; !initialized; {
		select {
		case ev, ok := <-s.c.Events():
			if !ok {
				return &SetupError{fmt.Errorf("the adapter closed the connection before it was initialized: %w", s.c.readErr())}
			}
			switch ev.Name {
			case "initialized":
				initialized = true
			case "terminated", "exited":
				return &SetupError{fmt.Errorf("the adapter ended the session before it was initialized")}
			default:
				s.event(ev)
			}
		case r, ok := <-lch:
			if !ok {
				return &SetupError{fmt.Errorf("%s: the adapter closed the connection: %w", launch.Command, s.c.readErr())}
			}
			if err := r.Err(); err != nil {
				return &SetupError{err}
			}
			launched, lch = true, nil
		case <-ctx.Done():
			return &SetupError{fmt.Errorf("waiting for the adapter to initialize: %w", ctx.Err())}
		}
	}
	if err := s.setBreakpoints(ctx); err != nil {
		return &SetupError{err}
	}
	if s.opt.Catch || len(s.opt.CatchFilters) > 0 {
		if err := s.setExceptions(ctx); err != nil {
			return &SetupError{err}
		}
	}
	if s.opt.OnReady != nil {
		s.opt.OnReady(s.t)
	}
	if s.caps.SupportsConfigurationDoneRequest {
		if err := s.c.ConfigurationDone(ctx); err != nil {
			return &SetupError{err}
		}
	}
	if !launched {
		// The launch builds the program for some adapters: wait as long
		// as the budget allows.
		if r, ok := <-launchWait(ctx, launch); !ok {
			return &SetupError{fmt.Errorf("%s: no response: %w", launch.Command, errOr(ctx.Err(), s.c.readErr()))}
		} else if err := r.Err(); err != nil {
			return &SetupError{err}
		}
	}
	s.loop(ctx)
	s.teardown(ctx)
	return nil
}

func errOr(a, b error) error {
	if a != nil {
		return a
	}
	return b
}

// launchWait delivers the launch response, or closes when ctx ends or the
// adapter goes away.
func launchWait(ctx context.Context, p *Pending) <-chan *Response {
	out := make(chan *Response, 1)
	go func() {
		defer close(out)
		select {
		case r, ok := <-p.ch:
			if ok {
				out <- r
			}
		case <-ctx.Done():
		}
	}()
	return out
}

func (s *session) setBreakpoints(ctx context.Context) error {
	var files []string
	byFile := map[string][]int{}
	for i, b := range s.opt.Breaks {
		if _, ok := byFile[b.File]; !ok {
			files = append(files, b.File)
		}
		byFile[b.File] = append(byFile[b.File], i)
		rec := BreakpointRecord{Spec: b.Spec, Path: s.opt.Paths.Rel(b.File), Line: b.Line, Condition: b.Condition}
		if b.Condition != "" && !s.caps.SupportsConditionalBreakpoints {
			s.warn("the adapter does not support conditional breakpoints; %s stops on every hit", b.Spec)
		}
		s.t.Breakpoints = append(s.t.Breakpoints, rec)
	}
	for _, f := range files {
		idx := byFile[f]
		bps := make([]godap.SourceBreakpoint, len(idx))
		for j, i := range idx {
			bps[j] = godap.SourceBreakpoint{Line: s.opt.Breaks[i].Line}
			if s.caps.SupportsConditionalBreakpoints {
				bps[j].Condition = s.opt.Breaks[i].Condition
			}
		}
		body, err := s.c.SetBreakpoints(ctx, godap.SetBreakpointsArguments{Source: godap.Source{Path: f}, Breakpoints: bps})
		if err != nil {
			return err
		}
		for j, i := range idx {
			rec := &s.t.Breakpoints[i]
			if j < len(body.Breakpoints) {
				got := body.Breakpoints[j]
				rec.Verified = got.Verified
				rec.Message = got.Message
				if got.Line != 0 && got.Line != rec.Line {
					rec.ActualLine = got.Line
				}
			}
			if !rec.Verified {
				why := ""
				if rec.Message != "" {
					why = ": " + rec.Message
				}
				s.warn("breakpoint %s is not verified%s; the run may stop nowhere there (a path the adapter cannot map, a line without code, an optimized build)", rec.Spec, why)
			}
		}
	}
	return nil
}

func (s *session) setExceptions(ctx context.Context) error {
	offered := map[string]bool{}
	var names []string
	for _, f := range s.caps.ExceptionBreakpointFilters {
		offered[f.Filter] = true
		if len(s.opt.CatchFilters) == 0 && f.Default {
			names = append(names, f.Filter)
		}
	}
	for _, n := range s.opt.CatchFilters {
		if !offered[n] {
			s.warn("exception filter %q is not one the adapter offers (%s)", n, strings.Join(filterNames(s.caps.ExceptionBreakpointFilters), ", "))
		}
		names = append(names, n)
	}
	if len(names) == 0 {
		s.warn("--catch set no exception filter: the adapter offers %s, none marked default; name one with --catch-filter",
			orNone(filterNames(s.caps.ExceptionBreakpointFilters)))
		return nil
	}
	if err := s.c.SetExceptionBreakpoints(ctx, names); err != nil {
		return err
	}
	s.t.ExceptionFilters = names
	return nil
}

func filterNames(fs []godap.ExceptionBreakpointsFilter) []string {
	out := make([]string, len(fs))
	for i, f := range fs {
		out[i] = f.Filter
	}
	return out
}

func orNone(names []string) string {
	if len(names) == 0 {
		return "no filters"
	}
	return strings.Join(names, ", ")
}

func (s *session) warn(format string, args ...any) {
	s.t.Warnings = append(s.t.Warnings, fmt.Sprintf(format, args...))
}

// loop records stops and output until the program ends or a limit does.
func (s *session) loop(ctx context.Context) {
	timeout := s.opt.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	maxStops := s.opt.MaxStops
	if maxStops <= 0 {
		maxStops = DefaultMaxStops
	}
	stops := 0
	for {
		select {
		case ev, ok := <-s.c.Events():
			if !ok {
				if s.t.End.ExitCode != nil {
					// Exited, then the adapter left without terminated.
					s.ended = true
					s.endWith(EndExited, "")
				} else {
					s.endWith(EndAdapterGone, "the adapter closed the connection")
				}
				return
			}
			switch ev.Name {
			case "stopped":
				var b godap.StoppedEventBody
				_ = json.Unmarshal(ev.Body, &b)
				stops++
				if err := s.recordStop(ctx, stops, b); err != nil {
					s.endWith(EndAdapterError, err.Error())
					return
				}
				if stops >= maxStops {
					s.t.Truncated.Stops = true
					s.endWith(EndMaxStops, fmt.Sprintf("stopped %d times (--max-stops)", stops))
					return
				}
				thread := s.t.Entries[len(s.t.Entries)-1].Thread
				if err := s.c.Continue(ctx, thread); err != nil {
					if ctx.Err() != nil {
						s.endWith(EndBudget, "the overall budget passed")
					} else {
						s.endWith(EndAdapterError, err.Error())
					}
					return
				}
			case "exited":
				var b godap.ExitedEventBody
				_ = json.Unmarshal(ev.Body, &b)
				code := b.ExitCode
				s.t.End.ExitCode = &code
			case "terminated":
				s.ended = true
				if s.t.End.ExitCode != nil {
					s.endWith(EndExited, "")
				} else {
					s.endWith(EndTerminated, "")
				}
				return
			default:
				s.event(ev)
			}
		case <-timer.C:
			s.endWith(EndTimeout, fmt.Sprintf("--timeout %s passed", timeout))
			return
		case <-ctx.Done():
			s.endWith(EndBudget, "the overall budget passed")
			return
		}
	}
}

func (s *session) endWith(reason, msg string) {
	s.t.End.Reason = reason
	s.t.End.Message = msg
}

// event handles output and capabilities; the rest need nothing.
func (s *session) event(ev Event) {
	switch ev.Name {
	case "output":
		var b godap.OutputEventBody
		_ = json.Unmarshal(ev.Body, &b)
		if b.Category == "telemetry" || b.Output == "" {
			return
		}
		text := b.Output
		if room := MaxOutput - s.outKept; len(text) > room {
			s.t.Truncated.OutputBytes += len(text) - max(room, 0)
			if room <= 0 {
				return
			}
			text = text[:room]
		}
		s.outKept += len(text)
		cat := b.Category
		if cat == "" {
			cat = "console"
		}
		// Adjacent output of one category reads as one piece.
		if n := len(s.t.Entries); n > 0 && s.t.Entries[n-1].Kind == KindOutput && s.t.Entries[n-1].Category == cat {
			s.t.Entries[n-1].Text += text
			return
		}
		s.t.Entries = append(s.t.Entries, Entry{Kind: KindOutput, Category: cat, Text: text})
	case "capabilities":
		var b godap.CapabilitiesEventBody
		if json.Unmarshal(ev.Body, &b) == nil {
			mergeCaps(&s.caps, b.Capabilities)
		}
	}
}

// mergeCaps applies a capabilities event: it only adds what the adapter
// newly supports.
func mergeCaps(c *godap.Capabilities, n godap.Capabilities) {
	c.SupportsConfigurationDoneRequest = c.SupportsConfigurationDoneRequest || n.SupportsConfigurationDoneRequest
	c.SupportsConditionalBreakpoints = c.SupportsConditionalBreakpoints || n.SupportsConditionalBreakpoints
	c.SupportsTerminateRequest = c.SupportsTerminateRequest || n.SupportsTerminateRequest
	c.SupportTerminateDebuggee = c.SupportTerminateDebuggee || n.SupportTerminateDebuggee
	if len(n.ExceptionBreakpointFilters) > 0 {
		c.ExceptionBreakpointFilters = n.ExceptionBreakpointFilters
	}
}

// recordStop records the stopping thread's stack, the top frame's locals,
// and the dumps.
func (s *session) recordStop(ctx context.Context, n int, b godap.StoppedEventBody) error {
	e := Entry{Kind: KindStop, Seq: n, Reason: b.Reason, Description: firstNonEmpty(b.Description, b.Text), Thread: b.ThreadId}
	if e.Thread == 0 {
		threads, err := s.c.Threads(ctx)
		if err != nil {
			return err
		}
		if len(threads) == 0 {
			return errors.New("stopped with no thread to inspect")
		}
		e.Thread = threads[0].Id
	}
	frames := s.opt.Frames
	if frames <= 0 {
		frames = DefaultFrames
	}
	stack, err := s.c.StackTrace(ctx, e.Thread, frames)
	if err != nil {
		return err
	}
	for i, f := range stack {
		if i >= frames {
			break
		}
		fr := Frame{Line: f.Line, Function: f.Name}
		if f.Source != nil {
			fr.Path = s.opt.Paths.Rel(f.Source.Path)
			if fr.Path == "" {
				fr.Path = f.Source.Name
			}
		}
		e.Frames = append(e.Frames, fr)
	}
	frameID := 0
	if len(stack) > 0 {
		frameID = stack[0].Id
		scopes, err := s.c.Scopes(ctx, frameID)
		if err != nil {
			return err
		}
		depth := s.opt.Depth
		if depth <= 0 {
			depth = DefaultDepth
		}
		for _, scope := range scopes {
			if scope.Expensive || scope.VariablesReference == 0 {
				continue
			}
			vars, err := s.variables(ctx, scope.VariablesReference, depth)
			if err != nil {
				return err
			}
			for i := range vars {
				vars[i].Scope = scope.Name
			}
			e.Locals = append(e.Locals, vars...)
		}
	}
	for _, expr := range s.opt.Dumps {
		e.Dumps = append(e.Dumps, s.evaluate(ctx, expr, frameID))
	}
	s.t.Entries = append(s.t.Entries, e)
	return nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// variables reads one container to depth levels.
func (s *session) variables(ctx context.Context, ref, depth int) ([]Variable, error) {
	vs, err := s.c.Variables(ctx, ref)
	if err != nil {
		return nil, err
	}
	out := make([]Variable, 0, min(len(vs), MaxVariables))
	for i, v := range vs {
		if i >= MaxVariables {
			s.t.Truncated.Variables += len(vs) - MaxVariables
			break
		}
		x := Variable{Name: v.Name, Value: s.cap(v.Value), Type: v.Type}
		if v.VariablesReference > 0 {
			if depth > 1 {
				kids, err := s.variables(ctx, v.VariablesReference, depth-1)
				if err != nil {
					return nil, err
				}
				x.Children = kids
			} else {
				s.t.Truncated.Variables++
			}
		}
		out = append(out, x)
	}
	return out, nil
}

// evaluate runs one --dump; an error is the dump's value, never fatal.
func (s *session) evaluate(ctx context.Context, expr string, frameID int) Dump {
	d := Dump{Expression: expr}
	body, err := s.c.Evaluate(ctx, expr, frameID)
	if err != nil {
		d.Error = s.cap(strings.TrimPrefix(err.Error(), "evaluate: "))
		return d
	}
	d.Value, d.Type = s.cap(body.Result), body.Type
	return d
}

// cap keeps a value to MaxValue characters and counts the cut.
func (s *session) cap(v string) string {
	if utf8.RuneCountInString(v) <= MaxValue {
		return v
	}
	s.t.Truncated.Values++
	r := []rune(v)
	return string(r[:MaxValue]) + fmt.Sprintf("... (+%d chars)", len(r)-MaxValue)
}

// teardown ends the debuggee for run (and attach with --terminate) and
// disconnects, even after the budget passed.
func (s *session) teardown(ctx context.Context) {
	tctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), teardownTimeout)
	defer cancel()
	kill := s.opt.Mode == ModeRun || s.opt.Terminate
	// Best effort: the adapter's tree is killed on Close anyway.
	if !s.ended && kill && s.caps.SupportsTerminateRequest {
		_ = s.c.Terminate(tctx)
	}
	_ = s.c.Disconnect(tctx, kill && !s.ended)
}

// Probe starts nothing: it initializes the adapter, returns its
// capabilities, and disconnects.
func Probe(ctx context.Context, c *Client, adapterID string) (godap.Capabilities, error) {
	caps, err := Initialize(ctx, c, adapterID)
	if err != nil {
		return caps, err
	}
	tctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), teardownTimeout)
	defer cancel()
	_ = c.Disconnect(tctx, false)
	return caps, nil
}
