---
type: Decision
title: "reliab reads interchange formats; adapters convert the rest"
description: "The core reads JUnit, TRX, mutation-testing-elements, Cobertura (counts only), and its own findings format. Tool-specific output is converted by separate adapter programs that anyone can write; reliab exposes the rules adapters share as CLI helpers and public Go packages, checks an adapter's output with reliab findings check, and the plugin's adapter skill walks an agent through writing one. reliab's own adapters live in cmd/adapters and ship beside the binary."
status: stable
tags: [cli, verify, adapters]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-18T19:30:00-07:00
---

# reliab reads interchange formats; adapters convert the rest

## Context

Running tools and parsing their output is the least valuable part of reliab:
CI already does it, and every tool-specific parser is code to maintain. The
first mutation reader was written inside the binary for cargo-mutants'
`outcomes.json`, which only one tool writes. Adapters were already keyed by
format, never by tool (paper P7), and a one-tool format breaks that.

## Decision

- **The core reads only formats many tools write:** `junit`, `trx`,
  `mutation-report` (the mutation-testing-elements JSON that Stryker and
  others write), and `cobertura`. `sarif` is allowed and still judged by
  exit code. Whatever the format, a declared `output` must be written: a
  command that exits 0 without it is a finding, not a pass.
- **Cobertura gives counts, never findings.** The command result records
  lines valid and covered (`coverage`), and `reliab report summary` shows
  them; a malformed file is a finding and the exit code still counts. An
  uncovered line is not a finding: rule R7 makes surviving mutants on
  changed lines drive new tests, and a finding per coverage gap would push
  an agent to write coverage-chasing tests. Changed-lines coverage is a CI
  backstop with a threshold; that gate belongs with the mutation threshold,
  as a profile field plus a baseline. The mutation threshold and baseline
  are built ([mutation-threshold](/decisions/mutation-threshold.md));
  changed-lines coverage is not yet.
- **An `output` may be a glob.** Every match is read and the findings are
  merged; every match is removed before the command runs. A test runner
  that writes one result file per project (a solution of several test
  projects) otherwise overwrites one file and loses all but the last
  project's failures, silently.
- **Test identity is unique within a command.** A TRX result names the test
  by method only, so the reader qualifies it with the class from its test
  definition (`Ns.Class.Method`); this changed TRX finding ids, which is
  acceptable before v0.1.0. A name a command still reports twice (two result
  files, or a format that does not say the class) is numbered `(#2)`, as
  mutants sharing a key already were.
- **Paths in findings are relative to the repository**, whatever the tool
  wrote: mutation-testing-elements keys are often absolute, or relative to
  the report's `projectRoot`. An absolute path made a mutant's id differ
  between checkouts, so a classification made locally never carried to CI.
- **A surviving mutant shows its mutated line.** The mutation-testing-elements
  report carries each file's `source`; the reader renders the mutant's span
  as a `-` line and the replacement as a `+` line (the location's end column
  is exclusive). The report also yields the counts the summary reads
  ([pr-evidence-summary](/decisions/pr-evidence-summary.md)).
- **Changed code is opt-in per command: `changed_only: true`.** For a
  mutation-report or cobertura output, verify keeps only the lines changed
  since the base, findings and counts alike, and the counts record
  `changedSince: <base>` so a reader knows what they cover. A command whose
  tool or adapter already filters (gremlins, cargo-mutants) leaves it off.
  It is opt-in rather than implied by the `mutation_changed` id because a
  tool's own changed-code mode can be coarser: Stryker.NET's `since`
  re-tests every mutant a changed test covers, and reports uncovered
  mutants on lines nobody touched.
- **One base for every changed-code question.** `gitx.ResolveBase`: a
  `--base` flag, else the profile's `base`, else the first of
  `origin/HEAD`, `origin/main`, `main`, `master` with a merge base. Verify
  (`changed_only`, `{base}`, `RELIAB_BASE`), `reliab report summary`, and
  `reliab changed-lines` all use it; changed lines are those since the merge
  base, committed or not, with untracked files whole (`gitx.ChangedLines`).
- **Everything else goes through `format: findings`**, reliab's own format
  (`schemas/findings.schema.json`): a list of findings, each with a `test`
  that is its identity across runs, and optional `location`, `message`,
  `replay`, `observed`, `expected`.
