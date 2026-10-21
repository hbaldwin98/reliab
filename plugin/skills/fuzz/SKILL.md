---
name: fuzz
description: Run coverage-guided fuzzing on one area of the code on demand, for a time box, then reproduce, shrink, and fix or classify each crash. Use when asked to fuzz, find crashes, or harden a parser, decoder, or protocol reader in a repository with reliability-profile.yaml.
argument-hint: <path>... [seconds]
---

# Fuzzing on demand

The person names an area and wants to know whether inputs nobody wrote a
test for break it. A fuzzer mutates inputs, guided by coverage, until it
finds one that crashes, hangs, or fails a check, then saves it.

## 1. Run it

If `reliability-profile.yaml` has no `fuzz` command, or T15 is not
applicable, stop and say so. Never run the fuzzer directly; reliab ingests
what the command writes.

```sh
reliab verify --only fuzz --scope <path> [--scope <path>...] --tier prepr
```

The tier sets the time box: `fuzz_seconds` is 120 at prepr and 1800 at
nightly unless the profile says otherwise. For a longer hunt, use
`--tier nightly`; say that you chose it and why. The command splits the
time among the fuzz targets under the scope, so more targets each get less
time: narrow the scope rather than lengthen the box when you care about one
target. The run writes `.reliab/reports/on-demand-latest.json`.

If the area has no fuzz targets, say so, and offer to write one: a target
takes the raw input the code takes from outside (bytes, a string, a
request) and calls the code with it. Its comment cites what the code must
tolerate: a format spec, a protocol doc, or a caller that passes untrusted
input. Write it only when the person agrees.

## 2. Act on each crash

Run `reliab report route`. For each finding:

1. Reproduce it from the saved input: re-run the target on that input alone
   (`go test -run '<Target>/<input>'`, `cargo fuzz run <target> <file>`),
   as the finding or the profile's notes say.
2. Shrink it if the tool can (`cargo fuzz tmin`); a short input makes the
   cause plain.
3. Decide what it is, as the verify skill says: a defect in new code (fix
   the product code and re-run), one that predates this work
   (`legacy-defect-or-quirk`: stop, a person decides), or a target that
   checks something the code never promised (`wrong-property`: fix or delete
   the target and say why).

The saved input is a regression seed under a protected path: commit it with
its fix and list it as a question for the person, who approves it with
`reliab approve <path>`. Never delete one to make a run pass.

## 3. Re-run

Re-run the same command until no new crash appears in the time box, then
run `reliab verify --tier inner`, because you changed code or tests.

## Report

Say what ran (command, scope, tier, seconds), each crash with its input,
cause, and classification or fix, any target you wrote and the source it
cites, and anything skipped and why.
