# Writing an adapter

reliab reads JUnit XML, TRX, mutation-testing-elements JSON, and Cobertura
itself. For any other tool, or one that exits 0 when it found failures, a
profile command runs the tool and then an **adapter**: a small program, in
any language, that reads what the tool wrote and writes reliab's findings
file. Nothing in reliab changes and nobody needs a commit here. Why it works
this way: [knowledge/decisions/external-adapters.md](../knowledge/decisions/external-adapters.md).

This page is the contract. `reliab docs adapters` prints it for the
installed version. In Claude Code, the plugin's `adapter` skill
(`/reliab:adapter <tool>`) walks an agent through writing one to it.

**When you need one.** A pass-or-fail tool, such as a build, needs no
adapter: a bare command is judged by its exit code. A tool that reports
several separate problems (a linter, an analyzer, a scanner) is better with
one even when its exit code is honest. Judged by exit code, all its problems
are one finding, `F-<command>`: a classification of it holds only while the
command fails the same way, so a new problem reopens it, but the problems
cannot be classified one by one. An adapter gives each its own id.

## The contract

**Run the tool, then the adapter.** The command names the adapter's file as
its `output` and sets `format: findings`:

```yaml
commands:
  unit:
    run: rm -f .reliab/out/specrun.json; specrun --report .reliab/out/specrun.json; python3 tools/reliab-adapter-specrun $? .reliab/out/specrun.json .reliab/out/unit.json
    format: findings
    output: .reliab/out/unit.json
```

reliab deletes `output` before the command runs, but not the tool's own
files: delete those first (as above), or ignore files older than
`RELIAB_COMMAND_START` (RFC 3339, set for each command), or a leftover
report from an earlier run looks like this run's result.

**The findings file is the whole result.** Once it parses, reliab no longer
looks at the command's exit code, so the adapter decides what the tool's
exit code meant. Pass it through (`$?` above) and:

- the tool ran and reported: write the file, `"findings": []` when nothing
  failed, and exit 0;
- the tool did not run, or its output is missing or unreadable: write no
  file, say why on stderr, and exit nonzero. reliab then reports the
  command as failed, with its exit code and log tail.

An adapter that writes an empty file when the tool never ran turns a broken
command into a pass.

**The file** (`schemas/findings.schema.json`):

```json
{
  "schemaVersion": "0.4.0",
  "findings": [
    {
      "test": "billing.invoice > rounds half up",
      "location": "src/billing/invoice.py:42",
      "message": "expected 1.01, got 1.00"
    }
  ]
}
```

**`test` is the finding's id across runs.** The same failure must give the
same `test` every run, or its classification is lost; every `test` in a
file is unique.

- A failing test: its fully qualified name.
- A mutant: `<file>: <description>`, without the line.
- When two findings would share a name, number the later ones ` (#2)`,
  ` (#3)`, in source order.
- Never a line number, timestamp, absolute or temp path, or random id. The
  line belongs in `location`.

**Paths are relative to the repository root**, not to the directory the
tool ran in. reliab makes an absolute path inside the repository relative;
anything else is taken as written.

**Optional fields:**

| Field | What |
|---|---|
| `location` | `file:line[:col]` |
| `message` | the failure text; reliab keeps the first 2000 characters |
| `replay` | path of a saved seed or input that reproduces the failure |
| `observed`, `expected` | the values, when the tool reports them |
| `diff` | for a changed snapshot: `additive-only` or `values-changed`, from `reliab snapdiff` |

**Mutation tools** also write the counts, so the report can say how many
mutants were tested and score a threshold:

```json
"mutants": { "killed": 37, "survived": 2, "notCovered": 1 },
"mutantFiles": { "src/billing/invoice.py": { "killed": 37, "survived": 2, "notCovered": 1 } }
```

`killed` includes timeouts. `mutantFiles` has the same counts per
repo-relative file and must add up to `mutants`. Every survived or
not-covered mutant is also a finding.

An adapter reads a snapshot tool's pending files and never accepts or
writes a snapshot: that is a person's decision.

## Helpers

These keep adapters agreeing with reliab, from any language:

- `reliab changed-lines [--base <ref>] [--json]`: the lines the branch
  changed, per file, from the same base as everything else in reliab
  (`{base}` or `RELIAB_BASE` in a profile command). Use it to keep only
  findings on changed code.
