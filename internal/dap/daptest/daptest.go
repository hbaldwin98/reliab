// Package daptest is a scripted fake debug adapter: it serves go-dap
// messages over a pipe, stdio, or a loopback TCP port and replays a
// Script's responses and events, so reliab's DAP client and debug sessions
// are tested without a real debugger, on every platform.
package daptest

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

	godap "github.com/google/go-dap"
)

// Script is what the fake adapter does.
type Script struct {
	Capabilities godap.Capabilities
	// Unverified maps a breakpoint line to the message the adapter gives
	// when it cannot place it; every other line is verified.
	Unverified map[int]string
	// Moved maps a requested breakpoint line to the line it lands on.
	Moved map[int]int
	// Steps play after configurationDone, in order. A Stop waits for
	// continue before the next step plays.
	Steps []Step
	// ExitCode is the program's, sent after the last step.
	ExitCode int
	// Hang keeps the program running after the last step: no exit.
	Hang bool
	// LaunchError fails the launch or attach request.
	LaunchError string
	// LaunchAfterConfigurationDone answers launch only after
	// configurationDone, as debugpy does; otherwise launch is answered
	// first and initialized follows, as delve does.
	LaunchAfterConfigurationDone bool
	// Reverse are reverse requests sent right after launch, such as
	// runInTerminal.
	Reverse []string
	// Noise are events the client does not handle, sent after initialize.
	Noise []string
	// Variables maps a variablesReference to its variables.
	Variables map[int][]godap.Variable
	// Eval maps an expression to its result; EvalErr to an error message.
	Eval    map[string]godap.EvaluateResponseBody
	EvalErr map[string]string
	// Silent drops the response to these commands, to test timeouts.
	Silent map[string]bool
	// NoTerminated sends exited without terminated, then hangs up.
	NoTerminated bool
	// CapabilitiesEvent, when set, is sent as a capabilities event after
	// initialize.
	CapabilitiesEvent *godap.Capabilities
	// Raw are messages sent as they are after initialize: stray responses,
	// unknown message types, malformed JSON.
	Raw []string
	// EmptyBodies answers these commands with no body at all.
	EmptyBodies map[string]bool
	// DropBreakpoints answers setBreakpoints with no breakpoints.
	DropBreakpoints bool
	// IgnoreLevels returns every frame whatever stackTrace's levels say.
	IgnoreLevels bool
	// NeverAnswerLaunch, with LaunchAfterConfigurationDone, never answers
	// the held launch.
	NeverAnswerLaunch bool
}

// Step is one piece of output or one stop.
type Step struct {
	Output   string
	Category string
	Stop     *Stop
}

// Stop is a stopped event and the state an inspection sees.
type Stop struct {
	Reason      string
	Description string
	// Thread is the stopped thread; 0 sends a stopped event without one.
	Thread int
	Frames []godap.StackFrame
	// Scopes default to one "Locals" scope with reference 1000.
	Scopes []godap.Scope
}

// Received is one message the fake adapter got from the client.
type Received struct {
	Seq       int
	Type      string
	Command   string
	Arguments json.RawMessage
	Success   bool
}

// Adapter is one served session.
type Adapter struct {
	sc   *Script
	conn io.ReadWriteCloser
	wmu  sync.Mutex
	seq  int

	mu       sync.Mutex
	received []Received

	step    int
	current *Stop
	held    int // seq of a launch held until configurationDone
	ended   bool
	done    chan struct{}
}

// Done is closed when the session ends.
func (a *Adapter) Done() <-chan struct{} { return a.done }

// Received returns what the client sent so far, in order.
func (a *Adapter) Received() []Received {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]Received(nil), a.received...)
}

// Commands returns the commands of the client's requests, in order.
func (a *Adapter) Commands() []string {
	var out []string
	for _, r := range a.Received() {
		if r.Type == "request" {
			out = append(out, r.Command)
		}
	}
	return out
}

// Args returns the arguments of every request with the command.
func (a *Adapter) Args(command string) []json.RawMessage {
	var out []json.RawMessage
	for _, r := range a.Received() {
		if r.Type == "request" && r.Command == command {
			out = append(out, r.Arguments)
		}
	}
	return out
}

// Serve plays sc over conn until the client disconnects or conn closes.
func Serve(conn io.ReadWriteCloser, sc *Script) *Adapter {
	a := &Adapter{sc: sc, conn: conn, done: make(chan struct{})}
	go a.serve()
	return a
}

// Pipe returns a connected client end and the adapter serving sc on the
// other end.
func Pipe(sc *Script) (io.ReadWriteCloser, *Adapter) {
	client, server := net.Pipe()
	return client, Serve(server, sc)
}

