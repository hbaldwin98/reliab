# Debugging with reliab

`reliab debug` runs a test or a program under the language's debug
adapter, stops where you ask, records what you ask, and writes a
transcript. It speaks the Debug Adapter Protocol (DAP) once, for every
language; the profile names the adapter and its launch and attach
arguments, as `commands` names the test tools. Use it when a test's output
does not show a failure's cause. The `/reliab:diagnose` skill drives it
from a symptom to a failing test.

It is non-interactive: no stepping. Name the test, the breakpoints, and the
expressions to dump; read the transcript; move the breakpoints; run again.

## The profile block

`debug` is optional, beside `host_adapter`. The profile is protected, so a
person adds the block; `reliab debug probe` checks it.

```yaml
debug:
  adapter:
    run: dlv dap --listen=127.0.0.1:{port}   # the command line; the first word is looked up on PATH
    transport: tcp                            # tcp | stdio
    ready: "DAP server listening"             # tcp only, optional: a regex on the adapter's output
  launch:                                     # the DAP launch request's arguments, verbatim after expansion
    mode: test
    program: "{program}"
    args: ["-test.run", "^{test}$"]
  attach:                                     # the DAP attach request's arguments
    mode: local
    processId: "{pid}"
  source_root: .                              # optional: adapter paths are made repo-relative from here
  notes: "anything the next reader needs"
```

- **`transport: stdio`**: the adapter is the child process and DAP runs on
  its stdin and stdout. **`tcp`**: reliab picks a free port on 127.0.0.1,
  starts the adapter with `{port}` filled in, waits for `ready` on its
  output (without it, retries the connection for 5 seconds), and connects.
  No remote hosts.
- **`launch` and `attach`** are passed through as the request's arguments.
  DAP never standardized them: write them from the adapter's documentation.
  reliab never interprets a key.
- **Placeholders** expand in `adapter.run` and in every string value of
  `launch` and `attach`, at any depth: `{port}` (tcp only), `{cwd}` and
  `{root}` (the repository root, absolute, with slashes), `{pid}` (from
  `--pid`), and any name given with `--set name=value`. A value that is
  exactly `{port}` or `{pid}` becomes a number. An unexpanded placeholder is
  an error before the adapter starts.
- **`run`** splits on spaces; single or double quotes group words, and a
  backslash is an ordinary character, so Windows paths need no escaping.
- The adapter runs from the repository root. Its own output goes to
  `.reliab/debug/adapter.log`.

`reliab profile validate` checks the block; `reliab doctor` checks the
adapter's first word is on PATH.

## Adapters

Each block below is marked **verified** (reliab ran `probe` and `run`
with it) or **illustrative** (written from the adapter's documentation, not
yet run). Check an illustrative block with `reliab debug probe` and one
`reliab debug run` before relying on it.

### Go: delve (verified: probe, run, and attach; dlv 1.27 on Windows)

```yaml
debug:
  adapter:
    run: dlv dap --listen=127.0.0.1:{port}
    transport: tcp
    ready: "DAP server listening"
  launch:
    mode: test
    program: "{program}"                      # a package directory: --set program=./internal/report
    args: ["-test.run", "^{test}$", "-test.count=1"]
    outputMode: remote                        # program output as DAP output events, in order with the stops
    output: "{root}/.reliab/debug/__debug_bin" # the debug build, out of the working tree
  attach:
    mode: local
    processId: "{pid}"
```

Install with `go install github.com/go-delve/delve/cmd/dlv@latest`. Delve
builds without optimizations itself, so locals are complete. `--catch`
sets `unrecovered-panic` and `runtime-fatal-throw`. Delve names functions
by package (`report.Route`).

### Python: debugpy (verified: probe and run under pytest, debugpy 1.8 on Windows; attach illustrative)

```yaml
debug:
  adapter:
    run: python -m debugpy.adapter
    transport: stdio
  launch:
    module: pytest
    args: ["-q", "-p", "no:cacheprovider", "{test}"]   # --set test=tests/test_calc.py::test_mean
    cwd: "{cwd}"
    console: internalConsole                           # output as DAP events; integratedTerminal would ask for runInTerminal
    justMyCode: false
  attach:
    processId: "{pid}"
```

`python` is the interpreter that has debugpy installed (a virtualenv's, by
path, if need be). Under pytest a failing test's exception never reaches
the top level, so `--catch` (debugpy's default `uncaught` filter) stops
only after pytest is done; use `--catch-filter userUnhandled` to stop
where the test raised. Locals come in two scopes, Locals and Globals.

### .NET: netcoredbg (illustrative)

```yaml
debug:
  adapter:
    run: netcoredbg --interpreter=vscode
    transport: stdio
  launch:
    program: dotnet
    args: ["test", "{project}", "--no-build", "--filter", "FullyQualifiedName~{test}"]
    cwd: "{cwd}"
    stopAtEntry: false
  attach:
    processId: "{pid}"
```

Microsoft's vsdbg speaks DAP too, but its license limits it to Visual
Studio and VS Code; netcoredbg is the open one. `dotnet test` runs the
tests in a child process (testhost), so breakpoints in test code may need
`attach` to the testhost's pid instead (`VSTEST_HOST_DEBUG=1` makes it wait
and print its pid). netcoredbg's first-chance exception filter is noisy:
prefer `--catch-filter` with a named filter over `--catch`.

### C, C++, Rust: lldb-dap (illustrative)

```yaml
debug:
  adapter:
    run: lldb-dap
    transport: stdio
  launch:
    program: "{root}/{program}"               # a built binary: --set program=target/debug/deps/mycrate-1a2b3c
    args: ["{test}", "--exact", "--nocapture"]
    cwd: "{cwd}"
  attach:
    pid: "{pid}"
```

