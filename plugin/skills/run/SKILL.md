---
name: run
description: Run the project's tests through reliab when a change is done or before a merge, then act on each failure. Use when finishing a change, checking a branch before a PR, or testing one area, in a repository with reliab.yaml.
argument-hint: [change | pr | audit] [--target <path|file#Function>]...
---

# Run the tests

`reliab.yaml` lists the project's test commands. reliab runs the ones for a
context, in order, on a target, and prints what failed.

## 1. Pick the context and target

- `change`: a change is done (not after each file edit).
- `pr`: the branch is about to merge.
- `audit`: existing code, everything or one area.

The target defaults to the files changed since the base (`change`, `pr`) or
everything (`audit`). To look at one area, add `--target` with a project
file, a folder, a file, or `file#Function`, as many times as needed.

## 2. Plan first

```sh
reliab run <context> [--target ...] --plan
```

Read it: which commands run, in what order, the exact line each runs, and
each command's `about`, which says what its failure means. Commands it
skips are listed with the reason. Nothing runs.

## 3. Run

```sh
reliab run <context> [--target ...]
```

Exit 0: everything passed. Exit 1: something failed. Exit 2: a config or
usage error (fix `reliab.yaml` or the command line; `reliab profile check`
lists every problem).

The output is one line per command, then each failure's findings and its
`about`. A `blocked` command did not run because one it depends on failed:
fix that one first. Full logs are in `.reliab/run/<command>.log`.

## 4. Act on each failure

Decide what it is:

- **fix-code**: the product is wrong. Fix it.
- **fix-test**: the test or harness is wrong, or a test is missing. Fix it.
  A wrong property: delete it and say why.
- **ask-human**: only a person can say whether the behavior is intended,
  or the machine is broken (a missing service, no disk). Stop and ask,
  quoting the finding and the log.

Never change an assertion, widen a tolerance, or add a catch block to make
a test pass. A property or fuzz failure prints a seed: rerun it with
`reliab run <context> --seed <n>`.

Then run the same command again until it passes or only ask-human
findings are left.

## 5. Report

Say what you ran (the context, the target, the seed if anything failed),
what failed and what you did about each, and what is left for a person.
