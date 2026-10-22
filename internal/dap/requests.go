package dap

import (
	"context"

	godap "github.com/google/go-dap"
)

// The requests phase 1 implements (knowledge/decisions/debug-adapter-seam.md).
// initialize, launch, and attach are sent by Initialize and Session; the
// stepping requests are for the interactive sessions of phase 2.

// SetBreakpoints replaces the breakpoints in one source file.
func (c *Client) SetBreakpoints(ctx context.Context, args godap.SetBreakpointsArguments) (godap.SetBreakpointsResponseBody, error) {
	var b godap.SetBreakpointsResponseBody
	return b, c.Do(ctx, "setBreakpoints", args, &b)
}

// SetExceptionBreakpoints sets the exception filters.
func (c *Client) SetExceptionBreakpoints(ctx context.Context, filters []string) error {
	return c.Do(ctx, "setExceptionBreakpoints", godap.SetExceptionBreakpointsArguments{Filters: filters}, nil)
}

// ConfigurationDone ends the configuration sequence.
func (c *Client) ConfigurationDone(ctx context.Context) error {
	return c.Do(ctx, "configurationDone", nil, nil)
}

// Threads lists the debuggee's threads.
func (c *Client) Threads(ctx context.Context) ([]godap.Thread, error) {
	var b godap.ThreadsResponseBody
	err := c.Do(ctx, "threads", nil, &b)
	return b.Threads, err
}

// StackTrace returns up to levels frames of a thread (0: all).
func (c *Client) StackTrace(ctx context.Context, thread, levels int) ([]godap.StackFrame, error) {
	var b godap.StackTraceResponseBody
	err := c.Do(ctx, "stackTrace", godap.StackTraceArguments{ThreadId: thread, Levels: levels}, &b)
	return b.StackFrames, err
}

// Scopes returns a frame's scopes.
func (c *Client) Scopes(ctx context.Context, frame int) ([]godap.Scope, error) {
	var b godap.ScopesResponseBody
	err := c.Do(ctx, "scopes", godap.ScopesArguments{FrameId: frame}, &b)
	return b.Scopes, err
}

// Variables returns a container's variables.
func (c *Client) Variables(ctx context.Context, ref int) ([]godap.Variable, error) {
	var b godap.VariablesResponseBody
	err := c.Do(ctx, "variables", godap.VariablesArguments{VariablesReference: ref}, &b)
	return b.Variables, err
}

// Evaluate evaluates an expression in a frame (0: no frame) as a watch.
func (c *Client) Evaluate(ctx context.Context, expr string, frame int) (godap.EvaluateResponseBody, error) {
	var b godap.EvaluateResponseBody
	return b, c.Do(ctx, "evaluate", godap.EvaluateArguments{Expression: expr, FrameId: frame, Context: "watch"}, &b)
}

// Continue resumes a thread.
func (c *Client) Continue(ctx context.Context, thread int) error {
	return c.Do(ctx, "continue", godap.ContinueArguments{ThreadId: thread}, nil)
}

// Next steps over.
func (c *Client) Next(ctx context.Context, thread int) error {
	return c.Do(ctx, "next", godap.NextArguments{ThreadId: thread}, nil)
}

// StepIn steps into.
func (c *Client) StepIn(ctx context.Context, thread int) error {
	return c.Do(ctx, "stepIn", godap.StepInArguments{ThreadId: thread}, nil)
}

// StepOut steps out.
func (c *Client) StepOut(ctx context.Context, thread int) error {
	return c.Do(ctx, "stepOut", godap.StepOutArguments{ThreadId: thread}, nil)
}

// Pause suspends a thread.
func (c *Client) Pause(ctx context.Context, thread int) error {
	return c.Do(ctx, "pause", godap.PauseArguments{ThreadId: thread}, nil)
}

// Terminate asks the debuggee to end gracefully.
func (c *Client) Terminate(ctx context.Context) error {
	return c.Do(ctx, "terminate", godap.TerminateArguments{}, nil)
}

// Disconnect ends the session; terminate also ends the debuggee.
func (c *Client) Disconnect(ctx context.Context, terminate bool) error {
	return c.Do(ctx, "disconnect", godap.DisconnectArguments{TerminateDebuggee: terminate}, nil)
}
