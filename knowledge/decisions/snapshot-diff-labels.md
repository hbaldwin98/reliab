---
type: Decision
title: "Snapshot diffs are labelled additive-only or values-changed"
description: "A text snapshot or capture that no longer matches carries an optional diff label in the findings and report formats; route shows it. The rule is generic and public: pkg/snapdiff and reliab snapdiff, for any adapter. reliab-adapter-insta and reliab-adapter-verify read their tools' pending files."
status: draft
tags: [report, adapters, characterization]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-18T12:00:00-07:00
---

# Snapshot diffs are labelled additive-only or values-changed

## Context

Step 2 of the [roadmap](/decisions/agentic-flow-roadmap.md) pins a lean set
of a maintained codebase's most-used conversations as snapshots. A
deliberate new field then shows up as a diff in a handful of them. A person
should be able to approve that quickly and give anything else a real
review. Approval stays human ([R2](/rules/r2-protected-oracles.md)); the
label only helps the person decide.

## Decision

- **Two labels.** `additive-only`: lines were added, and none removed or
  changed. `values-changed`: some line was removed or changed. A snapshot
  with no accepted version yet is additive-only.
- **The rule is generic for text** and lives in `pkg/snapdiff`: a line
  diff (longest common subsequence) of the accepted text against the new
  one. Line endings and a final newline do not count. **One allowance:** a
  removed line that comes back in the same change with only a trailing
  comma added is a moved separator, not a change. Rendered JSON and similar
  lists put a comma on the old last item when an item is appended, and
  transcripts often render JSON one field per line, so without this a new
  last field would read as a change. A value that changes along with its
  comma is still a change. The diff marks such a line `~`.
- **The label travels as an optional field `diff`** on a finding, in both
  the findings format (schema 0.2.0) and the report (schema 0.3.0). It is a
  field rather than text in the message so that route, exports, and a
  future reviewer can act on it without parsing prose. Both changes are
  additive: older files still validate.
- **`report route` shows it** as `diff: additive only ...` or `diff: values
  changed or removed ...`, shows up to 24 lines of the labelled diff, and
  for an unclassified labelled finding gives its own advice. If the change
  meant to change that output, classify it `characterization-diff` and stop
  for a person. If it did not, it is a side effect: classify it
  `new-code-defect` and fix product code, never the snapshot.
- **insta is read by an adapter**, `reliab-adapter-insta`
  (`cmd/adapters/insta`), never the core
  ([external-adapters](/decisions/external-adapters.md)). The profile runs
  the tests with `INSTA_UPDATE=new`, so insta writes each mismatch as a
  pending `.snap.new` file without touching the accepted one. It also sets
  `INSTA_PENDING_DIR` (present in insta 1.49) to a directory under
  `.reliab/out`, so the pending files stay out of protected snapshot
  directories and out of the working tree's freshness hash. The adapter
  compares each pending file with its accepted file, ignoring insta's
  metadata header, and writes one labelled finding per snapshot. With
  `-junit` it also keeps failing tests that are not snapshot assertions.
  Pending inline snapshots (`.pending-snap`) become unlabelled findings.
- **Update 2026-10-18: the rule is a helper adapters call, not a core
  convention.** Pairing a pending snapshot with its accepted file is a
  tool's naming convention, so it stays in adapters
  ([external-adapters](/decisions/external-adapters.md)); reliab exposes
  the label itself. `pkg/snapdiff` is public, so an external Go adapter can
  import it; `CompareFiles` reads both files, treats a missing accepted file
  as new (additive-only), ignores a byte-order mark and line endings, and
  gives binary files no label. `reliab snapdiff <accepted> <pending>
  [--json]` prints the label, the added and removed line counts, and the
  diff for adapters in any language. reliab-adapter-insta uses the public
  package.
- **The .NET Verify library is read by `reliab-adapter-verify`**
  (`cmd/adapters/verify`). Verify writes each mismatch as `X.received.<ext>`
  beside the accepted `X.verified.<ext>` and has no pending directory, so
  received files sit in the source tree. The adapter labels each one against
  its verified file and joins it to the failing test that wrote it (from a
  TRX or JUnit report): the failure whose last name segment is a part of
  the received file's name (`Class.Method.received.txt` belongs to
  `Method`). That test keeps its identity and gains the label; a received
  file no failure claims is a finding named by its verified file. Only
  received files modified since `RELIAB_COMMAND_START` count: a mutation
  tool runs the snapshot tests against every mutant and leaves received
  files behind, which would otherwise be reported as this run's. The same
  holds for the test reports it reads: a runner that names its reports by
  time (`LogFilePrefix`) leaves earlier runs' beside this one's, and reliab
  only clears a command's declared output, not an adapter's inputs. Found
  running the .NET sample: a passing snapshot run reported the previous
  run's failures.

## Consequences

- A finding's id is its accepted snapshot's path, so a classification
  carries forward while that snapshot keeps failing.
- An `additive-only` label is not permission. It says what changed, not
  whether the change was meant.
- The label is only as good as the rendering. A format that rewrites
  unrelated lines when one thing changes (reflowed text, renumbered ids)
  reads as values-changed. Scrubbing such values is the harness's job.
- Inputs too large for the line table (over 2^24 cells after trimming the
  common start and end) are compared as line counts. Then a pure
  reordering reads as additive.
- The same rule labels the differences from
  [on-demand characterization](/decisions/on-demand-characterization.md).
