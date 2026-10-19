---
type: OpenQuestion
title: "Acknowledgements and classifications are local"
description: "They live in gitignored reports, so CI and teammates never see them; a committed findings queue would carry them."
status: draft
tags: [findings, ci]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T18:05:00-07:00
---

# Acknowledgements and classifications are local

Classifications and human acknowledgements are carried forward between runs
of the same tier on one machine ([verify-execution](/decisions/verify-execution.md)),
but reports are gitignored ([report-storage](/decisions/report-storage.md)).
On CI, or a teammate's checkout, a known legacy failure is open again and the
prepr gate fails until the test is fixed or quarantined.

Approvals of protected-path changes (`reliab approve`, added 2026-10-18;
[protected-path-approval](/decisions/protected-path-approval.md)) are local
the same way, in `.reliab/approvals.json`. There it is deliberate: the code
host's owner review of protected paths is the record CI trusts, and a
committed approval an agent could write would weaken it.

The paper's findings queue (deferred from v0) is the natural home: a
committed, protected file of acknowledged findings keyed by stable id, which
verify reads on every machine. Open: its format, whether acknowledgements
expire, and whether a quarantine list is the same thing.
