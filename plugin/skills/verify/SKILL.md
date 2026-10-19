---
name: verify
description: Run reliab verify when a code change is done and before a PR, then act on each finding by kind. Use when finishing a change in a repository with reliability-profile.yaml.
---

# Verify a change with reliab

The repository's `reliability-profile.yaml` says what runs. You never run the
test tools directly for verification; `reliab verify` runs them under a time
budget and writes the one report everything else reads.

## When

- When a change is done, before you stop or hand back:
  `reliab verify --tier inner` (budget about 5 minutes). Not after each file
  edit; finish the change first. The stop hook blocks stopping while product
  code changed since the last report.
- Before opening a PR or pushing: `reliab verify --tier prepr`. The push is
  denied unless this report is fresh, complete, and has no open findings.
  Then have the `reliab:reviewer` agent review the change, and write the PR
  evidence with `reliab report summary`; the summary skill has the steps.

## Then

1. Run `reliab report route`. It lists each open finding and what to do.
2. For each `unclassified` finding: reproduce it from its seed or saved input,
   shrink it to a minimal case, decide its kind, and record it:
   `reliab findings classify <id> --kind <kind> --reason "<one line>"`.
   Classify before fixing: the record stays in the run's history, and the
   PR summary lists the finding as fixed with that kind and reason. A
   finding you fixed first can still be classified; `classify` then
   records it on the last run that saw it.
3. Act by kind:
   - `new-code-defect`: fix the product code. Never change an assertion, widen
     a tolerance, or add a catch block to make a test pass.
   - `harness-defect`: fix the test harness.
   - `wrong-property`: delete the property and say why.
   - surviving mutant (`mutation_*` command): kill it with a test only when
     its behavior is intended, and cite where that is stated; the mutation
     skill has the steps. If nothing states it, classify it
     `requirement-silent` with the question. If the mutation cannot change
     behavior, classify it `equivalent-mutant` with a one-line reason.
   - `below-threshold`: a mutation score under the profile's threshold or a
     scope's baseline. It is its survivors' sum, not work of its own: handle
     each survivor as above, never write a test only to raise the score, and
     never classify it. Once its survivors are with a person or decided, it
     stops for a person, who accepts the score or says what to test.
   - the machine, not the code, failed (out of consoles, memory, disk, or
     network; a service the tests need is down): re-run the failing command
     once; if it fails the same way, classify it `environment` and quote the
     machine's error in the reason. A test failing for a reason you cannot
     explain is not `environment`.
   - `legacy-defect-or-quirk`, `characterization-diff`, `equivalent-mutant`,
     `requirement-silent`, `environment`, or `below-threshold`: stop and tell the user. A person decides; you
     never fix pinned behavior or approve a snapshot, and you never run
     `reliab findings ack`.
   - a failing case the tool saved under a protected path (a regression
     seed): commit it with the fix and tell the user it waits for their
     approval. A person runs `reliab approve <path> --by human:<name>`; you
     never do, and hooks deny it to you. The tool writes the case during
     the failing run; you only commit it. To bring one in from another
     branch or clone, re-run the failing property against the code before
     the fix so the tool writes it again, or stop and give the user the
     exact commands to copy it in.
4. Re-run the same tier until it exits 0, or until only findings that need a
   human remain.

## On demand

`reliab verify --only <command>... [--scope <path>...]` runs just the named
profile commands, under the `--tier` budget, and writes the on-demand
report. Use it when the person points you at an area and a kind of testing
(the mutation and properties skills say how). It never satisfies the stop
hook or the push gate; the inner and pre-PR tiers still run as above.

## Exit codes

`0` passed and complete, `1` open findings, `2` config error (run
`reliab doctor`), `3` incomplete because the budget ran out. Report what was
skipped and why; `reliab report route` lists budget skips and commands
skipped as not relevant to the change (the profile's `relevant_when`), with
what the change touched. A relevance skip is a claim about the change: if
you know the change does reach that layer, run it with
`reliab verify --only <command>` and say so.

## Reporting

In your summary say which tier ran, what was skipped and why, every finding
and how it was resolved, and every question for a human. In a PR
description, paste `reliab report summary` instead of writing it yourself.