- **An adapter is any program, written and hosted by anyone,** that turns a
  tool's output into a findings file: a script in the target repository, a
  tool on PATH, a per-stack plugin (as the paper's §7.6 has it), or a
  separate adapters repository. The profile command runs the tool and then
  the adapter; nothing in reliab changes and nobody needs a commit here:

  ```yaml
  mutation_changed:
    run: cargo mutants ... ; reliab-adapter-cargo-mutants -exit $? mutants.out .reliab/out/mutation.json
    format: findings
    output: .reliab/out/mutation.json
  ```
- **A findings file is the whole result.** Once it parses, reliab does not
  consult the command's exit code: the adapter knows what the tool's exit
  code meant (cargo-mutants exits 3 for timeouts; gremlins exits 0 with
  survivors). No file means the command failed, and the exit code and log
  tail become a finding. `mutation-report` is decisive the same way.
- **Adapters stay external; reliab exposes the rules they share as helpers.**
  Logic several adapters need is not turned into profile-declared
  conventions in the core. It is offered as CLI helpers any adapter can
  call, in any language, and as public Go packages external Go adapters can
  import (`internal/` cannot be imported from outside the module):
  - `reliab snapdiff <accepted> <pending> [--json]`: the
    [snapshot label](/decisions/snapshot-diff-labels.md) and the added and
    removed line counts; `pkg/snapdiff` (`Compare`, `CompareFiles`).
  - `reliab changed-lines [--base <ref>] [--json]`: the changed line ranges
    per file, from the one base resolution above. The gremlins adapter calls
    it instead of parsing `git diff` itself.
  - `pkg/findings`: writing the findings format, mutant keys, and counts.
  - `pkg/testresults` (added 2026-10-20): the JUnit and TRX failure readers
    the core uses, for an adapter that joins a tool's test report to its
    own output. The insta and Verify adapters had imported them from
    `internal/adapters`, which an adapter outside the module cannot.
  - `RELIAB_COMMAND_START`, set for each command, so an adapter can ignore
    files an earlier run left in the tree.
  - `reliab findings check <file>` (added 2026-10-19): reads a findings file
    as verify would and warns about what makes findings unstable or
    misleading once reported: a `test` that names a line or a path from
    this machine, a `location` or `mutantFiles` path that is not a file in
    the repository (relative to the tool's working directory, not the
    root), and mutant counts that disagree with the findings or each other.
    Exit 0 clean, 1 warnings, 2 a file verify would reject. It checks what
    can be checked from one file; whether ids hold across runs takes two.
- **The contract is one page for people and agents:**
  [docs/adapters.md](../../docs/adapters.md) (added 2026-10-19), embedded in the
  binary and printed by `reliab docs adapters`, so it matches the installed
  version and reaches a repository without reliab's source. It holds the
  contract, the helpers, `findings check`, and a complete example adapter
  for a made-up tool with its tests. The skill keeps only the workflow.
- **Writing an adapter is an agent task, driven by the plugin's `adapter`
  skill** (`/reliab:adapter <tool> [command id]`, added 2026-10-19). An
  adapter that writes an empty file when the tool never ran turns a broken
  command into a pass, and one whose ids move with line numbers loses every
  classification, so the skill holds the contract (exit and output,
  identity, fields, mutation counts) and makes the agent prove it on the
  tool's real output: fixtures from a clean, a failing, a moved, and a
  broken run, tests over them, `findings check` on each, and a probe
  through `reliab verify --only`. A committed profile is protected, so the
  agent hands the person the command to add. The onboard skill sends a
  small adapter here and keeps a large one an open question.
- **A tool that reports several separate problems is better with an
  adapter, even when its exit code is honest** (added 2026-10-19). Judged by
  exit code, every problem shares the command's one finding id
  (`F-<command>`). Classifications carried by id alone, so a person's
  acknowledgement of one pre-existing lint problem covered every later
  failure of the command, new problems included; a command-level finding
  now carries only while its failure's fingerprint is the same
  ([finding-classification](/decisions/finding-classification.md)), so
  that is no longer a hazard, but its problems still cannot be classified
  one by one. Only a pass-or-fail tool, such as a build, is
  left to its exit code. Found in the acceptance runs: asked to "hook go
  vet into reliab", one fresh session wired plain `go vet` by exit code and
  never reached the skill; with the rule stated, the next wrote an
  adapter. The example Go profiles now run golangci-lint through
  `reliab-adapter-golangci-lint`; reliab's own protected profile waits on
  the owner's approval of the same command.
- **reliab also hosts a few adapters, as a convenience:** the ones written
  while dogfooding live in `cmd/adapters/<tool>`. They are ordinary
  external adapters with no special access: they talk to the core only
  through the findings file, exactly as a third-party one would. They are
  built as `reliab-adapter-<tool>` (the prefix keeps them from shadowing the
  tool on PATH) and share `pkg/findings` for the file format and mutant
  keys.
  Releases ship them in the same archive as `reliab`, the install scripts
  install them beside it, and `scripts/dev-build.sh` builds them for local
  development.
- **`reliab verify` puts its own directory first on PATH** for profile
  commands, so a profile names an adapter without installing it separately.
- **Mutant identity is file and description without the line**, numbered in
  source order when several share it, so a finding keeps its id (and its
  classification) when code moves elsewhere in the file.

## Consequences

- reliab's own adapters: `cargo-mutants` (missed mutants with their diffs; a
  failing baseline as one finding; no outcomes and exit 0 means nothing to
  mutate) and `gremlins` (lived and not-covered mutants, from one or more
  per-package reports, filtered to lines changed since `-base`), and
  `insta` (added 2026-10-18: each pending `.snap.new` compared with its
  accepted snapshot and labelled
  [additive-only or values-changed](/decisions/snapshot-diff-labels.md),
  plus non-snapshot failures from a JUnit report), and `verify` (added
  2026-10-18: the .NET Verify library's `.received.` files labelled against
  their `.verified.` files with `pkg/snapdiff` and joined to the failing test
  that wrote them, from a TRX or JUnit report; only received files written
  since `RELIAB_COMMAND_START` count, because a mutation tool runs the
  snapshot tests against every mutant and leaves received files behind),
  and `golangci-lint` (added 2026-10-19, written with the `adapter` skill
  and docs/adapters.md as a dogfood of both: each issue in golangci-lint's
  JSON is a finding whose test is the repo-relative slash file, the
  linter, and the message without line or column, numbered when identical
  in one file; only exits 0 and 1 count as a run, since a compile error
  exits 7 with an empty issue list. golangci-lint's JUnit names a testcase
  by file:line:col with the OS's separators, so its ids would move with
  every edit and differ between Windows and CI).
