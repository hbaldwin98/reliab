package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/hbaldwin98/reliab/internal/adapters"
	"github.com/hbaldwin98/reliab/internal/proctree"
	"github.com/hbaldwin98/reliab/internal/report"
)

// LogDir holds each step's log, relative to the repo root.
const LogDir = ".reliab/run"

// Status is a step's outcome.
type Status string

const (
	Passed    Status = "passed"
	Failed    Status = "failed"
	Blocked   Status = "blocked" // an after failed, so it did not run
	TimedOut  Status = "timed out"
	Cancelled Status = "cancelled"
)

// StepResult is one step's outcome.
type StepResult struct {
	Step     *Step
	Status   Status
	ExitCode int
	Elapsed  time.Duration
	Log      string
	// BlockedBy names the after that failed.
	BlockedBy string
	Findings  []report.Finding
	// Outside counts findings dropped because they fall outside the target.
	Outside int
}

// Options configure a run.
type Options struct {
	Root  string
	Shell string
	// Parallel caps steps running at once; 0 means the CPU count.
	Parallel int
	// Progress, when set, gets a line as each step starts and ends.
	Progress io.Writer
	Now      func() time.Time
}

// Run executes a plan. Steps whose afters passed start as soon as a slot is
// free; an exclusive step waits until nothing runs and holds the machine.
// A step whose after did not pass is blocked, not run.
func Run(ctx context.Context, p *Plan, opt Options) []*StepResult {
	if opt.Parallel <= 0 {
		opt.Parallel = runtime.NumCPU()
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	_ = os.MkdirAll(filepath.Join(opt.Root, LogDir), 0o755)
	env := append(os.Environ(),
		"RELIAB_CONTEXT="+p.Context,
		"RELIAB_BASE="+p.Base,
		"RELIAB_TARGET_FILES="+strings.Join(p.Target.Files, "\n"),
		"RELIAB_TARGET_PROJECTS="+strings.Join(p.Target.Projects, "\n"),
		"RELIAB_TARGET_TESTS="+strings.Join(p.Target.Tests, "\n"),
	)

	results := map[string]*StepResult{}
	done := make(chan *StepResult)
	running, exclusive := 0, false
	pending := append([]*Step(nil), p.Steps...)

	// next returns the next step to start and the steps that became blocked.
	next := func() (*Step, []*StepResult) {
		var blocked []*StepResult
		for i := 0; i < len(pending); i++ {
			s := pending[i]
			wait, bad := false, ""
			for _, a := range s.After {
				r := results[a]
				switch {
				case r == nil:
					wait = true
				case r.Status != Passed:
					bad = a
				}
			}
			if bad != "" {
				blocked = append(blocked, &StepResult{Step: s, Status: Blocked, BlockedBy: bad})
				pending = append(pending[:i], pending[i+1:]...)
				i--
				continue
			}
			if wait || exclusive || running >= opt.Parallel || (s.Exclusive && running > 0) {
				continue
			}
			pending = append(pending[:i], pending[i+1:]...)
			return s, blocked
		}
		return nil, blocked
	}

	for {
		for {
			s, blocked := next()
			for _, b := range blocked {
				results[b.Step.Name] = b
				progress(opt.Progress, "%-10s blocked: %s did not pass", b.Step.Name, b.BlockedBy)
			}
			if s == nil {
				if len(blocked) > 0 {
					continue // a block can unblock a later check
				}
				break
			}
			running++
			if s.Exclusive {
				exclusive = true
			}
			progress(opt.Progress, "%-10s started", s.Name)
			go func(s *Step) { done <- runStep(ctx, p, s, opt, env) }(s)
		}
		idle := running == 0
		if idle {
			break
		}
		r := <-done
		running--
		if r.Step.Exclusive {
			exclusive = false
		}
		results[r.Step.Name] = r
		progress(opt.Progress, "%-10s %s in %s", r.Step.Name, r.Status, r.Elapsed.Round(100*time.Millisecond))
	}
	out := make([]*StepResult, 0, len(p.Steps))
	for _, s := range p.Steps {
		out = append(out, results[s.Name])
	}
	return out
}

func progress(w io.Writer, format string, a ...any) {
	if w != nil {
		fmt.Fprintf(w, format+"\n", a...)
	}
}

func runStep(ctx context.Context, p *Plan, s *Step, opt Options, env []string) *StepResult {
	res := &StepResult{Step: s, Log: LogDir + "/" + s.Name + ".log"}
	if s.Output != nil {
		for _, f := range outputFiles(opt.Root, s.Output.File) {
			if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
				res.Status, res.ExitCode = Failed, -1
				res.Findings = []report.Finding{finding(s.Name, "cannot remove the last run's output: "+err.Error())}
				return res
			}
		}
	}
	logPath := filepath.Join(opt.Root, filepath.FromSlash(res.Log))
	logFile, err := os.Create(logPath)
	if err != nil {
		res.Status, res.ExitCode = Failed, -1
		res.Findings = []report.Finding{finding(s.Name, "cannot create log: "+err.Error())}
		return res
	}
	defer func() { _ = logFile.Close() }()

	runCtx := ctx
	if s.Timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, s.Timeout)
		defer cancel()
	}
	started := opt.Now()
	cmd := exec.CommandContext(runCtx, opt.Shell, "-c", s.Line)
	cmd.Dir = opt.Root
	cmd.Env = env
	cmd.Stdout, cmd.Stderr = logFile, logFile
	cmd.WaitDelay = 5 * time.Second
	afterStart, cleanup := proctree.Track(cmd)
	runErr := cmd.Start()
	if runErr == nil {
		if err := afterStart(); err != nil {
			fmt.Fprintf(logFile, "reliab: cannot track process tree: %v\n", err)
		}
		runErr = cmd.Wait()
	}
	cleanup()
	res.Elapsed = opt.Now().Sub(started)

	switch {
	case ctx.Err() != nil:
		res.Status = Cancelled
		return res
	case runCtx.Err() != nil:
		res.Status, res.ExitCode = TimedOut, -1
		res.Findings = []report.Finding{finding(s.Name, fmt.Sprintf("timed out after %s", s.Timeout))}
		return res
	}
	var exitErr *exec.ExitError
	switch {
	case runErr == nil:
	case errors.As(runErr, &exitErr):
		res.ExitCode = exitErr.ExitCode()
	default:
		res.Status, res.ExitCode = Failed, -1
		res.Findings = []report.Finding{finding(s.Name, "cannot run: "+runErr.Error())}
		return res
	}
	_ = logFile.Sync()
	in := adapters.Input{Command: s.Name, Root: opt.Root, ExitCode: res.ExitCode, LogTail: tailFile(logPath, 30)}
	if s.Output != nil {
		in.Format, in.Display, in.Declared = s.Output.Parse, s.Output.File, true
		in.Outputs = outputFiles(opt.Root, s.Output.File)
	}
	for _, f := range adapters.Findings(in).Findings {
		if file, line, ok := location(f.Location); ok && !p.Target.Contains(file, line) {
			res.Outside++
			continue
		}
		res.Findings = append(res.Findings, f)
	}
	res.Status = Passed
	if res.ExitCode != 0 || len(res.Findings) > 0 {
		res.Status = Failed
	}
	return res
}

var locationRe = regexp.MustCompile(`^(.+?):(\d+)`)

// location splits a finding's "file:line[:col]" location.
func location(loc string) (string, int, bool) {
	if loc == "" {
		return "", 0, false
	}
	m := locationRe.FindStringSubmatch(loc)
	if m == nil {
		return filepath.ToSlash(loc), 0, true
	}
	n, _ := strconv.Atoi(m[2])
	return filepath.ToSlash(m[1]), n, true
}

// outputFiles returns the absolute paths matching an output path or glob
// (** crosses directories), relative to root.
func outputFiles(root, pattern string) []string {
	rels, _ := doublestar.Glob(os.DirFS(root), pattern, doublestar.WithFilesOnly())
	out := make([]string, len(rels))
	for i, r := range rels {
		out[i] = filepath.Join(root, filepath.FromSlash(r))
	}
	return out
}

func finding(cmd, msg string) report.Finding {
	return report.Finding{Kind: "unclassified", Command: cmd, Message: msg, Status: "open"}
}

func tailFile(path string, n int) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
