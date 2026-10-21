# reliab

A stack-neutral testing CLI and Claude Code plugin: one tool that runs a
project's tests in tiers (the edit loop, before a merge, nightly) and, on
demand, mutation, property, fuzz, snapshot, and end-to-end runs, turning
each tool's output into findings. Use it from your shell, or let an agent
drive it through the plugin's skills.

- What reliab is and why: [knowledge/overview.md](knowledge/overview.md)
- Design decisions: [knowledge/decisions/](knowledge/decisions/)
- v0 scope: [knowledge/decisions/v0-scope.md](knowledge/decisions/v0-scope.md)

## Adopt reliab in a repository

1. **Install** the plugin in Claude Code (see
   [Install the plugin](#install-the-plugin-claude-code)). Its launcher puts
   `reliab` on the agent's PATH. For your own shell or CI, install the
   binary and the adapters into `~/.reliab/bin` (then add it to your PATH):

   ```sh
   curl -fsSL https://raw.githubusercontent.com/hbaldwin98/reliab/main/scripts/install.sh | sh -s -- 0.2.0
   ```

   ```powershell
   & ([scriptblock]::Create((irm https://raw.githubusercontent.com/hbaldwin98/reliab/main/scripts/install.ps1))) -Version 0.2.0
   ```

   Check it with `reliab version`.
2. **Initialise** the repository:

   ```sh
   reliab profile init --starting-condition maintained   # greenfield | maintained | legacy
   ```

   `init` writes a `reliability-profile.yaml` skeleton, adds `.reliab/` to
   `.gitignore`, merges the plugin and its marketplace into
   `.claude/settings.json`, adds the testing rules to `AGENTS.md`, and pins
   the version in `.reliab-version`. It keeps what is already there and is
   safe to re-run after changing `protected_paths`.
3. **Onboard** (stage S0): in a Claude Code session in the repository, run
   `/reliab:onboard`. The agent asks only what the code cannot tell (the
   starting condition, which files are protected oracles, who approves
   them), runs `init` if you have not, inventories the test projects, re-runs
   the suite to list flaky tests, and fills each tool role T1 to T14 only
   after a probe ran here and reliab read its output (T15, fuzzing, is
   reliab's own and optional); anything else is marked
   not applicable with a reason or recorded under `corrections`. It wires
   the commands and tiers (inner under 5 minutes), configuring each tool for
   speed here (diff-only mutation, job counts, fuzz time boxes), runs `reliab doctor` and
   both tiers, and stops with an S0 report: what is filled and why not,
   timings, flaky tests, and open questions. It never edits product code or
   existing tests. Complete example profiles for made-up Go, Rust, and .NET
   services are in [templates/examples/](templates/examples/).
4. **Commit** the profile and the files `init` wrote, then run the probes on
   a CI agent and wire the [CI](#ci) commands; that finishes S0.

Day to day the agent runs, guided by the plugin's skills and hooks:

```sh
reliab verify --tier inner      # when a change is done, before stopping (budget 5 min)
reliab verify --tier prepr      # before a branch merges into main (budget 15 min)
reliab report route             # what to do about each finding
reliab findings classify <id> --kind <kind> --reason "..."
```

To test one area on demand, name the commands and the paths:

```sh
reliab verify --only mutation_full --scope internal/report --tier prepr
reliab verify --only fuzz --scope internal/parser --tier nightly   # fuzz_seconds: 120 prepr, 1800 nightly
reliab verify --only e2e --tier prepr
```

This runs only those commands, under the tier's budget, with `{impacted}`,
`{scope}`, and `RELIAB_SCOPE` narrowed to the paths, and writes a separate
on-demand report that never satisfies the stop or push hooks.

To name the tests that guard a change, in any tier:

```sh
reliab verify --tier prepr --tests "<one test run, in the runner's syntax>" --mutation-tests A,B
```

The profile's commands receive them as `{tests}`, `{mutation_tests}`,
`RELIAB_TESTS`, and `RELIAB_MUTATION_TESTS`, the report records them, and a
command with `needs: [tests]` leaves the run incomplete until they are named.

The plugin's `mutation`, `properties`, and `fuzz` skills drive the whole
loop for one area.

Around a refactor or migration, capture an area's behavior, change it, and
compare; the profile's `characterize` command writes the transcripts:

```sh
reliab characterize capture --scope crates/service  # before the change
reliab characterize compare                         # after; each difference is labelled
reliab characterize discard                         # captures live in .reliab/ and are never committed
```

The `characterize` skill drives this cycle.

Before a branch merges into main, by a PR or locally, and once the change is
committed, the plugin's `reliab:reviewer` agent re-runs the pre-PR tier in
a clean worktree, reads the diff, and records a verdict that only it may
write (`reliab report verdict`); with `require_reviewer_verdict: true` the
pre-push reminder asks for a pass for the current tree. The PR's evidence section is
pasted from the report, not written by the agent:

```sh
reliab report summary           # Markdown for the PR body; --format json for tooling
```

The `summary` skill drives both.

People decide findings that need a human:
`reliab findings ack <id> --by human:<name>`.

## Tool output

reliab reads JUnit XML, TRX, and mutation-testing-elements reports directly,
and counts lines in Cobertura coverage. A command's `output` may be a glob
when a tool writes one file per project. With `changed_only: true`, mutants
and coverage are kept to the lines changed since the profile's `base`.
For any other tool, a profile command runs the tool and then an adapter that
writes reliab's findings format (`schemas/findings.schema.json`), and sets
`format: findings`. An adapter is any program; reliab ships
`reliab-adapter-cargo-mutants`, `reliab-adapter-golangci-lint`,
`reliab-adapter-gremlins`, `reliab-adapter-insta`, and `reliab-adapter-verify` (snapshot diffs labelled
additive-only or values-changed) beside its binary and puts them on PATH for
profile commands. Adapters share reliab's rules through helpers:
`reliab snapdiff <accepted> <pending>` labels a snapshot change,
`reliab changed-lines` prints the lines changed since the base, and Go
adapters can import `pkg/snapdiff` and `pkg/findings`. The contract, the
helpers, and a complete example adapter are in
[docs/adapters.md](docs/adapters.md); `reliab docs adapters` prints it. To
have an agent write one, run the plugin's `adapter` skill
(`/reliab:adapter <tool>`): it captures the tool's real output as fixtures, tests the adapter on them, and checks each file with
`reliab findings check <file>`, which reads it as verify would and warns
about unstable ids, paths outside the repository, and counts that disagree.
See [knowledge/decisions/external-adapters.md](knowledge/decisions/external-adapters.md).

## CI

reliab writes no CI configuration. Wire these into whatever CI you use:

```sh
reliab verify --tier prepr                       # every push; exit 0 passes
reliab gate protected --base origin/main         # every PR; exit 1 means a human must approve
reliab verify --tier nightly                     # on a schedule
reliab report export --format junit -o report.xml   # or --format sarif
```

Install the version pinned in `.reliab-version` with `scripts/install.sh`
(or `scripts/install.ps1` on Windows without a POSIX shell). They download
from GitHub Releases over HTTPS with no credentials; an authenticated `gh`
is used instead when present, and is optional. To use a mirror, set
`RELIAB_DOWNLOAD_URL` (and `RELIAB_TOKEN` if the mirror needs it). `RELIAB_DOWNLOAD_URL` may also be a local directory or a `file://` URL
holding the archive and `checksums.txt`.

Exit codes: `0` passed and complete, `1` open findings, `2` config error,
`3` incomplete (the budget ran out, or a command needs tests named).

## Install the plugin (Claude Code)

```
/plugin marketplace add hbaldwin98/reliab
/plugin install reliab@reliab
```

Repositories set up with `reliab profile init` register the marketplace and
enable the plugin for everyone who trusts the workspace. The plugin's launcher
downloads the matching binary into `~/.cache/reliab/<version>/` on first use
(`$XDG_CACHE_HOME/reliab` when that is set; on Windows under your user
profile, through Git Bash).

Every release archive also holds the plugin, so you can load it without the
marketplace: extract `reliab_<version>_<os>_<arch>.tar.gz` and start Claude
Code with `claude --plugin-dir <extracted>/plugin`. The launcher still
installs its version into the cache on first use; set `RELIAB_BIN` to the
extracted `reliab` binary to run that one instead.

### What the hooks do

The hooks remind and never block. Session start briefs the workflow and
open findings; a stop with unverified product changes gets one nudge; a push
or PR gets a note on what the pre-PR report lacks; and an edit to a
protected path, a person-only reliab command, or a match for the profile's
`protected_commands` or `protected_edits` gets a note that a person approves
it. The CI gates above are authoritative.

## Develop

```sh
go build ./cmd/reliab
go test ./...
```

`go.mod` pins the toolchain; an older local Go downloads it automatically.
To try the plugin against a local build, run `scripts/dev-build.sh`: it builds
reliab and the adapters into `~/.reliab/dev`, which an unreleased plugin
(version 0.0.0) runs instead of downloading a release. `RELIAB_BIN` overrides
the binary outright.

## Release

1. On a commit ci passed on, set `version` in `plugin/.claude-plugin/plugin.json`
   to `X.Y.Z` and commit it, with at most the release's line in
   `knowledge/log.md`: the release workflow takes ci's pass on the parent for
   that commit, and refuses one that changes anything else until ci passes on it.
   To check the archives first, run `goreleaser check` and
   `goreleaser release --snapshot --clean` (goreleaser v2): `dist/` gets one
   archive per OS and architecture, each holding `reliab`, every
   `reliab-adapter-*`, the plugin directory, and the README, plus
   `checksums.txt`.
2. Tag that commit `vX.Y.Z` and push the tag. The release workflow checks the
   tag matches the plugin version, runs the tests, and publishes binaries and
   checksums with goreleaser.
