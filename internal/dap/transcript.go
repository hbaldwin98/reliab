package dap

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// TranscriptVersion is the schemaVersion of schemas/debug-transcript.schema.json.
const TranscriptVersion = "1.0.0"

// Dir holds the transcripts, under the repository root.
const Dir = ".reliab/debug"

// Modes.
const (
	ModeRun    = "run"
	ModeAttach = "attach"
)

// End reasons.
const (
	EndExited       = "exited"        // the debuggee exited
	EndTerminated   = "terminated"    // the adapter ended the session
	EndMaxStops     = "max-stops"     // --max-stops was reached
	EndTimeout      = "timeout"       // --timeout passed
	EndBudget       = "budget"        // the overall budget passed
	EndAdapterError = "adapter-error" // a request failed mid-run
	EndAdapterGone  = "adapter-gone"  // the adapter closed the connection
)

// Transcript is one debug run or attach, as written to .reliab/debug/.
type Transcript struct {
	SchemaVersion    string             `json:"schemaVersion"`
	Mode             string             `json:"mode"`
	StartedAt        string             `json:"startedAt"`
	DurationSec      float64            `json:"durationSec"`
	Adapter          AdapterInfo        `json:"adapter"`
	Request          RequestInfo        `json:"request"`
	Breakpoints      []BreakpointRecord `json:"breakpoints"`
	ExceptionFilters []string           `json:"exceptionFilters"`
	Warnings         []string           `json:"warnings,omitempty"`
	Entries          []Entry            `json:"entries"`
	ReverseRequests  []ReverseRequest   `json:"reverseRequests,omitempty"`
	Ignored          map[string]int     `json:"ignored,omitempty"`
	End              End                `json:"end"`
	Truncated        Truncated          `json:"truncated"`
	// ExitCode is reliab's own exit code for the run.
	ExitCode int `json:"exitCode"`
}

// AdapterInfo is the adapter as started.
type AdapterInfo struct {
	Run       string `json:"run"`
	Transport string `json:"transport"`
	Port      int    `json:"port,omitempty"`
}

// RequestInfo is the launch or attach request as sent.
type RequestInfo struct {
	Command   string `json:"command"`
	Arguments any    `json:"arguments"`
}

// BreakpointRecord is one --break and what the adapter made of it.
type BreakpointRecord struct {
	Spec      string `json:"spec"`
	Path      string `json:"path"`
	Line      int    `json:"line"`
	Condition string `json:"condition,omitempty"`
	Verified  bool   `json:"verified"`
	// ActualLine is where the adapter placed it, when not Line.
	ActualLine int    `json:"actualLine,omitempty"`
	Message    string `json:"message,omitempty"`
}

// Entry is a stop or a piece of program output, in arrival order.
type Entry struct {
	Kind string `json:"kind"` // stop or output
	// stop
	Seq         int        `json:"seq,omitempty"`
	Reason      string     `json:"reason,omitempty"`
	Description string     `json:"description,omitempty"`
	Thread      int        `json:"thread,omitempty"`
	Frames      []Frame    `json:"frames,omitempty"`
	Locals      []Variable `json:"locals,omitempty"`
	Dumps       []Dump     `json:"dumps,omitempty"`
	// output
	Category string `json:"category,omitempty"`
	Text     string `json:"text,omitempty"`
}

// Entry kinds.
const (
	KindStop   = "stop"
	KindOutput = "output"
)

// Frame is one stack frame.
type Frame struct {
	Path     string `json:"path,omitempty"`
	Line     int    `json:"line"`
	Function string `json:"function"`
}

// Variable is a local, with its children to --depth.
type Variable struct {
	Name     string     `json:"name"`
	Value    string     `json:"value"`
	Type     string     `json:"type,omitempty"`
	Scope    string     `json:"scope,omitempty"`
	Children []Variable `json:"children,omitempty"`
}

// Dump is one --dump expression evaluated in the top frame.
type Dump struct {
	Expression string `json:"expression"`
	Value      string `json:"value,omitempty"`
	Type       string `json:"type,omitempty"`
	Error      string `json:"error,omitempty"`
}

// End says how the session ended.
type End struct {
	Reason string `json:"reason"`
	// ExitCode is the debuggee's, when it exited.
	ExitCode *int   `json:"exitCode,omitempty"`
	Message  string `json:"message,omitempty"`
}

// Truncated counts what the transcript left out.
type Truncated struct {
	// Stops is true when --max-stops ended the run.
	Stops bool `json:"stops"`
	// Variables were not expanded: past --depth or past the per-container cap.
	Variables int `json:"variables"`
	// Values were cut at the value cap.
	Values int `json:"values"`
	// OutputBytes of program output were dropped past the output cap.
	OutputBytes int `json:"outputBytes"`
}

// Stops returns the transcript's stop entries.
func (t *Transcript) Stops() []Entry {
	var out []Entry
	for _, e := range t.Entries {
		if e.Kind == KindStop {
			out = append(out, e)
		}
	}
	return out
}

