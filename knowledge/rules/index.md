# Hard rules

* [R1: A failing test is a finding](r1-failing-test-is-a-finding.md) - The agent never changes an assertion, widens a tolerance, or adds a catch block to make a test pass.
* [R2: Protected oracles change only with human approval](r2-protected-oracles.md) - Snapshots, the regression corpus, scrubbers, mutant suppressions, and hand-maintained API specs change only with human approval.
* [R3: Every property cites its source of intent](r3-cite-intent.md) - Each specification property names a ticket, document, rule card, or dependent caller in a comment.
* [R4: Reproduce, shrink, classify](r4-reproduce-shrink-classify.md) - Every failure is reproduced from its seed or saved input, shrunk, and classified before it is reported.
* [R5: Self-review before reporting](r5-self-review.md) - Check each new test reaches the changed code, cannot pass vacuously, and is grounded.
* [R6: Rank findings before a human sees them](r6-rank-findings.md) - Findings are scored on a rubric; only the top band interrupts a human.
* [R7: Surviving mutants drive new tests](r7-survivors-drive-tests.md) - Surviving mutants on changed lines, not coverage gaps, drive new tests; equivalent-mutant calls are logged.
* [R8: Time-box every run and report skips](r8-time-box-and-report-skips.md) - Every run is time-boxed and reports what it skipped and why.
* [R9: Escalate behavior a snapshot pins](r9-escalate-pinned-behavior.md) - Behavior that looks wrong but is pinned by a characterization test is escalated, never fixed by the agent.
