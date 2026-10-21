<!-- reliab:begin -->
## Testing rules (reliab)

### Always
- Run `reliab verify --tier inner` when a change is done, before you stop or hand back; not after each file edit. Run `reliab verify --tier prepr` before a branch merges into main, by a PR or a local merge. Commit, then have the `reliab:reviewer` agent review the change (it reviews committed work only); for a PR, paste `reliab report summary` as the PR body's evidence section, adding nothing it does not support.
- Read `reliab report route` and act per kind.
- Write properties from intent. Each property cites its source in a comment: a ticket, a doc, a reviewed rule card, or a caller that depends on the behavior.
- Reproduce every failure from its seed, shrink it, and classify it before reporting: `reliab findings classify <id> --kind <kind> --reason "..."`.
- Commit a failing case your property tool saves under a protected path (a regression seed) with its fix, and list it as a question for a human: a person approves it with `reliab approve <path>`. The tool writes it during the failing run; you only commit it. You cannot copy one in from another branch or clone: re-run the failing property against the code before the fix so the tool writes it again, or stop and give the person the exact commands to copy it.
- Before reporting, check each new test can fail: no swallowed exceptions, no missing assertions, and the generator reaches the interesting cases.
- Report what you ran, what you skipped, and why.

### Never
- Never change an assertion, widen a tolerance, or add a catch block to make a test pass.
- Never edit protected paths (see `protected_paths` in reliability-profile.yaml) or anything under `.reliab/`, and never run a command that approves or rewrites one (`protected_commands`, such as a snapshot tool's accept).
- Never "fix" behavior that a snapshot protects.
- Never run `reliab findings ack`; it records a human decision.
- Never run `reliab approve`; a person approves a protected-path change.
- Never run `reliab report verdict`; only the reviewer agent records a verdict.

### When a test fails
- new-code-defect -> fix product code, re-run.
- harness-defect -> fix the harness, re-run.
- wrong-property -> delete it, write down why.
- legacy-defect-or-quirk -> stop; a human decides.
- characterization-diff -> stop and ask a human.
- equivalent-mutant -> stop; a human confirms it.
- requirement-silent -> stop; a human says whether the behavior is intended.
- below-threshold -> never classified; handle its survivors, never a test only to raise the score; then stop, a human accepts the score.
- environment -> only after re-running the failing command once, with the machine's error quoted in the reason (resources, network, a missing service; never a test you cannot explain); stop, a human fixes the machine.

### Mutation testing
- Kill a surviving mutant only when its behavior is intended: the test cites where (a ticket, a doc, a rule, a caller that depends on it). The implementation is not a source.
- If nothing states the behavior, do not pin it: classify it `requirement-silent` with the question.
- If no test can kill it because it cannot change behavior, classify it `equivalent-mutant` with a one-line reason.
<!-- reliab:end -->
