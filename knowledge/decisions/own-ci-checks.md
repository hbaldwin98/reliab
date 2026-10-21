---
type: Decision
title: "reliab's own CI checks are scripts; a tag publishes only code ci passed on"
description: "reliab's own workflows call scripts under scripts/ that run the same locally: a blocking lint job, a names-and-authors check fed from a secret, tests with no global git config, and a release gate that needs a green ci run on the tag's commit or its parent under a version bump alone."
status: stable
tags: [ci, release]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-20T19:30:00-07:00
---

# reliab's own CI checks are scripts; a tag publishes only code ci passed on

## Context

This is reliab's own repository's CI
([own-release-pipeline](/decisions/own-release-pipeline.md)), not a template:
reliab itself writes no CI files ([ci-agnostic](/decisions/ci-agnostic.md)).
v0.1.3 was tagged on commits CI had never run, and macOS and Windows failed
after the tag was public. Nothing checked formatting, whitespace, line endings,
or that the public repository stays free of private names
([context-agnostic](/decisions/context-agnostic.md),
[public-repo](/decisions/public-repo.md)).

## Decision

- Every check is a POSIX sh script under `scripts/` that runs the same in Git
  Bash, Linux, and macOS; a workflow step only calls it, so a check can be run
  and debugged locally.
- **Lint** (`scripts/lint.sh [BASE]`, ci job `lint`, blocking): `gofmt -l`
  lists nothing; `git diff --check` from BASE's merge base (skipped without a
  base); no tracked text file holds a control byte other than tab and newline,
  CR included, since `.gitattributes` stores every text file with LF (`*.ps1`
  and `*.cmd` get CRLF only in the working tree); golangci-lint passes. All
  checks run and the script reports each failure; it takes about 3 seconds
  with a warm cache.
- golangci-lint is pinned in `tools/go.mod` (v2.15.0, profile role T14) and
  runs as `go tool -modfile=tools/go.mod golangci-lint`, so it is built with
  this module's toolchain (the profile's correction: a golangci-lint built
  with an older Go refuses the module). A separate modfile rather than a
  `tool` line in `go.mod`: the linter's ~200 modules would otherwise join the
  module's graph and raise versions the reliab binary builds with (adding it
  to `go.mod` moved `spf13/pflag` and `golang.org/x/text`). `tools/` holds no
  Go code, so `./...` never reaches it. The dogfood job installs the same pin
  for the profile's analyzers command.
- **Names and authors** (`scripts/check-names.sh RANGE`, in the `lint` job):
  every non-merge commit the push or pull request adds has a GitHub noreply
  author email; no tracked path or text, and no commit message in the range,
  holds a name from `NAME_DENYLIST` (case-insensitive, whole words). The names
  never live in the repository, not even hashed: the owner keeps them in the
  repository secret `NAME_DENYLIST`. With it empty (a fork's pull request
  gets no secrets) the names check prints that it is skipped and passes. A
  match prints where it is, never the name, since public CI logs are public.
- **Tests** run with `GIT_CONFIG_GLOBAL=/dev/null` on every OS, so a test that
  leans on a developer's git identity or settings fails in CI; the test
  helpers already commit with a fixed identity.
- **Release gate** (`scripts/release-gate.sh SHA`, first step of `release`):
  a tag publishes only when a ci run succeeded on the tag's commit, or when
  that commit changes only `plugin/.claude-plugin/plugin.json` and ci
  succeeded on its parent. The release order is: get main green on all three
  OSes, commit the version bump alone, tag it, push the tag, push main, then
  update the README's install lines; the tag goes first because plugin.json's
  version makes the launcher download that release, so ci has not run on the
  bump commit when the release starts. Waiting for a ci run on the tag would
  need main pushed first, which the order forbids.
- The README's install lines name plugin.json's version
  (`internal/repocheck`), or the version plugin.json held before the last
  commit that changed it: the README follows the release, and commits may land
  between the bump and the README update. The answer depends only on
  plugin.json, the README, and plugin.json's history, so commits that leave
  plugin.json alone neither change it nor stale go test's cache, which keys on
  the two files. A README two versions behind fails at the next bump. The ci
  test job fetches the whole history; a shallow clone that cannot tell allows
  it.

## Consequences

A missing or empty `NAME_DENYLIST` secret silently turns the names check
into an authors-only check; the log says so. The release gate needs the
`actions: read` permission and `gh`, which GitHub's runners have.