type message struct {
	Seq        int             `json:"seq"`
	Type       string          `json:"type"`
	Command    string          `json:"command"`
	Arguments  json.RawMessage `json:"arguments"`
	RequestSeq int             `json:"request_seq"`
	Success    bool            `json:"success"`
}

func (a *Adapter) serve() {
	defer close(a.done)
	defer func() { _ = a.conn.Close() }()
	r := bufio.NewReader(a.conn)
	for {
		raw, err := godap.ReadBaseMessage(r)
		if err != nil {
			return
		}
		var m message
		if json.Unmarshal(raw, &m) != nil {
			return
		}
		a.mu.Lock()
		a.received = append(a.received, Received{Seq: m.Seq, Type: m.Type, Command: m.Command, Arguments: m.Arguments, Success: m.Success})
		a.mu.Unlock()
		if m.Type != "request" {
			continue
		}
		if a.sc.Silent[m.Command] {
			continue
		}
		if !a.handle(m) {
			return
		}
	}
}

func (a *Adapter) send(v map[string]any) {
	a.wmu.Lock()
	defer a.wmu.Unlock()
	a.seq++
	v["seq"] = a.seq
	raw, _ := json.Marshal(v)
	_ = godap.WriteBaseMessage(a.conn, raw)
}

func (a *Adapter) respond(m message, body any) {
	v := map[string]any{"type": "response", "request_seq": m.Seq, "command": m.Command, "success": true}
	if body != nil && !a.sc.EmptyBodies[m.Command] {
		v["body"] = body
	}
	a.send(v)
}

func (a *Adapter) fail(m message, msg string) {
	a.send(map[string]any{
		"type": "response", "request_seq": m.Seq, "command": m.Command, "success": false, "message": msg,
		"body": map[string]any{"error": map[string]any{"id": 1, "format": msg, "showUser": true}},
	})
}

func (a *Adapter) event(name string, body any) {
	v := map[string]any{"type": "event", "event": name}
	if body != nil {
		v["body"] = body
	}
	a.send(v)
}

// handle answers one request; false ends the session.
func (a *Adapter) handle(m message) bool {
	sc := a.sc
	switch m.Command {
	case "initialize":
		a.respond(m, sc.Capabilities)
		for _, n := range sc.Noise {
			a.event(n, map[string]any{})
		}
		if sc.CapabilitiesEvent != nil {
			a.event("capabilities", map[string]any{"capabilities": sc.CapabilitiesEvent})
		}
		for _, raw := range sc.Raw {
			a.wmu.Lock()
			_ = godap.WriteBaseMessage(a.conn, []byte(raw))
			a.wmu.Unlock()
		}
	case "launch", "attach":
		if sc.LaunchError != "" {
			a.fail(m, sc.LaunchError)
			return true
		}
		for _, cmd := range sc.Reverse {
			a.send(map[string]any{"type": "request", "command": cmd, "arguments": map[string]any{"kind": "integrated", "args": []string{"prog"}}})
		}
		if sc.LaunchAfterConfigurationDone {
			a.held = m.Seq
			a.event("initialized", nil)
			return true
		}
		a.respond(m, nil)
		a.event("initialized", nil)
	case "setBreakpoints":
		var args godap.SetBreakpointsArguments
		_ = json.Unmarshal(m.Arguments, &args)
		out := make([]godap.Breakpoint, len(args.Breakpoints))
		for i, b := range args.Breakpoints {
			msg, bad := sc.Unverified[b.Line]
			line := b.Line
			if to, ok := sc.Moved[b.Line]; ok {
				line = to
			}
			out[i] = godap.Breakpoint{Id: i + 1, Verified: !bad, Message: msg, Line: line}
		}
		if sc.DropBreakpoints {
			out = []godap.Breakpoint{}
		}
		a.respond(m, godap.SetBreakpointsResponseBody{Breakpoints: out})
	case "setExceptionBreakpoints":
		a.respond(m, nil)
	case "configurationDone":
		a.respond(m, nil)
		if a.held != 0 && !sc.NeverAnswerLaunch {
			a.respond(message{Seq: a.held, Command: "launch"}, nil)
			a.held = 0
		}
		a.play()
	case "threads":
		a.respond(m, godap.ThreadsResponseBody{Threads: []godap.Thread{{Id: 1, Name: "main"}, {Id: 2, Name: "worker"}}})
	case "stackTrace":
		var args godap.StackTraceArguments
		_ = json.Unmarshal(m.Arguments, &args)
		var frames []godap.StackFrame
		if a.current != nil {
			frames = a.current.Frames
		}
		if !sc.IgnoreLevels && args.Levels > 0 && len(frames) > args.Levels {
			frames = frames[:args.Levels]
		}
		a.respond(m, godap.StackTraceResponseBody{StackFrames: frames, TotalFrames: len(frames)})
	case "scopes":
		scopes := []godap.Scope{{Name: "Locals", VariablesReference: 1000}}
		if a.current != nil && a.current.Scopes != nil {
			scopes = a.current.Scopes
		}
		a.respond(m, godap.ScopesResponseBody{Scopes: scopes})
	case "variables":
		var args godap.VariablesArguments
		_ = json.Unmarshal(m.Arguments, &args)
		vs := sc.Variables[args.VariablesReference]
		if vs == nil {
			vs = []godap.Variable{}
		}
		a.respond(m, godap.VariablesResponseBody{Variables: vs})
	case "evaluate":
		var args godap.EvaluateArguments
		_ = json.Unmarshal(m.Arguments, &args)
		if msg, ok := sc.EvalErr[args.Expression]; ok {
			a.fail(m, msg)
		} else if r, ok := sc.Eval[args.Expression]; ok {
			a.respond(m, r)
		} else {
			a.fail(m, "could not evaluate "+args.Expression)
		}
	case "continue":
		a.respond(m, godap.ContinueResponseBody{AllThreadsContinued: true})
		a.play()
	case "next", "stepIn", "stepOut", "pause":
		a.respond(m, nil)
	case "terminate":
		a.respond(m, nil)
		a.finish()
	case "disconnect":
		a.respond(m, nil)
		return false
	default:
		a.fail(m, "unsupported request "+m.Command)
	}
	return true
}

