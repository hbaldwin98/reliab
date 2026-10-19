# Tool roles

* [T1: Unit test framework](t1-unit-test-framework.md) - Hosts every other role's tests; normally the framework already in use.
* [T10: Data-store fault injection](t10-store-faults.md) - Seeded failures at the connection or pool, inside the application process.
* [T11: Data-store-level tests](t11-store-tests.md) - Test procedures, triggers, constraints where logic lives in the store.
* [T12: Coverage](t12-coverage.md) - Changed-lines coverage as a backstop; machine-readable output.
* [T13: Shadow experiments](t13-shadow-experiments.md) - Run old and new implementations side by side on production reads.
* [T14: Static analysis](t14-static-analysis.md) - Warnings as errors on new and changed files; bug and style rules.
* [T2: Property-based testing](t2-property-testing.md) - Generators per input type, shrinking, seed replay, callable from T1.
* [T3: Snapshot / approval testing](t3-snapshot.md) - Serialize a response, diff against a committed file, pluggable scrubbers.
* [T4: Mutation testing](t4-mutation.md) - Mutate changed lines only; report survivors per line; threshold gate.
* [T5: In-process test host](t5-in-process-host.md) - Start the application inside the test process; one adapter class per runtime.
* [T6: Typed API client](t6-typed-client.md) - Call the application from tests with its own request types.
* [T7: API fuzzing from specification](t7-api-fuzzing.md) - Generate requests from the API description; flag server errors and schema drift.
* [T8: Network fault injection](t8-network-faults.md) - Latency, drops, resets between the application and its stores.
* [T9: External HTTP stubs](t9-http-stubs.md) - Scripted 500s, timeouts, malformed bodies per dependency.
