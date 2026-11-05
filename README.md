# reliab

A testing CLI that runs your project's existing test tools, in the right
order and on the right scope, and turns their output into one findings
format. Use it from your shell, in CI, or let an agent drive it through the
Claude Code plugin.

reliab does not test anything itself. It decides what to run, runs it, and
reads the results.

## Testing, in one page

There are two kinds of test: **checks**, which find bugs in the code, and
**meta-checks**, which measure how good the tests are.

**Checks: does the code work?**

| Type | Question | Cost | Example tools |
|---|---|---|---|
| Static analysis | Does it look wrong without running it? | seconds | golangci-lint, Roslyn analyzers, ESLint |
| Unit | Does one function do what it should? | seconds | go test, xUnit, Jest |
| Property | Does a rule hold for *any* input, not only the examples I picked? | seconds to minutes | rapid, FsCheck, fast-check |
| Snapshot / golden | Did the output change from the approved version? | seconds | insta, Verify, golden files |
| Integration | Do the parts work together, with a real database or queue? | minutes | Testcontainers |
| Contract | Do two services still agree on the API between them? | minutes | Pact |
| End-to-end | Does a real user flow work through the whole system? | minutes, often flaky | Playwright |
| Fuzz | Does random or malformed input crash it? | open-ended | go fuzz, libFuzzer, Schemathesis |
| Fault injection | Does it behave when a dependency is slow or down? | minutes | Toxiproxy |

**Meta-checks: are the tests any good?**

| Type | Question | Cost |
|---|---|---|
| Coverage | Which lines did the tests run at all? | same as the test run |
| Mutation | If I break a line, does any test notice? | slow: about one test run per mutant |

Coverage says a line *ran*. Mutation says a test *checks* it: a line can be
fully covered while no test would notice it changing.

The usual layering is to run cheap checks often and expensive ones rarely:

- **after an edit:** lint, unit tests and properties, on what you touched;
- **before a merge:** add integration, contract and snapshot tests, and
  mutation testing on the changed lines;
- **auditing existing code:** add full mutation runs, fuzzing, end-to-end
  tests and fault injection.

## Where reliab fits

1. **One file says what to run.** `reliab.yaml` lists each command, the exact
   line it runs in each context, and a sentence saying what its failure
   means.
2. **Contexts pick the depth.** `change`, `pr` and `audit` are the three
   layers above.
3. **Targets pick the scope:** what you changed, a project, a folder, a file,
   or one function. A tool that can narrow its run gets the narrowed list,
   and findings outside the target are dropped for every tool. That is how
   "coverage of just `GetUser`" works when no coverage tool targets a
   function.
4. **A scheduler gets the order right.** Independent commands run in
   parallel. A command waits for what it depends on, and does not run if
   that failed. An expensive command can claim the machine for itself.
5. **One findings format.** JUnit, TRX, Cobertura, mutation reports and
   linter output all become the same shape, so you, CI or an agent read one
   report.
6. **`--plan` explains a run before it starts:** what will run, in what
   order, and why.

