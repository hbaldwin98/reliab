package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	godap "github.com/google/go-dap"
	"github.com/spf13/cobra"

	"github.com/hbaldwin98/reliab/internal/dap"
	"github.com/hbaldwin98/reliab/internal/profile"
)

// The debug commands, per knowledge/decisions/debug-adapter-seam.md.

// DefaultDebugBudget bounds a whole debug run or attach: adapter start,
// the build some adapters do in launch, the run, and teardown.
const DefaultDebugBudget = 10 * time.Minute

// adapterLog is where the adapter's own output goes during a session.
const adapterLog = dap.Dir + "/adapter.log"

// debugEnv is a loaded debug section with its placeholders' values.
type debugEnv struct {
	root string
	p    *profile.Profile
	vars map[string]string
	argv []string
	port int
}

// loadDebug reads the profile's debug section and fills the built-in
// placeholders; pid is 0 when not attaching.
func loadDebug(file string, sets []string, pid int) (*debugEnv, error) {
	root, err := repoRoot()
	if err != nil {
		return nil, withCode(ExitConfig, err)
	}
	p, err := profile.Load(filepath.Join(root, file))
	if err != nil {
		return nil, withCode(ExitConfig, err)
	}
	if p.Debug == nil {
		return nil, withCode(ExitConfig, fmt.Errorf("%s has no debug section: reliab docs debug has the block for each adapter; the profile is protected, so a person adds it", file))
	}
	vars, err := dap.ParseSets(sets)
	if err != nil {
		return nil, withCode(ExitConfig, err)
	}
	slashRoot := filepath.ToSlash(root)
	vars["cwd"], vars["root"] = slashRoot, slashRoot
	if pid > 0 {
		vars["pid"] = strconv.Itoa(pid)
	}
	env := &debugEnv{root: root, p: p, vars: vars}
	if p.Debug.Adapter.Transport == dap.TransportTCP {
		if env.port, err = dap.FreePort(); err != nil {
			return nil, withCode(ExitConfig, fmt.Errorf("no free loopback port: %w", err))
		}
		vars["port"] = strconv.Itoa(env.port)
	}
	words, err := dap.SplitCommand(p.Debug.Adapter.Run)
	if err != nil {
		return nil, withCode(ExitConfig, fmt.Errorf("debug.adapter.run: %w", err))
	}
	if len(words) == 0 {
		return nil, withCode(ExitConfig, errors.New("debug.adapter.run is empty"))
	}
	missing := map[string]bool{}
	for _, w := range words {
		env.argv = append(env.argv, dap.ExpandString(w, vars, missing))
	}
	if len(missing) > 0 {
		names := make([]string, 0, len(missing))
		for n := range missing {
			names = append(names, "{"+n+"}")
		}
		sort.Strings(names)
		return nil, withCode(ExitConfig, fmt.Errorf("unexpanded placeholder %s in debug.adapter.run; give each with --set name=value", strings.Join(names, ", ")))
	}
	return env, nil
}

// start starts the adapter with its output in .reliab/debug/adapter.log.
func (e *debugEnv) start(ctx context.Context) (*dap.Client, func(), error) {
	a := e.p.Debug.Adapter
	var ready *regexp.Regexp
	if a.Ready != "" {
		ready = regexp.MustCompile(a.Ready) // profile.Parse has compiled it
	}
	dir := filepath.Join(e.root, filepath.FromSlash(dap.Dir))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, withCode(ExitConfig, err)
	}
	log, err := os.Create(filepath.Join(e.root, filepath.FromSlash(adapterLog)))
	if err != nil {
		return nil, nil, withCode(ExitConfig, err)
	}
	c, err := dap.Start(ctx, dap.AdapterConfig{Argv: e.argv, Transport: a.Transport, Port: e.port, Ready: ready, Dir: e.root, Log: log})
	if err != nil {
		_ = log.Close()
		return nil, nil, err
	}
	return c, func() { _ = c.Close(); _ = log.Close() }, nil
}

