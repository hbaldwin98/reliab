---
name: onboard
description: Adopt reliab in the current repository (stage S0) - write the profile, inventory the tests, probe each tool role T1 to T14 on the real code, wire commands and tiers within budget, and stop at the S0 gate with a report for the person. Use when asked to onboard, adopt, set up, or install reliab in a repository, or to fill in reliability-profile.yaml.
argument-hint: [greenfield | maintained | legacy]
---

# Onboard a repository (stage S0)

A profile's fillings are claims until they run on this codebase. S0 turns
each claim into evidence: a role is filled only after a command ran here and
produced output reliab ingested. Package metadata and READMEs tell you what
to probe, never what works. You finish at the S0 gate with a report; S1 and
later stages are the person's call.

## Ground rules

- **Never edit product code or existing tests**, and never convert, upgrade,
  or reformat a project to make a tool fit. If a tool needs that, it is a
  question for the person.
- Never edit protected paths or anything under `.reliab/`, never run a
  command that approves an oracle, and never quarantine, skip, or retry a
  test to make the suite green.
- You may write: `reliability-profile.yaml` until the person commits it,
  what `reliab profile init` writes, and helper scripts for profile
  commands in one directory the person agrees to (say `scripts/reliab/`), listed under `paths.tests` so
  they count as harness, not product. Installing a tool into the repository
  (a tool manifest, a dev dependency) changes what every developer gets:
  ask first.
- Probes that need new code (a failing test, a first property) go in a
  scratch worktree outside the repository and are thrown away:
  `git worktree add "${TMPDIR:-/tmp}/reliab-probe" HEAD`, copy in the
  profile and any helper scripts you have not committed, work and run
  `reliab verify --only ...` there, then `git worktree remove --force` it.
  Nothing from a scratch worktree is committed.
- Time-box: S0 should finish in under a day. Give a role about 20 minutes;
  if its probe is not working by then, record what you found and move on.
- Run every probe through `reliab verify --only <command>`, never the tool
  alone: the point is that reliab ingests the output. Use `--tier nightly`
  (no budget) while probing, so a slow command is timed, not killed.
- Keep a shell command that writes files apart from one that reads
  `.reliab/` or a protected path. The pre-bash hook treats every path in a
  command that writes anything as a write target, so `sed -i x.cs && cat
  .reliab/reports/...` is denied; run them as two commands.

## 1. Ask only what the code cannot tell

Look first: the git history (age, activity), the test projects and how many
tests they hold, snapshot or golden directories, saved corpora, mutant
suppression files, hand-maintained API specs, CODEOWNERS, and CI files
(read them; never edit them). Then ask the person, in one message, with
your recommendation for each:

- **Starting condition** (if not given as the argument): `greenfield` (new
  code; the oracle is stated intent), `maintained` (intent plus a suite of
  unknown adequacy), or `legacy` (yesterday's behavior is the oracle;
  intent partly lost). Say what you saw that suggests one.
- **Protected oracles**: list each candidate you found (snapshots, golden
  files, corpora, scrubbers, mutant suppressions, hand-maintained specs) and
  ask which change only with a human's approval.
- **Who approves them**: a person or team, for the report and for wiring
  `reliab gate protected` into CI later.
- **A planned runtime move**, if the code shows two runtimes (for
  `runtime.target`).

Do not ask what you can find out by running something.

## 2. Initialise

```sh
reliab profile init --starting-condition <condition>
```

It writes the profile skeleton (unless one exists), ignores `.reliab/`,
merges deny rules and the plugin into `.claude/settings.json`, adds the
testing rules to `AGENTS.md`, and pins the version. If a profile already
exists, init keeps it: read it, and treat every filling in it as a claim to
probe like any other. Set `paths` now (product, tests, ignore); a file that
matches none counts as product, so a gap makes the hooks stricter, not
looser. Put the agreed oracles in `protected_paths`, beside
`reliability-profile.yaml`, which init lists there: the profile guards its
own thresholds, relevance rules, and baseline. Protection of the profile
takes effect once the person commits it; until then it is yours to write.
After that, every edit to it is denied to agents and needs the person's
`reliab approve reliability-profile.yaml --by human:<name>`.

The `examples/` directory beside this skill has complete profiles for
made-up Go, Rust, and .NET services to copy shapes from; their tools are
examples, not recommendations.

## 3. Inventory, read-only

Write down, for the report:

- languages, runtimes, and build entry points (solution, workspace, module);
- each test project or package: framework, runner, target runtime, rough
  test count, and how it reports results (JUnit, TRX, or nothing yet);
- how tests are selected today: categories, traits, tags, filters, and what
  CI runs on a pull request versus on a schedule;
- tools already in use for each role (properties, snapshots, mutation,
  fuzzing, coverage, analyzers), with versions from the tool itself
  (`<tool> --version`), not from a lock file alone;
- what the code talks to: an HTTP API and its spec, data stores, external
  HTTP services, queues. These decide which of T5 to T11 apply.

