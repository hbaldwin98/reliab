# Open questions

* [Acknowledgements and classifications are local](acks-are-local.md) - They live in gitignored reports, so CI and teammates never see them; a committed findings queue would carry them.
* [What exactly is the canary break](canary-mechanism.md) - The paper names reliab canary but never defines which bug is injected, where, or how it is reverted.
* [How is an equivalent mutant closed](equivalent-mutants.md) - Closed: the agent classifies it equivalent-mutant, a human kind, and a person acknowledges it or excludes the mutant.
* [Which language is the testdata fixture service](fixture-language.md) - The fixture under testdata/ serves reliab own CLI tests; its ecosystem is not chosen yet.
* [Does a hook deny hold when permissions are bypassed](hook-deny-under-bypass.md) - Closed without a spike: the design no longer depends on the answers; the Windows launcher question was settled by a local check.
* [Should reliab emit OKF](okf-output.md) - Writing profile corrections and findings as OKF concepts would let that knowledge travel across repositories and agents.
* [What is the R6 scoring rubric](r6-scoring-rubric.md) - R6 ranks findings on reproducible, grounded, user-visible, and severity, but no scale or bands are defined.
* [Replace the test-weakened heuristic with a mutation check](test-weakened-mutation-check.md) - Instead of pattern-matching removed assertions, re-run the mutants the removed assertion used to kill.
* [Can the tree hash miss a same-size edit](tree-hash-racy-edits.md) - Closed: yes, it could. The temporary index now keeps the real index's mtime, so git's racy-entry check sees the edit.
