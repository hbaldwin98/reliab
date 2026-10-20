---
name: adapter
description: Hook a tool into reliab through an adapter that turns its output into reliab's findings file, tested on the tool's real output and wired into the profile. Use when a tool reports several separate problems per run (linters, analyzers, scanners), exits 0 with failures, or writes a format reliab does not read, in a repository with reliability-profile.yaml.
argument-hint: <tool> [command id]
---

# Write an adapter

An adapter is a small program a profile command runs after the tool. It
reads what the tool wrote and writes reliab's findings file (`format:
findings`). Once that file parses, it is the whole result: reliab no longer
looks at the command's exit code. So an adapter that writes an empty file
when the tool never ran turns a broken command into a pass, and one whose
ids move with line numbers loses every classification. The contract that prevents both is one page:

```sh
reliab docs adapters
```

Read it before step 3; it is for the installed version, and its example is
a complete adapter with its tests.

## 1. Check that one is needed

- **The tool is pass or fail (a build):** a bare command, judged by its
  exit code, is enough. A tool that reports several separate problems is
  better with an adapter, even when its exit code is honest: judged by exit
  code, all its problems are one finding, which reopens whenever any of
  them changes, so none can be classified or acknowledged on its own. An
  adapter gives each problem its own id.
- **The tool can write a format reliab reads:** JUnit XML, TRX,
  mutation-testing-elements JSON (`mutation-report`), or Cobertura. Look
  for a reporter flag in the tool's docs or `--help`. If one exists and the
  tool's exit code is honest, use that format and stop: no adapter.
- **reliab ships one:** `reliab-adapter-cargo-mutants`,
  `reliab-adapter-golangci-lint`, `reliab-adapter-gremlins`,
  `reliab-adapter-insta`, `reliab-adapter-verify`; each prints its usage with `-h`. Use it if it fits.

Done when you can name the bare command, format, or shipped adapter that
fits, or say why none does.

## 2. Capture the tool's real output

In a scratch git worktree outside the repository, run the tool exactly as
the profile command will, and keep each run's output files and exit code:

1. **clean**: on the code as it is.
2. **failing**: with a planted failure (a broken assertion, a survivor, a
   changed snapshot), or two if the tool reports several kinds.
3. **moved**: the same planted failure with a few blank lines inserted above
   it, so its line number changes.
4. **broken**: the tool cannot run (a compile error), and, if it can happen,
   its output missing or left over from an earlier run.

Read the tool's docs for what each exit code means, and note the source.
Copy the outputs into the repository's test data as **fixtures**, trimmed to
what the adapter reads. Discard the worktree.

Done when you hold all four fixtures, their exit codes, and a cited meaning
for each exit code.

## 3. Write it to the contract

Write it in the repository's own language, or as a short script, at a path
the repository keeps tooling in, named `reliab-adapter-<tool>`. Follow
every rule in `reliab docs adapters`, and use its helpers rather than
re-deriving what they compute (changed lines, snapshot labels).

## 4. Test it on the fixtures

Write tests in the repository's test framework that run the adapter on each
fixture and assert what it writes, then check each written file the way
verify will read it:

```sh
reliab findings check <findings.json>
```

It exits 0 for a clean file, 1 with warnings (a test that names a line or a
machine path, a path that is not in the repository, counts that disagree
with the findings), and 2 for a file verify would reject. Run it where the
fixture's files exist (the scratch worktree, for a file you planted
there), or it warns that they are not in the repository. Fix the adapter
for every warning; a warning you think is wrong goes in your report with
why.

Done when:

- **clean** writes a file with no findings, and `check` exits 0;
- **failing** writes one finding per planted failure, naming the right test
  and location, and `check` exits 0;
- **moved** writes the same `test` values as **failing** (compare the
  `check` output of the two);
- **broken** writes no file and the adapter exits nonzero.

## 5. Wire it and probe it

Write the profile command the way the contract's first example does: the
tool, then the adapter with the tool's exit code, `format: findings`, and
the adapter's file as `output`. Its id is the role's command, which sets
the tiers it runs in: `unit` (T1), `integration_tests` (slow tests with a
host or real services), `properties` (T2), `snapshots` (T3),
`mutation_changed` or `mutation_full` (T4), `api_fuzz` (T7), `faults_http`
(T8, T9), `faults_db` (T10), `store_tests` (T11), `coverage` (T12),
`analyzers` (T14). Fill the role, or add the adapter to its `notes`.

Probe it in a scratch worktree with the command added there:
`reliab verify --only <id> --tier prepr` once clean and once with a planted
failure, then `reliab report route`. Run the adapter there only
through `reliab verify`: `.reliab/` is written only by reliab, and the hooks
deny a shell command that writes under it. The planted failure must become a
finding with the right test and location; the clean run, none.

Then wire it in the repository. `reliability-profile.yaml` is protected
once committed: if it is, hand the person the command block and the role
notes to add and approve, and stop there. Otherwise add them, and run
`reliab verify --only <id>` on the clean repository.

Done when the probe shows the planted failure through route and the clean
run shows none.

## Report

Say which tool and why an adapter was needed, the fixtures and what each
showed, the exit-code meanings and their sources, the `check` results, the
probe, and anything you could not test (a failure mode you could not
provoke).

