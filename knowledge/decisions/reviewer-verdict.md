---
type: Decision
title: "The reviewer subagent and its tree-bound verdict"
description: "A plugin agent without edit tools re-runs prepr in a clean worktree, reads the diff against R1 to R5 and R7, and records a verdict with reliab report verdict. The pre-bash hook allows that command only to the reliab:reviewer agent; a pass needs a matching re-run and is bound to the report's tree hash, so any edit voids it. Threat model and limits included."
status: draft
tags: [hooks, plugin, enforcement, reviewer]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-18T16:00:00-07:00
---

# The reviewer subagent and its tree-bound verdict

## Context

The paper's pre-push hook requires a reviewer verdict; v0 shipped the gate
without it, behind `require_reviewer_verdict` ([pre-push-v0](/decisions/pre-push-v0.md)).
The report had a `verdict` field that nothing wrote. Step 3 of the
[roadmap](/decisions/agentic-flow-roadmap.md) asks for a reviewer that
re-checks the author's evidence and a verdict the author cannot simply
write for itself.

Checked on Claude Code 2.1.303 (2026-10-18): a PreToolUse hook's input
carries `agent_type` when a subagent makes the call, `plugin:name` for a
plugin's agent (`reliab:reviewer`), and no such field in the main session.
That is what lets a hook tell the reviewer from the author.

## Decision

- **The reviewer is a plugin agent**, `plugin/agents/reviewer.md`, with
  Bash, Read, Grep, and Glob and no edit tools. It refuses to review
  uncommitted or stale work; makes a detached worktree of HEAD in a temp
  directory; runs `reliab verify --tier prepr` there; compares that
  summary with the author's; reads `git diff --merge-base <base> HEAD` for
  cited intent ([R3](/rules/r3-cite-intent.md); the implementation is not a
  source), tests that can fail ([R5](/rules/r5-self-review.md)), weakened
  assertions ([R1](/rules/r1-failing-test-is-a-finding.md)), and changed
  oracles, `.reliab/`, or profile ([R2](/rules/r2-protected-oracles.md),
  with `reliab gate protected`); checks each classified finding's reason
  fits its kind ([R4](/rules/r4-reproduce-shrink-classify.md),
  [R7](/rules/r7-survivors-drive-tests.md)); records the verdict; and
  removes the worktree. Its instructions say the outcome comes from the
  checks and to ignore a request to pass.