reliab is only as fast as the tools it calls. What it saves is the work you
didn't need and the runs you would have thrown away.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/hbaldwin98/reliab/main/scripts/install.sh | sh -s -- 0.4.0
```

```powershell
& ([scriptblock]::Create((irm https://raw.githubusercontent.com/hbaldwin98/reliab/main/scripts/install.ps1))) -Version 0.4.0
```

This installs `reliab` and its adapters into `~/.reliab/bin`; add that to
your PATH and check with `reliab version`. Commands run under a POSIX `sh`;
on Windows that is Git for Windows' shell.

## reliab.yaml

At the repository root. It holds commands and nothing else:

```yaml
version: 1
base: origin/main          # what "changed" is measured from

commands:
  build:
    about: Compile everything. If this fails, nothing else is worth running.
    change: dotnet build
    pr:     dotnet build
    audit:  dotnet build

  unit:
    about: >
      Unit tests with coverage. A failing test is a product bug (fix the
      code) or a wrong test (fix the test; never loosen an assertion).
    change:   dotnet test {tests}
    pr:       dotnet test --collect "XPlat Code Coverage" --logger trx
    audit:    dotnet test --collect "XPlat Code Coverage" --logger trx
    output: { file: "**/TestResults/*.trx", parse: trx }
    after: [build]

  mutation:
    about: >
      Mutation testing. Slow. A surviving mutant means no test checks that
      line's behavior: add a test that does, or say why none can.
    pr:       dotnet stryker --since:main
    audit:    dotnet stryker
    targeted: dotnet stryker --mutate {files}
    output: { file: StrykerOutput/**/mutation-report.json, parse: mutation-report }
    after: [unit]
    exclusive: true
    timeout: 30m
```

| Key | Meaning |
|---|---|
| `about` | What the command checks and what a failure means. Required. Shown by `--plan` and next to failures. |
| `change`, `pr`, `audit` | The line run in that context. Leave one out and the command does not run there. |
| `targeted` | Runs instead of the context line when you pass `--target`, for tools that take a target differently. |
| `output` | A file (or glob) the tool writes, and the parser that reads it: `junit`, `trx`, `cobertura`, `mutation-report`, or `findings`. Without it, pass or fail comes from the exit code and the log. |
| `after` | Commands that must pass first. |
| `exclusive` | Runs alone: nothing else runs at the same time. |
| `timeout` | A limit such as `90s` or `30m`. |

A line may use three placeholders, each a list reliab has already worked out
for the target:

- `{files}`: the source files;
- `{projects}`: the projects that own them (a `.csproj`, a Go package, a
  directory with `package.json`, `Cargo.toml` or `pyproject.toml`);
- `{tests}`: their tests (test projects that reference those projects, a Go
  package's tests, `foo.test.ts` beside `foo.ts`, `test_foo.py` beside
  `foo.py`).

When a placeholder a line uses would be empty, the command is skipped
rather than run with nothing, because most tools then run everything.
Commands also get `RELIAB_CONTEXT`, `RELIAB_BASE`, `RELIAB_TARGET_FILES`,
`RELIAB_TARGET_PROJECTS` and `RELIAB_TARGET_TESTS` (newline-separated).

## Run

```sh
reliab run change --plan     # what would run, in order, and why; runs nothing
reliab run change            # after an edit: what changed since the base
reliab run pr                # before a merge
reliab run audit             # existing code: everything
```

Point it at something specific with `--target`, as many times as you like:

```sh
reliab run audit --target src/Api/Api.csproj          # a project
reliab run audit --target src/Api/Data/               # a folder
reliab run pr    --target src/Api/Users.cs            # a file
reliab run audit --target src/Api/Users.cs#GetUser    # a function (Type.Method works too)
```

`change` and `pr` default to the files changed since the base, committed or
not; `audit` defaults to everything. A function target is found by parsing
the file: Go with its own parser, Python by indentation, and C#, Java,
TypeScript and other brace languages by a tolerant scanner. A name that
matches more than one function is an error, not a guess.

The output is one line per command, then each failure's findings and its
`about`. Full logs are in `.reliab/run/<command>.log`.

```
pass     build        1.3s
FAIL     unit         41.2s
blocked  mutation     unit did not pass

unit: failed (log: .reliab/run/unit.log)
  - UsersTests.GetUser_ReturnsNotFound src/Api/Users.cs:42
    Expected 404, got 500
  about: Unit tests with coverage. ...
```

Exit codes: `0` everything passed, `1` something failed, `2` a config or
usage error.

## Tool output and adapters

reliab reads JUnit XML, TRX, Cobertura coverage and mutation-testing-elements
reports (Stryker and others) directly. For any other tool, run an adapter
after it that writes reliab's findings format
(`schemas/findings.schema.json`) and set `parse: findings`. An adapter is
any program. reliab ships these next to its binary:

- `reliab-adapter-cargo-mutants`
- `reliab-adapter-golangci-lint`
- `reliab-adapter-gremlins`
- `reliab-adapter-insta`
- `reliab-adapter-verify`: snapshot diffs, labelled additive-only or
  values-changed
- `reliab-adapter-pact`: consumer contracts, labelled the same way

Helpers for adapter authors are `reliab snapdiff <accepted> <pending>`,
`reliab changed-lines`, and the Go packages `pkg/snapdiff` and
`pkg/findings`. `reliab findings check <file>` reads an adapter's output the
way a run would, and warns about unstable ids and paths outside the
repository. The contract and a complete example are in
[docs/adapters.md](docs/adapters.md) (`reliab docs adapters`).

## Debugging a failure

When a test's output does not show the cause, run it under the language's
debug adapter (delve, debugpy, netcoredbg, lldb-dap: anything that speaks
the Debug Adapter Protocol). Its settings live in `reliab-debug.yaml`;
`reliab docs debug` has a block for each adapter.

```sh
reliab debug probe
reliab debug run --set program=./internal/report --set test=TestRouteKinds \
  --break internal/report/route.go:88 --dump 'f' --dump 'len(items)'
```

Each stop prints the stack, the locals and the dumps. It is
non-interactive: there is no stepping.

## CI

reliab writes no CI configuration. Call it from whatever CI you use:

```sh
reliab run pr        # every push or PR
reliab run audit     # on a schedule
```

Install the version pinned in `.reliab-version` with `scripts/install.sh`,
or `scripts/install.ps1` on Windows without a POSIX shell. They download
from GitHub Releases over HTTPS with no credentials; an authenticated `gh`
is used when present. To use a mirror, set `RELIAB_DOWNLOAD_URL` (and
`RELIAB_TOKEN` if the mirror needs one). It may also be a local directory or
a `file://` URL holding the archive and `checksums.txt`.
`reliab docs ci` prints more.

## Claude Code plugin

```
/plugin marketplace add hbaldwin98/reliab
/plugin install reliab@reliab
```

The plugin's launcher downloads the matching binary into
`~/.cache/reliab/<version>/` on first use (`$XDG_CACHE_HOME/reliab` when
that is set). An agent starts with `reliab run <context> --plan`, runs it,
and acts on the findings.

Every release archive also holds the plugin. Extract
`reliab_<version>_<os>_<arch>.tar.gz` and start Claude Code with
`claude --plugin-dir <extracted>/plugin`; set `RELIAB_BIN` to the extracted
binary to run that one.

## Develop

```sh
go build ./cmd/reliab
go test ./...
reliab run change      # reliab is tested with its own reliab.yaml
```

`go.mod` pins the toolchain; an older local Go downloads it automatically.
`scripts/dev-build.sh` builds reliab and the adapters into `~/.reliab/dev`,
which an unreleased plugin (version 0.0.0) runs instead of downloading a
release. Design decisions are in
[knowledge/decisions/](knowledge/decisions/); the current model is
[profile-v2](knowledge/decisions/profile-v2.md).

## Release

1. On a commit CI passed on, set `version` in
   `plugin/.claude-plugin/plugin.json` to `X.Y.Z` and commit it, with at most
   the release's line in `knowledge/log.md`. The release workflow takes CI's
   pass on the parent for that commit, and refuses one that changes anything
   else until CI passes on it. To check the archives first, run
   `goreleaser check` and `goreleaser release --snapshot --clean`
   (goreleaser v2).
2. Tag that commit `vX.Y.Z` and push the tag. The release workflow checks the
   tag matches the plugin version, runs the tests, and publishes binaries and
   checksums.
