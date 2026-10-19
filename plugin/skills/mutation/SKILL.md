---
name: mutation
description: Run mutation testing on one area of the code on demand, then kill each surviving mutant with a test or explain why it cannot be killed. Use when asked to mutation-test, find weak tests, or check test strength for a path in a repository with reliability-profile.yaml.
argument-hint: <path>... [changed]
---

# Mutation testing on demand

The person names an area (paths) and wants to know whether the tests there
would notice a change in behavior. Each surviving mutant is a change no test
noticed. You kill it with a test, or show it cannot change behavior.

## 1. Run it

Pick the profile command:

- `mutation_full`: every mutant under the area. The default.
- `mutation_changed`: only mutants on lines this branch changed, when the
  person asks about their change.

If `reliability-profile.yaml` defines neither, or T4 is not applicable, stop
and say so; never run the mutation tool directly (listing mutants to size the run, below, is the exception).

```sh
reliab verify --only mutation_full --scope <path> [--scope <path>...] --tier prepr
```

`--tier` sets the budget only (prepr is 15 minutes). The run writes
`.reliab/reports/on-demand-latest.json`; it never stands in for the inner or
pre-PR report.

Size it before you run it. If the profile's mutation tool can list mutants
without running them (`cargo mutants --list -f <path>`, `gremlins unleash
--dry-run <pkg>`), count them for the scope; listing is the one direct use
of the tool allowed here. A mutant costs about a build plus a test run, so a
scope that cannot finish in 15 minutes goes straight to `--tier nightly` (no
budget); say that you chose it and why. On exit 3 (budget ran out anyway)
narrow the scope, or re-run with `--tier nightly` and say that you did.

## 2. Act on each survivor

Run `reliab report route`. For each surviving mutant:

1. Read the mutation: the location, the operator, and the diff if shown.
   Say in one sentence what behavior it changes.
2. Find where that behavior is intended: a ticket, a doc, a reviewed rule
   card, or a caller that depends on it. The implementation is not a source:
   that the code does something now does not mean anyone needs it to.
3. Decide which of three it is. A mutant is killed only when the behavior
   matters; a test that pins incidental behavior to raise the score is a
   change-detector, and worse than the survivor.
   - **Intended, and you found where:** write a test that fails with the
     mutation and passes without it. Its comment names the source (file and
     section, ticket, rule, or the caller and what it relies on). Assert the
     behavior, not the line: the test should pass against any correct
     implementation.
   - **Real behavior, but nothing states it:** do not write a test. Record
     the question and move on; a person answers it:
     `reliab findings classify <id> --kind requirement-silent --reason "<the question>"`.
   - **Cannot change observable behavior:** no test can kill it. Classify it;
     a person confirms it later:
     `reliab findings classify <id> --kind equivalent-mutant --reason "<one line>"`.
     Code this build never compiles (a `cfg` for another platform, a feature
     flag that is off) is not equivalent: the mutant survives because nothing
     here runs it. Classify that `environment` and name the platform that
     can test it.

Never change product code to kill a mutant. If your new test fails on the
unmutated code, that is a finding: reproduce it, shrink it, and classify it
(`legacy-defect-or-quirk` for behavior that predates this work: stop, a
person decides).

A `below-threshold` finding says the score is short of the profile's
threshold or a scope's baseline, and by how many. It is not a reason to
write tests: handle its survivors as above, and it changes as they do.
Never classify it.

## 3. Re-run

Re-run the same `reliab verify --only ...` command until every survivor is
killed or classified. Then run `reliab verify --tier inner`, because you
changed tests, and act on it as the verify skill says.

## Report

Say what ran (command, scope, tier budget, time), how many mutants survived,
which tests killed which and the source each cites, each equivalent mutant
and its reason, each `requirement-silent` question, anything skipped and why.
