---
type: Decision
title: "A mutation threshold on changed code, and a baseline ratchet per scope"
description: "A mutation command may set a threshold; a protected baseline file records a score per scope that runs must not fall below. verify scores each mutation command and adds a below-threshold finding per shortfall, derived from the survivors and never classified; report summary shows the scores; reliab gate mutation fails below them in CI. A person writes the baseline with reliab baseline write."
status: draft
tags: [mutation, gates, report, profile]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-18T21:00:00-07:00
sources:
  - resource: /references/evidence-stack-paper.md
    title: "Table 14, S3 exit criterion; the CI gate on changed-code mutation score"
---

# A mutation threshold on changed code, and a baseline ratchet per scope

## Context

The S3 exit criterion is "mutation threshold enforced on changed code", and
the CI gate is "mutation score on changed code, fails below the agreed
score". [mutation-baseline-first](/decisions/mutation-baseline-first.md)
already says how a maintained codebase gets there: measure first, then hold
the line. Reports carried each mutation command's counts
([pr-evidence-summary](/decisions/pr-evidence-summary.md)) but nothing
compared them with anything.

The risk is the one [R7](/rules/r7-survivors-drive-tests.md) names: a score
to reach invites tests written for the score. Survivors are already
findings, each with its own route (kill it when the behavior is intended and
the test cites where; `requirement-silent`; `equivalent-mutant`), so a
threshold must not add work of its own or count them twice.

## Decision

- **The score rule.** A mutation command's score is killed over counted.
  Counted is killed, survived, and not covered, less the survivors a person
  confirmed equivalent:
  - Timeouts count as killed, as the readers already judge them
    (gremlins' `TIMED OUT`, mutation-testing-elements' `Timeout`), never
    as survivors (confirmed 2026-10-20, with a test from a gremlins
    report): a mutant whose tests ran past the tool's limit changed
    behavior enough to hang or slow them, which the suite notices;
    calling it a survivor would ask for a test of a hang, and leaving it
    out would let the machine's load change the counted total. Stryker
    and PIT count a timeout as detected too. Load is what makes a mutant
    time out that a quiet machine reports as surviving, so heavy runs go
    one at a time ([heavy-run-lock](/decisions/heavy-run-lock.md)).
  - Not covered counts against the score: a mutant no test reaches is one
    the suite would not notice.
  - Mutants the tool did not test (did not build, skipped, not viable) are
    not counted; the readers and adapters already leave them out.
  - A `requirement-silent` survivor counts, answered or not: its behavior is
    unpinned until a test citing the answer kills it.
  - An `equivalent-mutant` survivor counts until a person acknowledges it;
    then it leaves the count. The agent's classification alone cannot raise
    the score.
  - A command that reported no counts, or was skipped, has no score.
  Nothing counted is not below anything: there is nothing to hold.
- **Profile.** A `mutation_*` command may set `threshold`, a percentage
  (another command setting one is a config error). The profile may name a
  `mutation_baseline` file, which must match `protected_paths` (R2): it is
  an oracle, changed only with a person's approval.
- **The baseline file** (`schemas/baseline.schema.json`) lists scopes, each
  a path glob with a recorded score (and the killed, counted, command,
  time, and tree it was recorded from). Every mutation command is held to
  every scope it measured mutants in: a changed-code run's mutants in a
  scope must score at least the scope's recorded score.
  A profile that names no baseline file, or names one not written yet, is
  held to no scope; a baseline file that does not validate is a config
  error, so a broken ratchet never passes silently. The score rule applies
  inside a scope too: not covered mutants there count, and confirmed
  equivalents there leave the count. Scoring a scope
  needs per-file counts: the findings format (0.4.0) and report (0.6.0)
  gain an optional `mutantFiles`, which the mutation-testing-elements
  reader and `reliab-adapter-gremlins` write. Without them a scope is
  `unmeasured`, verify says so, and the gate fails.
- **`reliab baseline propose | write`.** Propose scores each `--scope`
  (default: the recorded ones) from a mutation command's per-file counts
  (default `mutation_full` in the on-demand report, from `verify --only
  mutation_full --scope <area>`) and prints the file and what changes. The
  score rule applies, so confirmed equivalents leave a scope's count. The
  report must be fresh and complete. Write writes it for a person to review
  and commit, and refuses to lower a recorded score without
  `--allow-lower`: it is a ratchet. The pre-bash hook denies `baseline
  write` to agents, as it denies `findings ack`; an agent shows the
  proposal and asks.