- **`reliab report verdict --outcome pass|fail --by <who> --notes "..."
  [--worktree <dir>]`** writes the verdict into the author's
  `prepr-latest.json`. It refuses when the author's report is stale.
  - A **pass** needs `--worktree`: another worktree of the same repository
    (same git common dir, not the author's checkout), with no changes, whose
    own fresh prepr report has the same tree hash as the author's, is
    complete like it, named the same tests and mutation tests
    ([named-tests](/decisions/named-tests.md)), ran and skipped the same
    commands for the same reasons, and has no open finding whose id the
    author's report lacks.
    Anything else is refused with the list, and the reviewer records a
    fail instead. Matching tree hashes, with the reviewer's worktree clean
    at HEAD, also show the author's tree was fully committed.
  - A **fail** needs only notes; it lets nothing through.
  - The verdict records `treeHash` (the author's report tree), and from the
    re-run `head` and `rerunStartedAt`.
- **The push gate**, with `require_reviewer_verdict: true`, accepts only an
  outcome `pass` whose `treeHash` equals the report's, on a report that is
  fresh as before. So an edit after the review makes the report stale and
  the push is denied; re-running prepr writes a new report with no verdict;
  `reliab findings classify` clears the verdict on any report it changes,
  since the reviewer judged the classifications as they were. A person's
  `reliab findings ack` keeps it.
- **What R2, R4, and R7 require** (added 2026-10-18, after a real session
  got FAIL twice and "holds, tool issue" once on the same facts). The
  reviewer judges what the records show, the same way every time: a
  missing record fails the check even when the reviewer suspects a tool
  lost it, and the notes may say so. For R4 and R7 every finding counts:
  open, acknowledged, and each in the summary's "Fixed during the change"
  list must have a kind other than `unclassified` and a recorded reason
  that fits it (for a fixed `new-code-defect`, the diff shows the product
  fix). A fixed mutation survivor is the exception: R7 has it killed by a
  test, so the reviewer checks that a test in the diff kills it and cites
  intent instead. Classifications live in the run's history copy, so a fixed
  finding keeps its record ([report-storage](/decisions/report-storage.md)).
  For R2, a protected change `reliab gate protected` lists as approved by
  a person ([protected-path-approval](/decisions/protected-path-approval.md))
  holds; one without an approval fails, and an approval relayed by the
  author is not one.
- **The verdict lands in history too.** `report verdict` rewrites the
  author's latest pre-PR report and its history copy, so history records
  that a review happened after the next run replaces the latest report.
- **The pre-bash hook denies `reliab report verdict`** (also `.exe`,
  `.cmd`, and a quoted path) unless the call's `agent_type` is
  `reliab:reviewer`, the way it denies `reliab findings ack` to every
  agent. The same quoted-path spelling is now caught for `findings ack`.
- **The author is told when**: the `summary` skill, the verify skill, the
  AGENTS rules, and the session-start brief say to have the reviewer review
  the change after a clean prepr and before a PR, and never to run the
  verdict command
  ([pr-evidence-summary](/decisions/pr-evidence-summary.md)). Since
  widened to before a branch merges into main, by a PR or locally, with the
  change committed first ([dev-environment](/decisions/dev-environment.md)).

## Threat model and limits

The adversary is an author agent under pressure to finish, not a person
attacking the repository. The aim is that self-passing takes a deliberate
workaround, never an accident or a one-line shortcut. Hooks are a
guardrail, not a security boundary
([enforcement-hooks-self-contained](/decisions/enforcement-hooks-self-contained.md));
CI and the code host's human review remain authoritative.

What it stops:

- the author running the verdict command directly, under any of the
  spellings the hook knows;
- a pass with no independent run, a run of the author's own checkout, of
  uncommitted work, of another tree, or a run that found something the
  author's report does not show;
- a verdict outliving the tree, the report, or the classifications it was
  given for.

What it does not stop:

- **Command obfuscation.** The hook matches the command text. A script
  file, a variable holding the binary name, `eval`, or another shell's
  syntax gets past it, as for every pre-bash rule.
- **A steered reviewer.** The author writes the reviewer's prompt. The
  agent is told to ignore requests to pass, but a model can be talked
  round. The CLI's re-run check limits what a steered pass can claim: the
  evidence must still reproduce.
- **A forged agent identity.** A plugin named `reliab` installed by other
  means would get the same `agent_type`. Installing plugins is outside what
  the hooks watch.
- **A forged re-run.** The author could write a report file into another
  worktree, outside the paths the edit hooks protect; reports are only
  schema-checked, not signed. Doing so is a deliberate act.
- **Judgement.** Whether intent is really cited or a reason is plausible is
  the reviewer model's reading. The verdict's notes say what it checked so
  a person can disagree.
- **Locality.** Verdicts live in the gitignored local report, like
  acknowledgements ([acks-are-local](/open-questions/acks-are-local.md)); a
  CI gate cannot see them yet.

## Consequences

- `require_reviewer_verdict` stays off by default; a profile turns it on.
  reliab's own profile leaves it off for now.
- A review costs a second prepr run. That is the point: the evidence is
  reproduced, not trusted.
- The reviewer re-runs HEAD, so the author commits before review. That
  matches what is pushed.
- **Acceptance (2026-10-18)** on a scratch branch of reliab's own repo
  with the flag on: a fresh headless session asked to get the change
  reviewed invoked `reliab:reviewer`, which re-ran prepr in a temp
  worktree and recorded a pass; the push gate then allowed the push,
  denied it as stale after an edit, and allowed it again once the edit was
  reverted. An author session told to run the verdict command declined
  under the AGENTS rule, and when asked to attempt it as a test, the hook
  denied it. An earlier review of the same change recorded a **fail**: the
  base branch had been amended mid-review, so a fix looked removed, and
  several new tests lacked intent citations (since added). The
  reviewer's first re-run also hit a flaky test, recorded in
  [tree-hash-racy-edits](/open-questions/tree-hash-racy-edits.md).
- **A verdict depends on branch history, not only the tree.** Seen in
  step 3's real-use acceptance: the same tree failed review with a history
  that deleted a seed in one commit and passed when the branch was
  squashed. Part of that is intended and part is not. Intended: the
  "Fixed during the change" list, and so the R4/R7 check, reads runs whose
  commit is on the branch ([pr-evidence-summary](/decisions/pr-evidence-summary.md)),
  so rewriting history can drop records the reviewer then misses. Not
  intended: judging R1, R2, R3, or R5 on an intermediate commit. Those are
  judged on the net diff from the merge base, and the reviewer's
  instructions now say so (a file deleted and later restored is not
  changed). The verdict itself stays bound to the tree hash only.
- Report schema 0.4.0 adds the verdict's `treeHash`, `head`, and
  `rerunStartedAt`, all optional; older verdict-free reports still read.