`lldb-dap` was `lldb-vscode` before LLVM 18. Build the test binary first
(`cargo test --no-run` prints its path); DAP launches a binary, not a
build.

## reliab debug probe

Starts the adapter, sends `initialize`, prints the capabilities `run`
relies on (exception breakpoint filters, conditional breakpoints, log
points, `terminate`), and disconnects. Exit 0 when the adapter answered;
2, with the adapter's output, when it did not. `--json` prints the DAP
capabilities. Run it after adding the block, and first when `run`
misbehaves.

## reliab debug run

```sh
reliab debug run --set program=./internal/report --set test=TestRouteKinds \
  --break internal/report/route.go:88 \
  --break 'internal/report/route.go:120 if f.Kind == "below-threshold"' \
  --dump 'f' --dump 'len(items)' --catch --timeout 60s
```

| Flag | Default | What it does |
|---|---|---|
| `--set name=value` | | Fills `{name}` (repeat). |
| `--break file:line` | | A breakpoint; `'file:line if cond'` adds a condition when the adapter supports them. The file is relative to the repository root (repeat). |
| `--dump expr` | | Evaluated in the top frame at each stop; an error is recorded as the value, never fatal (repeat). |
| `--catch` | off | Stop on exceptions: the adapter's filters marked default. |
| `--catch-filter name` | | Set this filter instead of the defaults, as `probe` lists them (repeat). |
| `--frames n` | 10 | Stack frames recorded per stop. |
| `--depth n` | 1 | Levels of the top frame's locals to expand. |
| `--max-stops n` | 20 | Stops recorded before the run ends, so a breakpoint in a hot loop ends. |
| `--timeout d` | 120s | How long the program may run, from its start. |
| `--budget d` | 10m | The whole command's limit: adapter start, the build some adapters do in `launch`, the run, and teardown. |
| `--width n` | 160 | Where the text rendering cuts a long value, with a marker. |
| `--json` | off | Print the transcript instead of the text rendering. |
| `-f file` | `reliability-profile.yaml` | The profile. |

What it does: expands `launch`, starts the adapter, sends `initialize` and
`launch`; on `initialized` sets the breakpoints (grouped per file) and,
with `--catch`, the exception filters; then `configurationDone`. A
breakpoint the adapter does not verify is a warning printed before the
program runs: the commonest reason a run stops nowhere (a path the adapter
cannot map, a line without code, an optimized build). On each stop it
records the reason, the stopping thread's top frames, the top frame's
locals, and each dump, then continues. It ends when the program exits,
after `--max-stops` stops, or at `--timeout`, and ends the program. The
adapter's `runInTerminal` and `startDebugging` requests are declined and
recorded.

The text rendering, for an agent's eyes:

```text
adapter: dlv dap --listen=127.0.0.1:43622 (tcp, port 43622)
breakpoint internal/profile/profile.go:215: verified
[stdout] Building tests of ./internal/profile
== stop 1: breakpoint, thread 8
  at internal/profile/profile.go:215 profile.Parse
     internal/profile/profile_test.go:65 profile.TestParseMinimal
  locals:
    err = error nil
    p = profile.Profile {SchemaVersion: "0.1.0", Name: "tiny", StartingCondition: "green... [cut, 179 more chars]
  dumps:
    p.Name = "tiny"
    len(p.Commands) = 2
[stdout] PASS
end: exited, program exit code 0; 1 stop(s), 1.6s; exit 0; transcript .reliab/debug/20261021-192118.json
```

## reliab debug attach --pid <pid>

The same loop over the profile's `attach` block, with `{pid}` from
`--pid`, which is required. At the end it disconnects and leaves the
process running unless `--terminate` is given. It attaches only to what
the adapter can reach from this machine. Attach only to a process you
started or the person named.

## The transcript

Each run writes `.reliab/debug/<yyyymmdd-hhmmss>.json` and a copy at
`.reliab/debug/last.json`, under `schemas/debug-transcript.schema.json`
(schemaVersion 1.0.0):

- `adapter` (the expanded command line, transport, port), `request` (the
  expanded launch or attach arguments as sent);
- `breakpoints` with their `verified` state, `exceptionFilters`,
  `warnings`;
- `entries`, in arrival order: each a `stop` (`seq`, `reason`,
  `description`, `thread`, `frames` as repo-relative slash `path`, `line`,
  `function`; `locals` with their `scope` and `children` to `--depth`;
  `dumps` with `value` or `error`) or an `output` (`category`, `text`);
- `reverseRequests` the adapter sent, `ignored` messages counted by kind;
- `end` (`reason`: exited, terminated, max-stops, timeout, budget,
  adapter-error, adapter-gone; the program's `exitCode`), `durationSec`,
  `truncated` (stops past `--max-stops`, variables past `--depth` or the
  100-per-scope cap, values cut at 4096 characters, output past 1 MiB), and
  reliab's `exitCode`.

Paths inside the repository are repo-relative with slashes, through
`source_root`, so a transcript reads the same on Windows and CI; others
(the standard library, a module cache) stay absolute, with slashes.
Secrets are not scrubbed: `.reliab/` is gitignored and outside the report
freshness hash. Never commit a transcript, and never paste one whole into
a PR; quote the lines that show the cause.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | The program ran to its end with no exception stop. |
| 1 | At least one exception stop, or `--max-stops`, `--timeout`, or a failed request ended the run. |
| 2 | The profile, the adapter, or the launch failed before the program ran. |
| 3 | `--budget` passed. |

A failing test whose program exits non-zero is still exit 0 when nothing
stopped at an exception: the transcript's `end.exitCode` says how the
program ended.
