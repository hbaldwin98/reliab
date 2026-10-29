---
type: Role
title: "T16: Consumer-driven contracts"
description: "Run the consumer's tests against a mock of each owned service it calls and record what it needs from each as a contract file, in a directory it is given. reliab's own role, optional."
tags: [tool-role]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-29T12:00:00-07:00
---

# T16: Consumer-driven contracts

**Must:** run the consumer's tests against a mock of each provider it calls,
fail a test whose requests the mock did not expect, and write one contract
per consumer and provider pair to a directory it is given.

**Example fillings (illustrative, unverified):** Pact (pact-js, pact-go,
PactNet, pact-python, pact-jvm).

The paper stubs dependencies for faults (T9) and fuzzes an API from its own
spec (T7), but records nothing about what a consumer depends on. reliab adds
T16 for the consumer side only
([consumer-contracts](/decisions/consumer-contracts.md)); verifying a
contract against its provider belongs to the provider's repository. A
profile may leave it out; when it lists it, it fills it or marks it
`applicable: false` with a reason, as with every role.
