---
type: Decision
title: "Debugging through the Debug Adapter Protocol: one client, per-language adapters in the profile"
description: "reliab speaks DAP once, generically; the profile names the language's debug adapter and its launch and attach arguments, as commands name test tools. The first mode is non-interactive: run a test or program under the adapter, stop where asked, dump what was asked, and write a transcript the diagnose skill turns into a regression test. Interactive sessions and an MCP front door come second."
status: draft
tags: [cli, plugin, debugging, departure]
generated:
  by: claude-code/claude-fable-5-1
  at: 2026-10-21T15:30:00-07:00
---

# Debugging through the Debug Adapter Protocol

## Context

reliab's loop already contains most of a debugging session: every failure
is reproduced from its seed, shrunk, classified, and fixed in product code
([finding-classification](/decisions/finding-classification.md)); the
`characterize` command starts the system and records what it does
([on-demand-characterization](/decisions/on-demand-characterization.md));
and any signal can become a finding through `format: findings`
([external-adapters](/decisions/external-adapters.md)). What is missing is
a way to see a running program's state when a test's output is not enough,
and a guided way in from a symptom (a stack trace, a log line, a failing
request) rather than from a test run.

A debugger is stack-specific and reliab is not ([context-agnostic](/decisions/context-agnostic.md)).
The Debug Adapter Protocol (DAP) is the seam: it standardizes the
conversation (launch or attach, breakpoints, stopped events, threads,
stack, scopes, variables, evaluate, step, continue) and leaves adapter
startup and launch arguments to each adapter. Delve (`dlv dap`),
netcoredbg, debugpy, lldb-dap, CodeLLDB, gdb 14+ (`gdb -i dap`), rdbg,
and the PHP, Kotlin, Elixir and Haskell adapters all speak it. Two need
more than a minimal client: js-debug starts child sessions through the
`startDebugging` reverse request, and Java's adapter runs inside the JDT
language server. Microsoft's vsdbg speaks it but its license limits it to
Visual Studio and VS Code, so .NET uses netcoredbg here. nvim-dap is
proof the model works: one generic client, a configuration per adapter.

Agents step badly and dump well. Told "run this test under the debugger,
stop at this line, print these expressions", an agent gets a deterministic,
re-runnable transcript it can reason over and attach to a finding; told to
step, it loses the thread. So the first mode is non-interactive.

The paper has no debugging step; this is a departure
([paper-as-design-intent](/decisions/paper-as-design-intent.md)).

## Decision

### Phase 1: the client, `reliab debug run | attach | probe`, the `diagnose` skill

**A generic DAP client in `internal/dap`.** Types and framing come from
`github.com/google/go-dap` (the module Delve's own DAP server uses; confirm
its license before adding it to `go.mod`). The client owns: starting the
adapter process, the transport, request and response correlation by `seq`,
event dispatch, a per-request timeout, and killing the adapter's process
tree on exit (reuse verify's tree kill,
[verify-execution](/decisions/verify-execution.md)). It implements these
requests: `initialize`, `launch`, `attach`, `setBreakpoints`,
`setExceptionBreakpoints`, `configurationDone`, `threads`, `stackTrace`,
`scopes`, `variables`, `evaluate`, `continue`, `next`, `stepIn`,
`stepOut`, `pause`, `disconnect`, `terminate`; and handles these events:
`initialized`, `stopped`, `continued`, `output`, `exited`, `terminated`,
`capabilities`. The reverse requests `runInTerminal` and `startDebugging`
get an error response in phase 1, and the transcript records that the
adapter asked (this is how js-debug support will be scoped in phase 2).
Everything else the adapter sends is ignored and counted.

**Two transports.** `stdio`: the adapter is the child process and the
protocol runs on its stdin and stdout; the adapter's stderr goes to the
log. `tcp`: reliab picks a free port on 127.0.0.1, starts the adapter with
`{port}` expanded, waits for the adapter's `ready` pattern on its stdout or
stderr (or, without one, retries the connection for up to five seconds),
then connects. No other transport, and no remote host: reliab adds no
network hop of its own.

**A `debug` section in the profile**, optional, beside `host_adapter`. It
is the only place an adapter is named, as `commands` is the only place a
tool is named.

```yaml
debug:
  adapter:
    run: dlv dap --listen=127.0.0.1:{port}   # stdio example: netcoredbg --interpreter=vscode
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
  notes: "dlv needs -gcflags=all=-N -l for full locals; go test -c first for large packages"
```

`launch` and `attach` are free-form maps passed through as the request's
arguments: DAP never standardized them, so the profile author writes them
from the adapter's documentation, and reliab never interprets a key.
Placeholders expand in every string value, at any depth: the built-ins
`{port}`, `{cwd}` (the repository root, absolute, slash-separated),
`{root}` (the same), `{pid}` (from `--pid`), and any name given on the
command line with `--set name=value`. An unexpanded placeholder is an
error before the adapter starts. `reliab profile validate` checks the
section against the schema; `reliab doctor` checks that the adapter's
first word is on PATH when the section is present (the `tools` check
already does this for commands).

