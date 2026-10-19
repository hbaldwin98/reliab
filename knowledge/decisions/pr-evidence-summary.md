---
type: Decision
title: "The PR's evidence section is rendered from the report"
description: "reliab report summary renders a tier's latest report (default prepr) as Markdown or JSON for a PR body: what ran, skips and why, mutation counts and survivors, findings open, acknowledged, and fixed during the change, and the questions for a human. It states only what reports say, and flags a stale or incomplete report at the top."
status: draft
tags: [report, plugin, pr]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-18T16:00:00-07:00
---

# The PR's evidence section is rendered from the report

## Context

Step 3 of the [roadmap](/decisions/agentic-flow-roadmap.md) asks for a PR a
person can trust because its evidence is generated, not written. An agent
asked to summarize its own testing tends to claim more than it ran ("all
tests pass") and to drop what it skipped. [R8](/rules/r8-time-box-and-report-skips.md)
already requires skips to be reported. The report holds the facts; what
was missing was a rendering of them, and two facts the report did not
hold: how many mutants were tested (it listed only survivors), and which
findings were fixed along the way (a finding that stops failing simply
leaves the next report).

## Decision

- **`reliab report summary [--tier prepr] [--format markdown|json] [--base
  <ref>]`** reads the tier's latest report and prints, in order: a warning
  line if the report is stale (the freshness rule of
  [report-freshness](/decisions/report-freshness.md), the same one verify
  and the push gate use) or incomplete; the run (tier, profile, tool
  version, start, elapsed of budget, tree, commit, seed); the reviewer
  verdict and whether it is for this tree; a table of commands that ran
  (exit code, finding count, time); a table of skips with the report's
  reason (budget, no command, role not applicable) and its detail, plus the
  profile's commands this tier does not run; mutation counts and every
  survivor with its kind, status, and classification reason; findings open,
  acknowledged by a person, fixed during the change, and reported earlier
  by commands this run did not repeat; and **Questions for a human**, one
  per open finding whose kind stops for a person (the triage table's STOP
  rows), each with what the person decides and the agent's recorded reason,
  then one per protected file changed since the base without a person's
  approval, followed by the approved ones and who approved them
  ([protected-path-approval](/decisions/protected-path-approval.md); added
  2026-10-18, when a summary said "None" while a seed waited on a person).
  `--format json` prints the same facts as one object. Its `questions` are
  the Markdown section's entries, from one source (`AllQuestions`):
  findings that need a person, each unapproved protected change (kind
  `protected-change`), and protected paths that could not be checked
  (`protected-unchecked`); the raw changes stay in `protected` too (added
  2026-10-19, when `questions` was empty while the Markdown listed an
  unapproved change, and a reviewer reading the JSON wrote a false note
  into its verdict).
- **Layout rules**, so a reader knows what an absent line means:
  - The top warns **Stale** when the tree changed, **Freshness unknown**
    (with the error) when the profile or tree cannot be read, and
    **Incomplete** when the budget ran out.
  - The commit, seed, and property cases appear when the report has them.
    Hashes are shortened to 12 characters. A budget of 0 reads
    "unlimited".
  - Ran, the finding counts, and Questions for a human always appear; an
    empty Ran says "(none)" and empty Questions say "None" (only when the
    protected paths were checked; otherwise a line says why not). Skipped,
    Mutation, the survivor table, and each finding table appear only when
    they have rows.
  - A mutation line reads `N tested, K killed, S survived`, adds `C not
    covered by any test` only when C is above 0, and says the tool reported
    no counts when it did not.
  - A finding's subject is its test, or `command <id>` for a whole-command
    failure, then `at <location>` when known and `(diff: <label>)` for a
    labelled snapshot. An acknowledged finding names who acknowledged it.
  - Questions are numbered from 1.
- **Facts only.** Every line is a field of a report, a count of them, or
  the profile's tier list. The agent's own words appear only as recorded
  classification reasons and verdict notes, quoted as such. Nothing says
  "passed" or "safe".
- **Mutant counts are recorded.** The findings format (0.3.0) gains an
  optional top-level `mutants: {killed, survived, notCovered}`, and each
  report command (report 0.4.0) an optional `mutants` read from its output.
  Killed includes timeouts, matching how survivors are already judged;
  mutants that did not build or were skipped are not counted. reliab reads
  them from the mutation-testing-elements format itself and from findings
  files; `reliab-adapter-gremlins` (after its changed-lines filter) and
  `reliab-adapter-cargo-mutants` write them. Tested is killed plus
  survived.
- **Fixed means seen earlier on this branch and gone now, though its command
  ran.** The report records `run.head`, the commit it ran on. The summary
  reads `.reliab/reports/history/` and takes the runs whose head lies
  between the merge base with `--base` and the current HEAD (so runs from
  another branch in the same checkout do not count), started before this
  report. A finding id one of them reported and this report lacks is
  **fixed** if this report ran the command that found it, and **not
  re-checked** otherwise (an on-demand `mutation_full` survivor, say). The
  base defaults to the first of `origin/HEAD`, `origin/main`, `main`,
  `master` that resolves; with none, the section says the earlier runs were
  not read. Verify itself still never marks a finding `fixed`; only the
  summary compares runs.
- **The `summary` skill** has the agent run prepr, get the reviewer's
  verdict ([reviewer-verdict](/decisions/reviewer-verdict.md)), and paste
  the output unchanged as the PR body's evidence section, adding nothing it
  does not support and never pasting a stale or incomplete one. The verify
  skill and the AGENTS rules point to it.

## Consequences

- A person can check the evidence line by line against
  `prepr-latest.json`; the only other inputs are the history reports and
  the profile's tier list.
- A fixed finding shows the kind and reason it was classified with, read
  from the history copy of the last run that saw it
  ([report-storage](/decisions/report-storage.md)). Before 2026-10-18
  classify wrote only the latest report, so such findings read
  `unclassified`.
- "Fixed" is only as good as finding ids are stable
  ([verify-execution](/decisions/verify-execution.md)). A renamed test reads
  as one fixed and one new finding.
- Reports written before 0.4.0 have no `head`, so they never count as
  earlier runs on a branch.
- Rewriting the branch's history loses them too. A run on a commit that
  was later amended, squashed, or rebased away has a head that is no
  longer an ancestor of HEAD, so its findings never count as fixed. Seen
  on this change itself: its first prepr run had 40 surviving mutants,
  all killed afterwards, but those runs were on WIP commits that were
  squashed, and the final summary reads "Fixed during the change: 0".
  Matching by tree instead of commit, or keeping a branch-local log of
  runs, would close the gap; neither is built.
- A **relevance** skip (a layer skipped because the change cannot affect
  it, the paper's "writes nothing" example) reads "not relevant to the
  change" with its detail, and commands with relevance rules say why they
  ran (added 2026-10-18: [relevance-skips](/decisions/relevance-skips.md)).
- Each mutation command's score against its threshold and baseline
  scopes is shown under its counts (added 2026-10-18:
  [mutation-threshold](/decisions/mutation-threshold.md)).
- The order of findings is the report's. Ranking them waits on the
  [R6 rubric](/open-questions/r6-scoring-rubric.md).