- `reliab snapdiff [--json] <accepted> <pending>`: the label for a changed
  snapshot.
- Go adapters can import `github.com/hbaldwin98/reliab/pkg/findings`
  (writes the file, the mutant keys, the counts), `pkg/snapdiff`, and
  `pkg/testresults` (the failing tests in a JUnit or TRX report, read as
  `reliab verify` reads them).
- `reliab verify` puts its own directory first on PATH for profile
  commands, so the adapters reliab ships (`reliab-adapter-cargo-mutants`,
  `-golangci-lint`, `-gremlins`, `-insta`, `-verify`) and `reliab` itself are always found.
  Their source in `cmd/adapters/` is a set of worked examples.

## Check a findings file

```sh
reliab findings check <findings.json>
```

It reads the file as `reliab verify` would and prints each finding's test
and location and the mutant counts. It warns about a test that names a line
or a path from this machine, a `location` or `mutantFiles` path that is not
a file in the repository, and counts that disagree. Exit 0 clean, 1
warnings, 2 a file verify would reject. Run it from inside the repository,
or paths are not checked.

## Example: a made-up tool

`specrun` is a made-up test runner. `specrun --report <file>` writes JSON
like this, exits 0 whether or not tests failed, and exits 2 when it could
not run at all:

```json
{"results": [
  {"suite": "billing.invoice", "name": "rounds half up", "status": "failed",
   "file": "src/billing/invoice.py", "line": 42, "message": "expected 1.01, got 1.00"},
  {"suite": "billing.invoice", "name": "totals lines", "status": "passed"}
]}
```

`tools/reliab-adapter-specrun`:

```python
#!/usr/bin/env python3
"""Convert a specrun report into reliab's findings file.

usage: reliab-adapter-specrun <specrun exit code> <report.json> <findings.json>
"""
import json
import sys


def convert(report):
    findings, seen = [], {}
    for r in report["results"]:
        if r["status"] != "failed":
            continue
        test = f'{r["suite"]} > {r["name"]}'
        seen[test] = seen.get(test, 0) + 1
        if seen[test] > 1:
            test += f" (#{seen[test]})"
        f = {"test": test, "message": r.get("message", "")}
        if r.get("file"):
            f["location"] = f'{r["file"]}:{r["line"]}' if r.get("line") else r["file"]
        findings.append(f)
    return {"schemaVersion": "0.4.0", "findings": findings}


def main(argv):
    if len(argv) != 4:
        print(__doc__.strip().splitlines()[-1], file=sys.stderr)
        return 2
    code, report_path, out = argv[1], argv[2], argv[3]
    # specrun exits 0 whether or not tests failed, and 2 when it could not run.
    if code != "0":
        print(f"specrun exited {code}: it did not run", file=sys.stderr)
        return 1
    try:
        with open(report_path, encoding="utf-8") as fh:
            doc = convert(json.load(fh))
    except (OSError, ValueError, KeyError) as e:
        print(f"{report_path}: cannot read specrun report: {e}", file=sys.stderr)
        return 1
    with open(out, "w", encoding="utf-8") as fh:
        json.dump(doc, fh, indent=2)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
```

specrun's report paths are relative to where it ran, the repository root,
so they are already repo-relative. A tool run from a subdirectory would
need its paths prefixed.

### Test it

Save real reports from the tool as fixtures: a clean run, a run with a
planted failure, the same failure after inserting a few lines above it, and
a run where the tool cannot start. Then check each one:

```sh
a=tools/reliab-adapter-specrun; d=tools/testdata/specrun
python3 $a 0 $d/clean.json /tmp/clean.json && reliab findings check /tmp/clean.json    # 0 findings, exit 0
python3 $a 0 $d/failing.json /tmp/failing.json && reliab findings check /tmp/failing.json  # the planted test, its file and line
python3 $a 0 $d/moved.json /tmp/moved.json && reliab findings check /tmp/moved.json    # the same test names as failing
python3 $a 2 $d/missing.json /tmp/broken.json; echo $?                          # 1, and no /tmp/broken.json
```

Put the same checks in the repository's own test framework so they keep
running. Then probe it through reliab with a planted failure: add the
command to the profile, run `reliab verify --only unit`, and confirm
`reliab report route` shows a finding naming the planted test and line.
