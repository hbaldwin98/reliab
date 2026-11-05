---
name: mutation
description: Run mutation testing on one area of the code, then kill each surviving mutant with a test or say why it should not be killed. Use when asked to mutation-test, find weak tests, or check test strength for a path in a repository with reliab.yaml.
argument-hint: <path>... [changed]
---

# Mutation testing on demand

The person names an area (paths) and wants to know whether the tests there
would notice a change in behavior. Each surviving mutant is a change no test
noticed. You kill it with a test, or say why you should not.

## 1. Run it

Find the mutation command in `reliab.yaml`: its `about` says it is mutation
testing, and its `output` usually has `parse: mutation-report`. If there is
none, stop and say so. Never run the mutation tool directly.

Look first, then run:

```sh
reliab run audit --target <path> [--target <path>...] --plan
reliab run audit --target <path> [--target <path>...]
```

Use `audit` for every mutant in the area; it is the default. Use `pr` when
the person asks about their change. A target can be a project file, a
folder, a file, or `file#Function`.

Mutation is slow: about one build and test run per mutant. If the planned
line would mutate far more than the person asked about, narrow the target.
A command that hits its `timeout` is reported as timed out; narrow the
target and say so.

Survivors outside the target are dropped from the output. The full log is
`.reliab/run/<command>.log`.

## 2. Act on each survivor

For each surviving mutant:

1. Read the mutation: the location, the operator, and the diff if shown.
   Say in one sentence what behavior it changes.
2. Find where that behavior is intended: a ticket, a doc, or a caller that
   depends on it. The implementation is not a source: that the code does
   something now does not mean anyone needs it to.
3. Decide which it is. Kill a mutant only when the behavior matters. A test
   that pins incidental behavior to raise the score is a change-detector,
   and worse than the survivor.
   - **Intended, and you found where (fix-test):** write a test that fails
     with the mutation and passes without it. Its comment names the source.
     Assert the behavior, not the line: the test should pass against any
     correct implementation.
   - **Real behavior, but nothing states it (ask-human):** do not write a
     test. Write the question down for the person.
   - **Cannot change observable behavior (ask-human):** no test can kill
     it. Give a one-line reason; a person confirms it. Code this build never
     compiles (a `cfg` for another platform, a flag that is off) is not
     equivalent: name the platform that can test it.

Never change product code to kill a mutant. If your new test fails on the
unmutated code, that is a finding: reproduce it and shrink it. If the code
is new on this branch, it is fix-code. If the behavior predates this work,
it is ask-human: stop, a person decides.

If the command has a `threshold`, a score below it fails the command. That
is not a reason to write tests: handle the survivors as above, and the
score changes as they do. Never write a test only to raise the score.

## 3. Re-run

Re-run the same command until every survivor is killed or listed as a
question. Then run `reliab run change`, because you changed tests.

## Report

Say what ran (command line, target, time), how many mutants survived, which
tests killed which and the source each cites, each mutant you think cannot
be killed and why, each question for the person, and anything skipped and
why.