**`reliab debug probe`** starts the adapter, sends `initialize`, prints the
adapter's capabilities (exception breakpoint filters, whether it supports
conditional breakpoints, log points, `terminate`), disconnects, and exits
0; any failure prints the adapter's output and exits 2. This is the
onboarding probe and the first thing the skill tells an agent to run when
`debug run` misbehaves.

**`reliab debug run`** is the non-interactive mode and the point of phase 1.

```sh
reliab debug run --set program=./internal/report --set test=TestRouteKinds \
  --break internal/report/route.go:88 \
  --break 'internal/report/route.go:120 if f.Kind == "below-threshold"' \
  --dump 'f' --dump 'len(items)' --catch --timeout 60s
```

It: expands `launch`, starts the adapter, sends `initialize` and `launch`;
on `initialized` sends the breakpoints (grouped per file, with conditions
when the adapter supports them) and, with `--catch`, `setExceptionBreakpoints`
with the adapter's filters marked `default: true` (or those named by
`--catch-filter`); then `configurationDone`. On each `stopped` event it
records the reason and description, the stopping thread's stack (top
`--frames` frames, default 10), the top frame's locals (`scopes` then
`variables`, one level; `--depth` expands further, default 1), and each
`--dump` expression evaluated in the top frame (an evaluation error is
recorded as the value, never fatal); then it continues. It stops recording
and terminates the debuggee after `--max-stops` stops (default 20, so a
breakpoint in a hot loop ends), on `--timeout` (default 120s), or when the
debuggee exits. Program output arrives as `output` events and is kept in
order with the stops. Every `--break` whose `setBreakpoints` response says
`verified: false` is a warning printed before the program runs: an
unverified breakpoint is the commonest reason a run "stops nowhere" (a
path the adapter cannot map, a line without code, an optimized build).

**`reliab debug attach --pid <pid>`** is the same loop over the profile's
`attach` block: breakpoints, dumps, `--catch`, then `disconnect` after
`--max-stops`, `--timeout`, or the process's exit, leaving the process
running unless `--terminate` is given. It attaches only to what the
adapter can reach from this machine; a profile that points an adapter at
a remote host is the author's decision, and the session-start hook names
a `debug` section in its reminder as it names the on-demand skills.

**The transcript** is written to `.reliab/debug/<yyyymmdd-hhmmss>.json`
with `.reliab/debug/last.json` as a copy, under `schemas/debug-transcript.schema.json`
(schemaVersion 1): the adapter command, the expanded launch or attach
arguments, the requested breakpoints with their verified state, the
exception filters set, then an ordered list of entries, each a `stop`
(seq, reason, description, thread, frames as repo-relative slash path,
line, function; locals; dumps) or `output` (category, text), then the
exit code or disconnect reason, the duration, and what was truncated
(stops past `--max-stops`, variables past `--depth`, output past a size
cap). Paths from the adapter are made repo-relative with `source_root`
and slash-separated, so a transcript reads the same on Windows and CI.
Secrets are not scrubbed: the transcript lives under `.reliab/`, which is
gitignored and outside the freshness hash, and the skill never pastes a
transcript whole into a PR. `--json` prints the transcript instead of the
text rendering. Exit codes: 0 the program ran to its end with no exception
stop; 1 at least one exception stop, or the stop or timeout limit ended
the run; 2 the profile, the adapter, or the launch failed before the
program ran; 3 the overall budget passed (the tier convention,
[tiers-budgets-exit-codes](/decisions/tiers-budgets-exit-codes.md)).

**The text rendering** is for the agent's eyes: one block per stop, with
the reason, the top frames as `path:line func`, the locals and dumps as
`name = value` lines, interleaved program output marked by category, and
a one-line tail with the exit code and the path of the JSON transcript.
Long values are cut at a width with a marker, never silently.

**The `diagnose` skill** (`/reliab:diagnose <symptom>`) is the guide. The
person gives a symptom: a stack trace, a log line, a failing request, a
flaky test, "it hangs when". The skill:

1. States the symptom in one line and names the layer it should live in
   once fixed (a unit test, a property, a snapshot, a fuzz seed, a store
   fault test).
2. Reproduces it. First by the cheapest test that shows it: an existing
   failing test, a new test written from the symptom, or a `characterize
   capture` of the conversation. Only when the failure's cause is not
   visible from the output does it run `reliab debug run` with the test
   named, breakpoints at the suspect lines, and dumps of the values the
   hypothesis depends on; it says what it expected each dump to show
   before reading the transcript. With no `debug` section in the profile
   it says so and offers the one-block addition for the person to add (a
   committed profile is protected), with `reliab debug probe` as the
   check.
3. Narrows: moves breakpoints, bisects with `git bisect run` over the
   reproducing test when the symptom is a regression, re-runs. Each run
   is a new transcript; the skill keeps a short log of hypothesis, run,
   and what the run showed.
