---
type: Layer
title: "Layer 2: Specification properties"
description: "Does new code do what was intended? Oracle: invariants, relations, and models taken from stated intent."
tags: [evidence-stack]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
sources:
  - resource: /references/evidence-stack-paper.md
    title: "§3.2, Table 2; §5.3, §5.5, §5.7"
---

# Layer 2: Specification properties

Where the agent adds the most value and is most likely to fool itself;
[layer 3](/layers/3-mutation.md) exists to catch its mistakes.

| Technique | Question | Oracle |
|---|---|---|
| Specification properties | Does new code do what was intended? | Invariants from tickets, docs, call sites |
| Metamorphic relations | Are related answers consistent? | Relations between outputs |
| Model-based tests | Does a sequence of operations match a simple model? | Small in-memory reference model |
| API fuzzing | Can any request crash an endpoint? | No server errors; responses match the schema |
| Specification checks | Does the API description match the code? | Registered routes and request types; linter rules |

Roles: [T2](/roles/t2-property-testing.md), [T7](/roles/t7-api-fuzzing.md).
Every property cites its source of intent ([R3](/rules/r3-cite-intent.md)).
Mine metamorphic relations from existing tests first; treat LLM-proposed ones
as candidates (about half are invalid).
