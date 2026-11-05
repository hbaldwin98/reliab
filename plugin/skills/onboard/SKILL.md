---
name: onboard
description: Set up reliab in the current repository - write reliab.yaml from the test tools the project already uses, check each command runs here, and add the testing rules to AGENTS.md. Use when asked to onboard, adopt, set up, or install reliab in a repository, or to write or migrate reliab.yaml.
---

# Set up reliab in a repository

The goal is a `reliab.yaml` whose every command has run here. A command
that was never run is a guess.

## 1. Start from what exists

- If `reliability-profile.yaml` exists, run `reliab profile migrate`, read
  the notes it prints, and go to step 3.
- Otherwise find the tools the project already runs: the build, the test
  runner(s) and their projects, linters, coverage, mutation (Stryker,
  cargo-mutants, gremlins), property and fuzz tools, snapshot and contract
  tests, end-to-end suites. Look at CI config, scripts, the package or
  project files, and the README. Ask the person only what the code cannot
  tell you.

## 2. Write reliab.yaml

The examples in `examples/` (Go, Rust, .NET) are complete. Per command:

- `about`: what it checks and what its failure means, in a sentence or two.
  An agent reads it next to every failure.
- One line per context it belongs in. `change` is the inner loop: cheap
  commands only, narrowed with `{projects}`, `{files}` or `{tests}` where
  the tool allows. `pr` adds the slower checks, with mutation on changed
  code only. `audit` runs everything, in full.
- `output` with a parser (`junit`, `trx`, `cobertura`, `mutation-report`,
  `findings`) whenever the tool can write one: findings beat a log tail. For
  any other tool, see the `adapter` skill.
- `after` for real dependencies: tests after the build, mutation after the
  tests. `exclusive` for commands that want the whole machine (mutation,
  fuzzing). `timeout` for anything open-ended. `threshold` for a mutation
  score the team holds.
- `targeted` only for a tool that takes a target differently from its
  context line.

Make each command fast here: diff-only mutation in `pr`, the test runner's
parallelism, fuzz time limits on the command line.

## 3. Prove each command runs

```sh
reliab doctor
reliab run change --plan
reliab run change
reliab run pr
```

Fix every command that fails for a reason other than a real test failure
(a wrong path, a missing tool, an output file the tool never wrote). Note
how long each context takes; `change` should take a few minutes at most.

Add `.reliab/` to `.gitignore`.

## 4. Add the testing rules

Run `reliab docs overview` and copy the block between `<!-- reliab:begin -->`
and `<!-- reliab:end -->`, markers included, into the repository's `AGENTS.md`.

## 5. Report

Show the person `reliab.yaml`, the time each context took, any real test
failures you found (do not fix them as part of onboarding), and anything
you could not wire.
