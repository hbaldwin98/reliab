---
name: adapter
description: Write an adapter that turns a tool's output into reliab's findings file, test it on the tool's real output, and wire it into the profile. Use when a tool exits 0 with failures or writes a format reliab does not read, in a repository with reliability-profile.yaml.
argument-hint: <tool> [command id]
---

# Write an adapter

An adapter is a small program a profile command runs after the tool. It
reads what the tool wrote and writes reliab's findings file
(`schemas/findings.schema.json`, `format: findings`). Once that file parses,
it is the whole result: reliab no longer looks at the command's exit code.
So an adapter that writes an empty file when the tool never ran turns a
broken command into a pass. The contract below exists to prevent that, and
to keep each finding's id stable so classifications carry across runs.

## 1. Check that one is needed

- **The tool can write a format reliab reads:** JUnit XML, TRX,
  mutation-testing-elements JSON (`mutation-report`), or Cobertura. Look
  for a reporter flag in the tool's docs or `--help`. If one exists and the
  tool's exit code is honest, use that format and stop: no adapter.
- **reliab ships one:** `reliab-adapter-cargo-mutants`,
  `reliab-adapter-gremlins`, `reliab-adapter-insta`,
  `reliab-adapter-verify`; each prints its usage with `-h`. Use it if it fits.

Done when you can name the format or shipped adapter that fits, or say why
none does.

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
the repository keeps tooling in, named `reliab-adapter-<tool>`. Go adapters
can import `github.com/hbaldwin98/reliab/pkg/findings`, which writes the
file and mutant keys; reliab's own four are examples
(<https://github.com/hbaldwin98/reliab/tree/main/cmd/adapters>).
Follow every rule under [Contract](#contract).

## 4. Test it on the fixtures

Write tests in the repository's test framework that run the adapter on each
fixture and assert what it writes, then check each written file the way
verify will read it:

```sh
reliab findings check <findings.json>
```

It exits 0 for a clean file, 1 with warnings (a test that names a line or a
machine path, a path that is not in the repository, counts that disagree
with the findings), and 2 for a file verify would reject. Fix the adapter
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

The profile command runs the tool, then the adapter, and names the
adapter's file as its `output`:

```yaml
mutation_changed:
  run: <tool> ... ; reliab-adapter-<tool> -exit $? <tool output> .reliab/out/mutation.json
  format: findings
  output: .reliab/out/mutation.json
```

`reliability-profile.yaml` is protected once committed: if it is, give the
person this block, and the role notes below, to add themselves, and stop
there. Otherwise add it.

Probe through reliab in a scratch worktree, as the onboard skill does:
`reliab verify --only <id> --tier prepr`, once clean and once with a
planted failure, then `reliab report route`. The planted failure must
become a finding with the right test and location; the clean run, none.
Record the adapter in the role's `notes`.

## Report

Say which tool and why an adapter was needed, the fixtures and what each
showed, the exit-code meanings and their sources, the `check` results, the
probe, and anything you could not test (a failure mode you could not
provoke).

## Contract

**Exit and output.**

- The tool ran and reported: write the findings file, with `"findings": []`
  when it found nothing, and exit 0.
- The tool did not run, or its output is missing or unreadable: write no
  file, print why on stderr, and exit nonzero. reliab then reports the
  command as failed with its exit code and log tail.
- When the tool's exit code says something its output does not (a failing
  baseline, nothing to test), take it as `-exit $?` and decide with it.
- Files from an earlier run look like new failures. Delete the tool's
  output before it runs (in the profile command), or ignore files older
  than `RELIAB_COMMAND_START` (RFC 3339, set for each command).

**Identity.** `test` is the finding's id across runs, so the same failure
must give the same `test` every time, and every `test` in a file is unique.

- A failing test: its fully qualified name (namespace, class, method,
  parameters).
- A mutant: `<file>: <description>`, without the line; mutants sharing one
  are numbered ` (#2)`, ` (#3)` in source order.
- Never a line number, a timestamp, an absolute or temp path, or a random
  id. The line goes in `location`.

**Fields.**

- `location`: `file:line[:col]`, the file relative to the repository root,
  not the tool's working directory.
- `message`: the failure text. reliab keeps the first 2000 characters.
- `replay`: the path of a saved seed or input that reproduces it.
- `observed`, `expected`: the values, when the tool reports them.
- `diff`: for a changed snapshot, the label from
  `reliab snapdiff --json <accepted> <pending>`. An adapter reads pending
  snapshots and never accepts or writes one.

**Mutation tools.** Add `mutants` (`killed`, timeouts included;
`survived`; `notCovered`) and `mutantFiles` (the same counts per
repo-relative file), and make every survived or not-covered mutant a
finding. To keep only changed lines, read
`reliab changed-lines --json --base <base>` (the profile command gets the
base as `{base}` or `RELIAB_BASE`).

**Version.** `schemaVersion` is the findings schema version the adapter was
written against (`pkg/findings.SchemaVersion`).
