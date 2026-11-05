---
name: fuzz
description: Run coverage-guided fuzzing on one area of the code, for a time box, then reproduce, shrink, and fix or report each crash. Use when asked to fuzz, find crashes, or harden a parser, decoder, or protocol reader in a repository with reliab.yaml.
argument-hint: <path>... [seconds]
---

# Fuzzing on demand

The person names an area and wants to know whether inputs nobody wrote a
test for break it. A fuzzer mutates inputs, guided by coverage, until it
finds one that crashes, hangs, or fails a check, then saves it.

## 1. Run it

Find the fuzz command in `reliab.yaml`: its `about` says so, and it usually
runs only in `audit`. If there is none, stop and say so, and offer the
person a command block to add. Never run the fuzzer directly; reliab reads
what the command writes.

```sh
reliab run audit --target <path> --plan
reliab run audit --target <path>
```

The command's own line sets the time box. If it uses `{files}` or
`{projects}`, the target narrows which fuzz targets run. More targets each
get less time, so narrow the target rather than lengthen the box when you
care about one. The full log is `.reliab/run/<command>.log`.

If the area has no fuzz targets, say so, and offer to write one: a target
takes the raw input the code takes from outside (bytes, a string, a
request) and calls the code with it. Its comment cites what the code must
tolerate: a format spec, a protocol doc, or a caller that passes untrusted
input. Write it only when the person agrees.

## 2. Act on each crash

For each finding:

1. Reproduce it from the saved input: re-run the target on that input alone
   (`go test -run '<Target>/<input>'`, `cargo fuzz run <target> <file>`).
2. Shrink it if the tool can (`cargo fuzz tmin`); a short input makes the
   cause plain.
3. Decide what it is: a defect in new code (fix-code: fix the product code
   and re-run), one that predates this work (ask-human: stop, a person
   decides), or a target that checks something the code never promised
   (fix-test: fix or delete the target and say why).

Commit the saved input with its fix as a regression seed. Never delete one
to make a run pass.

## 3. Re-run

Re-run the same command until no new crash appears in the time box, then
run `reliab run change`, because you changed code or tests.

## Report

Say what ran (command line, target, seconds), each crash with its input,
cause, and fix or question, any target you wrote and the source it cites,
and anything skipped and why.