- **verify scores every mutation command** after carrying classifications
  forward, records the result as the command's `score` (killed, counted,
  confirmed and pending equivalents, the threshold, and each scope it was
  held to), and adds one **`below-threshold`** finding per check that falls
  short. Its test states the numbers ("mutation score 70.0% below the
  threshold 80% (7 killed of 10 counted)"); its message says how many more
  kills reach it, the counts the score came from (killed, survived, not
  covered, less any confirmed equivalents), and the survivor findings it
  counts, or that it counts none (a tool can report counts without listing
  every survivor). It names the first 10 survivors, then "and N more"; the
  survivor findings themselves are the full list (the owner's call of
  2026-10-19).
- **The kind is `below-threshold`, derived, never classified.** It is not a
  defect the agent fixes and not a fact a person confirms about one mutant;
  it is the sum of the survivors. So:
  - `reliab findings classify` refuses it.
  - It is not an agent kind: the stop hook does not block on it, because
    its survivors already do while they need the agent. It does block the
    push gate while open, like every open finding.
  - Route sends the agent to its survivors while any still needs the agent
    ("handle those first ... never write a test only to raise the score"),
    and for a baseline scope's shortfall only to the survivors in that
    scope, the ones it counts.
    Once none does, it stops for a person, who accepts the score for the
    change (`reliab findings ack`) or says what should be tested.
  - Its id comes from its numbers, so an acknowledgement holds only while
    they stay the same: accepting 70% once does not accept 50% later.
  - It is recomputed whenever its inputs change: by verify, and by
    `findings classify` and `ack` on the report they update, so a person
    confirming an equivalent mutant moves the score at once. When they
    cannot (the profile or the baseline file does not load), they still
    record the change and say, on stderr, that the scores and any
    shortfall were left as they were; never silently.
- **The profile guards the threshold** (the owner's call of 2026-10-19).
  `threshold` and `mutation_baseline` live in the profile, and the profile
  lists itself in `protected_paths`, so an agent can neither lower a
  threshold nor point the baseline elsewhere: the hooks deny the edit, and
  `reliab gate protected` and the summary list a profile change until a
  person approves it with `reliab approve`
  ([protected-path-approval](/decisions/protected-path-approval.md)). The
  gate also holds the base profile's globs, so a branch that drops the
  profile's own entry is still caught.
- **`reliab report summary`** shows each score under the mutation counts,
  with the threshold and each baseline scope as met, **below**, or not
  scored; a shortfall is a question for a human.
- **`reliab gate mutation [--tier prepr]`**, agent-free like `gate
  protected`, re-scores the tier's latest report from its counts (so a
  report written before the threshold was set is still judged) and fails
  when a mutation command the tier runs and the profile holds to something
  is below it, reported no counts, has an unmeasured scope, did not run, or
  the report is stale. A person's acknowledgement does not pass it:
  acknowledgements are local ([acks-are-local](/open-questions/acks-are-local.md)),
  and in CI the human decision is the code host's override of a required
  check.

## Consequences

- Timeouts count as killed, so a loaded machine can kill a mutant by
  timing out that a quiet one reports as a survivor: seen on this change,
  where one mutant survived in the author's prepr and timed out in the
  reviewer's re-run, so the two runs' counts differed by one.

- R7 holds: the threshold never asks for a test by itself, and the
  `requirement-silent` rule still forbids pinning unstated behavior, so a
  repository can sit below its threshold until people answer questions.
  That is the intended pressure: on the questions, not on the tests.
- A reviewer's clean re-run has no acknowledgements, so after a person
  confirms an equivalent mutant the author's score and the re-run's differ,
  and the re-run's shortfall id is one the author's report lacks; the
  verdict then fails until acknowledgements are shared (the same open
  question).
- Changed-lines coverage, which [external-adapters](/decisions/external-adapters.md)
  places beside this gate, is not built.
- reliab's own profile sets `threshold` on `mutation_changed` and protects
  itself; it names no baseline file yet, because writing one is a person's
  decision.
