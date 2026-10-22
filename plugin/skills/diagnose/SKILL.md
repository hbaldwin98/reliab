---
name: diagnose
description: Take a symptom (a stack trace, a log line, a failing request, a flaky test, "it hangs when") to its cause and a failing-then-passing test, using reliab debug run to see the program's state when a test's output does not show the cause. Use when asked to diagnose, debug, or find the cause of a failure in a repository with reliability-profile.yaml.
argument-hint: <symptom>
---

# Diagnose a symptom

The person gives a symptom and wants its cause, fixed, with a test that
pins it. A debugger is a view into the tests, not a layer: the run that
shows the cause is evidence for you, and the test you write is the
evidence that stays.

## 1. State it

Say the symptom in one line, and name the layer its test belongs in once
fixed: a unit test, a property, a snapshot, a fuzz seed, or a store fault
test. Pick the cheapest layer that would have caught it.

## 2. Reproduce it

Start with the cheapest test that shows it: an existing failing test, a
new test written from the symptom, or a `reliab characterize capture` of
the conversation. Run it and see it fail the way the symptom says.

Only when the output does not show the cause, run the test under the
debugger:

```sh
reliab debug run --set program=<package or file> --set test=<test name> \
  --break <file>:<line> [--break '<file>:<line> if <condition>'] \
  --dump '<expression>' [--dump ...] [--catch]
```

Breakpoints go at the suspect lines; dumps are the values your hypothesis
depends on. Before you read the transcript, write down what you expect each
dump to show. A value that differs from your expectation is the lead; a run
that matches it rules the hypothesis out.

- A `warning: breakpoint ... is not verified` means the run will not stop
  there: a path the adapter cannot map, a line without code, an optimized
  build. Move the breakpoint before reading anything into "it never
  stopped".
- `--catch` stops at an exception or panic; `reliab debug probe` lists the
  adapter's filters for `--catch-filter`.
- The placeholders `--set` fills, and the block for each adapter, are in
  `reliab docs debug`.

If the profile has no `debug` section, say so. Offer the block for the
repository's language from `reliab docs debug`, for the person to add (the
profile is protected), with `reliab debug probe` as the check, and go on
with what the tests' output shows.

## 3. Narrow it

Move the breakpoints toward the cause and run again. When the symptom is a
regression, `git bisect run` over the reproducing test finds the commit
that introduced it. Each run writes a new transcript; keep a short log:

| Hypothesis | Run (breakpoints, dumps) | Expected | Shown |
|---|---|---|---|

Stop narrowing when you can say the cause in one sentence.

## 4. Pin it, then fix it

Write the failing test first, in the layer from step 1. Its comment cites
the symptom as its source (where it was reported, or the person's words).
See it fail for the reason you found. Then fix the product code, never the
test, and see it pass.

A crash input or a captured request becomes a regression seed under a
protected path: commit it with the fix and list it as a question for the
person, who approves it with `reliab approve <path>`.

## 5. Verify and report

Run `reliab verify --tier inner` and act on `reliab report route`. Classify
the finding: `new-code-defect` when the change you are working on caused
it; `legacy-defect-or-quirk` when it predates the change, and then stop, a
person decides.

Report: the symptom, the cause in one sentence, the test that now pins it,
the transcripts' paths (`.reliab/debug/...`), quoting only the lines that
show the cause, and anything you could not reproduce.

## Never

- Attach (`reliab debug attach`) to a process you did not start, unless the
  person names its pid.
- Edit a transcript, or paste one whole into a PR.
- Widen an assertion or a tolerance, or catch an exception, to make the
  reproduction pass.
