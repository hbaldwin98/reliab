---
name: characterize
description: Capture an area's current behavior before a refactor or migration, make the change, compare, and throw the capture away. Use when asked to characterize an area, pin behavior before a refactor, or check that a refactor or migration kept behavior, in a repository with reliability-profile.yaml.
argument-hint: <path>... [the change]
---

# Characterization on demand

The person names an area (paths) and a change that should keep its behavior:
a refactor, or a migration with stated differences. You capture what the
area does now, make the change, and compare. The capture is a temporary
oracle: it lives under `.reliab/`, is never committed, and nobody approves
it. Capture, scrub, compare, stabilize; never approve a capture.

## 1. Check the profile

`reliability-profile.yaml` must define a `characterize` command, which
writes transcripts of the area's behavior into `{capture}`. If it does not,
stop and say so: what to capture and how to scrub it is the project's
decision. Never write captures yourself or run the project's capture tool
directly.

## 2. Capture before you change anything

```sh
reliab characterize capture --scope <path> [--scope <path>...]
```

On exit 1 the capture failed: run `reliab report route`, reproduce, and
classify it as for any failure. On "a capture already exists", ask the
person whether it is theirs; run `reliab characterize discard` only if it is
stale.

## 3. Stabilize

Run `reliab characterize compare` once before changing any code. Every
difference now is nondeterminism the capture does not scrub (times, ids,
ordering, temp paths), not a change in behavior. Do not go on with an
unstable capture. Classify each such difference `harness-defect`, naming
what varies, and make the `characterize` command or its harness scrub it
(never in a protected path; if you cannot, stop and tell the person). Then
discard, capture, and compare again. That is safe only now, before any
product code has changed.

## 4. Make the change, then compare

Make the change. Then:

```sh
reliab characterize compare
reliab report route
```

Each difference is a finding labelled `additive-only` (lines added, nothing
removed or changed) or `values-changed` (something removed or changed).
For each one:

- **The person stated this difference** (the migration is meant to change
  it, and you can cite where): classify it `characterization-diff` with the
  citation, and show it to the person. They decide.
- **Nobody stated it:** it is a side effect of your change. Classify it
  `new-code-defect`, fix the product code, and compare again. A refactor
  states no differences, so every difference in a refactor is this case.
- **You cannot tell:** classify it `characterization-diff` with the
  question and stop.

An `additive-only` label is not permission: new output from a refactor is
still a side effect. Never capture again to make a difference go away;
capturing after the change approves it unseen, and reliab refuses while a
capture exists.

Repeat compare until it reports no differences, or every remaining one is
`characterization-diff` and waiting on the person.

## 5. Discard

When compare is clean or the person has decided each difference:

```sh
reliab characterize discard
```

Then run `reliab verify --tier inner`, because you changed code, and act on
it as the verify skill says.

## Report

Say what was captured (scope, file count), whether the capture was stable,
each difference with its label and what you did about it, each question for
the person, that the capture was discarded, and anything skipped and why.
