# reliab in this repository

reliab is a testing CLI and a Claude Code plugin. It runs the repository's
own test tools, in order and on a target, and turns their output into one
findings format. The commands are in `reliab.yaml`; reliab knows nothing
about the stack beyond what that file says. It never replaces a test
framework: it runs the one the repository has.

## What to run when

| When | Command |
|---|---|
| Before anything: what would run, and why | `reliab run <context> --plan` |
| A change is done | `reliab run change` |
| A branch is about to merge | `reliab run pr` |
| Existing code, everything | `reliab run audit` |
| One area | add `--target <project file, folder, file, or file#Function>` |
| Rerun a property or fuzz failure | add `--seed <n>` (a failed run prints it) |

`change` and `pr` look at the files changed since the base; `audit` looks
at everything. A command runs in a context when `reliab.yaml` gives it a
line there. Independent commands run in parallel; a command waits for the
ones in its `after`, and is `blocked` if one failed. An `exclusive` command
runs alone.

Each command has an `about`: what it checks and what its failure means.
`--plan` shows it, and so does every failure.

Exit codes: `0` everything passed, `1` something failed, `2` a config or
usage error. Logs are in `.reliab/run/<command>.log`.

## Findings

A tool's output becomes findings: a test, a location, a message. Whoever
acts on one decides its kind:

- **fix-code**: the product code is wrong;
- **fix-test**: the test, the harness, or a missing test is wrong;
- **ask-human**: only a person can say whether the behavior is intended, or
  the machine is broken.

With `--target`, findings outside the target's files and lines are dropped.

## The plugin's skills

- `/reliab:run`: run a context, act on each failure.
- `/reliab:onboard`: write `reliab.yaml` from the tools the project uses.
- `/reliab:mutation`: mutation-test an area and handle the survivors.
- `/reliab:properties`: write property tests for an area from its intent.
- `/reliab:fuzz`: fuzz an area's inputs.
- `/reliab:diagnose`: take a symptom to its cause with `reliab debug`.
- `/reliab:adapter`: write an adapter for a tool reliab cannot read.

## Testing rules for agents

Copy this block into the repository's `AGENTS.md`:

<!-- reliab:begin -->
## Testing rules (reliab)

### Workflow
- Start with `reliab run <context> --plan`: it shows what will run, in what order, and what each command's failure means.
- Run `reliab run change` when a change is done, not after each file edit. Run `reliab run pr` before a branch merges.
- To look at one area, add `--target` with a project, folder, file, or `file#Function`.
- Write tests from intent. Each new test cites its source in a comment: a ticket, a doc, or a caller that depends on the behavior. The implementation is not a source.
- Reproduce a property or fuzz failure from the seed the run prints (`--seed`), and shrink it before fixing it.
- Check that each new test can fail: no swallowed exceptions, no missing assertions, and the generator reaches the interesting cases.
- Report what you ran, what failed, and what you skipped and why.

### When something fails
Decide what kind of failure it is, then act:
- fix-code: the product code is wrong. Fix it and run again.
- fix-test: the test, the harness, or a missing test is wrong. Fix it and run again. If a property is wrong, delete it and say why.
- ask-human: only a person can say whether the behavior is intended, or the machine is broken. Stop and ask, quoting the evidence.

Never change an assertion, widen a tolerance, or add a catch block to make a test pass.

### Mutation testing
- Kill a surviving mutant only when its behavior is stated somewhere (a ticket, a doc, a caller that depends on it), and cite that in the test.
- If nothing states the behavior, don't pin it. Ask a person whether it is intended.
- If a mutant cannot change behavior, say so in one line. It needs no test.
- Never write a test only to raise the score.
<!-- reliab:end -->
