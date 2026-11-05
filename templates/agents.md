## Testing rules (reliab)

### Workflow
- Start with `reliab run <context> --plan`: it shows what will run, in what order, and what each command's failure means.
- Run `reliab run change` when a change is done, not after each file edit. Run `reliab run pr` before a branch merges.
- To look at one area, add `--target` with a project, folder, file, or `file#Function`.
- Write tests from intent. Each new test cites its source in a comment: a ticket, a doc, or a caller that depends on the behavior. The implementation is not a source.
- Reproduce a property or fuzz failure from the seed the run prints (`--seed`), and shrink it before fixing it.
- Check that each new test can fail: no swallowed exceptions, no missing assertions, and the generator reaches the interesting cases.
- Report what you ran, what failed, and what you skipped and why.

### When something fails
Decide what kind of failure it is, then act:
- fix-code: the product code is wrong. Fix it and run again.
- fix-test: the test, the harness, or a missing test is wrong. Fix it and run again. If a property is wrong, delete it and say why.
- ask-human: only a person can say whether the behavior is intended, or the machine is broken. Stop and ask, quoting the evidence.

Never change an assertion, widen a tolerance, or add a catch block to make a test pass.

### Mutation testing
- Kill a surviving mutant only when its behavior is stated somewhere (a ticket, a doc, a caller that depends on it), and cite that in the test.
- If nothing states the behavior, don't pin it. Ask a person whether it is intended.
- If a mutant cannot change behavior, say so in one line. It needs no test.
- Never write a test only to raise the score.
