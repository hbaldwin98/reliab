# Evidence layers

* [Layer 0: Static analysis](0-static-analysis.md) - Is it obviously wrong? Oracle: compiler, analyzers, type annotations.
* [Layer 1: Characterization and regression corpus](1-characterization.md) - Did existing behavior change, and do past failures stay fixed? Oracle: recorded behavior and saved inputs.
* [Layer 2: Specification properties](2-specification.md) - Does new code do what was intended? Oracle: invariants, relations, and models taken from stated intent.
* [Layer 3: Mutation testing](3-mutation.md) - Would the tests catch a bug here? Oracle: mutants killed by the suite.
* [Layer 4: Failure behavior](4-failure-behavior.md) - Does it fail safely, and is logic in the data store right? Oracle: no partial writes, bounded retries, no hangs; store-native assertions.
* [Layer 5: Differential replay](5-differential-replay.md) - Does new code match old on real inputs? Oracle: outputs and state of the previous build or code path.
