---
name: summary
description: Get the reviewer's verdict on a change, then write the evidence section of the PR description from reliab's pre-PR report. Use when opening a PR, pushing a change for review, or writing a PR description in a repository with reliability-profile.yaml.
---

# PR evidence from the report

A person reading the PR should be able to check every line of its evidence
against the report. So the evidence section is reliab's output, pasted, and
a separate reviewer has re-checked it first.

## 1. A clean pre-PR run

Commit the change. Run `reliab verify --tier prepr` and act on
`reliab report route` as the verify skill says, until it exits 0 or only
findings that wait for a person remain.

If the change commits a file under a protected path (a regression seed, say),
ask the user to approve it before the review: the reviewer fails a change
with an unapproved protected file. Run `reliab report summary` to see which.

## 2. The reviewer

Have the `reliab:reviewer` agent review the change (the Agent tool, with
that agent type). Tell it the base branch. It re-runs prepr in a clean
worktree of your commit, reads the diff, and records a verdict. Never run
`reliab report verdict` yourself; hooks deny it, and a verdict you wrote
would mean nothing.

- **pass**: go on.
- **fail**: its notes list each problem and where. Fix what is yours to fix
  (product code, a missing citation, a test that cannot fail), commit, and
  start again at step 1. A problem a person must decide is a question for
  the user, not something to argue the reviewer out of.

Any edit after the review voids the verdict: the report goes stale, and the
pre-push reminder asks for a new review.

## 3. The evidence section

```sh
reliab report summary
```

Paste its output as the PR body's evidence section, unchanged. Above it,
write what the change does and why. Within and about the evidence:

- Add nothing the report does not support. No "all tests pass", "fully
  covered", or "safe" unless a line of the summary says exactly that.
- Do not reword, reorder, round, or drop its lines. If something in it is
  wrong, the report is wrong: fix the cause and re-run, never the text.
- If it starts with **Stale** or **Incomplete**, do not paste it. Re-run
  prepr (and the review) first.
- Repeat the **Questions for a human** to the user as well; the PR waits on
  their answers. They include every protected file the branch changed that
  no person has approved, such as a committed regression seed. The user
  approves one with `reliab approve <path> --by human:<name>`; you never
  run it. Their approval covers that content only: if you edit the file
  afterwards, it is a question again, and the review starts over.

`reliab report summary --format json` gives the same facts for tooling.
