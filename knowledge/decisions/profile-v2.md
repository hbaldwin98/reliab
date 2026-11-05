---
type: Decision
title: "Profile v2: the profile is the commands, run per context and target"
description: "reliab.yaml lists commands, each with the literal line it runs per context (change, pr, audit) and an optional targeted line. A target (changed files, a project, folder, file or function) narrows the run where the tool allows and always filters findings and coverage to it. Roles, tiers, budgets, governance and corrections leave the profile; debug moves to reliab-debug.yaml. Breaking; reliab profile migrate converts old profiles."
status: accepted
tags: [profile, verify, report, breaking]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-11-05T00:00:00-08:00
supersedes:
  - relevance-skips.md
---

# Profile v2: the profile is the commands

## Context

Projects that use reliab find it slow and hard to follow. The profile mixes
what to run with a theory of testing (roles T1-T16, applicability claims,
verified dates, tiers and budgets, protected paths, corrections), so a
person or an LLM must learn a vocabulary before answering "what do I run,
and when?". Reports spend more lines on skipped layers than on results.
Commands run with no ordering beyond a fixed list: an expensive mutation run
starts even when cheaper checks already failed, and the same suite can run
twice (unit, then coverage). Scope is either "changed since base" or
everything; there is no way to point reliab at one project, folder or
function.

## Decision

### The file

`reliab.yaml` at the repo root. It holds commands and nothing else of
substance:

```yaml
version: 1
base: origin/main        # what "changed" is measured from; default origin/main

commands:
  unit:
    about: Unit tests with coverage in the same pass.
    change:   dotnet test
    pr:       dotnet test --collect "XPlat Code Coverage"
    audit:    dotnet test --collect "XPlat Code Coverage"
    targeted: dotnet test {projects} --collect "XPlat Code Coverage"
    output: { file: .reliab/unit.xml, parse: junit }
    after: [build]
  mutation:
    about: >
      Mutation testing. Slow. A surviving mutant means no test checks that
      line's behavior: add a test that does, or say why it cannot.
    pr:       dotnet stryker --since:main
    audit:    dotnet stryker
    targeted: dotnet stryker --mutate {files}
    output: { file: .reliab/mutation.json, parse: mutation-report }
    after: [unit]
    exclusive: true
    timeout: 30m
```

| Key | Meaning |
|---|---|
| `about` | What the command checks and what its failure means. Shown by `--plan` and in the report; written for an LLM. |
| `change`, `pr`, `audit` | The literal line run in that context. Absent: the command does not run in that context. |
| `targeted` | The line run when `--target` is given. Absent: the context line runs and results are filtered. |
| `output` | A file the tool writes and the parser (`junit`, `trx`, `cobertura`, `mutation-report`, `findings`) that turns it into findings. Absent: pass/fail from the exit code plus the log. |
| `after` | Commands that must pass first. |
| `exclusive` | Runs alone; nothing else runs at the same time. |
| `timeout` | Per-command limit. |

### Contexts

`reliab run change | pr | audit`. Context picks which commands run.
`change` is the inner loop, `pr` is a branch about to merge, `audit` is
existing code. They replace tiers and budgets.

### Targets

`--target` names what to look at; default is changed files for `change`
and `pr`, everything for `audit`.

- a project file (`Api.csproj`, `go.mod` package dir, `package.json` dir)
- a folder
- a file
- a function: `path/to/File.cs#GetUser`
- several, repeated `--target`

reliab resolves a target to files, line ranges, owning projects, and the
matching tests. A target acts twice:

1. **Narrow the run.** Any line may use three placeholders, each an
   already-resolved list of the target: `{files}`, `{projects}`, `{tests}`.
   No other placeholders exist. `change: go test {projects}` tests the
   changed packages. With `--target`, a `targeted` line, when present,
   replaces the context line for tools that take a target differently.
   A placeholder that resolves empty skips the command (an empty list
   would make most tools run everything); a placeholder in a line whose
   target is everything is an error.
2. **Filter the results** always: findings and coverage outside the
   target's files and line ranges are dropped from the report. This is how
   function coverage works when no tool targets a function.

Scripts also get `RELIAB_BASE`, `RELIAB_CONTEXT`, `RELIAB_TARGET_FILES`,
`RELIAB_TARGET_PROJECTS`.

Function line ranges come from a cheap per-language parser (Go AST; a
tolerant parser for C#, TS and others), not a language server.

### Plan and schedule

`reliab run <context> --plan` prints what would run, in order, with each
command's filled-in line and `about`, and runs nothing. It is the first
call an agent makes.

Commands with no unmet `after` run in parallel; an `exclusive` command
waits for an idle slot and holds it. A command whose `after` failed is
reported as blocked, not run.

### Findings

Adapters stay: they make every tool speak one finding format. A finding
gets one of three kinds: `fix-code`, `fix-test`, `ask-human`.

### Removed

Roles, tiers, budgets, `impacted` maps, `relevant_when`, `paths`,
`protected_paths`, `approve`, `gate protected`, the reviewer verdict,
`corrections`, `runtime`, `starting_condition`. Debug adapter settings move
to `reliab-debug.yaml`. `reliab profile migrate` converts a
`reliability-profile.yaml`.

## Phases

1. Schema, loader, target resolver, `reliab run <context> [--target] [--plan]`
   with the scheduler. Old commands keep working.
2. Short report: pass/fail per command, findings, next step.
3. Remove governance, tiers and roles; `profile migrate`; rewrite AGENTS.md.
4. Measure on a consuming project.

## Deferred

Picking only the tests that touch a targeted function, from a prior
coverage run.