// ComputeExitCode is reliab's exit code for the transcript: 1 for an
// exception stop or a stop, timeout, or adapter limit; 3 when the overall
// budget passed; 0 when the program ran to its end.
func ComputeExitCode(t *Transcript) int {
	for _, e := range t.Entries {
		if e.Kind == KindStop && e.Reason == "exception" {
			return 1
		}
	}
	switch t.End.Reason {
	case EndExited, EndTerminated:
		return 0
	case EndBudget:
		return 3
	}
	return 1
}

// Write saves the transcript as .reliab/debug/<yyyymmdd-hhmmss>.json and
// copies it to last.json. It returns the repo-relative slash path.
func Write(root string, t *Transcript, now time.Time) (string, error) {
	dir := filepath.Join(root, filepath.FromSlash(Dir))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	raw, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return "", err
	}
	raw = append(raw, '\n')
	stamp := now.Format("20060102-150405")
	name := stamp + ".json"
	for i := 2; ; i++ {
		f, err := os.OpenFile(filepath.Join(dir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if os.IsExist(err) {
			name = fmt.Sprintf("%s-%d.json", stamp, i)
			continue
		}
		if err != nil {
			return "", err
		}
		_, werr := f.Write(raw)
		if err := errors.Join(werr, f.Close()); err != nil {
			return "", err
		}
		break
	}
	if err := os.WriteFile(filepath.Join(dir, "last.json"), raw, 0o644); err != nil {
		return "", err
	}
	return Dir + "/" + name, nil
}

// PathMapper makes adapter paths repo-relative and slash-separated.
type PathMapper struct {
	// Root is the repository root, absolute.
	Root string
	// Base is source_root resolved against Root: where the adapter's
	// relative paths start and, when outside Root, the prefix its
	// absolute paths carry.
	Base string
}

// NewPathMapper resolves sourceRoot (default ".") against root.
func NewPathMapper(root, sourceRoot string) PathMapper {
	if sourceRoot == "" {
		sourceRoot = "."
	}
	base := sourceRoot
	if !isAbs(sourceRoot) {
		base = filepath.Join(root, filepath.FromSlash(sourceRoot))
	}
	return PathMapper{Root: root, Base: base}
}

// Rel maps an adapter path: under the root it becomes repo-relative; under
// a base outside the root, base-relative; anything else (the standard
// library, a module cache) stays absolute, slash-separated.
func (m PathMapper) Rel(p string) string {
	if p == "" {
		return ""
	}
	s := slash(p)
	if !isAbs(p) {
		s = slash(filepath.Join(m.Base, filepath.FromSlash(s)))
	}
	for _, root := range candidates(m.Root) {
		if rel, ok := under(s, root); ok {
			return rel
		}
	}
	for _, base := range candidates(m.Base) {
		if rel, ok := under(s, base); ok {
			return rel
		}
	}
	// The adapter may spell the root through a symlink the root itself was
	// resolved past (macOS: /var and /private/var); resolve its path too.
	if r, ok := resolved(s); ok && r != s {
		return m.Rel(r)
	}
	return s
}

// resolved is p with symlinks resolved, or, when p is not on disk, its
// directory resolved and its name kept.
func resolved(p string) (string, bool) {
	if r, err := filepath.EvalSymlinks(filepath.FromSlash(p)); err == nil {
		return slash(r), true
	}
	dir, name := path.Split(p)
	if dir == "" || dir == p {
		return "", false
	}
	r, err := filepath.EvalSymlinks(filepath.FromSlash(dir))
	if err != nil {
		return "", false
	}
	return slash(r) + "/" + name, true
}

// Abs is the absolute native path for a repo-relative or absolute file.
func (m PathMapper) Abs(p string) string {
	if isAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(m.Root, filepath.FromSlash(p))
}

// candidates are the spellings of dir an adapter may report: as given,
// and with symlinks or 8.3 short names resolved (the two may be equal).
func candidates(dir string) []string {
	out := []string{slash(filepath.Clean(dir))}
	if r, err := filepath.EvalSymlinks(dir); err == nil {
		out = append(out, slash(r))
	}
	return out
}

// under returns p relative to dir when p is inside it. Paths with a drive
// letter compare case-insensitively, as Windows does.
func under(p, dir string) (string, bool) {
	dir = strings.TrimSuffix(dir, "/")
	cp, cd := p, dir
	if hasDrive(p) {
		cp, cd = strings.ToLower(p), strings.ToLower(dir)
	}
	if cp == cd {
		return ".", true
	}
	if strings.HasPrefix(cp, cd+"/") {
		return p[len(dir)+1:], true
	}
	return "", false
}

// slash turns backslashes into slashes whatever the OS: an adapter on
// Windows reports C:\x\y.go even to a client that expects slashes.
func slash(p string) string { return strings.ReplaceAll(p, `\`, "/") }

func hasDrive(p string) bool {
	return len(p) >= 3 && p[1] == ':' && (p[2] == '\\' || p[2] == '/') &&
		(p[0] >= 'a' && p[0] <= 'z' || p[0] >= 'A' && p[0] <= 'Z')
}

func isAbs(p string) bool {
	return filepath.IsAbs(p) || hasDrive(p) || strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\\`)
}
