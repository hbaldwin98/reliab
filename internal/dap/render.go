package dap

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// DefaultWidth is where the text rendering cuts a long value.
const DefaultWidth = 160

// RenderSetup writes what was set before the program ran: the adapter, the
// breakpoints with their verified state, the exception filters, and the
// warnings.
func RenderSetup(w io.Writer, t *Transcript) {
	port := ""
	if t.Adapter.Port != 0 {
		port = fmt.Sprintf(", port %d", t.Adapter.Port)
	}
	fmt.Fprintf(w, "adapter: %s (%s%s)\n", t.Adapter.Run, t.Adapter.Transport, port)
	for _, b := range t.Breakpoints {
		state := "verified"
		if !b.Verified {
			state = "NOT verified"
		}
		if b.ActualLine != 0 {
			state += fmt.Sprintf(" at line %d", b.ActualLine)
		}
		fmt.Fprintf(w, "breakpoint %s: %s\n", b.Spec, state)
	}
	if len(t.ExceptionFilters) > 0 {
		fmt.Fprintf(w, "exception filters: %s\n", strings.Join(t.ExceptionFilters, ", "))
	}
	for _, msg := range t.Warnings {
		fmt.Fprintf(w, "warning: %s\n", msg)
	}
}

// RenderRun writes the stops and output in order, then what was cut and
// the one-line tail. width cuts long values (0: DefaultWidth).
func RenderRun(w io.Writer, t *Transcript, width int, path string) {
	if width <= 0 {
		width = DefaultWidth
	}
	for _, e := range t.Entries {
		switch e.Kind {
		case KindStop:
			renderStop(w, e, width)
		case KindOutput:
			renderOutput(w, e)
		}
	}
	for _, r := range t.ReverseRequests {
		fmt.Fprintf(w, "the adapter asked for %s (seq %d); reliab declined it\n", r.Command, r.Seq)
	}
	if len(t.Ignored) > 0 {
		fmt.Fprintf(w, "ignored from the adapter: %s\n", strings.Join(IgnoredText(t.Ignored), ", "))
	}
	if cut := truncatedText(t.Truncated); cut != "" {
		fmt.Fprintf(w, "truncated: %s\n", cut)
	}
	fmt.Fprintln(w, tail(t, path))
}

// Render writes the whole text rendering.
func Render(w io.Writer, t *Transcript, width int, path string) {
	RenderSetup(w, t)
	RenderRun(w, t, width, path)
}

func renderStop(w io.Writer, e Entry, width int) {
	head := fmt.Sprintf("== stop %d: %s", e.Seq, e.Reason)
	if e.Description != "" && e.Description != e.Reason {
		head += " (" + oneLine(e.Description) + ")"
	}
	fmt.Fprintf(w, "%s, thread %d\n", head, e.Thread)
	for i, f := range e.Frames {
		lead := "     "
		if i == 0 {
			lead = "  at "
		}
		loc := f.Path
		if loc == "" {
			loc = "?"
		}
		fmt.Fprintf(w, "%s%s:%d %s\n", lead, loc, f.Line, f.Function)
	}
	// One heading per scope when the adapter gives several (debugpy's
	// Locals and Globals); one plain heading otherwise.
	scopes := map[string]bool{}
	for _, v := range e.Locals {
		scopes[v.Scope] = true
	}
	for i, v := range e.Locals {
		if i == 0 || v.Scope != e.Locals[i-1].Scope {
			if len(scopes) > 1 {
				fmt.Fprintf(w, "  locals (%s):\n", v.Scope)
			} else if i == 0 {
				fmt.Fprintln(w, "  locals:")
			}
		}
		renderVar(w, v, "    ", width)
	}
	if len(e.Dumps) > 0 {
		fmt.Fprintln(w, "  dumps:")
		for _, d := range e.Dumps {
			val := d.Value
			if d.Error != "" {
				val = "<error: " + d.Error + ">"
			}
			fmt.Fprintf(w, "    %s = %s\n", d.Expression, cut(val, width))
		}
	}
}

func renderVar(w io.Writer, v Variable, indent string, width int) {
	fmt.Fprintf(w, "%s%s = %s\n", indent, v.Name, cut(v.Value, width))
	for _, c := range v.Children {
		renderVar(w, c, indent+"  ", width)
	}
}

func renderOutput(w io.Writer, e Entry) {
	text := strings.TrimSuffix(e.Text, "\n")
	for _, line := range strings.Split(text, "\n") {
		fmt.Fprintf(w, "[%s] %s\n", e.Category, strings.TrimSuffix(line, "\r"))
	}
}

// cut keeps a value on one line and to width characters, saying so when it
// cuts.
func cut(v string, width int) string {
	v = oneLine(v)
	n := utf8.RuneCountInString(v)
	if n <= width {
		return v
	}
	r := []rune(v)
	return string(r[:width]) + fmt.Sprintf("... [cut, %d more chars]", n-width)
}

func oneLine(s string) string {
	return strings.NewReplacer("\r\n", `\n`, "\n", `\n`, "\r", `\r`).Replace(s)
}

func truncatedText(t Truncated) string {
	var parts []string
	if t.Stops {
		parts = append(parts, "stops past --max-stops")
	}
	if t.Variables > 0 {
		parts = append(parts, fmt.Sprintf("%d variable(s) not expanded (past --depth or the per-scope cap)", t.Variables))
	}
	if t.Values > 0 {
		parts = append(parts, fmt.Sprintf("%d value(s) cut at %d chars", t.Values, MaxValue))
	}
	if t.OutputBytes > 0 {
		parts = append(parts, fmt.Sprintf("%d byte(s) of program output past %d", t.OutputBytes, MaxOutput))
	}
	return strings.Join(parts, "; ")
}

// tail is the one-line summary: how it ended, the exit code, the path.
func tail(t *Transcript, path string) string {
	end := t.End.Reason
	if t.End.ExitCode != nil {
		end += fmt.Sprintf(", program exit code %d", *t.End.ExitCode)
	}
	if t.End.Message != "" {
		end += ": " + oneLine(t.End.Message)
	}
	n := len(t.Stops())
	s := fmt.Sprintf("end: %s; %d stop(s), %.1fs; exit %d", end, n, t.DurationSec, t.ExitCode)
	if path != "" {
		s += "; transcript " + path
	}
	return s
}
