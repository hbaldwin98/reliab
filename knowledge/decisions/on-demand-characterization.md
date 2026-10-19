---
type: Decision
title: "On-demand characterization: capture, change, compare, discard"
description: "A characterize profile command writes transcripts of an area into {capture}; reliab characterize capture, compare, and discard keep the captures under .reliab/, label each difference, and never commit or approve anything. The characterize skill drives the cycle."
status: draft
tags: [cli, characterization, plugin, schema]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-18T12:00:00-07:00
---

# On-demand characterization: capture, change, compare, discard

## Context

Step 2 of the [roadmap](/decisions/agentic-flow-roadmap.md) puts
characterization's main use on a maintained codebase at refactors and
migrations, on demand. Capture what an area does, make the change, compare,
then throw the capture away. Nothing is committed, so nothing needs
approval. This is the paper's planned `characterize` skill ("capture,
scrub, snapshot, stabilize; never approve a snapshot"). How to capture
behavior is the project's business: reliab cannot drive a socket API or a
CLI it knows nothing about. Comparing text is generic.

## Decision

- **A profile command, `characterize`**, joins the fixed command ids
  ([schema-v0-shape](/decisions/schema-v0-shape.md)). It writes one text
  file per transcript into the directory reliab names, given as the
  placeholder `{capture}` and the variable `RELIAB_CAPTURE_DIR` (absolute,
  slash-separated). File names are the command's choice; the same case must
  get the same name each run. `{scope}`, `{impacted}`, and `RELIAB_SCOPE`
  carry `--scope` as for `verify --only`. A command may ignore them and
  capture everything it knows, if that is quick.
- **Only `reliab characterize` runs it.** `verify --only characterize` and
  a tier that lists it are config errors, since `{capture}` would name
  nothing. It is in no built-in tier.
- **`reliab characterize capture [--scope <path>...]`** runs the command
  into `.reliab/characterize/baseline/` and records the scope, tree hash,
  time, and file count beside it. It fails (exit 1, a finding) when the
  command fails or writes nothing, and then keeps no baseline. **It refuses
  while a baseline exists:** capturing after the change would make the
  changed behavior the baseline and approve every difference unseen.
  Starting over takes an explicit `discard`.
- **`reliab characterize compare`** runs the command again over the
  recorded scope into `.reliab/characterize/current/` and compares the two
  directories file by file. Each file that differs, appeared, or
  disappeared is a finding from command `characterize`, labelled by the
  [snapshot diff rule](/decisions/snapshot-diff-labels.md). A file that
  disappeared is values-changed. Exit 0 means no differences, 1 means
  differences or a failed capture. The baseline stays, so compare can run
  again after each fix.
- **`reliab characterize discard`** removes `.reliab/characterize/`.
- **Both runs write the on-demand report**
  ([on-demand-verify](/decisions/on-demand-verify.md)), with `only:
  [characterize]`, so route, classify, and ack work as for any finding, and
  the stop and push hooks never mistake it for a tier's report. They reuse
  verify's runner: the tier's budget (`--tier`, default inner), the shell,
  the log, and PATH with the adapters.
- **Captures stay out of git** because `.reliab/` is gitignored
  (`profile init` adds it) and outside the freshness hash. They are never
  in a protected path, and agents cannot edit them, since `.reliab/**` is
  always denied. That is why `discard` is a reliab command.
- **The plugin's `characterize` skill** drives the cycle. It checks the
  profile, captures, then compares once before any change to prove the
  capture is stable (a difference then is unscrubbed nondeterminism, a
  harness defect). Then it makes the change, compares, and acts on each
  difference: one the person stated is `characterization-diff` for them;
  one nobody stated is a side effect, `new-code-defect`, fixed in product
  code. Last it discards and runs the inner tier.

## Consequences

- The characterize command is often the snapshot tests run in a capture
  mode: the same conversations, written to files instead of asserted. A
  project that wants an area captured that no pinned case covers adds a
  capture case to its harness before capturing.
- On-demand classifications carry forward only from the previous on-demand
  report. A capture or compare overwrites a mutation run's on-demand
  report, as any on-demand run does.
- The baseline records its tree hash but compare does not check it: the
  tree is meant to change in between.
- A consumer's profile and test host need small changes: a `characterize`
  command, and a capture mode that writes each transcript to
  `$RELIAB_CAPTURE_DIR` instead of asserting it.