4. Writes the failing test first, under the layer from step 1, citing the
   symptom as its source (R3), then fixes product code, never a test
   ([testing rules](/rules/index.md)). A crash input or a captured request becomes
   a regression seed under a protected path, committed with the fix and
   listed as a question for the person.
5. Runs `reliab verify --tier inner`, classifies the finding
   (`new-code-defect`, or `legacy-defect-or-quirk` and stop), and reports:
   the symptom, the cause in one sentence, the test that now pins it, the
   transcripts' paths, what it could not reproduce.

It never attaches to a process it did not start unless the person names
the pid, never edits a transcript, and never widens an assertion to make
the reproduction pass.

**A docs page, `docs/debug.md`**, embedded and printed by `reliab docs
debug`: the profile block for each adapter reliab was tried with (delve,
netcoredbg, debugpy, lldb-dap at least, each marked verified or
illustrative), the `run` and `attach` flags, the transcript shape, and the
exit codes. The README gets a short section; `knowledge/overview.md` names
the skill (a repocheck test holds the overview to every user-invocable
skill). The onboard skill gains an optional step: when the repository's
language has a known adapter, propose the `debug` block, run `reliab debug
probe`, and record the result or the reason it is left out. Debugging is
not a tool role: roles are test layers with a "must"; a debugger is a
view into them, so it is a profile section, not a T16.

**Tests.** `internal/dap/daptest` is a scripted fake adapter (a Go
function serving go-dap messages over a pipe or a local TCP port) that
replays canned responses and events, so the client, the stop loop, the
breakpoint verification warning, the limits, the transcript, and the
rendering are tested without a real debugger, on every platform. One
integration test debugs a small Go test in a temporary module with the
real `dlv dap`, and is skipped when `dlv` is not on PATH; reliab's own CI
installs delve in the test job so the integration test runs there
([own-ci-checks](/decisions/own-ci-checks.md)). On Windows the delve test
also checks that backslash absolute paths come out repo-relative with
slashes.

**Done when** ([agentic-flow-roadmap](/decisions/agentic-flow-roadmap.md):
every step ends with real use):

- On reliab's own repository, `reliab debug run` on a named test with a
  breakpoint and two dumps prints the locals and the dump values and
  writes a transcript; `reliab debug probe` prints delve's capabilities;
  `reliab doctor` reports the adapter.
- A fresh session given `/reliab:diagnose` with a planted defect's symptom
  (a wrong value, not a crash, so the test output alone does not show the
  cause) reproduces it, runs `debug run` at least once with a stated
  expectation, ends with a failing-then-passing test under the right
  layer, and classifies the finding.
- The same two checks on a consumer repository in another ecosystem with
  netcoredbg or debugpy, from the docs page's block alone.

### Phase 2, decided now, built after phase 1 is accepted

- **Interactive sessions.** `reliab debug session start | break | continue
  | next | step | stack | vars | eval | stop`, over a daemon that holds the
  adapter connection: a Unix socket under `.reliab/debug/` on POSIX, a
  loopback TCP port with a token file on Windows. The daemon writes the
  same transcript, appended as the session goes.
- **An MCP server** the plugin declares (`.mcp.json`), exposing the session
  verbs as tools for an agent, over the same daemon. Phase 1 needs none:
  `debug run` is a command an agent already knows how to call.
- **js-debug and friends.** `startDebugging` handled by opening the child
  session on the same transport; `runInTerminal` handled by running the
  command in reliab's own shell with its output captured. Scoped by what
  phase 1 transcripts show adapters asking for.
- **From transcript to evidence.** `reliab report summary` lists the
  transcripts a change's diagnosis produced, by path and symptom, when the
  `diagnose` skill records them; and a `findings import` for an external
  failure (a stack trace, an incident export) so a symptom enters `route`
  and `classify` as a finding from the start.

## Consequences

- A departure from the paper, recorded under
  [paper-as-design-intent](/decisions/paper-as-design-intent.md): the
  paper has no debugging step; reliab adds one as a view into the layers,
  not a layer.
- The profile schema gains an optional `debug` object (`adapter` with
  `run`, `transport`, optional `ready`; `launch`; `attach`; optional
  `source_root`; optional `notes`); `schemas/` gains
  `debug-transcript.schema.json`; both follow
  [schemas-and-versioning](/decisions/schemas-and-versioning.md).
- `go.mod` gains `github.com/google/go-dap`. It is the only new dependency.
- `.reliab/debug/` joins the gitignored, never-edited tree; transcripts are
  never committed and never pasted whole into a PR.
- The plugin gains the `diagnose` skill; the binary gains `reliab debug
  run | attach | probe` and `reliab docs debug`; doctor, onboard, the
  README, the overview and the session-start reminder mention the section.
- Open after phase 1: whether a transcript belongs in the report at all
  (it is evidence of a diagnosis, not of a test); how Java's adapter is
  started outside the language server; whether `--catch` should default on
  for `run` (delve's panic filter is cheap, netcoredbg's first-chance
  exceptions are noisy).