// play sends steps until a stop or the end.
func (a *Adapter) play() {
	a.current = nil
	for a.step < len(a.sc.Steps) {
		st := a.sc.Steps[a.step]
		a.step++
		if st.Stop != nil {
			a.current = st.Stop
			body := map[string]any{"reason": st.Stop.Reason, "allThreadsStopped": true}
			if st.Stop.Description != "" {
				body["description"] = st.Stop.Description
			}
			if st.Stop.Thread != 0 {
				body["threadId"] = st.Stop.Thread
			}
			a.event("stopped", body)
			return
		}
		cat := st.Category
		if cat == "" {
			cat = "stdout"
		}
		a.event("output", map[string]any{"category": cat, "output": st.Output})
	}
	if !a.sc.Hang {
		a.finish()
	}
}

func (a *Adapter) finish() {
	if a.ended {
		return
	}
	a.ended = true
	a.event("exited", map[string]any{"exitCode": a.sc.ExitCode})
	if a.sc.NoTerminated {
		_ = a.conn.Close()
		return
	}
	a.event("terminated", nil)
}

// ChildArg is the first argument that makes a test binary act as the fake
// adapter (see Main).
const ChildArg = "daptest-adapter"

// Main makes a test binary a fake adapter when it is run as
//
//	<test binary> daptest-adapter stdio <script>
//	<test binary> daptest-adapter tcp <port> <script> [ready line [delay ms]]
//
// With a delay, the adapter waits that long before it listens and prints
// the ready line.
//
// Call it first in TestMain; it returns when the binary is not a child.
func Main(scripts map[string]*Script) {
	if len(os.Args) < 4 || os.Args[1] != ChildArg {
		return
	}
	os.Exit(child(os.Args[2:], scripts))
}

func child(args []string, scripts map[string]*Script) int {
	fmt.Fprintln(os.Stderr, "fake adapter starting")
	switch args[0] {
	case "stdio":
		sc := scripts[args[1]]
		if sc == nil {
			fmt.Fprintln(os.Stderr, "no script", args[1])
			return 2
		}
		a := Serve(stdio{}, sc)
		<-a.Done()
		return 0
	case "tcp":
		if len(args) < 3 {
			return 2
		}
		port, err := strconv.Atoi(args[1])
		sc := scripts[args[2]]
		if err != nil || sc == nil {
			fmt.Fprintln(os.Stderr, "bad tcp arguments", args)
			return 2
		}
		if len(args) > 4 {
			ms, _ := strconv.Atoi(args[4])
			time.Sleep(time.Duration(ms) * time.Millisecond)
		}
		l, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		if len(args) > 3 {
			fmt.Fprintln(os.Stdout, args[3])
		}
		conn, err := l.Accept()
		_ = l.Close()
		if err != nil {
			return 2
		}
		a := Serve(conn, sc)
		<-a.Done()
		return 0
	}
	return 2
}

// stdio is the child's stdin and stdout as one connection.
type stdio struct{}

func (stdio) Read(b []byte) (int, error)  { return os.Stdin.Read(b) }
func (stdio) Write(b []byte) (int, error) { return os.Stdout.Write(b) }
func (stdio) Close() error                { return os.Stdout.Close() }