## 4. Build, the suite, and flakiness

Wire `build` and `unit` first. `unit` names its result file so failures
become one finding per test:

```yaml
unit:
  run: <runner> <its result-file flag> {impacted}
  format: junit            # or trx
  output: .reliab/out/unit.xml
```

A runner that writes one file per project needs an output glob
(`.reliab/out/unit/*.trx`) or a single run over all projects; a fixed file
name that each project overwrites keeps only the last project's results.

Run the whole suite three times (five if a run takes under a minute):

```sh
reliab verify --only build,unit --tier nightly
```

After each run, save the failing test names from
`.reliab/reports/on-demand-latest.json` (read it; never edit it). Then:

- **Fails in every run**: an existing failure. Reproduce it once by hand
  and classify it: `legacy-defect-or-quirk`, or `environment` only after
  a re-run with the machine's error quoted in the reason.
- **Fails in some runs**: flaky. Classify it `legacy-defect-or-quirk` with
  "flaky: failed N of M runs" and what varies, if you can tell.
- Quarantine nothing. Every one of these goes in the report for the person.

Record the median run time; it decides the tiers in step 6. If the suite
cannot build or run at all, stop here and report: S0 needs a suite that
runs.

## 5. Probe each role

For each role T1 to T14, in this order: find the tool in use (or the
ecosystem's usual one, if none is), wire its command, probe it, and record
the outcome. A probe has two halves: a real run on this repository through
reliab, and a deliberate failure (in a scratch worktree) that reliab turns
into a finding naming the right test or line. A role whose tool has never
produced a finding through reliab is not verified.

| Role | Command | The probe shows |
|---|---|---|
| T1 unit framework | `unit` | the suite runs; a planted failing test becomes a finding with its name |
| T2 properties | `properties` | `{seed}` and `{cases}` (or `RELIAB_SEED`, `RELIAB_PROPERTY_CASES`) reach the tool; a failing property's seed and shrunk input are in the finding; the same seed reproduces it |
| T3 snapshots | `snapshots` | a changed snapshot fails the run and becomes a finding labelled `additive-only` or `values-changed`, without the tool accepting it (the label needs an adapter: use reliab's if it ships one for the tool) |
| T4 mutation | `mutation_changed`, `mutation_full` | on a branch with a change, a survivor on a changed line becomes a finding with file, line, and diff; the report has mutant counts |
| T5 in-process host | (inside `unit`) | an existing test starts the application in-process; name it |
| T6 typed client | (inside `unit`) | an existing test calls the application through its own request types; name it |
| T7 API fuzzing | `api_fuzz` | the fuzzer runs from the API's spec against a local instance; a failure carries a reproducer |
| T8 network faults | `faults_http` | an injected fault between the application and a store reaches a test |
| T9 HTTP stubs | `faults_http` | a scripted failure from a stubbed dependency reaches a test |
| T10 store faults | `faults_db` | a seeded store failure in the test host reaches a test |
| T11 store tests | `store_tests` | a test runs logic that lives in the store |
| T12 coverage | `coverage` | a machine-readable report (`cobertura`) that reliab reads |
| T14 analyzers | `analyzers` | a planted warning fails the run |

T13 (shadow experiments) needs a planned rewrite and production reads; it is
almost always `applicable: false` at S0.

Record each role as one of four:

- **Filled**: `{ tool: <name>, version: <from the tool>, verified: <today> }`,
  with `notes` for anything a later reader needs (a pin, a workaround).
- **Not applicable**: `{ applicable: false, reason: "<why this codebase does not need it>" }`.
  "Not in use yet" is not a reason a role does not apply.
- **Needed but missing**: `{ missing: true, reason: "<what is missing and what adding it takes>" }`
  when the codebase needs the role and has no tool for it yet. Put the
  choice in the open questions too (for example, "no property library in
  the test projects; adding one is a package reference in tests/X, the
  person's decision"). You may probe a candidate in a scratch worktree to
  show it would work, and say so. Its commands are skipped as
  `role-missing`, and `reliab doctor` lists the role.
- **A filling that failed**: append to `corrections` (never edit or remove
  an entry): the tool, what its metadata or docs claimed, what the probe
  found (quote the error), the decision (swap, pin, or drop), the source,
  and the date. Then probe the next candidate.

Tools that exit 0 with failures, or write a format reliab does not read,
need an adapter that writes reliab's findings format (`format: findings`).
reliab ships these beside its binary and puts them on PATH for profile
commands: `reliab-adapter-cargo-mutants`, `reliab-adapter-gremlins`,
`reliab-adapter-insta`, and `reliab-adapter-verify` (Verify snapshots).
Each prints its usage with `-h`; outside a profile command they sit beside
the binary, in `~/.cache/reliab/<version>/` for a release. Prefer one that fits over
reading the tool's raw output. If none fits and the tool's output is
simple, write a small adapter with the `adapter` skill, which tests it on
the tool's real output. A large one is an open question for the person, not
S0 work.

### What tools get wrong

These are the claims that most often fail a probe. Check each for the
tools you wire. When one bites, the workaround goes in the command and the
role's `notes`, and the failed claim goes in `corrections`:

- **Defaults that assume a branch.** A "since" or "changed" mode may
  compare with `master`, or treat any changed file (docs, config) as a
  reason to test everything. Pass `{base}` explicitly and probe on a branch
  that changes one non-code file.
- **One result file per project.** A runner that runs projects separately
  may overwrite a fixed result file name; probe with two failing projects
  and check both failures reach the report. Use a per-command prefix and an
  output glob.
- **Seed and case count do not reach the tool.** Some property libraries
  read neither from the environment; a small test helper must, and a probe
  must show the same seed reproduce the same failure.
- **Encoding and shells on Windows.** A tool whose redirected output is not
  UTF-8 may crash or garble its report; a command may need a POSIX path
  where the tool expects a native one.
- **A second runtime needs another build host.** An older framework's
  projects may build only with a full IDE build engine, or need reference
  packages; probe each runtime the repository targets, not only the newest.
- **Version coupling.** A plugin or adapter may support only one major
  version of the framework it extends; record the pins together.
- **Exit 0 with failures, or leftovers.** A tool may exit 0 when it found
  something, or leave files from an earlier run (snapshot `received` files,
  stale reports) that look like new failures. Probe a clean run after a
  failing one.
- **Commands that share build output.** A mutation tool may swap or
  restore binaries in the build output and leave an older build behind, so
  a later command in the tier tests code from before the change. Probe the
  tier, not only each command: change one product line, run prepr, and
  check every command saw the change. A helper that runs tests from build
  output rebuilds first.
- **Linked worktrees.** Agents often work in a linked git worktree, and some
  tools read the main checkout instead (a "since" mode diffing the wrong
  tree). If a probe fails only in the scratch worktree, say so in the
  correction. If you fall back to a slower mode (every mutant, then
  `changed_only`), time it in prepr and say in the report how it will
  scale with the codebase; never write a time into a comment that the
  report does not show.

## 6. Wire tiers within budget

- Every command that writes a result file has `format` and `output`, and
  outputs live under `.reliab/out/`.
- `{impacted}`: map each product area to the test arguments that select its
  tests, following how the repository already selects them (step 3). Check
  each entry: `reliab verify --only unit --scope <a path under the glob>`
  must run that area's tests and not the whole suite (read the log). When a
  filter needs its arguments joined by something other than a space, set
  `impacted.separator`.
- The inner tier must finish in under 5 minutes for a typical change. If
  `build` plus `unit` with `all` exceeds that, rely on the impacted map, and
  move slow commands to prepr with `tiers.inner.run`. Never raise the inner
  budget above 300 seconds without the person's say-so; prepr's default is
  15 minutes.
- **Protection for the tools in use** (R2):
  - `protected_paths`: the agreed oracles, as globs.
  - `protected_commands`: RE2 patterns for each command that accepts or
    rewrites an oracle without naming its path: a snapshot tool's accept or
    review command, an update flag, an environment variable that makes it
    accept.
  - `protected_edits`: RE2 patterns for text that switches acceptance on
    from inside test code (an auto-accept or auto-verify setting).
  Check each pattern against the tool's real command line, and re-run
  `reliab profile init` after changing `protected_paths` so the deny rules
  follow.

## 7. The gate

```sh
reliab profile validate
reliab doctor
reliab verify --tier inner
reliab verify --tier prepr
reliab report route
```

Fix what is the harness's (a command, a path, a pattern, a helper script)
and re-run. A finding in existing code is not yours to fix: classify it as
in step 4 and list it. Record both tiers' elapsed time from the reports.

S0 passes when every probe passed locally, the existing suite is green with
no test changes (or every failure is classified and listed), and every
failed claim is in `corrections`. The paper's gate also runs the probes on a
CI agent; reliab writes no CI configuration, so that is the person's next
step. Leave `verified_on` unset until it is done.

Stop here. Do not start S1 work, add tests, or commit unless the person
asks. Tell the person that committing the profile turns its own protection
on: re-run `reliab profile init` after the commit so the deny rules include
it, and later profile changes go through `reliab approve`.

## The S0 report

Give the person:

1. **Profile**: starting condition, paths, protected oracles and who
   approves them, and the files you wrote or changed.
2. **Roles**: a table of T1 to T14: filled (tool, version, the probe and its
   finding), not applicable (reason), or corrected (what failed).
3. **Timings**: each command's time, the inner and prepr tiers' totals
   against their budgets, and what moved between tiers and why.
4. **The suite**: test projects and counts, runs made, tests failing in
   every run, flaky tests (failed N of M), each with its classification.
   Nothing was quarantined.
5. **Open questions**: each choice that is the person's (a tool to add, a
   project conversion, an oracle to protect, a CI probe), with your
   recommendation.
6. **Skipped**: anything you did not probe and why.