- **gremlins runs per package.** On Windows, gremlins 0.6.0 keys coverage by
  backslash paths and looks mutants up by slash paths, so from the module
  root every mutant is "not covered" and none is tested
  (go-gremlins/gremlins#222). Inside one package the paths match.
  `scripts/mutation.sh changed` runs it on each changed package. Before
  this, reliab's own T4 passed while testing nothing; S0 missed it because
  the diff on `main` is empty.
- **gremlins mutates only changed lines (`--diff`).** Its `--diff` keys
  changed lines by `git diff`'s repository-relative paths and looks mutants
  up by package-relative ones, so in a per-package run it skipped every
  mutant. `scripts/mutation.sh changed` runs gremlins from the package
  directory with no path argument, and has git print paths relative to it
  (`diff.relative`) in zero-context hunks (`diff.context=0`: gremlins takes a
  hunk's added lines as one contiguous range). A package holding an
  untracked Go file is mutated whole, since `git diff` omits it. The adapter
  still filters through `reliab changed-lines`. Without it, a release's
  prepr mutated 1,134 mutants across 11 packages to keep the changed ones,
  and ran out of its 900-second budget at about 630.
- **gremlins runs with two workers and a 4x timeout.** With its default of
  one worker per CPU, tests run so slowly under the load that gremlins
  times them out, and a timed-out mutant counts as killed (the
  mutation-testing-elements convention, kept). On a 16-CPU Windows machine
  all 68 mutants of `internal/setup` timed out; four workers and a 10x
  timeout still timed out 10 and showed 4 survivors; two workers showed 13
  survivors and 1 timeout, matching mutants applied by hand (14). So a
  score measured under load is too high, and survivors appear and vanish
  between runs. `--test-cpu` makes every mutant read as killed, so it is
  never passed. Two workers take about twice as long as the default. The
  timeout is 4x since 2026-10-20: on `internal/guard`, 4x and 10x gave the
  same 8 survivors and 14 timeouts, and each timeout (a mutated loop that
  never ends) waits that multiple of the package's test time.
- **Go coverage does not instrument `case` expressions,** so gremlins reports
  a mutant in one as not covered even when tests would kill it. Keep
  conditions in case bodies rather than case expressions in code gremlins
  covers.
- **gremlins mutants in a file this platform does not build are dropped**
  (added 2026-10-20). gremlins mutates every Go file in the package, also
  one a build constraint or `_GOOS` name leaves out here (`lock_unix.go` on
  Windows), and calls its mutants not covered, since no test here can
  compile them. The adapter checks each file's name and constraints
  (`go/build`'s `MatchFile`, one header read per file) and neither reports
  nor counts those mutants; the platform that builds the file measures it.
  A file it cannot read counts as built. Before, a Windows prepr on a
  change to `lock_unix.go` held a survivor no test could ever reach.
- `report route` gives surviving mutants their own advice (kill it with a
  test, or say it is equivalent) and shows enough of the diff to see the
  mutated line. A command that failed without reporting a test (a build
  error, a crashed tool, a missing result file) gets its own advice and its
  log path, never the mutant advice. A long message keeps its last lines,
  where tools often say how to reproduce the failure.
- These came from checking reliab against a .NET service's usual tools
  (NUnit with TRX, FsCheck, Verify, Stryker.NET, Schemathesis, AltCover):
  each gap was a format or behavior the core already owned, or a rule
  adapters share, and none needed a tool name in the core.
- A surviving mutant no test can kill is classified `equivalent-mutant`, a
  human kind ([equivalent-mutants](/open-questions/equivalent-mutants.md)).
- Refines [schema-v0-shape](/decisions/schema-v0-shape.md) and
  [verify-execution](/decisions/verify-execution.md).