func (e *debugEnv) adapterID() string {
	base := filepath.Base(filepath.FromSlash(e.argv[0]))
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// adapterFailure prints what the adapter said and returns exit 2.
func adapterFailure(cmd *cobra.Command, err error, output string) error {
	var se *dap.StartError
	if errors.As(err, &se) && output == "" {
		output = se.Output
	}
	if output = strings.TrimSpace(output); output != "" {
		w := cmd.ErrOrStderr()
		fmt.Fprintln(w, "adapter output:")
		for _, line := range strings.Split(output, "\n") {
			fmt.Fprintln(w, "  "+strings.TrimRight(line, "\r"))
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return withCode(ExitIncomplete, fmt.Errorf("the budget passed before the program ran: %w", err))
	}
	return withCode(ExitConfig, err)
}

func newDebugCmd() *cobra.Command {
	return group("debug", "Run a test or program under the profile's debug adapter and record what it shows",
		newDebugProbeCmd(), newDebugSessionCmd(dap.ModeRun), newDebugSessionCmd(dap.ModeAttach))
}

func newDebugProbeCmd() *cobra.Command {
	var file string
	var sets []string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "probe",
		Short: "Start the debug adapter, print its capabilities, and disconnect",
		Long: "Starts the profile's debug adapter, sends initialize, prints the capabilities that matter to\n" +
			"reliab debug run (exception breakpoint filters, conditional breakpoints, log points,\n" +
			"terminate), and disconnects. Run it after adding a debug section, and first when debug run\n" +
			"misbehaves. Exit 0 when the adapter answered; 2 with the adapter's output when it did not.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			env, err := loadDebug(file, sets, 0)
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()
			ctx, cancel := context.WithTimeout(ctx, time.Minute)
			defer cancel()
			c, closeAll, err := env.start(ctx)
			if err != nil {
				return adapterFailure(cmd, err, "")
			}
			caps, err := dap.Probe(ctx, c, env.adapterID())
			output := c.AdapterOutput()
			closeAll()
			if err != nil {
				return adapterFailure(cmd, err, output)
			}
			out := cmd.OutOrStdout()
			if asJSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(caps)
			}
			renderCaps(out, strings.Join(env.argv, " "), env.p.Debug.Adapter.Transport, caps)
			return nil
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", profile.DefaultPath, "profile, relative to the repo root")
	cmd.Flags().StringArrayVar(&sets, "set", nil, "fill a {name} placeholder in debug.adapter.run: name=value (repeat)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the adapter's capabilities as DAP JSON")
	return cmd
}

func renderCaps(w io.Writer, run, transport string, c godap.Capabilities) {
	yes := func(b bool) string {
		if b {
			return "yes"
		}
		return "no"
	}
	fmt.Fprintf(w, "adapter: %s (%s)\n", run, transport)
	if len(c.ExceptionBreakpointFilters) == 0 {
		fmt.Fprintln(w, "exception breakpoint filters: none (--catch sets nothing)")
	} else {
		fmt.Fprintln(w, "exception breakpoint filters (--catch sets the default ones; --catch-filter names others):")
		for _, f := range c.ExceptionBreakpointFilters {
			def := ""
			if f.Default {
				def = " (default)"
			}
			fmt.Fprintf(w, "  %s: %s%s\n", f.Filter, f.Label, def)
		}
	}
	fmt.Fprintf(w, "conditional breakpoints: %s\n", yes(c.SupportsConditionalBreakpoints))
	fmt.Fprintf(w, "hit-count conditions: %s\n", yes(c.SupportsHitConditionalBreakpoints))
	fmt.Fprintf(w, "log points: %s\n", yes(c.SupportsLogPoints))
	fmt.Fprintf(w, "function breakpoints: %s\n", yes(c.SupportsFunctionBreakpoints))
	fmt.Fprintf(w, "configurationDone: %s\n", yes(c.SupportsConfigurationDoneRequest))
	fmt.Fprintf(w, "terminate request: %s\n", yes(c.SupportsTerminateRequest))
	fmt.Fprintf(w, "terminate debuggee on disconnect: %s\n", yes(c.SupportTerminateDebuggee))
}

func newDebugSessionCmd(mode string) *cobra.Command {
	var file string
	var sets, breaks, dumps, catchFilters []string
	var catch, asJSON, terminate bool
	var frames, depth, maxStops, width, pid int
	var timeout, budget time.Duration
	block := "launch"
	if mode == dap.ModeAttach {
		block = "attach"
	}
	cmd := &cobra.Command{
		Use:   mode,
		Short: "Run a test or program under the debug adapter, record each stop, and write a transcript",
		Long: "Expands the profile's debug." + block + " block, starts the adapter, sets each --break (grouped per\n" +
			"file; 'file:line if cond' when the adapter supports conditions), and with --catch the adapter's\n" +
			"default exception filters. Each stop records the reason, the top --frames frames, the top frame's\n" +
			"locals to --depth, and each --dump evaluated there (an error is recorded, never fatal); then it\n" +
			"continues. It ends after --max-stops stops, after --timeout from the program's start, or when the\n" +
			"program exits. A --break the adapter does not verify is a warning before the program runs.\n" +
			"The transcript goes to " + dap.Dir + "/<yyyymmdd-hhmmss>.json and last.json; --json prints it.\n" +
			"Exit codes: 0 the program ran to its end with no exception stop, 1 an exception stop or a stop\n" +
			"or timeout limit ended it, 2 the profile, the adapter, or the " + block + " failed before the program\n" +
			"ran, 3 --budget passed.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if mode == dap.ModeAttach && pid <= 0 {
				return withCode(ExitConfig, errors.New("--pid is required: debug attach attaches only to a process you name"))
			}
			env, err := loadDebug(file, sets, pid)
			if err != nil {
				return err
			}
			raw := env.p.Debug.Launch
			if mode == dap.ModeAttach {
				raw = env.p.Debug.Attach
			}
			if raw == nil {
				return withCode(ExitConfig, fmt.Errorf("the profile's debug section has no %s block", block))
			}
			expanded, err := dap.Expand("debug."+block, raw, env.vars)
			if err != nil {
				return withCode(ExitConfig, err)
			}
			paths := dap.NewPathMapper(env.root, env.p.Debug.SourceRoot)
			opt := dap.Options{
				Mode: mode, Arguments: expanded.(map[string]any), Dumps: dumps,
				Catch: catch, CatchFilters: catchFilters, Frames: frames, Depth: depth, MaxStops: maxStops,
				Timeout: timeout, Terminate: terminate, Paths: paths, AdapterID: env.adapterID(),
			}
			for _, b := range breaks {
				br, err := dap.ParseBreak(b, paths)
				if err != nil {
					return withCode(ExitConfig, err)
				}
				opt.Breaks = append(opt.Breaks, br)
			}
			out := cmd.OutOrStdout()
			tr := &dap.Transcript{
				SchemaVersion: dap.TranscriptVersion,
				Adapter:       dap.AdapterInfo{Run: strings.Join(env.argv, " "), Transport: env.p.Debug.Adapter.Transport, Port: env.port},
			}
			opt.OnReady = func(t *dap.Transcript) {
				if !asJSON {
					dap.RenderSetup(out, t)
				} else {
					for _, w := range t.Warnings {
						fmt.Fprintln(cmd.ErrOrStderr(), "warning:", w)
					}
				}
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()
			ctx, cancel := context.WithTimeout(ctx, budget)
			defer cancel()
			started := time.Now()
			tr.StartedAt = started.UTC().Format(time.RFC3339)
			c, closeAll, err := env.start(ctx)
			if err != nil {
				return adapterFailure(cmd, err, "")
			}
			err = dap.Session(ctx, c, tr, opt)
			output := c.AdapterOutput()
			closeAll()
			if err != nil {
				return adapterFailure(cmd, err, output)
			}
			tr.DurationSec = float64(time.Since(started).Milliseconds()) / 1000
			path, err := dap.Write(env.root, tr, started)
			if err != nil {
				return withCode(ExitConfig, err)
			}
			if asJSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				if err := enc.Encode(tr); err != nil {
					return err
				}
			} else {
				dap.RenderRun(out, tr, width, path)
			}
			switch tr.ExitCode {
			case ExitFindings:
				return withCode(ExitFindings, fmt.Errorf("the run ended %s; see %s", endText(tr), path))
			case ExitIncomplete:
				return withCode(ExitIncomplete, fmt.Errorf("the budget passed; see %s", path))
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVarP(&file, "file", "f", profile.DefaultPath, "profile, relative to the repo root")
	f.StringArrayVar(&sets, "set", nil, "fill a {name} placeholder in the debug section: name=value (repeat)")
	f.StringArrayVar(&breaks, "break", nil, "a breakpoint, file:line or 'file:line if condition'; the file is relative to the repo root (repeat)")
	f.StringArrayVar(&dumps, "dump", nil, "an expression to evaluate in the top frame at each stop (repeat)")
	f.BoolVar(&catch, "catch", false, "stop on exceptions: the adapter's default exception filters")
	f.StringArrayVar(&catchFilters, "catch-filter", nil, "an exception filter to set instead of the defaults, as reliab debug probe lists them (repeat)")
	f.IntVar(&frames, "frames", dap.DefaultFrames, "stack frames to record at each stop")
	f.IntVar(&depth, "depth", dap.DefaultDepth, "levels of the top frame's locals to expand")
	f.IntVar(&maxStops, "max-stops", dap.DefaultMaxStops, "stops to record before ending the run")
	f.DurationVar(&timeout, "timeout", dap.DefaultTimeout, "how long the program may run, from its start")
	f.DurationVar(&budget, "budget", DefaultDebugBudget, "the whole command's limit, adapter start and build included")
	f.IntVar(&width, "width", dap.DefaultWidth, "cut values longer than this in the text rendering")
	f.BoolVar(&asJSON, "json", false, "print the transcript instead of the text rendering")
	if mode == dap.ModeAttach {
		cmd.Short = "Attach to a running process under the debug adapter, record each stop, and write a transcript"
		cmd.Long = strings.Replace(cmd.Long, "or when the\nprogram exits.", "or when the\nprocess exits; then it disconnects and leaves the process running unless --terminate.", 1)
		f.IntVar(&pid, "pid", 0, "the process to attach to, as {pid} in debug.attach (required)")
		f.BoolVar(&terminate, "terminate", false, "end the process when the session ends")
	}
	return cmd
}

func endText(t *dap.Transcript) string {
	for _, e := range t.Entries {
		if e.Kind == dap.KindStop && e.Reason == "exception" {
			return "at an exception"
		}
	}
	// Every end that exits 1 without an exception carries a message.
	return "by " + t.End.Reason + " (" + t.End.Message + ")"
}
